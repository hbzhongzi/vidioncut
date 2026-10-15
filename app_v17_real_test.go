package main

// 真实 ASR 环境端到端测试（opt-in）。
//
// 与 app_v17_test.go 的区别：那个用假 sidecar 覆盖"链路是否通"，
// 这个用**真实 faster-whisper sidecar + 真实模型**覆盖"真实 Whisper 输出能否
// 驱动候选扫描→保留段剪切→合并成片"，即两者之间唯一未被覆盖的接缝。
//
// 运行前提：scripts/bootstrap-asr.ps1 已跑通（模型已缓存）。
// 运行方式：
//     # 终端 A：起真实 sidecar
//     $env:HF_HOME="E:/models/hf"; $env:HF_HUB_OFFLINE="1"
//     asr\.venv\Scripts\python.exe asr\serve.py --port 4178
//     # 终端 B：跑测试
//     $env:VIDEOCUT_REAL_ASR=1
//     go test . -run TestRealASR -v -timeout 15m
//
// 环境变量：
//     VIDEOCUT_REAL_ASR=1       必须，否则 Skip
//     VIDEOCUT_ASR_PORT         默认 4178
//     VIDEOCUT_ASR_TEST_VIDEO   默认 asr/testdata/sample.mp4（合成：3 段音 + 3 段静音）

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"videocut/internal/asr"
	"videocut/internal/config"
	"videocut/internal/mcpserver"
	"videocut/internal/service"
)

