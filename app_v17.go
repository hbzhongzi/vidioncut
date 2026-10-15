package main

// v1.7 §19.8 Studio 审核面板的 Wails 绑定（M11）：
// ASR 状态、MCP 调用日志、edits 账本读取/生成/重算、保留段剪切（批量+合并链）。
// 业务逻辑全部复用 internal/service、internal/mcpserver、internal/asr、internal/edits，零重复实现。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"videocut/internal/asr"
	"videocut/internal/edits"
	"videocut/internal/mcpserver"
	"videocut/internal/service"
)

// ---- ASR 状态（Codex 面板） ----

// ASRStatusInfo ASR sidecar 运行期状态快照。
type ASRStatusInfo struct {
	Enabled        bool     `json:"enabled"`
	Host           string   `json:"host"`
	Port           int      `json:"port"`
	Model          string   `json:"model"`
	ComputeType    string   `json:"compute_type"`
	HFHome         string   `json:"hf_home"`
	IdleTimeoutMin int      `json:"idle_timeout_min"`
	Hotwords       []string `json:"hotwords"`
	Python         string   `json:"python"`          // 解析到的 venv python（空=未找到）
	Healthy        bool     `json:"healthy"`         // 回环端口当前有健康实例（不管由谁拉起）
	RuntimeMissing bool     `json:"runtime_missing"` // true=用户需运行 bootstrap 脚本
	Hint           string   `json:"hint"`            // 面向用户的操作指引
}

// ASRStatus 查询 ASR 配置与 sidecar 健康状态（Codex 面板轮询）。
func (a *App) ASRStatus() ASRStatusInfo {
	hotwords := a.cfg.ASRHotwords
	if hotwords == nil {
		hotwords = []string{}
	}
	st := ASRStatusInfo{
		Enabled:        a.cfg.ASREnabled,
		Host:           a.cfg.ASRHost,
		Port:           a.cfg.ASRPort,
		Model:          a.cfg.ASRModel,
		ComputeType:    a.cfg.ASRComputeType,
		HFHome:         a.cfg.ASRHFHome,
		IdleTimeoutMin: a.cfg.ASRIdleTimeoutMin,
		Hotwords:       hotwords,
	}
	// 防御：内部装配异常时不得 panic（此前先调 PythonPath 后判 nil，顺序错误）。
	if a.asr == nil {
		st.RuntimeMissing = true
		st.Hint = "ASR 客户端未初始化"
		return st
	}
	py, pyErr := a.asr.PythonPath()
	st.Python = py
	ctx, cancel := context.WithTimeout(a.appContext(), 4*time.Second)
	defer cancel()
	if err := a.asr.PortHealthy(ctx); err == nil {
		st.Healthy = true
	}
	switch {
	case !a.cfg.ASREnabled:
		st.RuntimeMissing = true
		st.Hint = "ASR 未启用：config.yaml 设置 asr.enabled=true（或环境变量 VIDEOCUT_ASR_ENABLED=true）"
	case pyErr != nil:
		st.RuntimeMissing = true
		st.Hint = "未找到 ASR venv。请运行 scripts/bootstrap-asr.ps1（首次下载模型约 0.5GB，国内自动走 hf-mirror）"
	case !st.Healthy:
		st.Hint = "sidecar 未运行：首次转录时自动拉起（模型加载约 10–60s）"
	default:
		st.Hint = "sidecar 就绪"
	}
	return st
}

// SetASRHotwords 透传热词表到 sidecar（Codex 面板热词管理）。
func (a *App) SetASRHotwords(words []string) error {
	if a.asr == nil {
		return fmt.Errorf("ASR 未启用")
	}
	ctx, cancel := context.WithTimeout(a.appContext(), 15*time.Second)
	defer cancel()
	return a.asr.SetHotwords(ctx, words)
}

// appContext 取出应用上下文（startup 未执行时退化为 Background，测试/极端时序安全）。
func (a *App) appContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

// ---- MCP 调用日志（Codex 面板"对话轨迹"） ----

// ReadMCPCallLog 读取最近 limit 条 MCP 工具调用记录（最新在前）。
func (a *App) ReadMCPCallLog(limit int) []mcpserver.CallEntry {
	return mcpserver.ReadCallLog(limit)
}

// ---- edits 账本（AI 粗剪面板） ----

