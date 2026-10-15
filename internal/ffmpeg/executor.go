package ffmpeg

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
)

// Run 执行 FFmpeg 命令并通过 progress channel 推送进度。
// 设计要点（开发文档 v1.1 §4.1）：
//   - 使用 -nostats -progress pipe:1，进度从 stdout 结构化读取，与 Windows 控制台代码页（GBK）无关。
//   - 不依赖 LANG 环境变量（Windows 下无效）。
//   - 通过 context 控制超时：超时后由 exec.CommandContext 自动 Kill 进程。
//
// args 不应包含 ffmpeg 本体；totalDuration 为源时长（秒），用于计算百分比；未知时传 0。
// progress channel 由内部 goroutine 关闭，调用方应先启动接收 goroutine 再调用。
func Run(ctx context.Context, ffmpegPath string, args []string, totalDuration float64, progress chan<- Progress) error {
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderrBuf := newSafeBuffer(4096)
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ffmpeg start: %w", err)
	}

	// 进度解析 goroutine 拥有 progress channel 的关闭权，避免进程被 Kill 后
	// 主协程 defer close 与 goroutine 仍在发送之间产生竞态（向已关闭 channel 发送会 panic）。
	go func() {
		defer close(progress)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 4*1024*1024)
		var outTime float64
		var speed string
		for scanner.Scan() {
			key, val, ok := splitKV(scanner.Text())
			if !ok {
				continue
			}
			switch key {
			case "out_time":
				if t, ok := parseOutTime(val); ok {
					outTime = t
					send(progress, totalDuration, outTime, speed, false)
				}
			case "speed":
				speed = val
			case "progress":
				if val == "end" {
					send(progress, totalDuration, outTime, speed, true)
				}
			}
		}
		// scanner 结束（管道关闭）后，确保发送完成态
		send(progress, totalDuration, outTime, speed, true)
	}()

	waitErr := cmd.Wait()
	if waitErr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("ffmpeg timeout")
		}
		// 过滤掉常见的、非致命的 stderr 噪声
		tail := stderrBuf.Tail()
		return fmt.Errorf("ffmpeg failed: %w; %s", waitErr, tail)
	}
	return nil
}

func send(ch chan<- Progress, total float64, outTime float64, speed string, done bool) {
	p := Progress{OutTime: outTime, Speed: speed, Done: done}
	if total > 0 {
		p.Percent = outTime / total * 100
		if p.Percent > 100 {
			p.Percent = 100
		}
	}
	select {
	case ch <- p:
	default:
	}
}

// ensure io imported (used by safeBuffer via io.Writer signature)
var _ io.Writer = (*safeBuffer)(nil)