// TestRealASR_FullChain 用真实 sidecar 跑通：真实转录 → 真实静音候选 → 真实剪切 → 合并 → 导稿。
//
// 素材说明：sample.mp4 是合成片（音调段 + 静音段），无人声。真实人声素材下 VAD
// 会正常工作；合成音下必须显式关闭 VAD，否则 Whisper 的 VAD 会把整段滤空、
// 得到 0 片段 0 候选，链路就无法被验证。这里关闭 VAD 后 Whisper 会输出跨静音的
// 片段，片段间隙 ≥1s 即被判为静音候选——从而真实触发后续剪切。
func TestRealASR_FullChain(t *testing.T) {
	if os.Getenv("VIDEOCUT_REAL_ASR") != "1" {
		t.Skip("设置 VIDEOCUT_REAL_ASR=1 并先起真实 sidecar 才会运行（见文件头注释）")
	}

	ff, fp := locateFF(t)
	port := 4178
	if p := os.Getenv("VIDEOCUT_ASR_PORT"); p != "" {
		if v, err := strconv.Atoi(p); err == nil {
			port = v
		}
	}
	video := os.Getenv("VIDEOCUT_ASR_TEST_VIDEO")
	if video == "" {
		wd, _ := os.Getwd()
		video = filepath.Join(wd, "asr", "testdata", "sample.mp4")
	}
	if !fileExists(video) {
		t.Fatalf("测试素材不存在: %s（可用 ffmpeg 合成，或设置 VIDEOCUT_ASR_TEST_VIDEO）", video)
	}

	dir := t.TempDir()
	cfg := config.Default()
	cfg.ASRHost = "127.0.0.1"
	cfg.ASRPort = port
	cfg.ASREnabled = true
	cfg.ASRPython = "" // 端口已占用则复用；置空让 EnsureRunning 走默认发现
	cfg.TimeoutSeconds = 300

	client := asr.NewClient(cfg)
	if err := client.Health(context.Background()); err != nil {
		t.Fatalf("真实 sidecar 不可达（127.0.0.1:%d）: %v\n"+
			"请先启动：asr\\.venv\\Scripts\\python.exe asr\\serve.py --port %d", port, err, port)
	}

	svc := service.NewService(cfg, ff, fp)
	app := NewApp(cfg, svc, client)

	// ① 真实转录（关闭 VAD 以让合成素材产出片段与静音间隙）
	vadOff := false
	res, err := client.Transcribe(context.Background(), asr.TranscribeRequest{
		VideoPath:  video,
		OutputBase: dir,
		Language:   "zh",
		VADFilter:  &vadOff,
	})
	if err != nil {
		t.Fatalf("真实转录失败: %v", err)
	}
	t.Logf("转录完成: duration=%.3fs segments=%d words=%d silences=%d",
		res.Duration, res.Segments, len(res.SignalsPath), res.Silences)
	if res.SRTPath == "" || !fileExists(res.SRTPath) {
		t.Fatalf("transcript.srt 未落盘: %q", res.SRTPath)
	}
	if res.SignalsPath == "" || !fileExists(res.SignalsPath) {
		t.Fatalf("signals.json 未落盘: %q", res.SignalsPath)
	}
	if res.Duration <= 0 {
		t.Fatalf("转录未上报源时长: %+v", res)
	}

	// ② 从真实 signals.json 扫描候选（必须与 MCP 工具共用同一实现）
	items, source, duration, err := mcpserver.BuildCandidateItems(dir, 1.0)
	if err != nil {
		t.Fatalf("候选扫描失败: %v", err)
	}
	t.Logf("真实候选: %d 项, source=%s, duration=%.3f", len(items), source, duration)
	if len(items) == 0 {
		t.Fatal("真实转录未产出任何候选（素材/模型异常），无法验证剪切链路")
	}

	// ③ 计算摘要与保留段
	view, err := app.AICutCompute(source, items)
	if err != nil {
		t.Fatalf("AICutCompute 失败: %v", err)
	}
	if !view.Valid {
		t.Fatalf("账本校验未通过: %s", view.ValidationError)
	}
	if len(view.KeepRanges) == 0 {
		t.Fatalf("保留段为空: %+v", view.KeepRanges)
	}
	t.Logf("保留段 %d 段, 删除 %.3fs, 保留 %.3fs",
		len(view.KeepRanges), view.Summary.DeletedSeconds, view.Summary.KeepSeconds)
	if view.Summary.DeletedSeconds <= 0 {
		t.Errorf("删除时长应 > 0，实际 %.3f", view.Summary.DeletedSeconds)
	}

	// ④ 真实剪切 + ⑤ 合并
	taskID, err := app.AICutSubmit(source, items, "reencode")
	if err != nil {
		t.Fatalf("AICutSubmit 失败: %v", err)
	}
	cutTask := waitTask(t, svc, taskID, 300*time.Second)
	if cutTask.Status != "completed" {
		t.Fatalf("剪切终态 %q: %s", cutTask.Status, cutTask.Error)
	}
	mergeID, err := app.MergeCutSegments(taskID, "")
	if err != nil {
		t.Fatalf("MergeCutSegments 失败: %v", err)
	}
	mergeTask := waitTask(t, svc, mergeID, 300*time.Second)
	if mergeTask.Status != "completed" {
		t.Fatalf("合成终态 %q: %s", mergeTask.Status, mergeTask.Error)
	}
	// 终片路径：合并任务回填 OutputPath；单段 remux 走批量任务，取唯一子项产物。
	finalPath := mergeTask.OutputPath
	if finalPath == "" && len(mergeTask.Items) == 1 {
		finalPath = mergeTask.Items[0].OutputPath
	}
	if finalPath == "" || !fileExists(finalPath) {
		t.Fatalf("成片缺失: %q", finalPath)
	}

	// ⑥ 成片时长应 ≈ 保留段之和（真实 ffmpeg 产物校验，容差 1.5s 容纳编码/容器开销）
	pr, err := app.Probe(finalPath)
	if err != nil {
		t.Fatalf("探测成片失败: %v", err)
	}
	want := view.Summary.KeepSeconds
	t.Logf("成片: %s 时长 %.3fs（期望≈%.3fs）", finalPath, pr.Duration, want)
	if diff := pr.Duration - want; diff > 1.5 || diff < -1.5 {
		t.Errorf("成片时长 %.3fs 与保留段 %.3fs 偏差 %.3fs 超出容差", pr.Duration, want, diff)
	}
	if pr.Duration >= duration {
		t.Errorf("成片时长 %.3fs 未短于源片 %.3fs，删除段未生效", pr.Duration, duration)
	}

	// ⑦ 导稿（真实 transcript.srt → md）
	mdPath, err := app.ExportTranscriptFile(dir, "md")
	if err != nil {
		t.Fatalf("ExportTranscriptFile 失败: %v", err)
	}
	if !fileExists(mdPath) {
		t.Fatalf("导稿产物缺失: %s", mdPath)
	}
}
