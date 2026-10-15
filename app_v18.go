package main

// M12 字幕功能的 Wails 绑定（字幕功能更新文档 §3.2/§5.7）：
// 外显导出 / 内嵌烧录 / 二次加工特效。业务逻辑全部复用 internal/mcpserver
// 的 subtitle_api.go（与 MCP 工具同一套实现，零重复）。

import (
	"fmt"
	"path/filepath"

	"videocut/internal/mcpserver"
	"videocut/internal/service"
	"videocut/internal/subtitles"
)

// SubtitlesView 字幕导出结果（前端字幕工作台第三步）。
type SubtitlesView struct {
	SRTPath       string          `json:"srt_path"`
	ASSPath       string          `json:"ass_path"`
	CueCount      int             `json:"cue_count"`
	SplitCount    int             `json:"split_count"` // 跨段词级断开条数（UI ⚡ 标注）
	FinalDuration float64         `json:"final_duration"`
	OutputDir     string          `json:"output_dir"`
	Cues          []subtitles.Cue `json:"cues"` // 成片时间轴字幕条（列表展示）
}

// MotionPlanView 二次加工结果（前端第四步；analysis 为五维分析条单一数据源，§5.7）。
type MotionPlanView struct {
	FXPath      string                    `json:"fx_path"`
	PlanPath    string                    `json:"plan_path"`
	Selected    []string                  `json:"selected"`
	Reasoning   string                    `json:"reasoning"`
	Rejected    []subtitles.RejectedSkill `json:"rejected"`
	Analysis    *subtitles.Analysis       `json:"analysis"`
	Static      bool                      `json:"static"`
	StyleID     string                    `json:"style_id"`
	Orientation string                    `json:"orientation"`
}

// deriveFinalVideo 推导成片路径：显式传入优先；否则按 AICutSubmit 派生规则
// <source 去扩展名>_cut<扩展名>（signals.json 记录 source）。
func deriveFinalVideo(workDir, finalVideo string) string {
	if finalVideo != "" {
		return finalVideo
	}
	sig, err := mcpserver.LoadSignals(workDir)
	if err != nil || sig.Source == "" {
		return ""
	}
	ext := filepath.Ext(sig.Source)
	base := sig.Source[:len(sig.Source)-len(ext)]
	return base + "_cut" + ext
}

// probeVideoSize 探测视频宽高（ASS PlayRes）；失败回落 0（RenderASS 回落 1920×1080）。
func (a *App) probeVideoSize(video string) (int, int) {
	if video == "" {
		return 0, 0
	}
	if probe, err := a.svc.Probe(a.appContext(), video); err == nil {
		return probe.Width, probe.Height
	}
	return 0, 0
}

// AICutExportSubtitles 粗剪成片后字幕重对齐 + 外显导出（第三步）。
// workDir 为转录输出根目录（work/signals.json 所在）；finalVideo 为成片路径
// （空则按派生规则推测）；字幕产物落成片同目录（§6 命名约定）。
func (a *App) AICutExportSubtitles(workDir, finalVideo, format, stylePreset string, maxChars, maxLines int) (*SubtitlesView, error) {
	if workDir == "" {
		return nil, fmt.Errorf("输出目录为空")
	}
	final := deriveFinalVideo(workDir, finalVideo)
	outputDir := workDir
	if final != "" {
		outputDir = filepath.Dir(final) // 与成片同目录
	}
	w, h := a.probeVideoSize(final)
	if format == "" {
		format = a.cfg.SubFormat
	}
	if stylePreset == "" {
		stylePreset = a.cfg.SubStylePreset
	}
	if maxChars <= 0 {
		maxChars = a.cfg.SubMaxCharsPerCue
	}
	if maxLines <= 0 {
		maxLines = a.cfg.SubMaxLines
	}
	res, err := mcpserver.ExportSubtitles(workDir, "", outputDir, mcpserver.ParseSubtitleFormat(format),
		stylePreset, maxChars, maxLines, w, h)
	if err != nil {
		return nil, err
	}
	view := &SubtitlesView{
		SRTPath: res.SRTPath, ASSPath: res.ASSPath,
		CueCount: res.CueCount, SplitCount: res.SplitCount,
		FinalDuration: res.FinalDuration, OutputDir: res.OutputDir,
	}
	// 回传字幕条列表（前端展示；与产物同源，保证"所见即所得"）
	cues, _, cerr := mcpserver.RemapForOutput(workDir, "", mcpserver.RemapOptions{
		MaxCharsPerCue: maxChars, MaxLines: maxLines,
	})
	if cerr == nil {
		view.Cues = cues
	}
	return view, nil
}