// EditsView 账本读取结果：账本原文 + 校验 + 摘要 + 保留段补集。
// 前端逐项开关后，把启用的 items 回传 AICutCompute / AICutSubmit 重算/剪切。
type EditsView struct {
	Path            string              `json:"path"`
	Ledger          edits.EditsLedger   `json:"ledger"`
	Valid           bool                `json:"valid"`
	ValidationError string              `json:"validation_error"`
	Summary         *edits.EditsSummary `json:"summary"`
	KeepRanges      []edits.KeepRange   `json:"keep_ranges"`
	SourceDuration  float64             `json:"source_duration"`
}

// buildEditsView 由账本与源时长构造视图（读盘/生成共用）。
func buildEditsView(path string, ledger *edits.EditsLedger, sourceDuration float64) *EditsView {
	view := &EditsView{Path: path, Ledger: *ledger, SourceDuration: sourceDuration}
	if err := edits.ValidateEdits(ledger, sourceDuration); err != nil {
		view.ValidationError = err.Error()
		return view
	}
	view.Valid = true
	sum := edits.Summarize(ledger, sourceDuration)
	view.Summary = &sum
	view.KeepRanges = edits.EditsToKeepRanges(ledger, sourceDuration)
	return view
}

// LoadEditsFile 打开文件对话框选择 edits.json。
func (a *App) LoadEditsFile() (string, error) {
	return a.SelectFile()
}

// ReadEditsLedger 读取并校验 edits.json；校验失败也返回视图（ValidationError 供 UI 展示），
// 仅文件不存在/解析失败返回 error。
func (a *App) ReadEditsLedger(path string) (*EditsView, error) {
	if path == "" {
		return nil, fmt.Errorf("路径为空")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 %s 失败: %w", path, err)
	}
	var ledger edits.EditsLedger
	if err := json.Unmarshal(data, &ledger); err != nil {
		return nil, fmt.Errorf("edits.json 解析失败: %w", err)
	}
	// M12：记录账本所在根目录（.../work/edits.json → .../），供 AICutSubmit
	// 把用户勾选后的最终账本写回同一位置，字幕步骤读盘才能拿到确认版。
	if filepath.Base(filepath.Dir(path)) == "work" {
		a.lastWorkDir = filepath.Dir(filepath.Dir(path))
	}
	dur := 0.0
	if ledger.Source != "" {
		if probe, perr := a.svc.Probe(a.appContext(), ledger.Source); perr == nil {
			dur = probe.Duration
		}
	}
	return buildEditsView(path, &ledger, dur), nil
}

// AICutGenerate 转录 + 机械扫描候选 + 写盘 work/edits.json，返回账本视图（M11 AI 粗剪"生成候选"）。
// source 为输入视频绝对路径，workDir 为输出根目录（其下 work/ 放 SRT/signals/edits.json）。
func (a *App) AICutGenerate(source, workDir string) (*EditsView, error) {
	if a.asr == nil {
		return nil, fmt.Errorf("ASR sidecar 未启用（config.yaml asr.enabled=true）")
	}
	if source == "" {
		return nil, fmt.Errorf("源文件为空")
	}
	if workDir == "" {
		return nil, fmt.Errorf("输出目录为空")
	}
	a.lastWorkDir = workDir // M12：供 AICutSubmit 写回最终账本
	ctx, cancel := context.WithTimeout(a.appContext(), 30*time.Minute)
	defer cancel()

	res, terr := a.asr.Transcribe(ctx, asr.TranscribeRequest{VideoPath: source, OutputBase: workDir})
	if terr != nil {
		if asr.IsRuntimeMissing(terr) {
			return nil, fmt.Errorf("asr_runtime_missing: %w", terr)
		}
		return nil, fmt.Errorf("转录失败: %w", terr)
	}

	items, _, _, err := mcpserver.BuildCandidateItems(workDir, 1.0)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		// 干净素材：给明确指引，而非返回一本"items 不能为空"的无效账本
		return nil, fmt.Errorf("未扫描到候选删除项（无 ≥1.0s 静音段、无口水词命中）：素材较干净，可手动编写 edits.json 后从「载入已有 edits.json」进入复核")
	}
	ledger := edits.EditsLedger{Version: edits.LedgerVersion, Source: source, Items: items}
	ledgerPath := filepath.Join(workDir, "work", "edits.json")
	data, merr := json.MarshalIndent(ledger, "", "  ")
	if merr != nil {
		return nil, fmt.Errorf("账本序列化失败: %w", merr)
	}
	// 写盘失败必须上报：此前 _ = WriteFile 静默吞错，用户拿到视图但
	// 后续「载入 edits.json」/Codex 读盘时报"文件不存在"，难以定位。
	if err := os.MkdirAll(filepath.Dir(ledgerPath), 0o755); err != nil {
		return nil, fmt.Errorf("创建目录 %s 失败: %w", filepath.Dir(ledgerPath), err)
	}
	if err := os.WriteFile(ledgerPath, data, 0o644); err != nil {
		return nil, fmt.Errorf("写入 %s 失败: %w", ledgerPath, err)
	}
	view := buildEditsView(ledgerPath, &ledger, res.Duration)
	if !view.Valid {
		// 自检不变式：扫描产物必须可直接通过校验，否则说明合并逻辑有缺陷
		return nil, fmt.Errorf("生成的账本未通过自检: %s", view.ValidationError)
	}
	return view, nil
}

