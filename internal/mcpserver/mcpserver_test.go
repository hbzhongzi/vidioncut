package mcpserver

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"videocut/internal/config"
	"videocut/internal/edits"
	"videocut/internal/service"
)

func newTestServer() *Server {
	cfg := config.Default()
	svc := service.NewService(cfg, "", "") // 无 FFmpeg：仅纯逻辑路径可过
	return New(cfg, svc, nil)
}

func callReq(name string, args map[string]any) mcp.CallToolRequest {
	r := mcp.CallToolRequest{}
	r.Params.Name = name
	r.Params.Arguments = args
	return r
}

// resultText 提取 text content（SDK 双载体：text + structuredContent）。
func resultText(res *mcp.CallToolResult) string {
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

func TestRegisterTools_All15WithSchema(t *testing.T) {
	s := newTestServer()
	tools := s.MCPServer().ListTools()
	want := []string{
		"videocut_probe", "videocut_trim", "videocut_trim_batch", "videocut_merge",
		"videocut_get_task", "videocut_cancel", "videocut_export_transcript",
		"videocut_validate_edits", "videocut_get_edits_summary",
		"videocut_process", "videocut_suggest_edits", "videocut_set_hotwords",
		// M12 字幕族（§3.3）
		"videocut_export_subtitles", "videocut_burn_subtitles", "videocut_animate_captions",
	}
	if len(tools) != len(want) {
		t.Fatalf("注册工具数 %d，期望 %d", len(tools), len(want))
	}
	for _, n := range want {
		if _, ok := tools[n]; !ok {
			t.Errorf("缺少工具: %s", n)
		}
	}
	// schema 断言：trim 必填 input_path/start/end
	trim := tools["videocut_trim"]
	if trim == nil {
		t.Fatal("videocut_trim 未注册")
	}
	req := trim.Tool.InputSchema.Required
	has := map[string]bool{}
	for _, r := range req {
		has[r] = true
	}
	for _, k := range []string{"input_path", "start", "end"} {
		if !has[k] {
			t.Errorf("videocut_trim schema 缺少必填 %s（Required=%v）", k, req)
		}
	}
	if _, ok := trim.Tool.InputSchema.Properties["input_path"]; !ok {
		t.Error("videocut_trim schema 缺少 input_path 属性定义")
	}
}

func TestFailResult_Structure(t *testing.T) {
	res := failResult("some_code", "some hint", "boom")
	if !res.IsError {
		t.Error("failResult 必须置 IsError=true")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(resultText(res)), &payload); err != nil {
		t.Fatalf("text content 不是 JSON: %v", err)
	}
	for _, k := range []string{"error", "code", "hint"} {
		if payload[k] == nil {
			t.Errorf("错误结构缺少字段 %s: %v", k, payload)
		}
	}
	if payload["code"] != "some_code" {
		t.Errorf("code=%v", payload["code"])
	}
	// structuredContent 同步携带
	sc, ok := res.StructuredContent.(map[string]any)
	if !ok || sc["code"] != "some_code" {
		t.Errorf("StructuredContent 异常: %v", res.StructuredContent)
	}
}

func TestValidateEdits_Tool(t *testing.T) {
	s := newTestServer()
	ctx := context.Background()

	// 合法账本（无 source_duration、source 文件不存在 → 跳过上界校验）
	ledger := `{"version":1,"source":"E:/nope/demo.mp4","items":[{"start":1,"end":2,"type":"filler_word"},{"start":5,"end":6,"type":"silence"}]}`
	res, err := s.toolValidateEdits(ctx, callReq("videocut_validate_edits", map[string]any{"edits_json": ledger}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("合法账本不应报错: %s", resultText(res))
	}
	sc, _ := res.StructuredContent.(map[string]any)
	if sc["valid"] != true {
		t.Errorf("valid 字段异常: %v", sc)
	}

	// 重叠账本 → edits_invalid + hint
	bad := `{"version":1,"source":"E:/nope/demo.mp4","items":[{"start":1,"end":5,"type":"silence"},{"start":4,"end":6,"type":"silence"}]}`
	res2, _ := s.toolValidateEdits(ctx, callReq("videocut_validate_edits", map[string]any{"edits_json": bad}))
	if !res2.IsError {
		t.Fatal("重叠账本应报错")
	}
	sc2, _ := res2.StructuredContent.(map[string]any)
	if sc2["code"] != codeEditsInvalid {
		t.Errorf("code=%v 期望 edits_invalid", sc2["code"])
	}
	if !strings.Contains(resultText(res2), "重叠") {
		t.Errorf("错误信息应含“重叠”: %s", resultText(res2))
	}

	// 缺失参数 → invalid_params
	res3, _ := s.toolValidateEdits(ctx, callReq("videocut_validate_edits", map[string]any{}))
	if !res3.IsError {
		t.Error("缺 edits_json 应报错")
	}
	sc3, _ := res3.StructuredContent.(map[string]any)
	if sc3["code"] != codeInvalidParams {
		t.Errorf("code=%v", sc3["code"])
	}
}

func TestGetTask_NotFound(t *testing.T) {
	s := newTestServer()
	res, _ := s.toolGetTask(context.Background(), callReq("videocut_get_task", map[string]any{"task_id": "trim_nope"}))
	if !res.IsError {
		t.Fatal("不存在任务应报错")
	}
	sc, _ := res.StructuredContent.(map[string]any)
	if sc["code"] != codeTaskNotFound {
		t.Errorf("code=%v 期望 task_not_found", sc["code"])
	}
}

func TestExportTranscript_Tool(t *testing.T) {
	dir := t.TempDir()
	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	srt := "1\n00:00:00,120 --> 00:00:03,480\n大家好\n\n2\n00:00:03,520 --> 00:00:08,960\n今天聊设计\n"
	if err := os.WriteFile(filepath.Join(work, "transcript.srt"), []byte(srt), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newTestServer()
	res, _ := s.toolExportTranscript(context.Background(), callReq("videocut_export_transcript", map[string]any{
		"base_dir": dir, "format": "md",
	}))
	if res.IsError {
		t.Fatalf("导出失败: %s", resultText(res))
	}
	if !strings.Contains(resultText(res), "# 文字稿") || !strings.Contains(resultText(res), "大家好") {
		t.Errorf("导出内容异常: %s", resultText(res))
	}

	// 缺失文件 → 报错
	res2, _ := s.toolExportTranscript(context.Background(), callReq("videocut_export_transcript", map[string]any{
		"base_dir": filepath.Join(dir, "empty"),
	}))
	if !res2.IsError {
		t.Error("缺 SRT 应报错")
	}
}

func TestSuggestEdits_FromSignals(t *testing.T) {
	dir := t.TempDir()
	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	sig := `{"source":"E:/clips/demo.mp4","duration":100,
	  "words":[{"start":1.0,"end":1.4,"word":"嗯"},{"start":2.0,"end":2.5,"word":"大家好"},{"start":9.0,"end":9.4,"word":"然后"}],
	  "silences":[{"start":30,"end":33.5},{"start":50,"end":50.4}]}`
	if err := os.WriteFile(filepath.Join(work, "signals.json"), []byte(sig), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newTestServer()
	res, _ := s.toolSuggestEdits(context.Background(), callReq("videocut_suggest_edits", map[string]any{"base_dir": dir}))
	if res.IsError {
		t.Fatalf("suggest 失败: %s", resultText(res))
	}
	// StructuredContent 保留原始 Go 类型；经 JSON 往返后统一为 float64
	raw, _ := json.Marshal(res.StructuredContent)
	var sc map[string]any
	_ = json.Unmarshal(raw, &sc)
	counts, _ := sc["counts"].(map[string]any)
	if counts["filler_word"] != float64(2) {
		t.Errorf("filler_word 候选数 %v 期望 2（嗯/然后）", counts["filler_word"])
	}
	if counts["silence"] != float64(1) {
		t.Errorf("silence 候选数 %v 期望 1（≥1s 的仅 3.5s 段）", counts["silence"])
	}
}

func TestProcess_NoASRClient(t *testing.T) {
	s := newTestServer() // asr=nil
	res, _ := s.toolProcess(context.Background(), callReq("videocut_process", map[string]any{
		"video_path": "E:/x.mp4", "output_base": "E:/out",
	}))
	if !res.IsError {
		t.Fatal("无 sidecar 应报错")
	}
	sc, _ := res.StructuredContent.(map[string]any)
	if sc["code"] != codeASRRuntimeMiss {
		t.Errorf("code=%v 期望 asr_runtime_missing", sc["code"])
	}
	if hint, _ := sc["hint"].(string); !strings.Contains(hint, "bootstrap-asr") {
		t.Errorf("hint 应指向 bootstrap 脚本: %v", hint)
	}
}

func TestKeepRangesToBatchItems(t *testing.T) {
	keeps := []edits.KeepRange{{Start: 0, End: 12.4}, {Start: 13.1, End: 45}, {Start: 47.5, End: 100}}
	items := keepRangesToBatchItems("E:/demo.mp4", keeps, "")
	if len(items) != 3 {
		t.Fatalf("items %d 期望 3", len(items))
	}
	for i, it := range items {
		if it.InputPath != "E:/demo.mp4" {
			t.Errorf("item %d InputPath=%s", i, it.InputPath)
		}
		if it.Strategy != "reencode" {
			t.Errorf("item %d Strategy=%s 期望 reencode（删词必须精确切分）", i, it.Strategy)
		}
	}
	if items[1].Start != 13.1 || items[1].End != 45 {
		t.Errorf("item 1 区间异常: %+v", items[1])
	}
}

func TestMatchFiller(t *testing.T) {
	cases := map[string]bool{
		"嗯": true, "呃": true, "然后": true, "OK": true, "okay": true,
		"大家好": false, "设计": false,
		// v1.8：真实 Whisper token 常带标点/语气后缀，剥离后应命中
		"那个，": true, "嗯！": true, "就是啊": true, "然后呢": true, " Okay ": true,
		// 英文整词匹配，避免子串误命中
		"look": false, "books": false,
		// 纯标点/空 token 不得命中
		"，。": false, "": false,
	}
	for w, want := range cases {
		if got := matchFiller(w); got != want {
			t.Errorf("matchFiller(%q)=%v 期望 %v", w, got, want)
		}
	}
}

// TestBuildCandidateItems_CoalesceOverlap 验证真实 Whisper 词级时间戳伪影
// （相邻 token 交叠 + 口水词落在静音段内）不会产出重叠账本（v1.8 修复）。
// 此前假 sidecar 测试无交叠数据，该断裂点在真实素材上才暴露。
func TestBuildCandidateItems_CoalesceOverlap(t *testing.T) {
	dir := t.TempDir()
	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	sig := `{"source":"E:/clips/demo.mp4","duration":60,
	  "words":[
	    {"start":3.00,"end":3.40,"word":"嗯"},
	    {"start":3.35,"end":3.70,"word":"呃，"},
	    {"start":10.0,"end":10.6,"word":"然后呢"},
	    {"start":10.55,"end":11.0,"word":"就是"},
	    {"start":30.2,"end":30.6,"word":"那个"}
	  ],
	  "silences":[{"start":30,"end":31},{"start":50,"end":52}]}`
	if err := os.WriteFile(filepath.Join(work, "signals.json"), []byte(sig), 0o644); err != nil {
		t.Fatal(err)
	}

	items, source, dur, err := BuildCandidateItems(dir, 1.0)
	if err != nil {
		t.Fatalf("BuildCandidateItems 失败: %v", err)
	}
	// 期望 4 项：[3.0,3.7]（嗯+呃合并）、[10,11]（然后呢+就是合并）、
	// [30,31]（静音吸收"那个"，类型 silence）、[50,52]
	if len(items) != 4 {
		t.Fatalf("候选 %d 期望 4: %+v", len(items), items)
	}
	want := []struct {
		start, end float64
		typ        string
	}{
		{3.0, 3.7, edits.TypeFillerWord},
		{10.0, 11.0, edits.TypeFillerWord},
		{30.0, 31.0, edits.TypeSilence},
		{50.0, 52.0, edits.TypeSilence},
	}
	for i, w := range want {
		got := items[i]
		if got.Start != w.start || got.End != w.end || got.Type != w.typ {
			t.Errorf("第 %d 项 = [%.2f,%.2f] %s，期望 [%.2f,%.2f] %s",
				i+1, got.Start, got.End, got.Type, w.start, w.end, w.typ)
		}
	}
	// 合并区间应保留文本与合并理由，供人工复核
	if !strings.Contains(items[0].Text, "嗯") || !strings.Contains(items[0].Text, "呃") {
		t.Errorf("合并项 text 丢失原文: %q", items[0].Text)
	}
	if !strings.Contains(items[2].Reason, "静音") || !strings.Contains(items[2].Reason, "口水词") {
		t.Errorf("合并项 reason 应含两类来源: %q", items[2].Reason)
	}
	// 核心不变式：产物必须直接通过账本校验（此前此处必然重叠失败）
	ledger := &edits.EditsLedger{Version: edits.LedgerVersion, Source: source, Items: items}
	if err := edits.ValidateEdits(ledger, dur); err != nil {
		t.Fatalf("合并后账本仍未通过校验: %v", err)
	}
}

// TestBuildCandidateItems_MinSilenceGuard 验证 min_silence 防御（v1.8）：
// NaN/Inf 静默失效、≤0 候选风暴、过大无产出，都必须显式报错。
func TestBuildCandidateItems_MinSilenceGuard(t *testing.T) {
	dir := t.TempDir()
	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	sig := `{"source":"x.mp4","duration":10,"words":[],"silences":[{"start":1,"end":1.2}]}`
	if err := os.WriteFile(filepath.Join(work, "signals.json"), []byte(sig), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		if _, _, _, err := BuildCandidateItems(dir, bad); err == nil {
			t.Errorf("min_silence=%v 应报错", bad)
		}
	}
	// 合法值照常工作
	if items, _, _, err := BuildCandidateItems(dir, 1.0); err != nil || len(items) != 0 {
		t.Errorf("min_silence=1.0 应滤除 0.2s 段且无错: items=%+v err=%v", items, err)
	}
}