// AICutBurnSubtitles 字幕内嵌烧录（第三步内嵌模式）：提交单项目批量任务
// （0→成片时长，reencode + ass 滤镜），返回 task_id，前端照常轮询进度/完成
// （复用 trim:progress / trim:done 事件契约，与 MergeCutSegments 同手法）。
func (a *App) AICutBurnSubtitles(videoPath, assPath, encoder string, crf int, preset string) (string, error) {
	if videoPath == "" {
		return "", fmt.Errorf("成片路径为空")
	}
	if assPath == "" {
		return "", fmt.Errorf("ASS 字幕路径为空")
	}
	probe, err := a.svc.Probe(a.appContext(), videoPath)
	if err != nil {
		return "", fmt.Errorf("探测成片失败: %w", err)
	}
	if encoder == "" {
		encoder = a.cfg.SubBurnEncoder
	}
	if crf <= 0 {
		crf = a.cfg.SubBurnCRF
	}
	if preset == "" {
		preset = a.cfg.SubBurnPreset
	}
	ext := filepath.Ext(videoPath)
	output := videoPath[:len(videoPath)-len(ext)] + "_burned" + ext
	return a.svc.TrimBatch(service.BatchRequest{Items: []service.BatchItem{{
		InputPath:  videoPath,
		Start:      0,
		End:        probe.Duration,
		Strategy:   "reencode", // 烧录必重编码（ass 滤镜在视频链上）
		OutputPath: output,
		Subtitle: &service.SubtitleBurn{
			AssPath: assPath, Encoder: encoder, CRF: crf, Preset: preset,
		},
	}}})
}

// AICutAnimateCaptions 字幕二次加工（第四步）：规则引擎自动选特效 → edited_fx.ass。
// mode 空 = 读 config（auto/off）；skills 非空 = 显式覆盖（Codex/高级用户通道）。
func (a *App) AICutAnimateCaptions(workDir, finalVideo, mode string, skills []string, maxSkills int) (*MotionPlanView, error) {
	if workDir == "" {
		return nil, fmt.Errorf("输出目录为空")
	}
	final := deriveFinalVideo(workDir, finalVideo)
	outputDir := workDir
	if final != "" {
		outputDir = filepath.Dir(final)
	}
	w, h := a.probeVideoSize(final)
	if mode == "" {
		mode = a.cfg.SubAnimate
	}
	if maxSkills <= 0 {
		maxSkills = a.cfg.SubMaxAutoSkills
	}
	res, err := mcpserver.AnimateCaptions(workDir, "", outputDir, a.cfg.SubStylePreset, a.cfg.SubKeywordColor,
		a.cfg.SubMaxCharsPerCue, a.cfg.SubMaxLines, mode, skills, maxSkills, a.cfg.SubDisabledSkills,
		a.cfg.ASRHotwords, a.cfg.SubKeywordMinFreq, w, h)
	if err != nil {
		return nil, err
	}
	orientation := ""
	switch {
	case w > h:
		orientation = "landscape"
	case h > 0:
		orientation = "portrait"
	}
	return &MotionPlanView{
		FXPath: res.FXPath, PlanPath: res.PlanPath,
		Selected: res.Selected, Reasoning: res.Reasoning, Rejected: res.Rejected,
		Analysis: res.Analysis, Static: res.Static,
		StyleID: a.cfg.SubStylePreset, Orientation: orientation,
	}, nil
}
