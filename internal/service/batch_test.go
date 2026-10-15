package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"videocut/internal/config"
)

// newLogicService 构造不依赖 FFmpeg 二进制的 Service（路径留空，
// 仅用于纯逻辑测试；涉及 probe 的用例在集成测试中覆盖）。
func newLogicService() *Service {
	return newTestService("", "")
}

// writeFile 创建占位文件（存在性校验用）。
func writeFile(p string) error {
	return os.WriteFile(p, []byte("x"), 0o644)
}

func TestAggregateProgress(t *testing.T) {
	mk := func(st ItemStatus, p float64) TrimItem {
		return TrimItem{Status: st, Progress: p}
	}
	durs := []float64{10, 20, 30}

	cases := []struct {
		name   string
		items  []TrimItem
		durs   []float64
		want   float64
		approx bool
	}{
		{"空列表", nil, nil, 0, false},
		{"全 pending", []TrimItem{mk(ItemStatusPending, 0), mk(ItemStatusPending, 0)}, durs[:2], 0, false},
		{"时长加权：1 项完成", []TrimItem{mk(ItemStatusCompleted, 100), mk(ItemStatusPending, 0), mk(ItemStatusPending, 0)}, durs, float64(10) / 60 * 100, true},
		{"时长加权：1 项失败也计完成量", []TrimItem{mk(ItemStatusFailed, 0), mk(ItemStatusPending, 0), mk(ItemStatusPending, 0)}, durs, float64(10) / 60 * 100, true},
		{"时长加权：处理中 50%", []TrimItem{mk(ItemStatusCompleted, 100), mk(ItemStatusProcessing, 50), mk(ItemStatusPending, 0)}, durs, float64(10+10) / 60 * 100, true},
		{"时长加权：全部完成=100", []TrimItem{mk(ItemStatusCompleted, 100), mk(ItemStatusCompleted, 100), mk(ItemStatusCompleted, 100)}, durs, 100, true},
		{"时长未知退化计数法：1/2", []TrimItem{mk(ItemStatusCompleted, 100), mk(ItemStatusPending, 0)}, []float64{0, 0}, 50, true},
		{"时长未知退化计数法：0/2", []TrimItem{mk(ItemStatusPending, 0), mk(ItemStatusPending, 0)}, []float64{0, 0}, 0, true},
		{"时长未知退化计数法：2/2", []TrimItem{mk(ItemStatusCompleted, 100), mk(ItemStatusFailed, 0)}, []float64{0, 0}, 100, true},
		{"durations 短于 items 不 panic", []TrimItem{mk(ItemStatusCompleted, 100), mk(ItemStatusProcessing, 50)}, []float64{10}, float64(10) / 10 * 100, true},
	}
	for _, c := range cases {
		got := aggregateProgress(c.items, c.durs)
		if c.approx {
			if got < c.want-0.01 || got > c.want+0.01 {
				t.Errorf("%s: aggregateProgress = %.4f, want ~%.4f", c.name, got, c.want)
			}
		} else if got != c.want {
			t.Errorf("%s: aggregateProgress = %.4f, want %.4f", c.name, got, c.want)
		}
	}
}

func TestAppendSeqAvoiding(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "out.mp4")
	taken := map[string]bool{}

	// 首次：原路径
	if got := appendSeqAvoiding(base, taken); got != base {
		t.Errorf("first = %q, want %q", got, base)
	}
	taken[base] = true
	// taken 命中：跳 _1
	if got := appendSeqAvoiding(base, taken); got != filepath.Join(dir, "out_1.mp4") {
		t.Errorf("second = %q, want out_1.mp4", got)
	}
	taken[filepath.Join(dir, "out_1.mp4")] = true
	// _1 亦被占用：跳 _2
	if got := appendSeqAvoiding(base, taken); got != filepath.Join(dir, "out_2.mp4") {
		t.Errorf("third = %q, want out_2.mp4", got)
	}
	// taken 为 nil 时退化为纯存在性判断
	if got := appendSeqAvoiding(filepath.Join(dir, "none.mp4"), nil); got != filepath.Join(dir, "none.mp4") {
		t.Errorf("nil taken = %q, want 原路径", got)
	}
}

