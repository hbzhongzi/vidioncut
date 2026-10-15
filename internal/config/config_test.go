package config

import (
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.FFmpegMode != "bundled" {
		t.Errorf("FFmpegMode=%q want bundled", cfg.FFmpegMode)
	}
	if cfg.FFmpegDir != "ffmpeg" {
		t.Errorf("FFmpegDir=%q want ffmpeg", cfg.FFmpegDir)
	}
	if cfg.DefaultStrategy != "stream_copy" {
		t.Errorf("DefaultStrategy=%q want stream_copy", cfg.DefaultStrategy)
	}
	if cfg.TimeoutSeconds != 300 {
		t.Errorf("TimeoutSeconds=%d want 300", cfg.TimeoutSeconds)
	}
	if cfg.OutputSuffix != "_trimmed" {
		t.Errorf("OutputSuffix=%q want _trimmed", cfg.OutputSuffix)
	}
}

func TestLoad_DefaultsWhenNoFile(t *testing.T) {
	t.Chdir(t.TempDir())
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.FFmpegMode != "bundled" || cfg.TimeoutSeconds != 300 {
		t.Errorf("Load did not return defaults: %+v", cfg)
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("VIDEOCUT_CONCURRENCY", "4")
	t.Setenv("VIDEOCUT_TRIM_DEFAULT_STRATEGY", "reencode")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Concurrency != 4 {
		t.Errorf("Concurrency=%d want 4 (env override)", cfg.Concurrency)
	}
	if cfg.DefaultStrategy != "reencode" {
		t.Errorf("DefaultStrategy=%q want reencode (env override)", cfg.DefaultStrategy)
	}
}

func TestLoad_EnvOverrideV14(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("VIDEOCUT_MERGE_DEFAULT_MODE", "filter")
	t.Setenv("VIDEOCUT_BATCH_MAX_ITEMS", "20")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MergeDefaultMode != "filter" {
		t.Errorf("MergeDefaultMode=%q want filter (env override)", cfg.MergeDefaultMode)
	}
	if cfg.BatchMaxItems != 20 {
		t.Errorf("BatchMaxItems=%d want 20 (env override)", cfg.BatchMaxItems)
	}
}

func TestDefault_V14Fields(t *testing.T) {
	cfg := Default()
	if cfg.MergeDefaultMode != "auto" {
		t.Errorf("MergeDefaultMode=%q want auto", cfg.MergeDefaultMode)
	}
	if cfg.BatchMaxItems != 50 {
		t.Errorf("BatchMaxItems=%d want 50", cfg.BatchMaxItems)
	}
}
