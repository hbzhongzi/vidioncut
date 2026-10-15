package mcpserver

// 字幕功能共享逻辑（M12）：MCP 工具与 GUI 绑定（app_v18.go）共用同一套实现，
// 避免业务规则两处漂移（与 ExportTranscriptFile 同模式，开发文档 §19.8）。
//
// 管线（字幕功能更新文档 §3.1）：signals.json + edits.json → 重映射（源轴→成片轴）
// → 预换行 → SRT/ASS 渲染；二次加工：分析 → 规则引擎 → override tags → fx.ass。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"videocut/internal/edits"
	"videocut/internal/subtitles"
)

// SubtitlePaths 字幕导出产物路径。
type SubtitlePaths struct {
	SRTPath string `json:"srt_path"`
	ASSPath string `json:"ass_path"`
}

// SubtitlesResult 导出结果（GUI 与 MCP 共用契约）。
type SubtitlesResult struct {
	SRTPath       string  `json:"srt_path"`
	ASSPath       string  `json:"ass_path"`
	CueCount      int     `json:"cue_count"`
	SplitCount    int     `json:"split_count"` // 跨段词级断开条数（UI 提示人工复核）
	FinalDuration float64 `json:"final_duration"`
	OutputDir     string  `json:"output_dir"`
}

// LoadSignals 读取 base_dir/work/signals.json（§19.5 产物约定）。
func LoadSignals(baseDir string) (*signalsFile, error) {
	sigPath := filepath.Join(baseDir, "work", "signals.json")
	data, err := os.ReadFile(sigPath)
	if err != nil {
		return nil, fmt.Errorf("读取 signals.json 失败（%s）：请先运行 videocut_process", sigPath)
	}
	var sig signalsFile
	if jerr := json.Unmarshal(data, &sig); jerr != nil {
		return nil, fmt.Errorf("signals.json 解析失败: %w", jerr)
	}
	return &sig, nil
}

// LoadLedgerForSubtitles 读取账本：显式路径优先，缺省 base_dir/work/edits.json；
// 账本不存在时按"零删"处理（全集保留——成片=源片，字幕整体平移 0）。
func LoadLedgerForSubtitles(baseDir, editsPath string) (*edits.EditsLedger, error) {
	p := editsPath
	if p == "" {
		p = filepath.Join(baseDir, "work", "edits.json")
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) && editsPath == "" {
			// 无账本 = 未删减：合法降级（全集保留）
			return &edits.EditsLedger{Version: edits.LedgerVersion, Items: nil}, nil
		}
		return nil, fmt.Errorf("读取 %s 失败: %w", p, err)
	}
	var l edits.EditsLedger
	if jerr := json.Unmarshal(data, &l); jerr != nil {
		return nil, fmt.Errorf("edits.json 解析失败: %w", jerr)
	}
	return &l, nil
}

// toSubtitleSegments / toSubtitleWords 转换 signals.json 结构为 subtitles 包输入。
func toSubtitleSegments(sig *signalsFile) []subtitles.Segment {
	out := make([]subtitles.Segment, 0, len(sig.Segments))
	for _, s := range sig.Segments {
		out = append(out, subtitles.Segment{Start: s.Start, End: s.End, Text: s.Text})
	}
	return out
}

func toSubtitleWords(sig *signalsFile) []subtitles.Word {
	out := make([]subtitles.Word, 0, len(sig.Words))
	for _, w := range sig.Words {
		out = append(out, subtitles.Word{Start: w.Start, End: w.End, Word: w.Word})
	}
	return out
}

// RemapOptions 重映射参数（分段与换行）。
type RemapOptions struct {
	MaxCharsPerCue int
	MaxLines       int
}

