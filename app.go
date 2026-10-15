package main

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"videocut/internal/asr"
	"videocut/internal/config"
	"videocut/internal/ffmpeg"
	"videocut/internal/service"
)

// App 是 Wails 暴露给前端的业务对象（开发文档 v1.1 §5.1）。
type App struct {
	ctx context.Context
	cfg *config.Config
	svc *service.Service
	asr *asr.Client // v1.7 §19.8：GUI 的 ASR 状态面板（懒加载，不主动拉起 sidecar）

	// lastWorkDir 最近一次 AICutGenerate 的输出根目录（M12）：AICutSubmit
	// 据此把用户勾选后的最终账本写回 work/edits.json，使后续字幕步骤
	// （AICutExportSubtitles/AICutAnimateCaptions 读盘）拿到的就是用户确认版，
	// 而非生成时的全量候选（真实端到端暴露的语义偏差，v1.8 补丁）。
	lastWorkDir string
}

// NewApp 构造 App。
func NewApp(cfg *config.Config, svc *service.Service, asrClient *asr.Client) *App {
	return &App{cfg: cfg, svc: svc, asr: asrClient}
}

// startup 由 Wails 生命周期调用，注入应用上下文。
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.svc.SetAppContext(ctx)
}

// Trim 提交裁剪任务，返回 task_id（异步）。
func (a *App) Trim(req service.TrimRequest) (string, error) {
	return a.svc.Trim(req)
}

// TrimBatch 提交批量裁剪任务，返回 batch_id（v1.4 §18.6，异步）。
func (a *App) TrimBatch(req service.BatchRequest) (string, error) {
	return a.svc.TrimBatch(req)
}

// Merge 提交合并任务，返回 task_id（v1.4 §18.6，异步）。
func (a *App) Merge(req service.MergeRequest) (string, error) {
	return a.svc.Merge(req)
}

// GetTask 查询任务状态/进度/结果（返回快照，避免与执行侧写入竞态）。
func (a *App) GetTask(id string) (*service.Task, error) {
	t, ok := a.svc.GetTaskSnapshot(id)
	if !ok {
		return nil, fmt.Errorf("任务不存在: %s", id)
	}
	return &t, nil
}

// ListTasks 返回历史任务列表。
func (a *App) ListTasks() []service.Task {
	return a.svc.ListTasks()
}

// Cancel 取消进行中的任务。
func (a *App) Cancel(id string) error {
	return a.svc.Cancel(id)
}

// RevealInFolder 在资源管理器中定位输出文件（Windows：explorer /select）。
func (a *App) RevealInFolder(path string) error {
	if path == "" {
		return fmt.Errorf("路径为空")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return exec.Command("explorer.exe", "/select,"+abs).Start()
}

// Probe 获取元数据（前端选文件后预校验区间）。
func (a *App) Probe(path string) (*ffmpeg.ProbeResult, error) {
	return a.svc.Probe(a.ctx, path)
}

// SelectFile 打开系统文件对话框，返回所选绝对路径。
func (a *App) SelectFile() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择音视频文件",
		Filters: []runtime.FileFilter{
			{DisplayName: "音视频", Pattern: "*.mp4;*.mkv;*.mov;*.avi;*.webm;*.mp3;*.aac;*.m4a;*.flac;*.wav"},
		},
	})
}

// SelectOutputDir 打开目录选择对话框，返回输出目录。
func (a *App) SelectOutputDir() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择输出目录",
	})
}

// ConfigGet 返回当前运行期配置快照。
func (a *App) ConfigGet() map[string]interface{} {
	return map[string]interface{}{
		"ffmpeg_mode":         a.cfg.FFmpegMode,
		"concurrency":         a.cfg.Concurrency,
		"default_strategy":    a.cfg.DefaultStrategy,
		"timeout_seconds":     a.cfg.TimeoutSeconds,
		"output_suffix":       a.cfg.OutputSuffix,
		"merge_default_mode":  a.cfg.MergeDefaultMode,
		"batch_max_items":     a.cfg.BatchMaxItems,
		"subtitles_mode":      a.cfg.SubMode,
		"subtitles_format":    a.cfg.SubFormat,
		"subtitles_style":     a.cfg.SubStylePreset,
		"subtitles_max_chars": a.cfg.SubMaxCharsPerCue,
		"subtitles_max_lines": a.cfg.SubMaxLines,
	}
}

// ConfigSet 运行期覆盖部分配置（best-effort；concurrency 变更下次启动生效）。
func (a *App) ConfigSet(key, value string) error {
	switch key {
	case "default_strategy":
		a.cfg.DefaultStrategy = value
	case "output_suffix":
		a.cfg.OutputSuffix = value
	case "merge_default_mode":
		switch value {
		case "auto", "demuxer", "filter":
			a.cfg.MergeDefaultMode = value
		default:
			return fmt.Errorf("未知合并模式: %s（可选 auto/demuxer/filter）", value)
		}
	case "batch_max_items":
		var v int
		if _, err := fmt.Sscanf(value, "%d", &v); err != nil {
			return err
		}
		if v <= 0 {
			return fmt.Errorf("batch_max_items 必须为正数")
		}
		a.cfg.BatchMaxItems = v
	case "timeout_seconds":
		var v int
		if _, err := fmt.Sscanf(value, "%d", &v); err != nil {
			return err
		}
		a.cfg.TimeoutSeconds = v
	case "concurrency":
		var v int
		if _, err := fmt.Sscanf(value, "%d", &v); err != nil {
			return err
		}
		a.cfg.Concurrency = v
	default:
		return fmt.Errorf("未知配置项: %s", key)
	}
	return nil
}