// AICutCompute 对当前启用的删除项重算校验/摘要/保留段（AI 粗剪逐项开关后实时刷新侧栏）。
// items 为空（用户取消全部删除）是合法的"全部保留"状态，返回有效视图而非校验失败。
func (a *App) AICutCompute(source string, items []edits.EditItem) (*EditsView, error) {
	if source == "" {
		return nil, fmt.Errorf("源文件为空")
	}
	dur := 0.0
	if probe, err := a.svc.Probe(a.appContext(), source); err == nil {
		dur = probe.Duration
	}
	ledger := edits.EditsLedger{Version: edits.LedgerVersion, Source: source, Items: items}
	if len(items) == 0 {
		view := &EditsView{SourceDuration: dur, Valid: true}
		sum := edits.Summarize(&ledger, dur)
		view.Summary = &sum
		view.KeepRanges = edits.EditsToKeepRanges(&ledger, dur)
		return view, nil
	}
	return buildEditsView("", &ledger, dur), nil
}

// AICutSubmit 将启用的删除项映射为保留段批量裁剪任务（reencode 精确切分），返回 task_id。
// 随后可用 MergeCutSegments 合并为单成片（与 videocut_trim_batch 行为一致）。
func (a *App) AICutSubmit(source string, items []edits.EditItem, strategy string) (string, error) {
	if source == "" {
		return "", fmt.Errorf("源文件为空")
	}
	// items 为空表示用户取消了全部删除候选，语义是“全部保留”，不是错误。
	// EditsToKeepRanges 会把空账本转换为覆盖全片的保留段。
	if items == nil {
		items = []edits.EditItem{}
	}
	if strategy == "" {
		strategy = "reencode"
	}
	ledger := edits.EditsLedger{Version: edits.LedgerVersion, Source: source, Items: items}
	dur := 0.0
	if probe, err := a.svc.Probe(a.appContext(), source); err == nil {
		dur = probe.Duration
	}
	if len(items) > 0 {
		if err := edits.ValidateEdits(&ledger, dur); err != nil {
			return "", fmt.Errorf("账本校验失败: %w", err)
		}
	}
	ranges := edits.EditsToKeepRanges(&ledger, dur)
	// 写回最终账本（M12）：字幕步骤（AICutExportSubtitles/AICutAnimateCaptions）
	// 读 work/edits.json 获取保留段。若只提交不写盘，字幕会按"零删"处理
	// （源时间轴），与成片不符——真实端到端测试暴露的语义偏差。
	// 写盘失败不阻断剪切（任务已提交），但向用户明示。
	if a.lastWorkDir != "" {
		ledgerPath := filepath.Join(a.lastWorkDir, "work", "edits.json")
		if data, merr := json.MarshalIndent(ledger, "", "  "); merr == nil {
			if err := os.MkdirAll(filepath.Dir(ledgerPath), 0o755); err != nil {
				return "", fmt.Errorf("创建目录 %s 失败: %w", filepath.Dir(ledgerPath), err)
			}
			if err := os.WriteFile(ledgerPath, data, 0o644); err != nil {
				return "", fmt.Errorf("写回最终账本 %s 失败: %w", ledgerPath, err)
			}
		}
	}
	return a.SubmitKeepRangeCut(source, ranges, strategy)
}

