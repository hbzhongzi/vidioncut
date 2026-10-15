package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	gruntime "runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sync/semaphore"

	"videocut/internal/config"
	"videocut/internal/ffmpeg"
)

// errCanceled 表示任务已被用户取消（v1.4）：由 Cancel 统一置态与报错，
// 执行侧收到此哨兵仅收尾，不再二次 emit。
var errCanceled = fmt.Errorf("task canceled")

// TrimRequest 裁剪请求（与前端 JSON 契约一致，开发文档 v1.1 §8.1）。
type TrimRequest struct {
	InputPath  string  `json:"input_path"`
	Start      float64 `json:"start"`
	End        float64 `json:"end"`
	Strategy   string  `json:"strategy"`
	OutputPath string  `json:"output_path"`
	Overwrite  bool    `json:"overwrite"`
}

// Service 裁剪业务编排（开发文档 v1.1 §4 / §5，v1.4 §18 扩展批量/合并）。
type Service struct {
	cfg         *config.Config
	ffmpegPath  string
	ffprobePath string
	appCtx      context.Context

	sem     *semaphore.Weighted
	mu      sync.RWMutex
	tasks   map[string]*Task
	cancels map[string]context.CancelFunc

	allowedPreview sync.Map // 允许预览的绝对路径白名单（安全边界 §10）
	idCounter      atomic.Int64

	// emitFn 事件发送函数（默认 runtime.EventsEmit）；测试可注入捕获。
	emitFn func(name string, data interface{})
}

// NewService 构造裁剪服务。concurrency<=0 时按 CPU 核心数限制。
func NewService(cfg *config.Config, ffmpegPath, ffprobePath string) *Service {
	conc := cfg.Concurrency
	if conc <= 0 {
		conc = gruntime.NumCPU()
	}
	return &Service{
		cfg:         cfg,
		ffmpegPath:  ffmpegPath,
		ffprobePath: ffprobePath,
		sem:         semaphore.NewWeighted(int64(conc)),
		tasks:       make(map[string]*Task),
		cancels:     make(map[string]context.CancelFunc),
	}
}

// SetAppContext 注入 Wails 应用上下文，用于向后端推送事件。
func (s *Service) SetAppContext(ctx context.Context) {
	s.appCtx = ctx
}

// RegisterPreviewPath 将路径加入预览白名单（仅用户经对话框选择/处理的文件可预览）。
func (s *Service) RegisterPreviewPath(p string) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return
	}
	s.allowedPreview.Store(abs, true)
}

// IsPreviewAllowed 校验预览路径是否在白名单内。
func (s *Service) IsPreviewAllowed(p string) bool {
	abs, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	_, ok := s.allowedPreview.Load(abs)
	return ok
}

// checkFFmpeg 在真正调用前确认二进制已就绪（开发文档 MVP：FFmpeg 随程序分发）。
func (s *Service) checkFFmpeg() error {
	if s.ffmpegPath == "" || s.ffprobePath == "" {
		return fmt.Errorf("FFmpeg 未配置：请将 ffmpeg.exe / ffprobe.exe 放入 resources/ffmpeg/ 目录后重启")
	}
	return nil
}

// Probe 获取元数据，并把路径登记到预览白名单。
func (s *Service) Probe(ctx context.Context, path string) (*ffmpeg.ProbeResult, error) {
	if err := s.checkFFmpeg(); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	res, err := ffmpeg.Probe(ctx, s.ffprobePath, path)
	if err == nil {
		s.RegisterPreviewPath(path)
	}
	return res, err
}

// GetTask 查询任务（返回内部指针，仅供包内已持锁的短临界区使用；
// 跨 goroutine 读取请用 GetTaskSnapshot，避免与执行侧写入竞态）。
func (s *Service) GetTask(id string) (*Task, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	return t, ok
}

// GetTaskSnapshot 返回任务状态快照（值拷贝 + Items 深拷贝）。
// Wails 绑定序列化与测试轮询均走此方法，避免读取与执行侧写入之间的数据竞态。
func (s *Service) GetTaskSnapshot(id string) (Task, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	if !ok {
		return Task{}, false
	}
	snap := *t
	snap.Items = append([]TrimItem(nil), t.Items...)
	return snap, true
}