// RemapForOutput 读 signals + 账本 → 重映射 + 预换行，返回成片时间轴字幕。
// sourceDuration 由 signals.json 记录（转录时写入）。
func RemapForOutput(baseDir, editsPath string, opts RemapOptions) ([]subtitles.Cue, float64, error) {
	sig, err := LoadSignals(baseDir)
	if err != nil {
		return nil, 0, err
	}
	ledger, err := LoadLedgerForSubtitles(baseDir, editsPath)
	if err != nil {
		return nil, 0, err
	}
	keeps := edits.EditsToKeepRanges(ledger, sig.Duration)
	cues, rerr := subtitles.RemapCues(toSubtitleSegments(sig), toSubtitleWords(sig), keeps, sig.Duration)
	if rerr != nil {
		return nil, 0, rerr
	}
	if opts.MaxCharsPerCue > 0 || opts.MaxLines > 0 {
		cues = subtitles.SegmentCues(cues, opts.MaxCharsPerCue, opts.MaxLines)
	}
	return cues, subtitles.FinalDuration(keeps), nil
}

// ExportSubtitles 外显导出（§4.3）：base_dir 的 work/ 产物 → edited.srt / edited.ass
// 落 outputDir（缺省 base_dir）。format: srt|ass|both。返回产物路径与统计。
func ExportSubtitles(baseDir, editsPath, outputDir, format, stylePreset string, maxChars, maxLines int, videoWidth, videoHeight int) (*SubtitlesResult, error) {
	if outputDir == "" {
		outputDir = baseDir
	}
	cues, finalDur, err := RemapForOutput(baseDir, editsPath, RemapOptions{MaxCharsPerCue: maxChars, MaxLines: maxLines})
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建输出目录 %s 失败: %w", outputDir, err)
	}
	res := &SubtitlesResult{CueCount: len(cues), FinalDuration: finalDur, OutputDir: outputDir}
	for _, c := range cues {
		if c.SplitAt {
			res.SplitCount++
		}
	}
	srtPath := filepath.Join(outputDir, "edited.srt")
	assPath := filepath.Join(outputDir, "edited.ass")
	switch format {
	case "srt":
		res.SRTPath = srtPath
		if werr := os.WriteFile(srtPath, []byte(subtitles.RenderSRT(cues)), 0o644); werr != nil {
			return nil, fmt.Errorf("写入 %s 失败: %w", srtPath, werr)
		}
	case "ass":
		res.ASSPath = assPath
		if werr := os.WriteFile(assPath, []byte(subtitles.RenderASS(cues, subtitles.GetPreset(stylePreset), videoWidth, videoHeight)), 0o644); werr != nil {
			return nil, fmt.Errorf("写入 %s 失败: %w", assPath, werr)
		}
	default: // both
		res.SRTPath, res.ASSPath = srtPath, assPath
		if werr := os.WriteFile(srtPath, []byte(subtitles.RenderSRT(cues)), 0o644); werr != nil {
			return nil, fmt.Errorf("写入 %s 失败: %w", srtPath, werr)
		}
		if werr := os.WriteFile(assPath, []byte(subtitles.RenderASS(cues, subtitles.GetPreset(stylePreset), videoWidth, videoHeight)), 0o644); werr != nil {
			return nil, fmt.Errorf("写入 %s 失败: %w", assPath, werr)
		}
	}
	return res, nil
}

// AnimateResult 二次加工结果（§5.6）。
type AnimateResult struct {
	FXPath    string                    `json:"fx_path"`   // edited_fx.ass
	PlanPath  string                    `json:"plan_path"` // motion-plan.json
	Selected  []string                  `json:"selected"`  // 选中特效
	Reasoning string                    `json:"reasoning"` // 选择理由
	Rejected  []subtitles.RejectedSkill `json:"rejected"`
	Analysis  *subtitles.Analysis       `json:"analysis"` // 五维特征（GUI 单一数据源）
	Static    bool                      `json:"static"`   // true=无特效命中（纯静态合法态）
}

