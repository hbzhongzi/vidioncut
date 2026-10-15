package ffmpeg

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeFakeFFmpeg 写出一个“假 ffmpeg”脚本，按 -progress 协议向 stdout 输出进度行。
// 忽略实际参数，仅用于验证 Run 的进度解析与超时逻辑（无需真实 FFmpeg 二进制）。
func writeFakeFFmpeg(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fakeffmpeg.cmd")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake ffmpeg: %v", err)
	}
	return path
}

func TestRun_ProgressParsing(t *testing.T) {
	fake := writeFakeFFmpeg(t, "@echo off\r\n"+
		"echo out_time=1.000\r\n"+
		"echo progress=continue\r\n"+
		"echo out_time=2.000\r\n"+
		"echo progress=end\r\n")

	ctx := context.Background()
	progressCh := make(chan Progress, 64)
	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, fake, []string{"-i", "in.mp4"}, 4.0, progressCh) }()

	var events []Progress
	for p := range progressCh {
		events = append(events, p)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected progress events, got none")
	}
	last := events[len(events)-1]
	if !last.Done {
		t.Error("last progress should be Done=true")
	}
	// 校验 out_time=2.0 时 Percent=50（totalDuration=4.0）
	var sawHalf bool
	for _, e := range events {
		if e.OutTime == 2.0 && e.Percent == 50.0 {
			sawHalf = true
		}
	}
	if !sawHalf {
		t.Errorf("expected OutTime=2.0 & Percent=50, events=%+v", events)
	}
}

func TestRun_Timeout(t *testing.T) {
	// ping -n 6 约 5s；上下文 800ms 超时，Run 应返回 timeout 错误并回收进程。
	fake := writeFakeFFmpeg(t, "@echo off\r\nping -n 6 127.0.0.1 >nul\r\n")

	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()

	progressCh := make(chan Progress, 64)
	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, fake, []string{"-i", "in.mp4"}, 0, progressCh) }()

	// 排空通道（goroutine 在进程被 kill 后会关闭它）
	go func() {
		for range progressCh {
		}
	}()

	runErr := <-errCh
	if runErr == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(runErr.Error(), "timeout") {
		t.Errorf("error should mention timeout, got %v", runErr)
	}
}

func TestRun_StartFailure(t *testing.T) {
	// 指向不存在的二进制，cmd.Start 直接失败。
	ctx := context.Background()
	progressCh := make(chan Progress, 8)
	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, "C:/no/such/ffmpeg.exe", []string{"-i", "x"}, 0, progressCh) }()
	go func() { for range progressCh {} }()
	if err := <-errCh; err == nil {
		t.Fatal("expected start error for missing binary")
	}
}
