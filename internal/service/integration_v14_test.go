package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"videocut/internal/config"
	"videocut/internal/ffmpeg"
)

// waitTaskFinal 轮询任务直到进入终态（completed/partial/failed），超时失败。
// 使用快照读取（GetTaskSnapshot），避免与执行侧写入竞态（-race 实测）。
func waitTaskFinal(t *testing.T, svc *Service, id string, timeout time.Duration) Task {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		tk, ok := svc.GetTaskSnapshot(id)
		if !ok {
			t.Fatalf("task %s not found", id)
		}
		switch tk.Status {
		case StatusCompleted, StatusPartial, StatusFailed:
			return tk
		}
		time.Sleep(150 * time.Millisecond)
	}
	tk, _ := svc.GetTaskSnapshot(id)
	t.Fatalf("task %s did not finish in %v, status=%q", id, timeout, tk.Status)
	return Task{}
}

// genClip 合成测试片：size 分辨率、dur 秒、视频+音频。
func genClip(t *testing.T, ff, path, size string, dur int) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), ff,
		"-f", "lavfi", "-i", "testsrc2=size="+size+":rate=25",
		"-f", "lavfi", "-i", "sine=frequency=440:duration="+strconv.Itoa(dur),
		"-t", strconv.Itoa(dur), "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac",
		"-y", path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate clip %s: %v\n%s", path, err, out)
	}
}

// TestIntegration_BatchTrim 批量 2 项：两项产物存在且时长正确（v1.4 §18.9 ①）。
func TestIntegration_BatchTrim(t *testing.T) {
	ff, fp := locateBinaries(t)
	dir := t.TempDir()
	src1 := filepath.Join(dir, "a.mp4")
	src2 := filepath.Join(dir, "b.mp4")
	genClip(t, ff, src1, "320x240", 5)
	genClip(t, ff, src2, "320x240", 5)

	cfg, _ := config.Load()
	svc := NewService(cfg, ff, fp)

	out1 := filepath.Join(dir, "a_cut.mp4")
	out2 := filepath.Join(dir, "b_cut.mp4")
	id, err := svc.TrimBatch(BatchRequest{Items: []BatchItem{
		{InputPath: src1, Start: 1, End: 3, Strategy: "stream_copy", OutputPath: out1},
		{InputPath: src2, Start: 0.5, End: 2.5, Strategy: "reencode", OutputPath: out2},
	}})
	if err != nil {
		t.Fatalf("TrimBatch: %v", err)
	}

	tk := waitTaskFinal(t, svc, id, 60*time.Second)
	if tk.Status != StatusCompleted {
		t.Fatalf("batch status = %q, want completed (error=%s)", tk.Status, tk.Error)
	}
	if tk.CompletedCount != 2 || tk.FailedCount != 0 {
		t.Errorf("counts = %d/%d, want 2/0", tk.CompletedCount, tk.FailedCount)
	}
	// 产物存在且时长正确
	assertProbeDur(t, fp, out1, 1.8, 3.5) // stream_copy 允许关键帧吸附
	assertProbeDur(t, fp, out2, 1.8, 2.2) // reencode 精确
	// 任务 items 均完成
	for i, it := range tk.Items {
		if it.Status != ItemStatusCompleted {
			t.Errorf("item %d status = %q, want completed", i, it.Status)
		}
	}
}

// TestIntegration_BatchPartial 批量部分失败：第 2 项输出目录不存在 → partial，
// 好项产物保留（v1.4 §18.9 ②）。
func TestIntegration_BatchPartial(t *testing.T) {
	ff, fp := locateBinaries(t)
	dir := t.TempDir()
	src1 := filepath.Join(dir, "a.mp4")
	src2 := filepath.Join(dir, "b.mp4")
	genClip(t, ff, src1, "320x240", 5)
	genClip(t, ff, src2, "320x240", 5)

	cfg, _ := config.Load()
	svc := NewService(cfg, ff, fp)

	out1 := filepath.Join(dir, "a_cut.mp4")
	badOut := filepath.Join(dir, "no_such_dir", "b_cut.mp4") // 父目录不存在 → 执行期失败
	id, err := svc.TrimBatch(BatchRequest{Items: []BatchItem{
		{InputPath: src1, Start: 0, End: 2, Strategy: "reencode", OutputPath: out1},
		{InputPath: src2, Start: 0, End: 2, Strategy: "reencode", OutputPath: badOut},
	}})
	if err != nil {
		t.Fatalf("TrimBatch: %v", err)
	}

	tk := waitTaskFinal(t, svc, id, 60*time.Second)
	if tk.Status != StatusPartial {
		t.Fatalf("batch status = %q, want partial", tk.Status)
	}
	if tk.CompletedCount != 1 || tk.FailedCount != 1 {
		t.Errorf("counts = %d/%d, want 1/1", tk.CompletedCount, tk.FailedCount)
	}
	if tk.Items[0].Status != ItemStatusCompleted {
		t.Errorf("item 0 = %q, want completed", tk.Items[0].Status)
	}
	if tk.Items[1].Status != ItemStatusFailed || tk.Items[1].Error == "" {
		t.Errorf("item 1 = %q err=%q, want failed with error", tk.Items[1].Status, tk.Items[1].Error)
	}
	// 好项产物保留
	if !fileExists(out1) {
		t.Error("good item output should be preserved")
	}
	assertProbeDur(t, fp, out1, 1.8, 2.2)
}