// ListTasks 返回全部任务（副本）。
func (s *Service) ListTasks() []Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		out = append(out, *t)
	}
	return out
}

// Cancel 取消进行中/排队中的任务（v1.4：批量取消所有未完成项，已成功项产物保留）。
func (s *Service) Cancel(id string) error {
	s.mu.Lock()
	cancel, ok := s.cancels[id]
	t, tok := s.tasks[id]
	// pending（排队等信号量）与 processing 都可取消；标记后由执行侧收尾时据此短路，避免二次报错。
	if tok && (t.Status == StatusProcessing || t.Status == StatusPending) {
		t.Status = StatusFailed
		t.Canceled = true
		t.Error = "已取消"
		// 批量/合并：未完成子项标记 canceled
		for i := range t.Items {
			if t.Items[i].Status == ItemStatusPending || t.Items[i].Status == ItemStatusProcessing {
				t.Items[i].Status = ItemStatusCanceled
			}
		}
	}
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("task not cancelable: %s", id)
	}
	cancel()
	// 仅在此处发送一次“已取消”，执行侧检测到 Canceled 后直接返回、不再 emit。
	s.emit("trim:error", map[string]interface{}{"task_id": id, "kind": t.kind(), "message": "已取消"})
	return nil
}

// isCanceled 判断任务是否已被用户取消（读 Canceled 标志）。
func (s *Service) isCanceled(task *Task) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return task.Canceled
}

// kind 读取任务类型（未设置时按 trim 兼容）。
func (t *Task) kind() string {
	if t.Kind == "" {
		return KindTrim
	}
	return t.Kind
}

// newTaskID 生成任务 ID。
func (s *Service) newTaskID(prefix string) string {
	return fmt.Sprintf("%s_%d_%d", prefix, time.Now().UnixNano(), s.idCounter.Add(1))
}

// Trim 提交一个裁剪任务（异步，立即返回 task_id）。
func (s *Service) Trim(req TrimRequest) (string, error) {
	if err := s.checkFFmpeg(); err != nil {
		return "", err
	}

	// 1. 廉价校验：空输入 / 文件存在（先于探测，避免对缺失文件启动 ffprobe）
	if req.InputPath == "" {
		return "", fmt.Errorf("input_path 不能为空")
	}
	if !fileExists(req.InputPath) {
		return "", fmt.Errorf("输入文件不存在: %s", req.InputPath)
	}

	// 2. 探测元数据（顺便登记预览白名单）
	probe, err := s.Probe(context.Background(), req.InputPath)
	if err != nil {
		return "", fmt.Errorf("探测失败: %w", err)
	}

	// 3. 校验区间与策略
	if err := validateRequest(req, probe.Duration); err != nil {
		return "", err
	}
	strategy, err := normalizeStrategy(req.Strategy, s.cfg.DefaultStrategy)
	if err != nil {
		return "", err
	}

	// 4. 输出路径
	out := req.OutputPath
	if out == "" {
		out = deriveOutput(req.InputPath, s.cfg.OutputSuffix)
	}
	if !req.Overwrite {
		out = appendSeq(out)
	}

	// 5. 建任务（v1.4：trim 亦携带 1 个 Items 项，前端展示统一）
	taskID := s.newTaskID("trim")
	task := &Task{
		TaskID:     taskID,
		Kind:       KindTrim,
		InputPath:  req.InputPath,
		OutputPath: out,
		Start:      req.Start,
		End:        req.End,
		Strategy:   string(strategy),
		Status:     StatusPending,
		CreatedAt:  time.Now(),
		Items: []TrimItem{{
			InputPath:  req.InputPath,
			OutputPath: out,
			Start:      req.Start,
			End:        req.End,
			Strategy:   string(strategy),
			Status:     ItemStatusPending,
			HasRange:   true,
		}},
	}
	s.mu.Lock()
	s.tasks[taskID] = task
	s.mu.Unlock()

	// 6. 异步执行
	go s.runSingle(task, strategy, probe.Duration)

	return taskID, nil
}

