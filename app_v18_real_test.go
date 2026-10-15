package main

// 真实 ASR 环境字幕端到端测试（opt-in，M12）。
//
// 与 app_v18_test.go（假 sidecar）的区别：那个验证"链路是否通"，
// 这个用**真实 faster-whisper sidecar + 真实模型**跑通
// "真实转录 → 真实候选 → 真实剪切 → 合并成片 → 字幕重对齐 → 外显导出 →
// 特效 → 内嵌烧录 → 抽帧验证字幕像素"，覆盖真实 Whisper 输出（词级时间戳
// 交叠、标点粘连）驱动字幕全链路——假 sidecar 数据太干净，从未暴露这些。
//
// 运行前提：scripts/bootstrap-asr.ps1 已跑通（模型已缓存）。
// 运行方式：
//     # 终端 A：起真实 sidecar
//     $env:HF_HOME="E:/models/hf"; $env:HF_HUB_OFFLINE="1"; $env:HF_HUB_DISABLE_XET="1"
//     asr\.venv\Scripts\python.exe asr\serve.py --port 4178
//     # 终端 B：跑测试
//     $env:VIDEOCUT_REAL_ASR=1
//     go test . -run TestRealSubtitle -v -timeout 20m
//
// 环境变量：
//     VIDEOCUT_REAL_ASR=1       必须，否则 Skip
//     VIDEOCUT_ASR_PORT         默认 4178
//     VIDEOCUT_ASR_TEST_VIDEO   默认 asr/testdata/sample.mp4

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"videocut/internal/asr"
	"videocut/internal/config"
	"videocut/internal/edits"
	"videocut/internal/mcpserver"
	"videocut/internal/service"
	"videocut/internal/subtitles"
)