// TestIntegration_MergeDemuxer 同参 2 段 demuxer 合并：时长=和（v1.4 §18.9 ③）。
func TestIntegration_MergeDemuxer(t *testing.T) {
	ff, fp := locateBinaries(t)
	dir := t.TempDir()
	s1 := filepath.Join(dir, "s1.mp4")
	s2 := filepath.Join(dir, "s2.mp4")
	genClip(t, ff, s1, "320x240", 3)
	genClip(t, ff, s2, "320x240", 3)

	cfg, _ := config.Load()
	svc := NewService(cfg, ff, fp)

	out := filepath.Join(dir, "merged.mp4")
	id, err := svc.Merge(MergeRequest{
		Items:      []MergeItem{{InputPath: s1}, {InputPath: s2}},
		Mode:       "demuxer",
		OutputPath: out,
	})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	tk := waitTaskFinal(t, svc, id, 60*time.Second)
	if tk.Status != StatusCompleted {
		t.Fatalf("merge status = %q, want completed (error=%s)", tk.Status, tk.Error)
	}
	if tk.MergeMode != MergeModeDemuxer {
		t.Errorf("merge mode = %q, want demuxer", tk.MergeMode)
	}
	assertProbeDur(t, fp, out, 5.5, 6.5)
}

// TestIntegration_MergeFilter 异分辨率 2 段 auto→filter 合并：时长=和且可 probe（v1.4 §18.9 ④）。
func TestIntegration_MergeFilter(t *testing.T) {
	ff, fp := locateBinaries(t)
	dir := t.TempDir()
	s1 := filepath.Join(dir, "small.mp4")
	s2 := filepath.Join(dir, "big.mp4")
	genClip(t, ff, s1, "320x240", 3)
	genClip(t, ff, s2, "640x360", 3)

	cfg, _ := config.Load()
	svc := NewService(cfg, ff, fp)

	out := filepath.Join(dir, "merged.mp4")
	id, err := svc.Merge(MergeRequest{
		Items:      []MergeItem{{InputPath: s1}, {InputPath: s2}},
		Mode:       "auto", // 异参 → 自动判定 filter
		OutputPath: out,
	})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	tk := waitTaskFinal(t, svc, id, 120*time.Second)
	if tk.Status != StatusCompleted {
		t.Fatalf("merge status = %q, want completed (error=%s)", tk.Status, tk.Error)
	}
	if tk.MergeMode != MergeModeFilter {
		t.Errorf("merge mode = %q, want filter", tk.MergeMode)
	}
	assertProbeDur(t, fp, out, 5.5, 6.5)
}

