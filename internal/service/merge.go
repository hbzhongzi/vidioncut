package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"videocut/internal/ffmpeg"
)

// MergeItem 合并片段（开发文档 v1.4 §18.4；有序，顺序即输出顺序）。
type MergeItem struct {
	InputPath string  `json:"input_path"`
	Start     float64 `json:"start"`
	End       float64 `json:"end"`
	HasRange  bool    `json:"has_range"` // 是否带区间（false=整段）
	Strategy  string  `json:"strategy"`  // hybrid 裁剪策略，缺省取默认策略
}

// MergeRequest 合并请求（开发文档 v1.4 §18.4）。
type MergeRequest struct {
	Items      []MergeItem `json:"items"`
	Mode       string      `json:"mode"` // auto | demuxer | filter，缺省取 merge.default_mode
	OutputPath string      `json:"output_path"`
	Overwrite  bool        `json:"overwrite"`
}

// Merge 提交合并任务（异步，立即返回 task_id）。
// 校验：≥2 项、输入存在、区间合法（带区间时）、输出父目录可写；
// 模式归一与 auto 判定（detectMergeMode）；输出缺省派生自首项。
func (s *Service) Merge(req MergeRequest) (string, error) {
	if err := s.checkFFmpeg(); err != nil {
		return "", err
	}
	if len(req.Items) < 2 {
		return "", fmt.Errorf("合并至少需要 2 个片段")
	}

	mode := req.Mode
	if mode == "" {
		mode = s.cfg.MergeDefaultMode
	}
	if mode == "" {
		mode = MergeModeAuto
	}
	switch mode {
	case MergeModeAuto, MergeModeDemuxer, MergeModeFilter:
	default:
		return "", fmt.Errorf("未知合并模式: %s（可选 auto/demuxer/filter）", req.Mode)
	}

	// 逐项校验 + 探测
	probes := make([]*ffmpeg.ProbeResult, len(req.Items))
	for i, it := range req.Items {
		if it.InputPath == "" {
			return "", fmt.Errorf("第 %d 项：input_path 不能为空", i+1)
		}
		if !fileExists(it.InputPath) {
			return "", fmt.Errorf("第 %d 项：输入文件不存在: %s", i+1, it.InputPath)
		}
		probe, err := s.Probe(context.Background(), it.InputPath)
		if err != nil {
			return "", fmt.Errorf("第 %d 项：探测失败: %w", i+1, err)
		}
		if it.HasRange {
			if it.Start < 0 {
				return "", fmt.Errorf("第 %d 项：开始时间必须为非负数", i+1)
			}
			if it.End <= it.Start {
				return "", fmt.Errorf("第 %d 项：结束时间必须大于开始时间", i+1)
			}
			if it.End > probe.Duration+0.05 {
				return "", fmt.Errorf("第 %d 项：结束时间(%.2f) 超过源时长(%.2f)", i+1, it.End, probe.Duration)
			}
		}
		if it.HasRange {
			if _, err := normalizeStrategy(it.Strategy, s.cfg.DefaultStrategy); err != nil {
				return "", fmt.Errorf("第 %d 项：%v", i+1, err)
			}
		}
		probes[i] = probe
	}

	// 输出路径：缺省派生自首项；父目录必须可写
	out := req.OutputPath
	if out == "" {
		out = deriveOutput(req.Items[0].InputPath, "_merged")
	}
	if err := ensureWritableDir(filepath.Dir(out)); err != nil {
		return "", err
	}
	if !req.Overwrite {
		out = appendSeq(out)
	}

	// auto 判定（显式指定时不判定）
	resolved := mode
	if mode == MergeModeAuto {
		resolved = ffmpeg.DetectMergeMode(probes)
	}

	// 建 Task
	items := make([]TrimItem, len(req.Items))
	segDurations := make([]float64, len(req.Items))
	var totalDur float64
	for i, it := range req.Items {
		strategy := it.Strategy
		if strategy == "" {
			strategy = s.cfg.DefaultStrategy
		}
		items[i] = TrimItem{
			InputPath:  it.InputPath,
			OutputPath: out, // 合并无单项输出，占位为最终输出
			Start:      it.Start,
			End:        it.End,
			Strategy:   strategy,
			Status:     ItemStatusPending,
			HasRange:   it.HasRange,
		}
		if it.HasRange {
			segDurations[i] = it.End - it.Start
		} else {
			segDurations[i] = probes[i].Duration
		}
		totalDur += segDurations[i]
	}

	taskID := s.newTaskID("merge")
	task := &Task{
		TaskID:    taskID,
		Kind:      KindMerge,
		OutputPath: out,
		MergeMode: resolved,
		Status:    StatusPending,
		CreatedAt: time.Now(),
		Items:     items,
	}
	s.mu.Lock()
	s.tasks[taskID] = task
	s.mu.Unlock()

	go s.runMerge(task, req.Items, probes, mode, resolved, segDurations, totalDur)

	return taskID, nil
}