// TestRealSubtitle_FullChain 真实环境字幕全链路（§8 端到端行）。
func TestRealSubtitle_FullChain(t *testing.T) {
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
		t.Fatalf("测试素材不存在: %s", video)
	}

	dir := t.TempDir()
	cfg := config.Default()
	cfg.ASRHost = "127.0.0.1"
	cfg.ASRPort = port
	cfg.ASREnabled = true
	cfg.TimeoutSeconds = 300

	client := asr.NewClient(cfg)
	if err := client.Health(context.Background()); err != nil {
		t.Fatalf("真实 sidecar 不可达（127.0.0.1:%d）: %v\n"+
			"请先启动：asr\\.venv\\Scripts\\python.exe asr\\serve.py --port %d", port, err, port)
	}

	svc := service.NewService(cfg, ff, fp)
	app := NewApp(cfg, svc, client)

	// ① 真实转录（关闭 VAD：合成素材无人声，VAD 会滤空整段）
	vadOff := false
	res, err := client.Transcribe(context.Background(), asr.TranscribeRequest{
		VideoPath: video, OutputBase: dir, Language: "zh", VADFilter: &vadOff,
	})
	if err != nil {
		t.Fatalf("真实转录失败: %v", err)
	}
	t.Logf("① 转录: duration=%.3fs segments=%d silences=%d", res.Duration, res.Segments, res.Silences)

	// ② 真实候选扫描（词级交叠/标点粘连的真实产物）
	items, source, duration, err := mcpserver.BuildCandidateItems(dir, 1.0)
	if err != nil {
		t.Fatalf("候选扫描失败: %v", err)
	}
	t.Logf("② 候选: %d 项, duration=%.3f", len(items), duration)
	// 合成素材（无人声）下 Whisper 的静音判定不稳定（相邻两次转录可能
	// silences=1 或 0）。候选为 0 时不阻断——本测试关注的是字幕链路
	// （重对齐/导出/特效/烧录），候选扫描已由 TestRealASR_FullChain 覆盖，
	// 故降级为人工账本（删 [4,8) 段）继续验证。
	if len(items) == 0 {
		items = []edits.EditItem{{
			Start: 4.0, End: 8.0, Type: edits.TypeSilence,
			Reason: "人工构造（真实素材零候选降级）",
		}}
		t.Logf("② 候选为 0，降级为人工账本: %+v", items)
	}

	// ③ 剪切 + 合并成片
	// 真实 GUI 流程中 AICutSubmit 会把勾选后的最终账本写回 work/edits.json
	// （字幕步骤读盘的数据源）；本测试绕过 AICutGenerate 直接转录，故手动
	// 落盘一份与候选一致的账本，模拟该行为。
	if werr := os.WriteFile(filepath.Join(dir, "work", "edits.json"),
		mustLedgerJSON(t, source, items), 0o644); werr != nil {
		t.Fatalf("写回 edits.json 失败: %v", werr)
	}
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
		t.Fatalf("合并终态 %q: %s", mergeTask.Status, mergeTask.Error)
	}
	finalPath := mergeTask.OutputPath
	if finalPath == "" && len(mergeTask.Items) == 1 {
		finalPath = mergeTask.Items[0].OutputPath
	}
	if finalPath == "" || !fileExists(finalPath) {
		t.Fatalf("成片缺失: %q", finalPath)
	}
	t.Logf("③ 成片: %s", finalPath)

	// ④ 字幕重对齐 + 外显导出（与成片同目录）
	view, err := app.AICutExportSubtitles(dir, finalPath, "both", "talking-head", 20, 2)
	if err != nil {
		t.Fatalf("AICutExportSubtitles 失败: %v", err)
	}
	t.Logf("④ 字幕重对齐: %d 条（跨段断开 %d 处），成片 %.3fs",
		view.CueCount, view.SplitCount, view.FinalDuration)
	if view.CueCount == 0 {
		t.Fatal("重对齐后字幕为空（真实 signals 异常）")
	}
	// 产物不变式（§4.1）：时间轴合法 + 不越界
	if verr := validateViewCues(view); verr != nil {
		t.Fatalf("字幕自检失败: %v", verr)
	}
	if !fileExists(view.SRTPath) || !fileExists(view.ASSPath) {
		t.Fatalf("外显产物缺失: %s / %s", view.SRTPath, view.ASSPath)
	}
	srt, _ := os.ReadFile(view.SRTPath)
	if strings.Count(string(srt), "-->") != view.CueCount {
		t.Errorf("SRT 条目数 %d 与 cue_count %d 不一致",
			strings.Count(string(srt), "-->"), view.CueCount)
	}

	// ⑤ 二次加工（真实语料走规则引擎）
	plan, err := app.AICutAnimateCaptions(dir, finalPath, "", nil, 0)
	if err != nil {
		t.Fatalf("AICutAnimateCaptions 失败: %v", err)
	}
	t.Logf("⑤ 特效: selected=%v static=%v reasoning=%q",
		plan.Selected, plan.Static, plan.Reasoning)
	if plan.Analysis == nil {
		t.Fatal("analysis 不应为空（GUI 单一数据源）")
	}
	if !plan.Static {
		if plan.FXPath == "" || !fileExists(plan.FXPath) {
			t.Fatalf("edited_fx.ass 缺失: %s", plan.FXPath)
		}
		fx, _ := os.ReadFile(plan.FXPath)
		if !strings.Contains(string(fx), "Dialogue:") {
			t.Error("fx.ass 应含 Dialogue 行")
		}
	}

	// ⑥ 内嵌烧录（fx.ass 优先；静态态用 ass）+ 抽帧验证字幕像素
	assForBurn := view.ASSPath
	if !plan.Static && plan.FXPath != "" {
		assForBurn = plan.FXPath
	}
	burnID, err := app.AICutBurnSubtitles(finalPath, assForBurn, "", 0, "")
	if err != nil {
		t.Fatalf("AICutBurnSubtitles 失败: %v", err)
	}
	burnTask := waitTask(t, svc, burnID, 300*time.Second)
	if burnTask.Status != "completed" {
		t.Fatalf("烧录终态 %q: %s", burnTask.Status, burnTask.Error)
	}
	burned := burnTask.OutputPath
	if burned == "" && len(burnTask.Items) == 1 {
		burned = burnTask.Items[0].OutputPath
	}
	if burned == "" || !fileExists(burned) {
		t.Fatalf("烧录产物缺失: %q", burned)
	}
	// 烧录产物时长 ≈ 成片时长（音轨直复制）
	bp, err := app.Probe(burned)
	if err != nil {
		t.Fatalf("探测烧录产物失败: %v", err)
	}
	fp2, err := app.Probe(finalPath)
	if err != nil {
		t.Fatalf("探测成片失败: %v", err)
	}
	t.Logf("⑥ 烧录: %s 时长 %.3fs（成片 %.3fs）", burned, bp.Duration, fp2.Duration)
	if bp.Duration < fp2.Duration-1.0 || bp.Duration > fp2.Duration+1.0 {
		t.Errorf("烧录产物时长 %.3fs 与成片 %.3fs 偏差超 1s", bp.Duration, fp2.Duration)
	}
	if !bp.HasAudio {
		t.Error("烧录产物应保留音轨（-c:a copy）")
	}

	// ⑦ 抽帧验证：字幕时间中点的帧应有像素差异（字幕真的烧进去了）
	at := view.FinalDuration / 2
	if c := firstCueMid(view); c > 0 {
		at = c
	}
	diff := framePixelDiff(t, ff, finalPath, burned, at)
	t.Logf("⑦ 抽帧 @%.2fs: 烧录前后帧差异 YAVG=%.3f", at, diff)
	if diff < 0.5 {
		t.Errorf("帧差异 %.3f 过小，字幕可能未渲染（检查 libass/字体）", diff)
	}
}

