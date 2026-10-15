package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDeriveOutput(t *testing.T) {
	if got := deriveOutput("C:/a/b.mp4", "_trimmed"); got != "C:/a/b_trimmed.mp4" {
		t.Errorf("deriveOutput = %q, want C:/a/b_trimmed.mp4", got)
	}
	if got := deriveOutput("movie.mov", "_cut"); got != "movie_cut.mov" {
		t.Errorf("deriveOutput = %q, want movie_cut.mov", got)
	}
}

func TestAppendSeq_NotExists(t *testing.T) {
	if got := appendSeq("C:/a/b_trimmed.mp4"); got != "C:/a/b_trimmed.mp4" {
		t.Errorf("appendSeq = %q, want 原路径", got)
	}
}

func TestAppendSeq_Exists(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "out.mp4")
	if err := os.WriteFile(base, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := appendSeq(base)
	want := filepath.Join(dir, "out_1.mp4")
	if got != want {
		t.Errorf("appendSeq = %q, want %q", got, want)
	}
	// 已存在 _1 时，跳到 _2
	if err := os.WriteFile(want, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := appendSeq(base); got != filepath.Join(dir, "out_2.mp4") {
		t.Errorf("appendSeq second = %q, want out_2.mp4", got)
	}
}

func TestFileExists(t *testing.T) {
	if fileExists("C:/no/such/path.mp4") {
		t.Error("should be false for missing file")
	}
	dir := t.TempDir()
	if fileExists(dir) {
		t.Error("should be false for a directory")
	}
	f := filepath.Join(dir, "f.txt")
	_ = os.WriteFile(f, []byte("x"), 0o644)
	if !fileExists(f) {
		t.Error("should be true for existing file")
	}
}

// TestTask_JSONContract 锁定 Task→前端的 JSON 键名契约（snake_case）。
// GetTask/ListTasks 经 Wails 绑定返回，前端 store 依赖这些键名渲染状态。
func TestTask_JSONContract(t *testing.T) {
	task := &Task{
		TaskID:    "trim_1",
		InputPath: "C:/a.mp4",
		Status:    StatusCompleted,
		Progress:  100,
		Duration:  2.0,
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
		"task_id", "input_path", "output_path", "start", "end", "strategy",
		"status", "progress", "duration", "error", "canceled", "created_at",
	} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing snake_case key %q in %s", k, string(b))
		}
	}
	// status 必须是字符串值（前端联合类型 'pending'|'processing'|...）
	if s, ok := m["status"].(string); !ok || s != "completed" {
		t.Errorf("status should be string 'completed', got %v (%T)", m["status"], m["status"])
	}
	// created_at 由 time.Time 序列化为 RFC3339 字符串
	if s, ok := m["created_at"].(string); !ok || s == "" {
		t.Errorf("created_at should be non-empty string, got %v (%T)", m["created_at"], m["created_at"])
	}
}
