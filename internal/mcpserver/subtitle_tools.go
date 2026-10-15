package mcpserver

// M12 字幕 MCP 工具（字幕功能更新文档 §3.3）：export / burn / animate。
// 与 GUI 绑定（app_v18.go）共用 subtitle_api.go 的同一套实现。

import (
	"context"
	"path/filepath"

	"github.com/mark3labs/mcp-go/mcp"

	"videocut/internal/service"
)

// registerSubtitleTools 注册 M12 工具（§3.3）。
func (s *Server) registerSubtitleTools() {
	s.addTool(mcp.NewTool("videocut_export_subtitles",
		mcp.WithDescription("粗剪成片后字幕重对齐 + 外显导出：读 <base_dir>/work/signals.json 与账本，映射到成片时间轴，输出 edited.srt / edited.ass（默认双格式）"),
		mcp.WithString("base_dir", mcp.Required(), mcp.Description("transcribe 输出目录（含 work/signals.json）")),
		mcp.WithString("edits_json", mcp.Description("edits.json 路径（缺省读 <base_dir>/work/edits.json；不存在按零删处理）")),
		mcp.WithString("output_dir", mcp.Description("字幕输出目录（缺省 = base_dir；建议传成片所在目录）")),
		mcp.WithString("format", mcp.Description("外显格式：srt | ass | both（默认 both）")),
		mcp.WithString("style_preset", mcp.Description("ASS 样式预设：default | talking-head | minimal（默认 default）")),
		mcp.WithNumber("max_chars_per_cue", mcp.Description("单条最大字数（默认 20，程序侧预换行）")),
		mcp.WithNumber("max_lines", mcp.Description("最多行数（默认 2）")),
	), s.toolExportSubtitles)

	s.addTool(mcp.NewTool("videocut_burn_subtitles",
		mcp.WithDescription("字幕内嵌烧录：ass 滤镜重编码（音轨直复制），异步任务返回 task_id，用 videocut_get_task 轮询"),
		mcp.WithString("video_path", mcp.Required(), mcp.Description("成片视频绝对路径（烧录输入）")),
		mcp.WithString("ass_path", mcp.Required(), mcp.Description("ASS 字幕文件绝对路径（edited.ass 或 edited_fx.ass）")),
		mcp.WithString("output_path", mcp.Description("烧录产物路径（缺省 <成片名>_burned.mp4）")),
		mcp.WithString("encoder", mcp.Description("编码器：libx264 | h264_nvenc | h264_qsv | h264_amf（默认 libx264；启动失败自动回退）")),
		mcp.WithNumber("crf", mcp.Description("画质 0–51（默认 18，越小越好）")),
		mcp.WithString("preset", mcp.Description("编码预设（默认 fast）")),
	), s.toolBurnSubtitles)

	s.addTool(mcp.NewTool("videocut_animate_captions",
		mcp.WithDescription("字幕二次加工：两级规则引擎自动选特效（关键词密度/内容类型/节奏/信息密度/情感强度），写 ASS override tags 产出 edited_fx.ass，选择理由落 motion-plan.json"),
		mcp.WithString("base_dir", mcp.Required(), mcp.Description("transcribe 输出目录（含 work/signals.json）")),
		mcp.WithString("edits_json", mcp.Description("edits.json 路径（缺省读 <base_dir>/work/edits.json）")),
		mcp.WithString("output_dir", mcp.Description("产物输出目录（缺省 = base_dir）")),
		mcp.WithString("mode", mcp.Description("auto | off（缺省读 config subtitles.animate；off=纯静态）")),
		mcp.WithArray("skills", mcp.Description("显式指定特效（Codex 覆盖通道；传入则跳过自动选择）"), mcp.WithStringItems()),
		mcp.WithNumber("max_skills", mcp.Description("自动选择上限（默认 2，读 config subtitles.max_auto_skills）")),
	), s.toolAnimateCaptions)
}

// ---- export_subtitles ----

func (s *Server) toolExportSubtitles(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	base, err := req.RequireString("base_dir")
	if err != nil {
		return failResult(codeInvalidParams, "", "缺少参数 base_dir"), nil
	}
	editsJSON := req.GetString("edits_json", "")
	outputDir := req.GetString("output_dir", "")
	format := ParseSubtitleFormat(req.GetString("format", "both"))
	preset := req.GetString("style_preset", s.cfg.SubStylePreset)
	maxChars, maxLines := ClampCueParams(req.GetInt("max_chars_per_cue", s.cfg.SubMaxCharsPerCue), req.GetInt("max_lines", s.cfg.SubMaxLines))

	// 成片宽高（ASS PlayRes）：优先探测成片，失败回落 0（RenderASS 内部回落 1920×1080）
	w, h := 0, 0
	if src := guessFinalVideo(base, editsJSON); src != "" {
		if probe, perr := s.svc.Probe(ctx, src); perr == nil {
			w, h = probe.Width, probe.Height
		}
	}

	res, terr := ExportSubtitles(base, editsJSON, outputDir, format, preset, maxChars, maxLines, w, h)
	if terr != nil {
		return failResult(codeInvalidParams, "", terr.Error()), nil
	}
	return jsonResult(map[string]any{
		"srt_path":       res.SRTPath,
		"ass_path":       res.ASSPath,
		"cue_count":      res.CueCount,
		"split_count":    res.SplitCount,
		"final_duration": res.FinalDuration,
		"output_dir":     res.OutputDir,
		"note":           "字幕为成片时间轴；跨段词级断开的条目建议人工复核断点语义。导入剪映/PR 见 cut-talking-head 技能说明",
	})
}

