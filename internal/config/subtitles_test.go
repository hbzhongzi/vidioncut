package config

// M12：subtitles 段加载测试（字幕功能更新文档 §4.5/§8；applyASRFile 同款手法）。

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestApplySubtitlesFile(t *testing.T) {
	dir := t.TempDir()
	yaml := `subtitles:
  mode: burn
  format: srt
  style_preset: talking-head
  max_chars_per_cue: 24
  max_lines: 3
  burn_crf: 20
  burn_encoder: h264_nvenc
  animate: off
  max_auto_skills: 3
  disabled_skills: [blur-reveal, marker-sweep]
  keyword_color: "#FF0000"
  keyword_min_freq: 5
`
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(dir)
	if err := v.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	applySubtitlesFile(v, cfg)

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"mode", cfg.SubMode, "burn"},
		{"format", cfg.SubFormat, "srt"},
		{"style_preset", cfg.SubStylePreset, "talking-head"},
		{"max_chars_per_cue", cfg.SubMaxCharsPerCue, 24},
		{"max_lines", cfg.SubMaxLines, 3},
		{"burn_crf", cfg.SubBurnCRF, 20},
		{"burn_encoder", cfg.SubBurnEncoder, "h264_nvenc"},
		{"animate", cfg.SubAnimate, "off"},
		{"max_auto_skills", cfg.SubMaxAutoSkills, 3},
		{"disabled_skills", len(cfg.SubDisabledSkills), 2},
		{"keyword_color", cfg.SubKeywordColor, "#FF0000"},
		{"keyword_min_freq", cfg.SubKeywordMinFreq, 5},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v 期望 %v", c.name, c.got, c.want)
		}
	}
}

func TestApplySubtitlesFile_UnsetKeepsDefault(t *testing.T) {
	// 空配置：全部保留 Default() 默认值（旧配置可用，兼容性 §7）
	v := viper.New()
	cfg := Default()
	applySubtitlesFile(v, cfg)
	if cfg.SubMode != "sidecar" || cfg.SubFormat != "both" || cfg.SubStylePreset != "default" ||
		cfg.SubMaxCharsPerCue != 20 || cfg.SubMaxLines != 2 || cfg.SubBurnCRF != 18 ||
		cfg.SubBurnEncoder != "libx264" || cfg.SubAnimate != "auto" || cfg.SubMaxAutoSkills != 2 ||
		cfg.SubKeywordColor != "#FFD700" || cfg.SubKeywordMinFreq != 3 {
		t.Errorf("空配置应保留默认值: %+v", cfg)
	}
	if cfg.SubDisabledSkills == nil {
		t.Error("disabled_skills 应为非 nil 空切片（JSON 消费者收到 [] 而非 null）")
	}
}

func TestSubtitleEnvOverrides(t *testing.T) {
	t.Setenv("VIDEOCUT_SUBTITLES_MODE", "both")
	t.Setenv("VIDEOCUT_SUBTITLES_MAX_CHARS_PER_CUE", "18")
	t.Setenv("VIDEOCUT_SUBTITLES_BURN_ENCODER", "h264_qsv")
	cfg := Default()
	applySubtitleEnvOverrides(cfg)
	if cfg.SubMode != "both" {
		t.Errorf("SubMode = %q 期望 both", cfg.SubMode)
	}
	if cfg.SubMaxCharsPerCue != 18 {
		t.Errorf("SubMaxCharsPerCue = %d 期望 18", cfg.SubMaxCharsPerCue)
	}
	if cfg.SubBurnEncoder != "h264_qsv" {
		t.Errorf("SubBurnEncoder = %q 期望 h264_qsv", cfg.SubBurnEncoder)
	}
}

func TestSubtitleEnvOverrides_InvalidIgnored(t *testing.T) {
	// 非法数值不覆盖（保持默认），不得 panic
	t.Setenv("VIDEOCUT_SUBTITLES_MAX_CHARS_PER_CUE", "abc")
	cfg := Default()
	applySubtitleEnvOverrides(cfg)
	if cfg.SubMaxCharsPerCue != 20 {
		t.Errorf("非法环境变量不应覆盖: %d", cfg.SubMaxCharsPerCue)
	}
}
