package mcpserver

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
	"testing"
	"time"

	"videocut/internal/asr"
	"videocut/internal/config"
	"videocut/internal/edits"
	"videocut/internal/service"
)

// locateBinariesV14 同 service 集成测试：向上查找 resources/ffmpeg。
func locateBinariesV14(t *testing.T) (string, string) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for dir := wd; ; dir = filepath.Dir(dir) {
		ff := filepath.Join(dir, "resources", "ffmpeg", "ffmpeg.exe")
		fp := filepath.Join(dir, "resources", "ffmpeg", "ffprobe.exe")
		if fileExistsV14(ff) && fileExistsV14(fp) {
			return ff, fp
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	t.Skip("ffmpeg/ffprobe not found under resources/ffmpeg")
	return "", ""
}

func fileExistsV14(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// fakeSidecar 起一个假 ASR sidecar（httptest）：
// /health 200；/transcribe 落盘 transcript.srt + signals.json；/hotwords 记录调用。
func fakeSidecar(t *testing.T, srtText string) (*httptest.Server, *[]string) {
	t.Helper()
	var hotwords []string
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/transcribe", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			VideoPath  string   `json:"video_path"`
			OutputBase string   `json:"output_base"`
			Language   string   `json:"language"`
			Hotwords   []string `json:"hotwords"`
			Model      string   `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		work := filepath.Join(req.OutputBase, "work")
		if err := os.MkdirAll(work, 0o755); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if err := os.WriteFile(filepath.Join(work, "transcript.srt"), []byte(srtText), 0o644); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		// 假信号：3.0-3.4s 有口水词“嗯”、40-41.5s 有静音；words 供 suggest 用
		src, _ := json.Marshal(filepath.ToSlash(req.VideoPath))
		sig := `{"source":` + string(src) + `,"duration":600,
		  "words":[{"start":3.0,"end":3.4,"word":"嗯"},{"start":10.0,"end":10.6,"word":"大家好"}],
		  "silences":[{"start":40,"end":41.5},{"start":100,"end":100.2}]}`
		if err := os.WriteFile(filepath.Join(work, "signals.json"), []byte(sig), 0o644); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"srt_path":"` + filepath.ToSlash(filepath.Join(work, "transcript.srt")) +
			`","signals_path":"` + filepath.ToSlash(filepath.Join(work, "signals.json")) +
			`","duration":600,"segments":2,"silences":1}`))
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

// asrConfigFor 把 cfg 的 ASR 指向测试 server。
func asrConfigFor(cfg *config.Config, srv *httptest.Server) {
	u, err := url.Parse(srv.URL)
	if err != nil {
		panic(err)
	}
	port, _ := strconv.Atoi(u.Port())
	cfg.ASRHost = u.Hostname()
	cfg.ASRPort = port
}

// TestIntegration_MCP_AICutFullChain 端到端（开发文档 §19.9）：
// 假 sidecar process → suggest → edits.json → validate → summary → 真实 FFmpeg 按保留段 cut → export。
func TestIntegration_MCP_AICutFullChain(t *testing.T) {
	ff, fp := locateBinariesV14(t)

	dir := t.TempDir()
	src := filepath.Join(dir, "demo.mp4")

	// 合成 45s 测试片（真实 cut 需要）
	gen := exec.CommandContext(context.Background(), ff,
		"-f", "lavfi", "-i", "testsrc2=size=320x240:rate=25",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=45",
		"-t", "45", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac",
		"-y", src,
	)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("generate source: %v\n%s", err, out)
	}

	srtText := "1\n00:00:00,000 --> 00:00:10,000\n大家好，欢迎来到演示\n\n2\n00:00:10,000 --> 00:00:20,000\n今天讲 AI 粗剪流程\n"
	srv, _ := fakeSidecar(t, srtText)

	cfg := config.Default()
	asrConfigFor(cfg, srv)
	cfg.TimeoutSeconds = 120

	svc := service.NewService(cfg, ff, fp)
	s := New(cfg, svc, asr.NewClient(cfg))
	ctx := context.Background()

	// ① process：假 sidecar 转录
	res, err := s.toolProcess(ctx, callReq("videocut_process", map[string]any{
		"video_path": src, "output_base": dir, "hotwords": []string{"videocut"},
	}))
	if err != nil || res.IsError {
		t.Fatalf("process 失败: %v %s", err, resultText(res))
	}

	// ② suggest：候选（嗯 3.0-3.4 filler；40-41.5 silence；100.2 段<1s 被滤除）
	res2, _ := s.toolSuggestEdits(ctx, callReq("videocut_suggest_edits", map[string]any{"base_dir": dir}))
	if res2.IsError {
		t.Fatalf("suggest 失败: %s", resultText(res2))
	}
	raw, _ := json.Marshal(res2.StructuredContent)
	var sug struct {
		Items    []edits.EditItem `json:"items"`
		Counts   map[string]int   `json:"counts"`
		Duration float64          `json:"duration"`
	}
	if err := json.Unmarshal(raw, &sug); err != nil {
		t.Fatalf("suggest 结构解析: %v", err)
	}
	if len(sug.Items) != 2 {
		t.Fatalf("候选 %d 期望 2（1 filler + 1 silence），得到 %+v", len(sug.Items), sug.Items)
	}

	// ③ 生成 edits.json 并落盘（模拟 Codex 写盘）
	ledger := edits.EditsLedger{Version: 1, Source: src, Items: sug.Items}
	ledgerJSON, _ := json.Marshal(ledger)
	editsPath := filepath.Join(dir, "edits.json")
	if err := os.WriteFile(editsPath, ledgerJSON, 0o644); err != nil {
		t.Fatal(err)
	}

	// ④ validate：source_duration 显式传 45（suggest 的假 duration=600 不可信）
	res4, _ := s.toolValidateEdits(ctx, callReq("videocut_validate_edits", map[string]any{
		"edits_json": string(ledgerJSON), "source_duration": 45,
	}))
	if res4.IsError {
		t.Fatalf("validate 失败: %s", resultText(res4))
	}

	// ⑤ summary：keep_ranges 应为 [0,3] [3.4,40] [41.5,45]
	res5, _ := s.toolGetEditsSummary(ctx, callReq("videocut_get_edits_summary", map[string]any{
		"edits_path": editsPath, "source_duration": 45,
	}))
	if res5.IsError {
		t.Fatalf("summary 失败: %s", resultText(res5))
	}
	raw5, _ := json.Marshal(res5.StructuredContent)
	var sum5 struct {
		KeepRanges []edits.KeepRange `json:"keep_ranges"`
	}
	if err := json.Unmarshal(raw5, &sum5); err != nil {
		t.Fatalf("summary 结构解析: %v", err)
	}
	if len(sum5.KeepRanges) != 3 {
		t.Fatalf("keep_ranges %d 期望 3: %+v", len(sum5.KeepRanges), sum5.KeepRanges)
	}
	var keepTotal float64
	for _, k := range sum5.KeepRanges {
		keepTotal += k.End - k.Start
	}
	// 45 - (0.4 + 1.5) = 43.1
	if keepTotal < 43.1-0.01 || keepTotal > 43.1+0.01 {
		t.Errorf("保留总时长 %v 期望 43.1", keepTotal)
	}

	// ⑥ cut：keep_ranges → TrimBatch（reencode 精确切分）
	items := keepRangesToBatchItems(src, sum5.KeepRanges, "reencode")
	taskID, err := svc.TrimBatch(service.BatchRequest{Items: items})
	if err != nil {
		t.Fatalf("TrimBatch: %v", err)
	}
	task := waitTaskFinalV14(t, svc, taskID, 120*time.Second)
	if task.Status != "completed" {
		t.Fatalf("cut 任务终态 %q（错误：%s）", task.Status, task.Error)
	}
	if len(task.Items) != 3 {
		t.Fatalf("子项 %d 期望 3", len(task.Items))
	}
	// 校验产物存在
	for i, it := range task.Items {
		if !fileExistsV14(it.OutputPath) {
			t.Errorf("子项 %d 产物不存在: %s", i, it.OutputPath)
		}
	}

	// ⑦ export：文字稿
	res7, _ := s.toolExportTranscript(ctx, callReq("videocut_export_transcript", map[string]any{
		"base_dir": dir, "format": "md",
	}))
	if res7.IsError {
		t.Fatalf("export 失败: %s", resultText(res7))
	}
	if !containsV14(resultText(res7), "AI 粗剪流程") {
		t.Errorf("导稿内容异常: %s", resultText(res7))
	}
}

