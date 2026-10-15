package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"videocut/internal/config"
)

func newTestService(ffmpegPath, ffprobePath string) *Service {
	cfg := config.Default()
	return NewService(cfg, ffmpegPath, ffprobePath)
}

func TestValidateRequest(t *testing.T) {
	const dur = 10.0
	dir := t.TempDir()
	real := filepath.Join(dir, "real.mp4")
	if err := os.WriteFile(real, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		req     TrimRequest
		wantErr bool
	}{
		{"empty input", TrimRequest{InputPath: "", Start: 0, End: 1}, true},
		{"missing file", TrimRequest{InputPath: "C:/no/such.mp4", Start: 0, End: 1}, true},
		{"start negative", TrimRequest{InputPath: real, Start: -1, End: 2}, true},
		{"end <= start", TrimRequest{InputPath: real, Start: 5, End: 5}, true},
		{"end > duration", TrimRequest{InputPath: real, Start: 1, End: dur + 1}, true},
		{"valid", TrimRequest{InputPath: real, Start: 1, End: 3}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateRequest(c.req, dur)
			if (err != nil) != c.wantErr {
				t.Errorf("validateRequest error=%v wantErr=%v", err, c.wantErr)
			}
		})
	}
}

func TestCheckFFmpeg(t *testing.T) {
	svc := newTestService("", "")
	if err := svc.checkFFmpeg(); err == nil {
		t.Error("expected error when ffmpeg not configured")
	}
	svc2 := newTestService("C:/ff/ffmpeg.exe", "C:/ff/ffprobe.exe")
	if err := svc2.checkFFmpeg(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTrim_NoFFmpeg(t *testing.T) {
	svc := newTestService("", "")
	_, err := svc.Trim(TrimRequest{InputPath: "C:/a/b.mp4", Start: 0, End: 1})
	if err == nil {
		t.Fatal("expected error when ffmpeg not configured")
	}
	if !strings.Contains(err.Error(), "FFmpeg 未配置") {
		t.Errorf("error should mention 未配置, got %v", err)
	}
}

func TestTrim_MissingInputFile(t *testing.T) {
	// 用假路径（非空）让 checkFFmpeg 通过，但文件不存在应直接报“输入文件不存在”。
	svc := newTestService("C:/dummy/ffmpeg.exe", "C:/dummy/ffprobe.exe")
	_, err := svc.Trim(TrimRequest{InputPath: "C:/no/such/file.mp4", Start: 0, End: 1})
	if err == nil {
		t.Fatal("expected error for missing input file")
	}
	if !strings.Contains(err.Error(), "输入文件不存在") {
		t.Errorf("error should mention 输入文件不存在, got %v", err)
	}
}

func TestPreviewWhitelist(t *testing.T) {
	svc := newTestService("C:/dummy/ffmpeg.exe", "C:/dummy/ffprobe.exe")
	if svc.IsPreviewAllowed("C:/videos/a.mp4") {
		t.Error("should be disallowed before registration")
	}
	svc.RegisterPreviewPath("C:/videos/a.mp4")
	if !svc.IsPreviewAllowed("C:/videos/a.mp4") {
		t.Error("should be allowed after registration")
	}
	if svc.IsPreviewAllowed("C:/videos/b.mp4") {
		t.Error("different path should remain disallowed")
	}
}

func TestGetListTasks(t *testing.T) {
	svc := newTestService("C:/dummy/ffmpeg.exe", "C:/dummy/ffprobe.exe")
	svc.tasks["t1"] = &Task{TaskID: "t1", Status: StatusCompleted}
	if _, ok := svc.GetTask("t1"); !ok {
		t.Error("GetTask should find t1")
	}
	if _, ok := svc.GetTask("nope"); ok {
		t.Error("GetTask should not find missing")
	}
	if n := len(svc.ListTasks()); n != 1 {
		t.Errorf("ListTasks len=%d want 1", n)
	}
}

func TestCancelNotFound(t *testing.T) {
	svc := newTestService("C:/dummy/ffmpeg.exe", "C:/dummy/ffprobe.exe")
	if err := svc.Cancel("ghost"); err == nil {
		t.Error("Cancel should error for unknown task")
	}
}

func TestCancel_MarksProcessingFailed(t *testing.T) {
	svc := newTestService("C:/dummy/ffmpeg.exe", "C:/dummy/ffprobe.exe")
	tk := &Task{TaskID: "t1", Status: StatusProcessing}
	svc.tasks["t1"] = tk
	_, cancel := context.WithCancel(context.Background())
	svc.cancels["t1"] = cancel
	if err := svc.Cancel("t1"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if tk.Status != StatusFailed || !tk.Canceled || tk.Error != "已取消" {
		t.Errorf("processing cancel: status=%q canceled=%v err=%q", tk.Status, tk.Canceled, tk.Error)
	}
	if svc.isCanceled(tk) != true {
		t.Error("isCanceled should be true after cancel")
	}
}

func TestCancel_PendingTask(t *testing.T) {
	// 排队中（pending）也应可取消，且标记 Canceled 以便 run() 短路、不再二次报错。
	svc := newTestService("C:/dummy/ffmpeg.exe", "C:/dummy/ffprobe.exe")
	tk := &Task{TaskID: "t2", Status: StatusPending}
	svc.tasks["t2"] = tk
	_, cancel := context.WithCancel(context.Background())
	svc.cancels["t2"] = cancel
	if err := svc.Cancel("t2"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if tk.Status != StatusFailed || !tk.Canceled {
		t.Errorf("pending cancel: %+v", tk)
	}
}

func TestFriendlyError(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"boom timeout boom", "处理超时，请重试或换重编码模式"},
		{"No such file or directory", "找不到输入文件"},
		{"Permission denied", "无权限写入输出目录"},
		{"weird ffmpeg crash", "裁剪失败，请检查输入文件或重试"},
	}
	for _, c := range cases {
		if got := friendlyError(errString(c.in)); got != c.want {
			t.Errorf("friendlyError(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }
