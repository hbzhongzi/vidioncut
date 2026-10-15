package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mark3labs/mcp-go/mcp"

	"videocut/internal/edits"
	"videocut/internal/service"
)

// registerTools 注册 M8 工具（§19.3）；M9 工具见 asr_tools.go。
func (s *Server) registerTools() {
	s.addTool(mcp.NewTool("videocut_probe",
		mcp.WithDescription("探测音视频元数据（时长/分辨率/编码/流信息），并把文件登记进预览白名单"),
		mcp.WithString("input_path", mcp.Required(), mcp.Description("视频文件绝对路径")),
	), s.toolProbe)

	s.addTool(mcp.NewTool("videocut_trim",
		mcp.WithDescription("提交单文件裁剪任务（异步，立即返回 task_id；用 videocut_get_task 轮询）"),
		mcp.WithString("input_path", mcp.Required(), mcp.Description("输入视频绝对路径")),
		mcp.WithNumber("start", mcp.Required(), mcp.Description("开始时间（秒）")),
		mcp.WithNumber("end", mcp.Required(), mcp.Description("结束时间（秒）")),
		mcp.WithString("strategy", mcp.Description("stream_copy | reencode | audio_only，缺省取配置默认")),
		mcp.WithString("output_path", mcp.Description("输出路径，缺省按输入名派生")),
		mcp.WithBoolean("overwrite", mcp.Description("允许覆盖已存在输出（默认 false，自动追加序号）")),
	), s.toolTrim)

	s.addTool(mcp.NewTool("videocut_trim_batch",
		mcp.WithDescription("提交批量裁剪任务（原子提交：任一项校验失败整批拒绝；异步返回 task_id）"),
		mcp.WithArray("items", mcp.Required(),
			mcp.Description("子项数组"),
			mcp.Items(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"input_path":  map[string]any{"type": "string", "description": "输入视频绝对路径"},
					"start":       map[string]any{"type": "number", "description": "开始时间（秒）"},
					"end":         map[string]any{"type": "number", "description": "结束时间（秒）"},
					"strategy":    map[string]any{"type": "string", "description": "stream_copy | reencode | audio_only"},
					"output_path": map[string]any{"type": "string", "description": "输出路径（可空）"},
				},
				"required": []string{"input_path", "start", "end"},
			}),
		),
	), s.toolTrimBatch)

	s.addTool(mcp.NewTool("videocut_merge",
		mcp.WithDescription("提交合并任务（≥2 个片段按顺序合并；auto 模式参数不一致时自动重编码；异步返回 task_id）"),
		mcp.WithArray("items", mcp.Required(),
			mcp.Description("片段数组（顺序即输出顺序）"),
			mcp.Items(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"input_path": map[string]any{"type": "string", "description": "片段绝对路径"},
					"start":      map[string]any{"type": "number", "description": "开始时间（秒，可空=整段）"},
					"end":        map[string]any{"type": "number", "description": "结束时间（秒，可空=整段）"},
					"strategy":   map[string]any{"type": "string", "description": "hybrid 裁剪策略"},
				},
				"required": []string{"input_path"},
			}),
		),
		mcp.WithString("mode", mcp.Description("auto | demuxer | filter，缺省取 merge.default_mode")),
		mcp.WithString("output_path", mcp.Description("输出路径，缺省派生自首项")),
		mcp.WithBoolean("overwrite", mcp.Description("允许覆盖（默认 false）")),
	), s.toolMerge)

	s.addTool(mcp.NewTool("videocut_get_task",
		mcp.WithDescription("查询任务状态/进度/逐项结果（轮询 trim/trim_batch/merge 的 task_id）"),
		mcp.WithString("task_id", mcp.Required(), mcp.Description("任务 ID")),
	), s.toolGetTask)

	s.addTool(mcp.NewTool("videocut_cancel",
		mcp.WithDescription("取消任务（pending/processing 均可；批量/合并仅取消未完成项，已完成项产物保留）"),
		mcp.WithString("task_id", mcp.Required(), mcp.Description("任务 ID")),
	), s.toolCancel)

	s.addTool(mcp.NewTool("videocut_export_transcript",
		mcp.WithDescription("导出文字稿：把 <base_dir>/work/transcript.srt 渲染为 Markdown 或纯文本"),
		mcp.WithString("base_dir", mcp.Required(), mcp.Description("转录输出目录（含 work/transcript.srt）")),
		mcp.WithString("format", mcp.Description("md | txt（默认 md）")),
	), s.toolExportTranscript)

	s.addTool(mcp.NewTool("videocut_validate_edits",
		mcp.WithDescription("校验删词账本 edits.json 合法性（version/source/items 升序/不重叠/不越界/类型合法），并返回统计摘要"),
		mcp.WithString("edits_json", mcp.Required(), mcp.Description("edits.json 的 JSON 文本")),
		mcp.WithNumber("source_duration", mcp.Description("源时长（秒，可空；缺省时探测 source 文件）")),
	), s.toolValidateEdits)

	s.addTool(mcp.NewTool("videocut_get_edits_summary",
		mcp.WithDescription("读取 edits.json 返回统计摘要（删除项数/总时长/按类型分组/保留段补集），供剪切前评估"),
		mcp.WithString("edits_path", mcp.Required(), mcp.Description("edits.json 文件路径")),
		mcp.WithNumber("source_duration", mcp.Description("源时长（秒，可空；缺省时探测 source 文件）")),
	), s.toolGetEditsSummary)
}