// runSingle 单文件裁剪执行（v1.4：基于 runItem 重构，外部行为与 v1.3 一致）。
func (s *Service) runSingle(task *Task, strategy ffmpeg.Strategy, totalDuration float64) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.cfg.TimeoutSeconds)*time.Second)
	s.mu.Lock()
	s.cancels[task.TaskID] = cancel
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.cancels, task.TaskID)
		s.mu.Unlock()
		cancel()
	}()

	s.mu.Lock()
	task.Status = StatusProcessing
	s.mu.Unlock()
	s.emit("trim:progress", map[string]interface{}{"task_id": task.TaskID, "kind": KindTrim, "percent": 0})

	item := &task.Items[0]
	err := s.runItem(ctx, task, item, totalDuration, func(p ffmpeg.Progress) {
		s.mu.Lock()
		task.Progress = p.Percent
		s.mu.Unlock()
		s.emit("trim:progress", map[string]interface{}{
			"task_id":  task.TaskID,
			"kind":     KindTrim,
			"percent":  p.Percent,
			"speed":    p.Speed,
			"out_time": p.OutTime,
		})
	})
	if err != nil {
		// 取消优先：状态/文案已由 Cancel 处理，直接收尾。
		if err == errCanceled {
			return
		}
		if ctx.Err() == context.DeadlineExceeded {
			s.fail(task, "处理超时，请重试或换重编码模式")
			return
		}
		s.fail(task, friendlyError(err))
		return
	}

	// 取消可能发生在产物校验前后：成功后又被取消，则不报完成。
	if s.isCanceled(task) {
		return
	}

	s.mu.Lock()
	task.Status = StatusCompleted
	task.Progress = 100
	task.Duration = task.End - task.Start
	s.mu.Unlock()
	s.emit("trim:done", map[string]interface{}{
		"task_id":     task.TaskID,
		"kind":        KindTrim,
		"output_path": task.OutputPath,
		"duration":    task.Duration,
	})
}

// runItem 执行单个裁剪单元（v1.4 §18.3：单文件/批量/合并 hybrid 共用）。
// 职责：信号量获取 → 同卷临时目录 → .part 写入 → rename 原子落地；
// 子项状态（processing/progress）在此维护，成败事件由调用方按任务类型分发。
// onProgress 每次进度更新回调（含完成态快照）。返回 errCanceled 表示任务已被取消。
func (s *Service) runItem(ctx context.Context, task *Task, item *TrimItem, totalDuration float64, onProgress func(ffmpeg.Progress)) error {
	// 并发信号量（与单文件任务、其他批量项共享同一全局信号量，不突破 concurrency 上限）
	if err := s.sem.Acquire(ctx, 1); err != nil {
		// 已被取消：Cancel 已置状态并发过“已取消”，此处直接收尾，避免二次报错。
		if s.isCanceled(task) {
			return errCanceled
		}
		return fmt.Errorf("并发获取超时（请稍后重试）")
	}
	defer s.sem.Release(1)

	// 置 processing
	s.mu.Lock()
	item.Status = ItemStatusProcessing
	s.mu.Unlock()

	// 输出先写同卷 .part，再 rename 原子落地（与输出同卷，天然规避跨盘失败，
	// 开发文档 v1.1 §4.4）。注意：不得在 MkdirTemp 失败时回退 os.TempDir()
	// 并 RemoveAll——那会递归删掉整个系统临时目录（v1.4 集成测试实测教训）。
	partPath := ffmpeg.PartPath(item.OutputPath)
	strategy := ffmpeg.Strategy(item.Strategy)
	var args []string
	if item.Subtitle != nil {
		// 字幕烧录（M12 §4.4）：ass 滤镜 + 音轨直复制；Strategy 固定 reencode 语义
		args = ffmpeg.BuildBurnArgs(item.InputPath, item.Subtitle.AssPath, item.OutputPath,
			item.Subtitle.Encoder, item.Subtitle.CRF, item.Subtitle.Preset)
	} else {
		args = ffmpeg.BuildTrimArgs(strategy, item.InputPath, item.OutputPath, item.Start, item.End)
	}

	// 单次执行封装：烧录硬件编码器失败时可复用同一进度/取消语义重试。
	runArgs := func(runArgs []string) error {
		progressCh := make(chan ffmpeg.Progress, 16)
		errCh := make(chan error, 1)
		go func() { errCh <- ffmpeg.Run(ctx, s.ffmpegPath, runArgs, totalDuration, progressCh) }()

		for p := range progressCh {
			s.mu.Lock()
			item.Progress = p.Percent
			s.mu.Unlock()
			if onProgress != nil {
				onProgress(p)
			}
		}
		return <-errCh
	}

	runErr := runArgs(args)
	if runErr != nil && item.Subtitle != nil && item.Subtitle.Encoder != "" && item.Subtitle.Encoder != "libx264" && ctx.Err() == nil {
		// 硬件编码器因机器能力/驱动不可用时，清理半成品并回退 CPU 编码。
		_ = os.Remove(partPath)
		args = ffmpeg.BuildBurnArgs(item.InputPath, item.Subtitle.AssPath, item.OutputPath,
			"libx264", item.Subtitle.CRF, item.Subtitle.Preset)
		runErr = runArgs(args)
	}
	if runErr != nil {
		// 取消优先：进程被 Kill 走此分支，但状态/文案已由 Cancel 处理，直接收尾。
		if s.isCanceled(task) {
			return errCanceled
		}
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("处理超时，请重试或换重编码模式")
		}
		return runErr
	}

	// 取消可能发生在产物校验前后：成功后又被取消，则不落地、不报完成。
	if s.isCanceled(task) {
		return errCanceled
	}

	// 校验产物
	if !fileExists(partPath) {
		return fmt.Errorf("未生成输出文件")
	}

	// 同卷 rename 到最终路径
	if err := os.Rename(partPath, item.OutputPath); err != nil {
		// 回退：复制（极少数跨卷场景）
		if copyErr := copyFile(partPath, item.OutputPath); copyErr != nil {
			return fmt.Errorf("写入输出失败: %w", err)
		}
		_ = os.Remove(partPath)
	}
	return nil
}

