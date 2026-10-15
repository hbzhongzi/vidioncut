package ffmpeg

import (
	"os"
	"path/filepath"
	"testing"

	"videocut/internal/config"
)

func TestResolve_BundledNotFound(t *testing.T) {
	cfg := &config.Config{FFmpegMode: "bundled", FFmpegDir: "nonexistent_ffmpeg_dir_xyz"}
	_, err := FFmpegPath(cfg)
	if err == nil {
		t.Skip("环境中恰好存在候选路径，跳过")
	}
}

func TestResolve_SystemMissing(t *testing.T) {
	cfg := &config.Config{FFmpegMode: "system"}
	_, err := FFmpegPath(cfg)
	if err == nil {
		t.Skip("环境中存在系统 ffmpeg，跳过")
	}
}

func TestResolve_BundledFound(t *testing.T) {
	dir := t.TempDir()
	ffDir := filepath.Join(dir, "ffmpeg")
	if err := os.MkdirAll(ffDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ffPath := filepath.Join(ffDir, "ffmpeg.exe")
	if err := os.WriteFile(ffPath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	fpPath := filepath.Join(ffDir, "ffprobe.exe")
	if err := os.WriteFile(fpPath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)

	cfg := &config.Config{FFmpegMode: "bundled", FFmpegDir: "ffmpeg"}
	got, err := FFmpegPath(cfg)
	if err != nil {
		t.Fatalf("FFmpegPath: %v", err)
	}
	if got != ffPath {
		t.Errorf("FFmpegPath = %q, want %q", got, ffPath)
	}
	got2, err := FFprobePath(cfg)
	if err != nil {
		t.Fatalf("FFprobePath: %v", err)
	}
	if got2 != fpPath {
		t.Errorf("FFprobePath = %q, want %q", got2, fpPath)
	}
}

func TestResolve_SystemMode(t *testing.T) {
	// 构造一个含 ffmpeg.exe 的临时目录并塞入 PATH，验证 LookPath 回退。
	dir := t.TempDir()
	bin := filepath.Join(dir, "ffmpeg.exe")
	if err := os.WriteFile(bin, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath)
	defer os.Setenv("PATH", oldPath)

	cfg := &config.Config{FFmpegMode: "system"}
	got, err := FFmpegPath(cfg)
	if err != nil {
		t.Fatalf("system mode should find ffmpeg in PATH: %v", err)
	}
	if got != bin {
		t.Errorf("system FFmpegPath = %q, want %q", got, bin)
	}
}