// AnimateCaptions 字幕二次加工（§5）：分析 → 两级规则引擎 → ASS override tags。
// mode: auto|off（off=纯静态导出，等价于普通 ASS）；skills 非空=显式覆盖（Codex 通道）。
func AnimateCaptions(baseDir, editsPath, outputDir, stylePreset, keywordColor string, maxChars, maxLines int,
	mode string, skills []string, maxSkills int, disabledSkills []string, hotwords []string, minFreq int,
	videoWidth, videoHeight int) (*AnimateResult, error) {
	if outputDir == "" {
		outputDir = baseDir
	}
	sig, err := LoadSignals(baseDir)
	if err != nil {
		return nil, err
	}
	ledger, err := LoadLedgerForSubtitles(baseDir, editsPath)
	if err != nil {
		return nil, err
	}
	keeps := edits.EditsToKeepRanges(ledger, sig.Duration)
	cues, rerr := subtitles.RemapCues(toSubtitleSegments(sig), toSubtitleWords(sig), keeps, sig.Duration)
	if rerr != nil {
		return nil, rerr
	}
	cues = subtitles.SegmentCues(cues, maxChars, maxLines)

	// 分析（数据源：原始 words 词频 + 重对齐后 cue）
	analysis := subtitles.Analyze(cues, toSubtitleWords(sig), hotwords, minFreq)

	res := &AnimateResult{Analysis: analysis}
	if mode == "off" {
		res.Static = true
	} else {
		auto := subtitles.SelectSkills(analysis, subtitles.SelectOptions{
			MaxSkills:      maxSkills,
			DisabledSkills: disabledSkills,
			ExplicitSkills: skills,
		})
		res.Selected = auto.Selected
		res.Reasoning = auto.Reasoning
		res.Rejected = auto.Rejected
		if len(auto.Selected) == 0 {
			res.Static = true
		}
		// 应用特效 → edited_fx.ass
		plan := &subtitles.MotionPlan{}
		plan.Meta.StyleID = stylePreset
		plan.Meta.AutoSkills = auto
		if videoWidth > videoHeight {
			plan.Meta.Orientation = "landscape"
		} else if videoHeight > 0 {
			plan.Meta.Orientation = "portrait"
		}
		fxCues := subtitles.ApplyEffects(cues, subtitles.EffectOptions{
			Plan: plan, Preset: subtitles.GetPreset(stylePreset),
			VideoWidth: videoWidth, VideoHeight: videoHeight, KeywordColor: keywordColor,
		})
		// 不变式自检（§5.6）：特效改写不得破坏时间轴合法性
		if verr := subtitles.ValidateCues(fxCues, subtitles.FinalDuration(keeps)); verr != nil {
			return nil, fmt.Errorf("特效产物自检失败: %w", verr)
		}
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			return nil, fmt.Errorf("创建输出目录 %s 失败: %w", outputDir, err)
		}
		fxPath := filepath.Join(outputDir, "edited_fx.ass")
		if werr := os.WriteFile(fxPath, []byte(subtitles.RenderASS(fxCues, subtitles.GetPreset(stylePreset), videoWidth, videoHeight)), 0o644); werr != nil {
			return nil, fmt.Errorf("写入 %s 失败: %w", fxPath, werr)
		}
		res.FXPath = fxPath
		planPath := filepath.Join(outputDir, "motion-plan.json")
		if pdata, merr := json.MarshalIndent(plan, "", "  "); merr == nil {
			if werr := os.WriteFile(planPath, pdata, 0o644); werr == nil {
				res.PlanPath = planPath
			}
		}
	}
	return res, nil
}

// ParseSubtitleFormat 归一化外显格式（srt/ass/both；非法回落 both）。
func ParseSubtitleFormat(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "srt", "ass", "both":
		return strings.ToLower(strings.TrimSpace(s))
	default:
		return "both"
	}
}

// ClampCueParams 分段参数防御（≤0 回落默认；过大封顶防预换行退化）。
func ClampCueParams(maxChars, maxLines int) (int, int) {
	if maxChars <= 0 {
		maxChars = 20
	}
	if maxChars > 100 {
		maxChars = 100
	}
	if maxLines <= 0 {
		maxLines = 2
	}
	if maxLines > 5 {
		maxLines = 5
	}
	return maxChars, maxLines
}