// runMerge 合并执行（v1.4 §18.4）：
//  1. hybrid（带区间）或非 ASCII 路径 → 临时目录 ASCII 规范化（seg_000.<ext>）
//  2. 按 resolved 模式执行 concat（demuxer 流复制 / filter 重编码）
//  3. auto 模式下 demuxer 失败 → 回退 filter 重试一次，并发 merge:notice
func (s *Service) runMerge(task *Task, reqItems []MergeItem, probes []*ffmpeg.ProbeResult, requestedMode, mode string, segDurations []float64, totalDuration float64) {
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
	s.emit("trim:progress", map[string]interface{}{"task_id": task.TaskID, "kind": KindMerge, "percent": 0})

	// 是否需要临时目录规范化：hybrid（带区间）或存在非 ASCII 路径
	// （一举两得：分割+合并一条龙；规避 concat demuxer 的中文路径兼容风险）
	hybrid := false
	nonASCII := false
	for _, it := range reqItems {
		if it.HasRange {
			hybrid = true
		}
		if !isASCIIPath(it.InputPath) {
			nonASCII = true
		}
	}
	needNormalize := hybrid || nonASCII

	var tempDir string
	if needNormalize {
		// 临时目录置于首个输入所在卷（硬链接规范化时免跨卷复制）。
		// 失败时绝不回退 os.TempDir()——defer 的 RemoveAll 会递归删掉整个系统临时目录
		// （v1.4 集成测试实测教训）；直接以明确错误收尾。
		td, err := os.MkdirTemp(filepath.Dir(reqItems[0].InputPath), "merge_"+task.TaskID)
		if err != nil {
			s.fail(task, fmt.Sprintf("无法创建合并临时目录: %v", err))
			return
		}
		tempDir = td
		defer func() { _ = os.RemoveAll(tempDir) }()
	}

	// 1. 生成合并输入（规范化或原路径）
	inputs := make([]string, len(reqItems))
	if needNormalize {
		// 裁剪阶段权重：demuxer 的 concat 近乎瞬时，裁剪占大头；filter 的 concat 为重编码，占大头
		cutWeight := 0.9
		if mode == MergeModeFilter {
			cutWeight = 0.1
		}
		var cutTotal float64
		for _, it := range reqItems {
			if it.HasRange {
				cutTotal += it.End - it.Start
			}
		}
		if cutTotal <= 0 {
			cutTotal = 1
		}
		var cutDone float64

		for i, it := range reqItems {
			if s.isCanceled(task) {
				return
			}
			ext := filepath.Ext(it.InputPath)
			if ext == "" || !isASCIIPath(ext) {
				ext = ".mp4"
			}
			segPath := filepath.Join(tempDir, fmt.Sprintf("seg_%03d%s", i, ext))

			if !it.HasRange {
				// 整段输入：硬链接优先（同卷零成本），失败回退复制
				if err := linkOrCopy(it.InputPath, segPath); err != nil {
					s.fail(task, fmt.Sprintf("第 %d 项：暂存失败: %v", i+1, err))
					return
				}
				s.mu.Lock()
				task.Items[i].Status = ItemStatusCompleted
				s.mu.Unlock()
				inputs[i] = segPath
				continue
			}

			// 带区间输入：裁剪到临时目录（复用 runItem 管线：信号量/同卷临时/.part+rename/超时 Kill）
			segItem := TrimItem{
				InputPath:  it.InputPath,
				OutputPath: segPath,
				Start:      it.Start,
				End:        it.End,
				Strategy:   task.Items[i].Strategy,
				Status:     ItemStatusPending,
				HasRange:   true,
			}
			dur := it.End - it.Start
			err := s.runItem(ctx, task, &segItem, probes[i].Duration, func(p ffmpeg.Progress) {
				frac := (cutDone + p.Percent/100*dur) / cutTotal
				overall := frac * cutWeight * 100
				s.mu.Lock()
				task.Progress = overall
				s.mu.Unlock()
				s.emit("trim:progress", map[string]interface{}{
					"task_id":    task.TaskID,
					"kind":       KindMerge,
					"item_index": i,
					"percent":    overall,
					"speed":      p.Speed,
				})
			})
			if err == errCanceled {
				return
			}
			if err != nil {
				msg := fmt.Sprintf("第 %d 项：裁剪失败: %v", i+1, err)
				if ctx.Err() == context.DeadlineExceeded {
					msg = fmt.Sprintf("第 %d 项：裁剪超时，请重试或换重编码模式", i+1)
				}
				s.mu.Lock()
				task.Items[i].Status = ItemStatusFailed
				task.Items[i].Error = msg
				s.mu.Unlock()
				s.fail(task, msg)
				return
			}
			cutDone += dur
			s.mu.Lock()
			task.Items[i].Status = ItemStatusCompleted
			s.mu.Unlock()
			inputs[i] = segPath
		}
	} else {
		for i, it := range reqItems {
			inputs[i] = it.InputPath
		}
	}

	if s.isCanceled(task) {
		return
	}

	// 2. 执行合并（demuxer / filter；auto 回退）
	partPath := ffmpeg.PartPath(task.OutputPath)
	runOnce := func(m string) error {
		var args []string
		if m == MergeModeDemuxer {
			listFile := filepath.Join(os.TempDir(), fmt.Sprintf("concat_%s.txt", task.TaskID))
			if tempDir != "" {
				listFile = filepath.Join(tempDir, "list.txt")
			}
			if err := ffmpeg.WriteConcatList(inputs, listFile); err != nil {
				return err
			}
			args = ffmpeg.BuildConcatListArgs(listFile, task.OutputPath)
		} else {
			filter, silent, err := ffmpeg.BuildConcatFilter(probes, segDurations)
			if err != nil {
				return err
			}
			args = ffmpeg.BuildConcatFilterArgs(inputs, silent, filter, task.OutputPath)
		}

		// 合并进程同样受全局信号量约束（与批量项/单文件任务共享上限）
		if err := s.sem.Acquire(ctx, 1); err != nil {
			if s.isCanceled(task) {
				return errCanceled
			}
			return fmt.Errorf("并发获取超时（请稍后重试）")
		}
		defer s.sem.Release(1)

		progressCh := make(chan ffmpeg.Progress, 16)
		errCh := make(chan error, 1)
		go func() { errCh <- ffmpeg.Run(ctx, s.ffmpegPath, args, totalDuration, progressCh) }()

		for p := range progressCh {
			s.mu.Lock()
			task.Progress = p.Percent
			s.mu.Unlock()
			s.emit("trim:progress", map[string]interface{}{
				"task_id": task.TaskID,
				"kind":    KindMerge,
				"percent": p.Percent,
				"speed":   p.Speed,
			})
		}
		return <-errCh
	}

	runErr := runOnce(mode)
	if runErr == errCanceled {
		return
	}
	// demuxer 失败回退：仅 auto 模式（显式指定时不回退，尊重用户选择）
	if runErr != nil && requestedMode == MergeModeAuto && mode == MergeModeDemuxer {
		s.emit("merge:notice", map[string]interface{}{
			"task_id": task.TaskID,
			"message": "流复制合并不兼容（时间基等），已自动切换重编码合并",
		})
		mode = MergeModeFilter
		s.mu.Lock()
		task.MergeMode = MergeModeFilter
		s.mu.Unlock()
		runErr = runOnce(mode)
		if runErr == errCanceled {
			return
		}
	}

	if runErr != nil {
		if s.isCanceled(task) {
			return
		}
		if ctx.Err() == context.DeadlineExceeded {
			s.fail(task, "处理超时，请重试或换重编码模式")
			return
		}
		s.fail(task, friendlyErrorWith(runErr, "合并失败，请检查输入文件或重试"))
		return
	}

	// 取消可能发生在产物校验前后
	if s.isCanceled(task) {
		return
	}

	// 校验产物并同卷 rename 落地
	if !fileExists(partPath) {
		s.fail(task, "未生成输出文件")
		return
	}
	if err := os.Rename(partPath, task.OutputPath); err != nil {
		if copyErr := copyFile(partPath, task.OutputPath); copyErr != nil {
			s.fail(task, "写入输出失败: "+err.Error())
			return
		}
		_ = os.Remove(partPath)
	}

	s.mu.Lock()
	task.Status = StatusCompleted
	task.Progress = 100
	task.Duration = totalDuration
	task.CompletedCount = len(task.Items)
	for i := range task.Items {
		task.Items[i].Status = ItemStatusCompleted
		task.Items[i].Progress = 100
	}
	s.mu.Unlock()
	s.emit("trim:done", map[string]interface{}{
		"task_id":     task.TaskID,
		"kind":        KindMerge,
		"output_path": task.OutputPath,
		"duration":    task.Duration,
		"completed":   task.CompletedCount,
		"failed":      0,
	})
}

