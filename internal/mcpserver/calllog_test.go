package mcpserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReadCallLogMissingFileReturnsEmptyArray(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "mcp-calls.jsonl")
	entries := readCallLogFile(path, 200)
	if entries == nil {
		t.Fatal("readCallLogFile must return an empty slice, not nil")
	}
	if len(entries) != 0 {
		t.Fatalf("readCallLogFile returned %d entries, want 0", len(entries))
	}

	b, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("marshal empty entries: %v", err)
	}
	if string(b) != "[]" {
		t.Fatalf("empty entries JSON = %s, want []", b)
	}
}

func TestReadCallLogSkipsInvalidLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp-calls.jsonl")
	content := "not-json\n" + `{"ts":"2026-10-14T12:00:00Z","tool":"videocut_probe","ok":true}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	entries := readCallLogFile(path, 200)
	if len(entries) != 1 || entries[0].Tool != "videocut_probe" {
		t.Fatalf("entries = %+v, want one valid entry", entries)
	}
}
