package service

import (
	"context"
	"fmt"
	"time"

	"videocut/internal/ffmpeg"
)

// BatchItem 批量裁剪子项请求（开发文档 v1.4 §18.6）。
type BatchItem struct {
	InputPath  string  `json:"input_path"`
	Start      float64 `json:"start"`
	End        float64 `json:"end"`
	Strategy   string  `json:"strategy"`
	OutputPath string  `json:"output_path"` // 可空，缺省按单文件规则派生

	// Subtitle 非空时本项走字幕烧录（M12 §4.4）：ass 滤镜重编码 + 音轨直复制，
	// 忽略 Strategy（内部固定 reencode 语义）。复用批量任务基础设施
	// （信号量/进度/取消/事件/原子落地），调用方以单项目提交保持 task_id 契约。
	Subtitle *SubtitleBurn `json:"subtitle,omitempty"`
}

// SubtitleBurn 字幕烧录参数（M12）。AssPath 为绝对路径（烧录时转义，§4.4 要点 1）。
type SubtitleBurn struct {
	AssPath string `json:"ass_path"`
	Encoder string `json:"encoder"` // 空=libx264（硬件编码失败由调用方回退）
	CRF     int    `json:"crf"`     // 0=18
	Preset  string `json:"preset"`  // 空=fast
}

// BatchRequest 批量裁剪请求（开发文档 v1.4 §18.6）。
type BatchRequest struct {
	Items []BatchItem `json:"items"`
}

// TrimBatch 提交批量裁剪任务（v1.4 §18.3）。
// 原子提交：预校验阶段逐项 probe（存在性/区间/策略），任一项不过则整批拒绝、不建 Task，
// 避免"启动一半后失败"的难解释状态。返回 Kind=batch 的 TaskID。
func (s *Service) TrimBatch(req BatchRequest) (string, error) {
	if err := s.checkFFmpeg(); err != nil {
		return "", err
	}
	if len(req.Items) == 0 {
		return "", fmt.Errorf("批量任务至少需要 1 项")
	}
	if s.cfg.BatchMaxItems > 0 && len(req.Items) > s.cfg.BatchMaxItems {
		return "", fmt.Errorf("单次批量最多 %d 项（当前 %d），可分批提交", s.cfg.BatchMaxItems, len(req.Items))
	}

	// 原子预校验：全部通过才建任务
	items := make([]TrimItem, 0, len(req.Items))
	durations := make([]float64, 0, len(req.Items))
	taken := map[string]bool{} // 同批输出防重（多个输出在提交时均未落盘，仅靠存在性无法防重）
	for i, bi := range req.Items {
		if bi.InputPath == "" {
			return "", fmt.Errorf("第 %d 项：input_path 不能为空", i+1)
		}
		if !fileExists(bi.InputPath) {
			return "", fmt.Errorf("第 %d 项：输入文件不存在: %s", i+1, bi.InputPath)
		}
		probe, err := s.Probe(context.Background(), bi.InputPath)
		if err != nil {
			return "", fmt.Errorf("第 %d 项：探测失败: %w", i+1, err)
		}
		if bi.Subtitle != nil {
			// 字幕烧录项：ASS 必须存在；区间照常校验（典型 0→duration 整片烧录）
			if bi.Subtitle.AssPath == "" {
				return "", fmt.Errorf("第 %d 项：字幕烧录缺少 ass_path", i+1)
			}
			if !fileExists(bi.Subtitle.AssPath) {
				return "", fmt.Errorf("第 %d 项：ASS 字幕文件不存在: %s", i+1, bi.Subtitle.AssPath)
			}
			if err := validateRequest(TrimRequest{InputPath: bi.InputPath, Start: bi.Start, End: bi.End}, probe.Duration); err != nil {
				return "", fmt.Errorf("第 %d 项：%v", i+1, err)
			}
		} else if err := validateRequest(TrimRequest{InputPath: bi.InputPath, Start: bi.Start, End: bi.End}, probe.Duration); err != nil {
			return "", fmt.Errorf("第 %d 项：%v", i+1, err)
		}
		strategy, err := normalizeStrategy(bi.Strategy, s.cfg.DefaultStrategy)
		if err != nil {
			return "", fmt.Errorf("第 %d 项：%v", i+1, err)
		}
		if bi.Subtitle != nil {
			strategy = ffmpeg.StrategyReencode // 烧录必重编码（ass 滤镜在视频链上）
		}
		out := bi.OutputPath
		if out == "" {
			out = deriveOutput(bi.InputPath, s.cfg.OutputSuffix)
		}
		out = appendSeqAvoiding(out, taken)
		taken[out] = true

		items = append(items, TrimItem{
			InputPath:  bi.InputPath,
			OutputPath: out,
			Start:      bi.Start,
			End:        bi.End,
			Strategy:   string(strategy),
			Status:     ItemStatusPending,
			HasRange:   true,
			Subtitle:   bi.Subtitle,
		})
		durations = append(durations, probe.Duration)
	}

	taskID := s.newTaskID("batch")
	task := &Task{
		TaskID:    taskID,
		Kind:      KindBatch,
		Status:    StatusPending,
		CreatedAt: time.Now(),
		Items:     items,
	}
	s.mu.Lock()
	s.tasks[taskID] = task
	s.mu.Unlock()

	go s.runBatch(task, durations)

	return taskID, nil
}