// SubmitKeepRangeCut 把保留段提交为批量裁剪任务（reencode 精确切分），返回 task_id。
// 产物：<源名>_cut_segNN<扩展名>（与源同目录）；随后可用 MergeCutSegments 合并为单成片。
func (a *App) SubmitKeepRangeCut(source string, ranges []edits.KeepRange, strategy string) (string, error) {
	if source == "" {
		return "", fmt.Errorf("源文件为空")
	}
	if len(ranges) == 0 {
		return "", fmt.Errorf("保留段为空")
	}
	if strategy == "" {
		strategy = "reencode"
	}
	ext := filepath.Ext(source)
	base := strings.TrimSuffix(source, ext)
	items := make([]service.BatchItem, 0, len(ranges))
	for i, r := range ranges {
		if r.End <= r.Start {
			return "", fmt.Errorf("第 %d 个保留段区间非法: %v–%v", i+1, r.Start, r.End)
		}
		items = append(items, service.BatchItem{
			InputPath:  source,
			Start:      r.Start,
			End:        r.End,
			Strategy:   strategy,
			OutputPath: fmt.Sprintf("%s_cut_seg%02d%s", base, i+1, ext),
		})
	}
	return a.svc.TrimBatch(service.BatchRequest{Items: items})
}

// MergeCutSegments 把已完成的批量裁剪产物合并为单一成片（AI 粗剪第二步）。
// outputPath 为空时派生自 task 输出（<源名>_cut<扩展名> 由首片段名推导）。
func (a *App) MergeCutSegments(taskID, outputPath string) (string, error) {
	if taskID == "" {
		return "", fmt.Errorf("task_id 为空")
	}
	t, ok := a.svc.GetTaskSnapshot(taskID)
	if !ok {
		return "", fmt.Errorf("任务不存在: %s", taskID)
	}
	if t.Kind != "batch" {
		return "", fmt.Errorf("任务 %s 不是批量裁剪任务（kind=%s）", taskID, t.Kind)
	}
	if len(t.Items) == 0 {
		return "", fmt.Errorf("任务 %s 无片段可合并", taskID) // 防御：TrimBatch 已保证 ≥1 项
	}
	items := make([]service.MergeItem, 0, len(t.Items))
	for i, it := range t.Items {
		if it.Status != "completed" || it.OutputPath == "" {
			return "", fmt.Errorf("第 %d 个片段未完成（status=%s），无法合并", i+1, it.Status)
		}
		items = append(items, service.MergeItem{InputPath: it.OutputPath})
	}
	if outputPath == "" {
		outputPath = deriveMergeOutput(items[0].InputPath)
	}
	// 只剩 1 段保留（典型：仅删掉开头静音）——合并无意义，直接 remux 成终片。
	// 仍复用批量管线提交，以保持异步语义与 task_id 契约一致（前端照常轮询进度/完成）。
	if len(items) == 1 {
		probe, perr := a.svc.Probe(a.appContext(), items[0].InputPath)
		if perr != nil {
			return "", fmt.Errorf("探测单段产物失败: %w", perr)
		}
		return a.svc.TrimBatch(service.BatchRequest{Items: []service.BatchItem{{
			InputPath:  items[0].InputPath,
			Start:      0,
			End:        probe.Duration,
			Strategy:   "stream_copy", // 纯 remux，无损且快
			OutputPath: outputPath,
		}}})
	}
	return a.svc.Merge(service.MergeRequest{Items: items, Mode: "auto", OutputPath: outputPath})
}

// deriveMergeOutput 由首片段名推导合并输出：xxx_cut_seg01.mp4 → xxx_cut.mp4。
func deriveMergeOutput(firstSeg string) string {
	ext := filepath.Ext(firstSeg)
	base := strings.TrimSuffix(firstSeg, ext)
	if idx := strings.LastIndex(base, "_cut_seg"); idx > 0 {
		base = base[:idx] + "_cut"
	} else {
		base += "_cut"
	}
	return base + ext
}

// ExportTranscriptFile 导稿：<baseDir>/work/transcript.srt → transcript.{md|txt}，返回产物路径。
func (a *App) ExportTranscriptFile(baseDir, format string) (string, error) {
	return mcpserver.ExportTranscriptFile(baseDir, format)
}
