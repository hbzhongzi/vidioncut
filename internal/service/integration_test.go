package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"videocut/internal/config"
	"videocut/internal/ffmpeg"
)

// locateBinaries 从当前工作目录向上查找项目 resources/ffmpeg/{ffmpeg,ffprobe}.exe。
// 找不到时跳过测试（避免把“缺失二进制”误报成功能失败）。
func locateBinaries(t *testing.T) (string, string) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for dir := wd; ; dir = filepath.Dir(dir) {
		ff := filepath.Join(dir, "resources", "ffmpeg", "ffmpeg.exe")
		fp := filepath.Join(dir, "resources", "ffmpeg", "ffprobe.exe")
		if fileExists(ff) && fileExists(fp) {
			return ff, fp
		}
		if dir == filepath.VolumeName(dir)+string(filepath.Separator) || filepath.Dir(dir) == dir {
			break
		}
	}
	t.Skip("ffmpeg/ffprobe not found under resources/ffmpeg (run scripts/copy-ffmpeg.ps1 or place binaries)")
	return "", ""
}

// TestIntegration_TrimRealClip 用 FFmpeg 合成测试片，端到端跑 Probe→Trim→校验。
// 同时验证两条路径：
//   - stream_copy：流复制快路径，按关键帧粗定位（输出时长允许吸附偏差）。
//   - reencode：重编码精确路径，输出时长应为 end-start。
func TestIntegration_TrimRealClip(t *testing.T) {
	ff, fp := locateBinaries(t)

	dir := t.TempDir()
	src := filepath.Join(dir, "source.mp4")

	// 1) 合成 5s 测试片（视频 testsrc2 + 音频 sine）
	gen := exec.CommandContext(context.Background(), ff,
		"-f", "lavfi", "-i", "testsrc2=size=320x240:rate=25",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=5",
		"-t", "5", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac",
		"-y", src,
	)
	if outBytes, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("generate source clip: %v\n%s", err, string(outBytes))
	}

	// 2) Probe 源片
	pr, err := ffmpeg.Probe(context.Background(), fp, src)
	if err != nil {
		t.Fatalf("probe source: %v", err)
	}
	if pr.Duration < 4.5 || pr.Duration > 5.5 {
		t.Fatalf("unexpected source duration: %.3f (want ~5.0)", pr.Duration)
	}

	// 3) 构造 Service
	cfg, _ := config.Load()
	svc := NewService(cfg, ff, fp)

	const start, end = 1.0, 3.0

	// 4a) stream_copy 快路径：输出时长允许关键帧吸附偏差 [2.5, 3.5]
	t.Run("stream_copy", func(t *testing.T) {
		out := filepath.Join(dir, "out_stream.mp4")
		if _, err := svc.Trim(TrimRequest{InputPath: src, OutputPath: out, Start: start, End: end, Strategy: "stream_copy"}); err != nil {
			t.Fatalf("Trim: %v", err)
		}
		assertTrimResult(t, svc, out, fp, 2.5, 3.5)
	})

	// 4b) reencode 精确路径：输出时长应为 end-start ± 0.2
	t.Run("reencode", func(t *testing.T) {
		out := filepath.Join(dir, "out_reencode.mp4")
		if _, err := svc.Trim(TrimRequest{InputPath: src, OutputPath: out, Start: start, End: end, Strategy: "reencode"}); err != nil {
			t.Fatalf("Trim: %v", err)
		}
		assertTrimResult(t, svc, out, fp, 1.8, 2.2)
	})
}

// assertTrimResult 轮询任务直到完成，校验输出存在、非空、时长落在 [minDur,maxDur]。
func assertTrimResult(t *testing.T, svc *Service, out, fp string, minDur, maxDur float64) {
	t.Helper()
	// 找到对应任务：以输出路径反向定位（任务表按 taskID 索引，这里用最新状态轮询 ListTasks）
	deadline := time.Now().Add(30 * time.Second)
	var status string
	for time.Now().Before(deadline) {
		done := false
		for _, tk := range svc.ListTasks() {
			if tk.OutputPath == out {
				status = string(tk.Status)
				if status == string(StatusCompleted) || status == string(StatusFailed) {
					if status == string(StatusFailed) {
						t.Fatalf("trim failed: %s", tk.Error)
					}
					done = true
				}
				break
			}
		}
		if done {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if status != string(StatusCompleted) {
		t.Fatalf("trim did not complete in time, status=%q out=%s", status, out)
	}

	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("output missing: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("output file is empty")
	}
	op, err := ffmpeg.Probe(context.Background(), fp, out)
	if err != nil {
		t.Fatalf("probe output: %v", err)
	}
	if op.Duration < minDur || op.Duration > maxDur {
		t.Fatalf("unexpected output duration: %.3f (want within [%.1f,%.1f])", op.Duration, minDur, maxDur)
	}
	t.Logf("OK: out=%s duration=%.3fs size=%d", out, op.Duration, info.Size())
}