// runBatch 批量执行（v1.4 §18.3）：逐项复用 runItem 管线，共享全局信号量，
// 时长加权聚合进度；终态 completed/partial/failed，取消同单文件语义。
func (s *Service) runBatch(task *Task, durations []float64) {
	// 批级取消上下文：Cancel 触发时 Kill 当前正在执行的 FFmpeg 进程
	ctx, cancel := context.WithCancel(context.Background())
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
	s.emit("trim:progress", map[string]interface{}{"task_id": task.TaskID, "kind": KindBatch, "percent": 0})

	for i := range task.Items {
		// 取消检查：Cancel 已把未完成项标记为 canceled，此处直接收尾
		if s.isCanceled(task) {
			return
		}
		item := &task.Items[i]

		// 每项独立超时：长批次不被单一总超时一刀切（单项沿用超时 Kill）
		itemCtx, itemCancel := context.WithTimeout(ctx, time.Duration(s.cfg.TimeoutSeconds)*time.Second)
		err := s.runItem(itemCtx, task, item, durations[i], func(p ffmpeg.Progress) {
			s.mu.Lock()
			agg := aggregateProgress(task.Items, durations)
			task.Progress = agg
			s.mu.Unlock()
			s.emit("trim:progress", map[string]interface{}{
				"task_id":      task.TaskID,
				"kind":         KindBatch,
				"item_index":   i,
				"percent":      agg,
				"item_percent": p.Percent,
				"speed":        p.Speed,
			})
		})
		timeout := itemCtx.Err() == context.DeadlineExceeded
		itemCancel()

		if err == errCanceled {
			return
		}
		if err != nil {
			if timeout {
				err = fmt.Errorf("处理超时，请重试或换重编码模式")
			}
			msg := friendlyError(err)
			s.mu.Lock()
			item.Status = ItemStatusFailed
			item.Error = msg
			task.FailedCount++
			s.mu.Unlock()
			// 单项失败不影响其他项（带 item_index，前端可局部提示）
			s.emit("trim:error", map[string]interface{}{
				"task_id":    task.TaskID,
				"kind":       KindBatch,
				"item_index": i,
				"message":    msg,
			})
			continue
		}

		s.mu.Lock()
		item.Status = ItemStatusCompleted
		item.Progress = 100
		task.CompletedCount++
		s.mu.Unlock()
		s.emit("trim:item_done", map[string]interface{}{
			"task_id":     task.TaskID,
			"item_index":  i,
			"output_path": item.OutputPath,
			"duration":    item.End - item.Start,
		})
	}

	// 终态汇总
	s.mu.Lock()
	total := len(task.Items)
	done := task.CompletedCount
	failed := task.FailedCount
	var sumDur float64
	for i := range task.Items {
		if task.Items[i].Status == ItemStatusCompleted {
			sumDur += task.Items[i].End - task.Items[i].Start
		}
	}
	task.Duration = sumDur
	task.Progress = 100
	switch {
	case failed == 0 && done == total:
		task.Status = StatusCompleted
	case done > 0:
		task.Status = StatusPartial
	default:
		task.Status = StatusFailed
	}
	s.mu.Unlock()

	if task.Status == StatusFailed {
		s.emit("trim:error", map[string]interface{}{
			"task_id": task.TaskID,
			"kind":    KindBatch,
			"message": "批量裁剪全部失败",
		})
		return
	}
	s.emit("trim:done", map[string]interface{}{
		"task_id":     task.TaskID,
		"kind":        KindBatch,
		"completed":   done,
		"failed":      failed,
		"duration":    task.Duration,
		"output_path": "",
	})
}

// aggregateProgress 批量聚合进度（v1.4 §18.3，纯函数）：
//
//	percent = (Σ已完成(含失败)项时长 + 当前项进度×当前项时长) / Σ全部项时长
//
// 时长未知（总和≤0）时退化为计数法 finished/total×100。
// 调用方需持有 s.mu（读写 task.Items）。
func aggregateProgress(items []TrimItem, totalDurations []float64) float64 {
	n := len(items)
	if n == 0 {
		return 0
	}
	dur := func(i int) float64 {
		if i < len(totalDurations) {
			return totalDurations[i]
		}
		return 0
	}
	var sum float64
	for i := 0; i < n; i++ {
		sum += dur(i)
	}
	if sum <= 0 {
		finished := 0
		for _, it := range items {
			if it.Status == ItemStatusCompleted || it.Status == ItemStatusFailed {
				finished++
			}
		}
		return float64(finished) / float64(n) * 100
	}
	var acc float64
	for i := 0; i < n; i++ {
		switch items[i].Status {
		case ItemStatusCompleted, ItemStatusFailed:
			acc += dur(i)
		case ItemStatusProcessing:
			acc += dur(i) * items[i].Progress / 100
		}
	}
	pct := acc / sum * 100
	if pct > 100 {
		pct = 100
	}
	return pct
}