func TestTrimBatch_AtomicReject(t *testing.T) {
	svc := newLogicService()
	dir := t.TempDir()
	real := filepath.Join(dir, "a.mp4")
	if err := writeFile(real); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		req  BatchRequest
	}{
		{"空 items", BatchRequest{}},
		{"空输入路径", BatchRequest{Items: []BatchItem{{InputPath: "", Start: 0, End: 1}}}},
		{"文件不存在", BatchRequest{Items: []BatchItem{{InputPath: filepath.Join(dir, "missing.mp4"), Start: 0, End: 1}}}},
		{"未知策略", BatchRequest{Items: []BatchItem{{InputPath: real, Start: 0, End: 1, Strategy: "bogus"}}}},
	}
	for _, c := range cases {
		_, err := svc.TrimBatch(c.req)
		if err == nil {
			t.Errorf("%s: expected rejection, got nil", c.name)
			continue
		}
		// 原子拒绝：不得留下任何任务
		if tasks := svc.ListTasks(); len(tasks) != 0 {
			t.Errorf("%s: rejected but %d tasks created", c.name, len(tasks))
		}
	}

	// 超 max_items 整批拒绝
	cfg := config.Default()
	cfg.BatchMaxItems = 2
	svc2 := NewService(cfg, "", "")
	_, err := svc2.TrimBatch(BatchRequest{Items: []BatchItem{
		{InputPath: real, Start: 0, End: 1},
		{InputPath: real, Start: 0, End: 1},
		{InputPath: real, Start: 0, End: 1},
	}})
	if err == nil {
		t.Error("over max_items: expected rejection")
	}
	if tasks := svc2.ListTasks(); len(tasks) != 0 {
		t.Errorf("over max_items: rejected but %d tasks created", len(tasks))
	}

	// checkFFmpeg 未就绪时先行拒绝（不触碰 probe）
	if _, err := svc.TrimBatch(BatchRequest{Items: []BatchItem{{InputPath: real, Start: 0, End: 1}}}); err == nil {
		t.Error("no ffmpeg: expected rejection")
	}
}

func TestMerge_ValidationReject(t *testing.T) {
	svc := newLogicService()
	dir := t.TempDir()
	real := filepath.Join(dir, "a.mp4")
	if err := writeFile(real); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		req  MergeRequest
	}{
		{"少于 2 项", MergeRequest{Items: []MergeItem{{InputPath: real}}}},
		{"空输入", MergeRequest{Items: []MergeItem{{InputPath: real}, {InputPath: ""}}}},
		{"文件不存在", MergeRequest{Items: []MergeItem{{InputPath: real}, {InputPath: filepath.Join(dir, "missing.mp4")}}}},
		{"未知模式", MergeRequest{Items: []MergeItem{{InputPath: real}, {InputPath: real}}, Mode: "bogus"}},
		{"输出目录不存在", MergeRequest{Items: []MergeItem{{InputPath: real}, {InputPath: real}}, OutputPath: filepath.Join(dir, "no_such_dir", "out.mp4")}},
	}
	for _, c := range cases {
		_, err := svc.Merge(c.req)
		if err == nil {
			t.Errorf("%s: expected rejection, got nil", c.name)
		}
	}
	// 全部拒绝：无任务残留（输出目录用例在 probe 前拦截，其余走存在性/模式校验）
	if tasks := svc.ListTasks(); len(tasks) != 0 {
		t.Errorf("rejected but %d tasks created", len(tasks))
	}
}

// TestTask_JSONContractV14 锁定 v1.4 扩展字段的 JSON 键名契约（snake_case）。
func TestTask_JSONContractV14(t *testing.T) {
	task := &Task{
		TaskID:          "batch_1",
		Kind:            KindBatch,
		Status:          StatusPartial,
		CompletedCount:  2,
		FailedCount:     1,
		MergeMode:       MergeModeAuto,
		Items:           []TrimItem{{InputPath: "C:/a.mp4", Status: ItemStatusCompleted, HasRange: true}},
	}
	b, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{
		"task_id", "kind", "input_path", "output_path", "start", "end", "strategy",
		"status", "progress", "duration", "error", "canceled", "created_at",
		"items", "completed_count", "failed_count", "merge_mode",
	} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing snake_case key %q in %s", k, string(b))
		}
	}
	if m["status"] != "partial" {
		t.Errorf("status = %v, want partial", m["status"])
	}
	items, ok := m["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items should be array of 1, got %v", m["items"])
	}
	it := items[0].(map[string]any)
	for _, k := range []string{"input_path", "output_path", "start", "end", "strategy", "status", "progress", "error", "has_range"} {
		if _, ok := it[k]; !ok {
			t.Errorf("missing item key %q in %s", k, string(b))
		}
	}
	if it["has_range"] != true {
		t.Errorf("has_range = %v, want true", it["has_range"])
	}
}

func TestTaskKind_DefaultTrim(t *testing.T) {
	task := &Task{}
	if got := task.kind(); got != KindTrim {
		t.Errorf("kind() = %q, want trim", got)
	}
}