// normalizeStrategy 归一化策略：空值取默认值，并校验合法性。
func normalizeStrategy(req, fallback string) (ffmpeg.Strategy, error) {
	strategy := ffmpeg.Strategy(req)
	if strategy == "" {
		strategy = ffmpeg.Strategy(fallback)
	}
	switch strategy {
	case ffmpeg.StrategyStreamCopy, ffmpeg.StrategyReencode, ffmpeg.StrategyAudioOnly:
		return strategy, nil
	default:
		return "", fmt.Errorf("未知策略: %s", req)
	}
}

// validateRequest 校验裁剪请求合法性（纯函数，不依赖 FFmpeg 二进制）。
// 顺序：空输入 → 文件存在 → start 非负 → end>start → end 不超源时长。
func validateRequest(req TrimRequest, duration float64) error {
	if req.InputPath == "" {
		return fmt.Errorf("input_path 不能为空")
	}
	if !fileExists(req.InputPath) {
		return fmt.Errorf("输入文件不存在: %s", req.InputPath)
	}
	if req.Start < 0 {
		return fmt.Errorf("start 必须为非负数")
	}
	if req.End <= req.Start {
		return fmt.Errorf("end 必须大于 start")
	}
	if req.End > duration+0.05 {
		return fmt.Errorf("end(%.2f) 超过源时长(%.2f)", req.End, duration)
	}
	return nil
}

func (s *Service) fail(task *Task, msg string) {
	s.mu.Lock()
	task.Status = StatusFailed
	task.Error = msg
	s.mu.Unlock()
	s.emit("trim:error", map[string]interface{}{"task_id": task.TaskID, "kind": task.kind(), "message": msg})
}

func (s *Service) emit(name string, data interface{}) {
	fn := s.emitFn
	if fn == nil {
		if s.appCtx == nil {
			return
		}
		fn = func(n string, d interface{}) { runtime.EventsEmit(s.appCtx, n, d) }
	}
	fn(name, data)
}

// friendlyError 将 FFmpeg 错误转为对用户友好的中文文案（不暴露原始英文 stderr）。
func friendlyError(err error) string {
	msg := err.Error()
	switch {
	case contains(msg, "timeout"):
		return "处理超时，请重试或换重编码模式"
	case contains(msg, "No such file"):
		return "找不到输入文件"
	case contains(msg, "Permission denied"):
		return "无权限写入输出目录"
	default:
		return "裁剪失败，请检查输入文件或重试"
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// copyFile 回退方案：跨卷时 rename 不可用时，整体复制后由调用方删除源。
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