// ensureWritableDir 校验目录存在且可写（v1.4 §18.7：合并 output_path 父目录可写）。
func ensureWritableDir(dir string) error {
	if dir == "" {
		dir = "."
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("输出目录不存在: %s", dir)
	}
	if !info.IsDir() {
		return fmt.Errorf("输出路径不是目录: %s", dir)
	}
	f, err := os.CreateTemp(dir, ".vc_w_*")
	if err != nil {
		return fmt.Errorf("输出目录不可写: %s", dir)
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return nil
}

// isASCIIPath 判断路径是否全为 ASCII（中文等非 ASCII 路径需经临时目录规范化）。
func isASCIIPath(p string) bool {
	for _, r := range p {
		if r > 127 {
			return false
		}
	}
	return true
}

// linkOrCopy 优先硬链接（同卷零成本），失败回退整体复制。
func linkOrCopy(src, dst string) error {
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	return copyFile(src, dst)
}

// friendlyErrorWith 同 friendlyError，但可定制默认文案。
func friendlyErrorWith(err error, def string) string {
	msg := err.Error()
	switch {
	case contains(msg, "timeout"):
		return "处理超时，请重试或换重编码模式"
	case contains(msg, "No such file"):
		return "找不到输入文件"
	case contains(msg, "Permission denied"):
		return "无权限写入输出目录"
	default:
		return def
	}
}