// guessFinalVideo 推测成片路径：<source 去扩展名>_cut<扩展名>（AICutSubmit 派生规则）。
func guessFinalVideo(baseDir, editsPath string) string {
	sig, err := LoadSignals(baseDir)
	if err != nil || sig.Source == "" {
		return ""
	}
	ext := filepath.Ext(sig.Source)
	base := sig.Source[:len(sig.Source)-len(ext)]
	return base + "_cut" + ext
}

// ---- burn_subtitles ----

func (s *Server) toolBurnSubtitles(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	video, err := req.RequireString("video_path")
	if err != nil {
		return failResult(codeInvalidParams, "", "缺少参数 video_path"), nil
	}
	ass, err := req.RequireString("ass_path")
	if err != nil {
		return failResult(codeInvalidParams, "", "缺少参数 ass_path"), nil
	}
	output := req.GetString("output_path", "")
	if output == "" {
		ext := filepath.Ext(video)
		output = video[:len(video)-len(ext)] + "_burned" + ext
	}
	encoder := req.GetString("encoder", s.cfg.SubBurnEncoder)
	if encoder == "" {
		encoder = "libx264"
	}
	crf := req.GetInt("crf", s.cfg.SubBurnCRF)
	preset := req.GetString("preset", s.cfg.SubBurnPreset)

	// 整片烧录（0→成片时长；先探测拿真实时长，预校验按上界钳位）
	probe, perr := s.svc.Probe(ctx, video)
	if perr != nil {
		return failResult(codeProbeFailed, "", "探测成片失败: "+perr.Error()), nil
	}
	taskID, terr := s.svc.TrimBatch(service.BatchRequest{Items: []service.BatchItem{{
		InputPath:  video,
		Start:      0,
		End:        probe.Duration,
		Strategy:   "reencode",
		OutputPath: output,
		Subtitle: &service.SubtitleBurn{
			AssPath: ass, Encoder: encoder, CRF: crf, Preset: preset,
		},
	}}})
	if terr != nil {
		return failResult(codeInvalidParams, "", terr.Error()), nil
	}
	return jsonResult(map[string]any{
		"task_id":     taskID,
		"output_path": output,
		"encoder":     encoder,
		"crf":         crf,
		"next":        "videocut_get_task 轮询进度；trim:done 事件带 output_path",
		"note":        "烧录为重编码任务（耗时随时长线性增长）；编码器启动失败时调用方可改 libx264 重试",
	})
}

// ---- animate_captions ----

func (s *Server) toolAnimateCaptions(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	base, err := req.RequireString("base_dir")
	if err != nil {
		return failResult(codeInvalidParams, "", "缺少参数 base_dir"), nil
	}
	editsJSON := req.GetString("edits_json", "")
	outputDir := req.GetString("output_dir", "")
	mode := req.GetString("mode", s.cfg.SubAnimate)
	skills := req.GetStringSlice("skills", nil)
	maxSkills := req.GetInt("max_skills", s.cfg.SubMaxAutoSkills)

	w, h := 0, 0
	if src := guessFinalVideo(base, editsJSON); src != "" {
		if probe, perr := s.svc.Probe(ctx, src); perr == nil {
			w, h = probe.Width, probe.Height
		}
	}

	res, terr := AnimateCaptions(base, editsJSON, outputDir, s.cfg.SubStylePreset, s.cfg.SubKeywordColor,
		s.cfg.SubMaxCharsPerCue, s.cfg.SubMaxLines, mode, skills, maxSkills, s.cfg.SubDisabledSkills,
		s.cfg.ASRHotwords, s.cfg.SubKeywordMinFreq, w, h)
	if terr != nil {
		return failResult(codeInvalidParams, "", terr.Error()), nil
	}
	out := map[string]any{
		"fx_path":     res.FXPath,
		"plan_path":   res.PlanPath,
		"selected":    res.Selected,
		"reasoning":   res.Reasoning,
		"rejected":    res.Rejected,
		"analysis":    res.Analysis,
		"static":      res.Static,
		"style_id":    s.cfg.SubStylePreset,
		"orientation": planOrientation(w, h),
	}
	if res.Static {
		out["note"] = "无特征命中或 mode=off：纯静态字幕（合法态）。如需指定特效，用 skills 参数显式传入"
	}
	return jsonResult(out)
}

func planOrientation(w, h int) string {
	switch {
	case w > h:
		return "landscape"
	case h > 0:
		return "portrait"
	default:
		return ""
	}
}