// validateViewCues 复用内部自检（SubtitlesView.Cues → subtitles.ValidateCues）。
func validateViewCues(v *SubtitlesView) error {
	return subtitles.ValidateCues(v.Cues, v.FinalDuration)
}

// firstCueMid 首条 cue 中点（抽帧落在字幕可见区间）。
func firstCueMid(v *SubtitlesView) float64 {
	if len(v.Cues) == 0 {
		return 0
	}
	return (v.Cues[0].Start + v.Cues[0].End) / 2
}

// framePixelDiff 抽取 t 秒帧做 blend difference，返回 YAVG（差异均值）。
// 与 internal/mcpserver/subtitle_integration_test.go 同款（包隔离无法共享）。
func framePixelDiff(t *testing.T, ff, plain, burned string, at float64) float64 {
	t.Helper()
	tmp := t.TempDir()
	a := filepath.Join(tmp, "a.png")
	b := filepath.Join(tmp, "b.png")
	for src, dst := range map[string]string{plain: a, burned: b} {
		if out, err := execFF(t, ff,
			"-hide_banner", "-loglevel", "error", "-ss", strconv.FormatFloat(at, 'f', 2, 64),
			"-i", src, "-frames:v", "1", "-y", dst); err != nil {
			t.Fatalf("抽帧失败: %v\n%s", err, out)
		}
	}
	out, err := execFF(t, ff,
		"-hide_banner", "-loglevel", "info",
		"-i", b, "-i", a,
		"-filter_complex", "blend=all_mode=difference,signalstats,metadata=print:key=lavfi.signalstats.YAVG",
		"-f", "null", "-")
	if err != nil {
		t.Fatalf("像素对比失败: %v\n%s", err, out)
	}
	lines := strings.Split(string(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if idx := strings.Index(lines[i], "YAVG="); idx >= 0 {
			if f, ferr := strconv.ParseFloat(strings.TrimSpace(lines[i][idx+5:]), 64); ferr == nil {
				return f
			}
		}
	}
	return 0
}

func execFF(t *testing.T, ff string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, ff, args...).CombinedOutput()
	return string(out), err
}

// mustLedgerJSON 序列化 edits.json（version=1）。
func mustLedgerJSON(t *testing.T, source string, items []edits.EditItem) []byte {
	t.Helper()
	data, err := json.MarshalIndent(map[string]any{
		"version": 1,
		"source":  source,
		"items":   items,
	}, "", "  ")
	if err != nil {
		t.Fatalf("序列化账本失败: %v", err)
	}
	return data
}
