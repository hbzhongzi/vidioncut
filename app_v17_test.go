package main

// M11 GUI 绑定链路测试（开发文档 §19.9）：直接调用 App 暴露给前端的 App 方法，
// 用真实 ffmpeg + 假 ASR sidecar 跑通 AI 粗剪全链路，确保前端对接无链路断裂。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"videocut/internal/asr"
	"videocut/internal/config"
	"videocut/internal/service"
)

// locateFF 向上查找 resources/ffmpeg/{ffmpeg,ffprobe}.exe。
func locateFF(t *testing.T) (string, string) {
	t.Helper()
	wd, _ := os.Getwd()
	for dir := wd; ; dir = filepath.Dir(dir) {
		ff := filepath.Join(dir, "resources", "ffmpeg", "ffmpeg.exe")
		fp := filepath.Join(dir, "resources", "ffmpeg", "ffprobe.exe")
		if fileExists(ff) && fileExists(fp) {
			return ff, fp
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	t.Skip("ffmpeg/ffprobe 未就位（运行 scripts/copy-ffmpeg.ps1）")
	return "", ""
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func TestASRStatus_NilHotwordsReturnsEmptyArray(t *testing.T) {
	cfg := config.Default()
	cfg.ASRHotwords = nil
	app := NewApp(cfg, service.NewService(cfg, "", ""), asr.NewClient(cfg))

	status := app.ASRStatus()
	if status.Hotwords == nil {
		t.Fatal("ASRStatus.Hotwords must be an empty array, not nil")
	}
	if got := len(status.Hotwords); got != 0 {
		t.Fatalf("ASRStatus.Hotwords length = %d, want 0", got)
	}
}

// fakeASR 起假 sidecar：/health 200、/transcribe 落盘 transcript.srt+signals.json、/hotwords 记录。
func fakeASR(t *testing.T, srt, signals string) (*httptest.Server, *[]string) {
	t.Helper()
	var hotwords []string
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/transcribe", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			VideoPath  string `json:"video_path"`
			OutputBase string `json:"output_base"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		work := filepath.Join(req.OutputBase, "work")
		if err := os.MkdirAll(work, 0o755); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if err := os.WriteFile(filepath.Join(work, "transcript.srt"), []byte(srt), 0o644); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if err := os.WriteFile(filepath.Join(work, "signals.json"), []byte(signals), 0o644); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"srt_path":"` + filepath.ToSlash(filepath.Join(work, "transcript.srt")) +
			`","signals_path":"` + filepath.ToSlash(filepath.Join(work, "signals.json")) +
			`","duration":45,"segments":2,"silences":1}`))
	})
	mux.HandleFunc("/hotwords", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Hotwords []string `json:"hotwords"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		hotwords = body.Hotwords
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &hotwords
}

// genSource 用 ffmpeg 合成指定秒数的测试片。
func genSource(t *testing.T, ff, dir string, secs int) string {
	t.Helper()
	src := filepath.Join(dir, "demo.mp4")
	cmd := exec.CommandContext(context.Background(), ff,
		"-f", "lavfi", "-i", "testsrc2=size=320x240:rate=25",
		"-f", "lavfi", "-i", "sine=frequency=440:duration="+strconv.Itoa(secs),
		"-t", strconv.Itoa(secs), "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac",
		"-y", src,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("合成源视频失败: %v\n%s", err, out)
	}
	return src
}

func waitTask(t *testing.T, svc *service.Service, id string, timeout time.Duration) service.Task {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		tk, ok := svc.GetTaskSnapshot(id)
		if !ok {
			t.Fatalf("任务 %s 不存在", id)
		}
		switch tk.Status {
		case "completed", "partial", "failed":
			return tk
		}
		time.Sleep(150 * time.Millisecond)
	}
	tk, _ := svc.GetTaskSnapshot(id)
	t.Fatalf("任务 %s 在 %v 内未完成, status=%q err=%s", id, timeout, tk.Status, tk.Error)
	return service.Task{}
}

// TestAICutFullChain_GUI 端到端验证 GUI 绑定层 AI 粗剪全链路（M11 对接）。
func TestAICutFullChain_GUI(t *testing.T) {
	ff, fp := locateFF(t)
	dir := t.TempDir()
	src := genSource(t, ff, dir, 45)

	srt := "1\n00:00:00,000 --> 00:00:10,000\n大家好，欢迎来到演示\n\n2\n00:00:10,000 --> 00:00:20,000\n今天讲 AI 粗剪流程\n"
	// 候选：3.0-3.4 口水词"嗯"；40-41.5 静音(≥1s 保留)；100-100.2 静音(<1s 滤除)。
	signals := `{"source":"` + filepath.ToSlash(src) + `","duration":45,` +
		`"words":[{"start":3.0,"end":3.4,"word":"嗯"},{"start":10.0,"end":10.6,"word":"大家好"}],` +
		`"silences":[{"start":40,"end":41.5},{"start":100,"end":100.2}]}`
	srv, hotwords := fakeASR(t, srt, signals)

	cfg := config.Default()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	cfg.ASRHost = u.Hostname()
	cfg.ASRPort = port
	cfg.ASREnabled = true
	cfg.TimeoutSeconds = 120

	svc := service.NewService(cfg, ff, fp)
	app := NewApp(cfg, svc, asr.NewClient(cfg))
	ctx := context.Background()

	// ① 生成候选（转录 + 机械扫描 + 写盘）
	view, err := app.AICutGenerate(src, dir)
	if err != nil {
		t.Fatalf("AICutGenerate 失败: %v", err)
	}
	if view == nil || len(view.Ledger.Items) != 2 {
		t.Fatalf("候选数 %d 期望 2", len(view.Ledger.Items))
	}
	if !view.Valid {
		t.Fatalf("账本校验未通过: %s", view.ValidationError)
	}
	if len(view.KeepRanges) != 3 {
		t.Fatalf("保留段 %d 期望 3: %+v", len(view.KeepRanges), view.KeepRanges)
	}
	// edits.json 已落盘
	if !fileExists(filepath.Join(dir, "work", "edits.json")) {
		t.Fatal("work/edits.json 未落盘")
	}

	// ② 逐项开关后实时重算（默认全启用，应与生成一致）
	view2, err := app.AICutCompute(src, view.Ledger.Items)
	if err != nil {
		t.Fatalf("AICutCompute 失败: %v", err)
	}
	if len(view2.KeepRanges) != 3 {
		t.Fatalf("重算保留段 %d 期望 3", len(view2.KeepRanges))
	}

	// ②b 取消全部删除项 → 合法的"全部保留"状态（v1.8：此前误报校验失败）
	view2b, err := app.AICutCompute(src, nil)
	if err != nil {
		t.Fatalf("AICutCompute(空) 失败: %v", err)
	}
	if !view2b.Valid {
		t.Fatalf("空删除项应有效: %s", view2b.ValidationError)
	}
	if len(view2b.KeepRanges) != 1 || view2b.KeepRanges[0].End != 45 {
		t.Fatalf("空删除项应保留全片: %+v", view2b.KeepRanges)
	}

	// ③ 提交保留段裁剪（reencode 精确切分）
	taskID, err := app.AICutSubmit(src, view.Ledger.Items, "reencode")
	if err != nil {
		t.Fatalf("AICutSubmit 失败: %v", err)
	}
	cutTask := waitTask(t, svc, taskID, 120*time.Second)
	if cutTask.Status != "completed" {
		t.Fatalf("裁剪终态 %q 错误: %s", cutTask.Status, cutTask.Error)
	}
	if len(cutTask.Items) != 3 {
		t.Fatalf("子片段 %d 期望 3", len(cutTask.Items))
	}
	for i, it := range cutTask.Items {
		if !fileExists(it.OutputPath) {
			t.Errorf("子片段 %d 产物缺失: %s", i, it.OutputPath)
		}
	}

	// ④ 合并为单成片
	mergeID, err := app.MergeCutSegments(taskID, "")
	if err != nil {
		t.Fatalf("MergeCutSegments 失败: %v", err)
	}
	mergeTask := waitTask(t, svc, mergeID, 120*time.Second)
	if mergeTask.Status != "completed" {
		t.Fatalf("合并终态 %q 错误: %s", mergeTask.Status, mergeTask.Error)
	}
	if mergeTask.OutputPath == "" || !fileExists(mergeTask.OutputPath) {
		t.Fatalf("合并产物缺失: %q", mergeTask.OutputPath)
	}
	// 派生名应为 demo_cut.mp4
	if !strings.HasSuffix(mergeTask.OutputPath, "demo_cut.mp4") {
		t.Errorf("合并产物命名异常: %s", mergeTask.OutputPath)
	}

	// ⑤ 导稿
	mdPath, err := app.ExportTranscriptFile(dir, "md")
	if err != nil {
		t.Fatalf("ExportTranscriptFile 失败: %v", err)
	}
	if !fileExists(mdPath) {
		t.Fatalf("导稿产物缺失: %s", mdPath)
	}
	md, _ := os.ReadFile(mdPath)
	if !strings.Contains(string(md), "AI 粗剪流程") {
		t.Errorf("导稿内容异常: %s", md)
	}

	// ⑥ 回读账本
	ledgerPath := filepath.Join(dir, "work", "edits.json")
	rv, err := app.ReadEditsLedger(ledgerPath)
	if err != nil {
		t.Fatalf("ReadEditsLedger 失败: %v", err)
	}
	if !rv.Valid || len(rv.Ledger.Items) != 2 {
		t.Fatalf("回读账本异常: valid=%v items=%d", rv.Valid, len(rv.Ledger.Items))
	}

	// ⑦ ASR 状态 / 热词 / 日志（不应 panic）
	st := app.ASRStatus()
	if !st.Healthy {
		t.Logf("提示: ASRStatus.Healthy=false（测试使用假 sidecar，端口健康探测应成功）")
	}
	if err := app.SetASRHotwords([]string{"videocut", "AI粗剪"}); err != nil {
		t.Fatalf("SetASRHotwords 失败: %v", err)
	}
	if len(*hotwords) != 2 {
		t.Errorf("热词未送达 sidecar: %+v", *hotwords)
	}
	log := app.ReadMCPCallLog(10)
	_ = log // GUI 侧不记 MCP 日志，仅验证不 panic
	_ = ctx
}

// TestAICutSubmit_AllKeep_GUI 验证取消全部删除候选后仍可提交“全部保留”。
func TestAICutSubmit_AllKeep_GUI(t *testing.T) {
	ff, fp := locateFF(t)
	dir := t.TempDir()
	src := genSource(t, ff, dir, 2)
	cfg := config.Default()
	cfg.TimeoutSeconds = 120
	svc := service.NewService(cfg, ff, fp)
	app := NewApp(cfg, svc, asr.NewClient(cfg))

	id, err := app.AICutSubmit(src, nil, "reencode")
	if err != nil {
		t.Fatalf("全部保留不应提交失败: %v", err)
	}
	tk := waitTask(t, svc, id, 60*time.Second)
	if tk.Status != "completed" {
		t.Fatalf("全部保留任务未完成: %s %s", tk.Status, tk.Error)
	}
	if len(tk.Items) != 1 || !fileExists(tk.Items[0].OutputPath) {
		t.Fatalf("全部保留应生成一个完整片段: %+v", tk.Items)
	}
}

// TestAICutRuntimeMissing_GUI 验证 sidecar 缺失时 GUI 绑定优雅降级（返回 error，不拉起 python/不挂起）。
func TestAICutRuntimeMissing_GUI(t *testing.T) {
	ff, fp := locateFF(t)
	dir := t.TempDir()
	src := genSource(t, ff, dir, 20)

	cfg := config.Default()
	cfg.ASRHost = "127.0.0.1"
	cfg.ASRPort = 49321                         // 无监听
	cfg.ASRPython = `C:\no-such-python-xyz.exe` // 强制 resolvePython 立即判定缺失，避免误拉起真实 sidecar
	cfg.ASREnabled = true
	svc := service.NewService(cfg, ff, fp)
	app := NewApp(cfg, svc, asr.NewClient(cfg))

	_, err := app.AICutGenerate(src, dir)
	if err == nil {
		t.Fatal("sidecar 缺失应返回 error")
	}
	// 降级信息应明确指向 bootstrap 脚本
	if !strings.Contains(err.Error(), "asr") {
		t.Errorf("降级信息未提及 ASR 缺失: %v", err)
	}
}

// TestAICutGenerate_NoCandidates 干净素材（无静音/口水词）应给出明确错误，
// 而非返回一本 "items 不能为空" 的无效账本（v1.8 修复）。
func TestAICutGenerate_NoCandidates(t *testing.T) {
	ff, fp := locateFF(t)
	dir := t.TempDir()
	src := genSource(t, ff, dir, 20)

	srt := "1\n00:00:00,000 --> 00:00:20,000\n干净素材无静音无口水词\n"
	// words 不含词表命中、silences 为空 → 候选 0 项
	signals := `{"source":"` + filepath.ToSlash(src) + `","duration":20,` +
		`"words":[{"start":1.0,"end":1.5,"word":"干净"}],"silences":[]}`
	srv, _ := fakeASR(t, srt, signals)

	cfg := config.Default()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	cfg.ASRHost = u.Hostname()
	cfg.ASRPort = port
	cfg.ASREnabled = true
	cfg.TimeoutSeconds = 120

	svc := service.NewService(cfg, ff, fp)
	app := NewApp(cfg, svc, asr.NewClient(cfg))

	_, err := app.AICutGenerate(src, dir)
	if err == nil {
		t.Fatal("无候选应返回 error")
	}
	if !strings.Contains(err.Error(), "未扫描到候选") {
		t.Errorf("错误信息不明确: %v", err)
	}
	// 失败时不得留下半成品 edits.json
	if fileExists(filepath.Join(dir, "work", "edits.json")) {
		t.Error("无候选时不应写盘 edits.json")
	}
}

// TestAICutGenerate_OverlapSignals 真实 Whisper 词级交叠数据经 GUI 全链
// 不得断裂（v1.8 修复：候选合并保证账本可校验）。
func TestAICutGenerate_OverlapSignals(t *testing.T) {
	ff, fp := locateFF(t)
	dir := t.TempDir()
	src := genSource(t, ff, dir, 30)

	srt := "1\n00:00:00,000 --> 00:00:30,000\n嗯 呃 然后呢 就是\n"
	// 相邻 token 交叠（DTW 伪影）+ 口水词落在静音段内
	signals := `{"source":"` + filepath.ToSlash(src) + `","duration":30,` +
		`"words":[{"start":3.0,"end":3.4,"word":"嗯"},{"start":3.35,"end":3.7,"word":"呃，"},` +
		`{"start":10.0,"end":10.6,"word":"然后呢"},{"start":10.55,"end":11.0,"word":"就是"}],` +
		`"silences":[{"start":20,"end":22}]}`
	srv, _ := fakeASR(t, srt, signals)

	cfg := config.Default()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	cfg.ASRHost = u.Hostname()
	cfg.ASRPort = port
	cfg.ASREnabled = true
	cfg.TimeoutSeconds = 120

	svc := service.NewService(cfg, ff, fp)
	app := NewApp(cfg, svc, asr.NewClient(cfg))

	view, err := app.AICutGenerate(src, dir)
	if err != nil {
		t.Fatalf("AICutGenerate 失败: %v", err)
	}
	if !view.Valid {
		t.Fatalf("账本校验未通过: %s", view.ValidationError)
	}
	// 嗯+呃 → 1 项；然后呢+就是 → 1 项；静音 → 1 项 = 3 项
	if len(view.Ledger.Items) != 3 {
		t.Fatalf("候选 %d 期望 3（两组合并 + 静音）: %+v", len(view.Ledger.Items), view.Ledger.Items)
	}
	// 全链可提交剪切
	taskID, err := app.AICutSubmit(src, view.Ledger.Items, "reencode")
	if err != nil {
		t.Fatalf("AICutSubmit 失败: %v", err)
	}
	cutTask := waitTask(t, svc, taskID, 120*time.Second)
	if cutTask.Status != "completed" {
		t.Fatalf("裁剪终态 %q: %s", cutTask.Status, cutTask.Error)
	}
}