// ---- probe ----

func (s *Server) toolProbe(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := req.RequireString("input_path")
	if err != nil {
		return failResult(codeInvalidParams, "", "缺少参数 input_path"), nil
	}
	res, err := s.svc.Probe(ctx, path)
	if err != nil {
		return failResult(codeProbeFailed, "", err.Error()), nil
	}
	return jsonResult(res)
}

// ---- trim ----

func (s *Server) toolTrim(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var r struct {
		InputPath  string  `json:"input_path"`
		Start      float64 `json:"start"`
		End        float64 `json:"end"`
		Strategy   string  `json:"strategy"`
		OutputPath string  `json:"output_path"`
		Overwrite  bool    `json:"overwrite"`
	}
	if err := req.BindArguments(&r); err != nil {
		return failResult(codeInvalidParams, "", "参数解析失败: "+err.Error()), nil
	}
	taskID, err := s.svc.Trim(service.TrimRequest{
		InputPath:  r.InputPath,
		Start:      r.Start,
		End:        r.End,
		Strategy:   r.Strategy,
		OutputPath: r.OutputPath,
		Overwrite:  r.Overwrite,
	})
	if err != nil {
		return failResult(codeInvalidParams, "", err.Error()), nil
	}
	return jsonResult(map[string]any{
		"task_id":    taskID,
		"kind":       "trim",
		"poll_with":  "videocut_get_task",
		"note":       "异步任务：用 videocut_get_task 轮询 task_id，直到 status 为 completed/failed",
		"start":      r.Start,
		"end":        r.End,
	})
}

// ---- trim_batch ----

func (s *Server) toolTrimBatch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var r struct {
		Items []service.BatchItem `json:"items"`
	}
	if err := req.BindArguments(&r); err != nil {
		return failResult(codeInvalidParams, "", "参数解析失败: "+err.Error()), nil
	}
	if len(r.Items) == 0 {
		return failResult(codeInvalidParams, "", "items 不能为空"), nil
	}
	taskID, err := s.svc.TrimBatch(service.BatchRequest{Items: r.Items})
	if err != nil {
		return failResult(codeInvalidParams, "", err.Error()), nil
	}
	return jsonResult(map[string]any{
		"task_id":   taskID,
		"kind":      "batch",
		"items":     len(r.Items),
		"poll_with": "videocut_get_task",
		"note":      "异步任务：终态为 completed / partial（部分成功，好项产物保留）/ failed",
	})
}

// ---- merge ----

func (s *Server) toolMerge(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var r struct {
		Items []struct {
			InputPath string  `json:"input_path"`
			Start     float64 `json:"start"`
			End       float64 `json:"end"`
			Strategy  string  `json:"strategy"`
		} `json:"items"`
		Mode       string `json:"mode"`
		OutputPath string `json:"output_path"`
		Overwrite  bool   `json:"overwrite"`
	}
	if err := req.BindArguments(&r); err != nil {
		return failResult(codeInvalidParams, "", "参数解析失败: "+err.Error()), nil
	}
	if len(r.Items) < 2 {
		return failResult(codeInvalidParams, "", "合并至少需要 2 个片段"), nil
	}
	items := make([]service.MergeItem, 0, len(r.Items))
	for _, it := range r.Items {
		items = append(items, service.MergeItem{
			InputPath: it.InputPath,
			Start:     it.Start,
			End:       it.End,
			HasRange:  it.Start > 0 || it.End > 0, // 传了区间即视为带区间
			Strategy:  it.Strategy,
		})
	}
	taskID, err := s.svc.Merge(service.MergeRequest{
		Items:      items,
		Mode:       r.Mode,
		OutputPath: r.OutputPath,
		Overwrite:  r.Overwrite,
	})
	if err != nil {
		return failResult(codeInvalidParams, "", err.Error()), nil
	}
	return jsonResult(map[string]any{
		"task_id":   taskID,
		"kind":      "merge",
		"poll_with": "videocut_get_task",
		"note":      "异步任务：auto 模式下参数不一致自动走重编码，并可能发 merge:notice",
	})
}