// TestIntegration_MergeHybrid 带区间裁剪后合并：时长=各区间和（v1.4 §18.9 ⑤）。
// 区间项用 reencode 保证帧精确（stream_copy 按关键帧吸附会使段长不可控）。
func TestIntegration_MergeHybrid(t *testing.T) {
	ff, fp := locateBinaries(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src.mp4")
	genClip(t, ff, src, "320x240", 6)

	cfg, _ := config.Load()
	svc := NewService(cfg, ff, fp)

	out := filepath.Join(dir, "hybrid.mp4")
	id, err := svc.Merge(MergeRequest{
		Items: []MergeItem{
			{InputPath: src, Start: 0, End: 2, HasRange: true, Strategy: "reencode"},
			{InputPath: src, Start: 2, End: 4, HasRange: true, Strategy: "reencode"},
		},
		Mode:       "auto",
		OutputPath: out,
	})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	tk := waitTaskFinal(t, svc, id, 120*time.Second)
	if tk.Status != StatusCompleted {
		t.Fatalf("hybrid merge status = %q, want completed (error=%s)", tk.Status, tk.Error)
	}
	// 2s + 2s = 4s（reencode 精确，留适当余量）
	assertProbeDur(t, fp, out, 3.8, 4.3)
}

// TestIntegration_MergeChinesePath 中文路径合并：经 ASCII 临时目录规范化后成功（v1.4 §18.9 ⑥）。
func TestIntegration_MergeChinesePath(t *testing.T) {
	ff, fp := locateBinaries(t)
	dir := filepath.Join(t.TempDir(), "中文目录")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	s1 := filepath.Join(dir, "片段一.mp4")
	s2 := filepath.Join(dir, "片段二.mp4")
	genClip(t, ff, s1, "320x240", 3)
	genClip(t, ff, s2, "320x240", 3)

	cfg, _ := config.Load()
	svc := NewService(cfg, ff, fp)

	out := filepath.Join(dir, "合并结果.mp4")
	id, err := svc.Merge(MergeRequest{
		Items:      []MergeItem{{InputPath: s1}, {InputPath: s2}},
		Mode:       "auto",
		OutputPath: out,
	})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	tk := waitTaskFinal(t, svc, id, 60*time.Second)
	if tk.Status != StatusCompleted {
		t.Fatalf("chinese-path merge status = %q, want completed (error=%s)", tk.Status, tk.Error)
	}
	assertProbeDur(t, fp, out, 5.5, 6.5)
}

// TestIntegration_MergeAutoFallback auto 模式 demuxer 失败 → 自动回退 filter 并发 notice。
// 用假 ffmpeg（.cmd）：遇到 -f concat 退出 1，否则创建输出文件；ffprobe 用真实二进制。
func TestIntegration_MergeAutoFallback(t *testing.T) {
	_, fp := locateBinaries(t)
	dir := t.TempDir()
	s1 := filepath.Join(dir, "s1.mp4")
	s2 := filepath.Join(dir, "s2.mp4")
	// 直接写最小合法 mp4 不可行，改用 lavfi 需要 ffmpeg——此处用 locateBinaries 的 ffmpeg 生成
	ff, _ := locateBinaries(t)
	genClip(t, ff, s1, "320x240", 3)
	genClip(t, ff, s2, "320x240", 3)

	// 假 ffmpeg：-f concat → 失败；其他 → 创建最后一个参数（输出 .part）
	fake := filepath.Join(dir, "fakeffmpeg.cmd")
	script := "@echo off\r\n" +
		"setlocal\r\n" +
		"set \"LAST=\"\r\n" +
		":parse\r\n" +
		"if \"%~1\"==\"\" goto end\r\n" +
		"if /i \"%~1\"==\"-f\" if /i \"%~2\"==\"concat\" exit /b 1\r\n" +
		"set \"LAST=%~1\"\r\n" +
		"shift\r\n" +
		"goto parse\r\n" +
		":end\r\n" +
		"if defined LAST type nul>\"%LAST%\"\r\n" +
		"exit /b 0\r\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg, _ := config.Load()
	svc := NewService(cfg, fake, fp)

	type notice struct {
		name string
		data map[string]interface{}
	}
	var notices []notice
	svc.emitFn = func(name string, data interface{}) {
		if name == "merge:notice" {
			if m, ok := data.(map[string]interface{}); ok {
				notices = append(notices, notice{name, m})
			}
		}
	}

	out := filepath.Join(dir, "merged.mp4")
	id, err := svc.Merge(MergeRequest{
		Items:      []MergeItem{{InputPath: s1}, {InputPath: s2}},
		Mode:       "auto", // 同参 → demuxer → 假进程失败 → 回退 filter
		OutputPath: out,
	})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	tk := waitTaskFinal(t, svc, id, 60*time.Second)
	if tk.Status != StatusCompleted {
		t.Fatalf("fallback merge status = %q, want completed (error=%s)", tk.Status, tk.Error)
	}
	if tk.MergeMode != MergeModeFilter {
		t.Errorf("merge mode = %q, want filter after fallback", tk.MergeMode)
	}
	if !fileExists(out) {
		t.Error("output should exist after fallback")
	}
	if len(notices) != 1 {
		t.Fatalf("expected 1 merge:notice, got %d", len(notices))
	}
	if msg, _ := notices[0].data["message"].(string); !strings.Contains(msg, "重编码") {
		t.Errorf("notice message = %q, want mention 重编码", msg)
	}
}

// assertProbeDur 校验输出存在且 probe 时长落在 [minDur,maxDur]。
func assertProbeDur(t *testing.T, fp, out string, minDur, maxDur float64) {
	t.Helper()
	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("output missing: %v", err)
	}
	if info.Size() == 0 {
		t.Fatalf("output empty: %s", out)
	}
	op, err := ffmpeg.Probe(context.Background(), fp, out)
	if err != nil {
		t.Fatalf("probe output %s: %v", out, err)
	}
	if op.Duration < minDur || op.Duration > maxDur {
		t.Fatalf("unexpected duration for %s: %.3f (want [%.1f,%.1f])", out, op.Duration, minDur, maxDur)
	}
	t.Logf("OK: %s duration=%.3fs size=%d", out, op.Duration, info.Size())
}