// TestIntegration_MCP_ASRRuntimeMissing sidecar 缺失 → asr_runtime_missing + hint（§19.5/§19.9）。
func TestIntegration_MCP_ASRRuntimeMissing(t *testing.T) {
	cfg := config.Default()
	cfg.ASRHost = "127.0.0.1"
	cfg.ASRPort = 39998 // 无监听
	cfg.ASRPython = ""  // 无 venv
	svc := service.NewService(cfg, "", "")
	s := New(cfg, svc, asr.NewClient(cfg))

	res, _ := s.toolProcess(context.Background(), callReq("videocut_process", map[string]any{
		"video_path": "E:/x.mp4", "output_base": "E:/out",
	}))
	if !res.IsError {
		t.Fatal("sidecar 缺失应报错")
	}
	sc, _ := res.StructuredContent.(map[string]any)
	if sc["code"] != codeASRRuntimeMiss {
		t.Errorf("code=%v 期望 asr_runtime_missing", sc["code"])
	}
	hint, _ := sc["hint"].(string)
	if !containsV14(hint, "bootstrap-asr.ps1") {
		t.Errorf("hint 应含 bootstrap 脚本指引: %q", hint)
	}
}

func waitTaskFinalV14(t *testing.T, svc *service.Service, id string, timeout time.Duration) service.Task {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		tk, ok := svc.GetTaskSnapshot(id)
		if !ok {
			t.Fatalf("task %s not found", id)
		}
		switch tk.Status {
		case "completed", "partial", "failed":
			return tk
		}
		time.Sleep(150 * time.Millisecond)
	}
	tk, _ := svc.GetTaskSnapshot(id)
	t.Fatalf("task %s 未在 %v 内完成, status=%q err=%s", id, timeout, tk.Status, tk.Error)
	return service.Task{}
}

func containsV14(s, sub string) bool {
	return len(s) >= len(sub) && (indexOfV14(s, sub) >= 0)
}

func indexOfV14(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