// ---- get_task / cancel ----

func (s *Server) toolGetTask(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, err := req.RequireString("task_id")
	if err != nil {
		return failResult(codeInvalidParams, "", "缺少参数 task_id"), nil
	}
	task, ok := s.svc.GetTaskSnapshot(id)
	if !ok {
		return failResult(codeTaskNotFound, "", "任务不存在: "+id), nil
	}
	return jsonResult(task)
}

func (s *Server) toolCancel(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, err := req.RequireString("task_id")
	if err != nil {
		return failResult(codeInvalidParams, "", "缺少参数 task_id"), nil
	}
	if err := s.svc.Cancel(id); err != nil {
		return failResult(codeTaskNotFound, "", err.Error()), nil
	}
	return jsonResult(map[string]any{"task_id": id, "canceled": true})
}

// ---- export_transcript ----

func (s *Server) toolExportTranscript(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	base, err := req.RequireString("base_dir")
	if err != nil {
		return failResult(codeInvalidParams, "", "缺少参数 base_dir"), nil
	}
	format := req.GetString("format", "md")
	srtPath := filepath.Join(base, "work", "transcript.srt")
	text, err := edits.ExportTranscript(srtPath, format)
	if err != nil {
		return failResult(codeInvalidParams, "", fmt.Sprintf("导出失败（%s）: %v", srtPath, err)), nil
	}
	return jsonResult(map[string]any{
		"format": format,
		"path":   srtPath,
		"bytes":  len(text),
		"content": text,
	})
}

// ---- validate_edits ----

func (s *Server) toolValidateEdits(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	raw, err := req.RequireString("edits_json")
	if err != nil {
		return failResult(codeInvalidParams, "", "缺少参数 edits_json"), nil
	}
	var ledger edits.EditsLedger
	if jerr := json.Unmarshal([]byte(raw), &ledger); jerr != nil {
		return failResult(codeInvalidParams, "", "edits_json 解析失败: "+jerr.Error()), nil
	}
	duration := req.GetFloat("source_duration", 0)
	if duration <= 0 && ledger.Source != "" {
		if probe, perr := s.svc.Probe(ctx, ledger.Source); perr == nil {
			duration = probe.Duration
		}
	}
	if err := edits.ValidateEdits(&ledger, duration); err != nil {
		return failResult(codeEditsInvalid, "修正后重新调用 videocut_validate_edits", err.Error()), nil
	}
	return jsonResult(map[string]any{
		"valid":   true,
		"summary": edits.Summarize(&ledger, duration),
	})
}

// ---- get_edits_summary ----

func (s *Server) toolGetEditsSummary(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := req.RequireString("edits_path")
	if err != nil {
		return failResult(codeInvalidParams, "", "缺少参数 edits_path"), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return failResult(codeInvalidParams, "", "读取账本失败: "+err.Error()), nil
	}
	var ledger edits.EditsLedger
	if jerr := json.Unmarshal(data, &ledger); jerr != nil {
		return failResult(codeInvalidParams, "", "edits.json 解析失败: "+jerr.Error()), nil
	}
	duration := req.GetFloat("source_duration", 0)
	if duration <= 0 && ledger.Source != "" {
		if probe, perr := s.svc.Probe(ctx, ledger.Source); perr == nil {
			duration = probe.Duration
		}
	}
	if err := edits.ValidateEdits(&ledger, duration); err != nil {
		return failResult(codeEditsInvalid, "", "账本非法: "+err.Error()), nil
	}
	keeps := edits.EditsToKeepRanges(&ledger, duration)
	return jsonResult(map[string]any{
		"summary":     edits.Summarize(&ledger, duration),
		"keep_ranges": keeps,
		"cut_with":    "videocut_trim_batch（把 keep_ranges 逐段作为 items 提交，strategy=reencode）",
	})
}
