package config

// M12 字幕配置加载（字幕功能更新文档 §4.5/§5.8）：subtitles.* 嵌套键显式映射。
// 与 applyASRFile 同一手法——viper 的 Unmarshal 对带点号 mapstructure 标签
// 的嵌套 key 覆盖不可靠（v1.7 实测教训），故逐键 IsSet 保护读取。

import (
	"os"
	"strconv"

	"github.com/spf13/viper"
)

// applySubtitlesFile 显式读取 config.yaml 的 subtitles.* 嵌套键。
// 仅当键存在时覆盖，未配置的键保留 Default() 默认值。
func applySubtitlesFile(v *viper.Viper, cfg *Config) {
	if v.IsSet("subtitles.mode") {
		cfg.SubMode = v.GetString("subtitles.mode")
	}
	if v.IsSet("subtitles.format") {
		cfg.SubFormat = v.GetString("subtitles.format")
	}
	if v.IsSet("subtitles.style_preset") {
		cfg.SubStylePreset = v.GetString("subtitles.style_preset")
	}
	if v.IsSet("subtitles.max_chars_per_cue") {
		cfg.SubMaxCharsPerCue = v.GetInt("subtitles.max_chars_per_cue")
	}
	if v.IsSet("subtitles.max_lines") {
		cfg.SubMaxLines = v.GetInt("subtitles.max_lines")
	}
	if v.IsSet("subtitles.font_name") {
		cfg.SubFontName = v.GetString("subtitles.font_name")
	}
	if v.IsSet("subtitles.font_size") {
		cfg.SubFontSize = v.GetInt("subtitles.font_size")
	}
	if v.IsSet("subtitles.outline_width") {
		cfg.SubOutlineWidth = v.GetFloat64("subtitles.outline_width")
	}
	if v.IsSet("subtitles.burn_crf") {
		cfg.SubBurnCRF = v.GetInt("subtitles.burn_crf")
	}
	if v.IsSet("subtitles.burn_preset") {
		cfg.SubBurnPreset = v.GetString("subtitles.burn_preset")
	}
	if v.IsSet("subtitles.burn_encoder") {
		cfg.SubBurnEncoder = v.GetString("subtitles.burn_encoder")
	}
	if v.IsSet("subtitles.animate") {
		cfg.SubAnimate = v.GetString("subtitles.animate")
	}
	if v.IsSet("subtitles.max_auto_skills") {
		cfg.SubMaxAutoSkills = v.GetInt("subtitles.max_auto_skills")
	}
	if v.IsSet("subtitles.disabled_skills") {
		cfg.SubDisabledSkills = v.GetStringSlice("subtitles.disabled_skills")
	}
	if v.IsSet("subtitles.keyword_color") {
		cfg.SubKeywordColor = v.GetString("subtitles.keyword_color")
	}
	if v.IsSet("subtitles.keyword_min_freq") {
		cfg.SubKeywordMinFreq = v.GetInt("subtitles.keyword_min_freq")
	}
}

// applySubtitleEnvOverrides 应用 VIDEOCUT_SUBTITLES_* 环境变量覆盖（applyEnvOverrides 尾部调用）。
func applySubtitleEnvOverrides(cfg *Config) {
	// 由 applyEnvOverrides 统一调用；此处仅放字幕段以控制单文件规模
	applyEnvString := func(env string, target *string) {
		if val := os.Getenv(env); val != "" {
			*target = val
		}
	}
	applyEnvInt := func(env string, target *int) {
		if val := os.Getenv(env); val != "" {
			if n, err := strconv.Atoi(val); err == nil {
				*target = n
			}
		}
	}
	applyEnvString("VIDEOCUT_SUBTITLES_MODE", &cfg.SubMode)
	applyEnvString("VIDEOCUT_SUBTITLES_FORMAT", &cfg.SubFormat)
	applyEnvString("VIDEOCUT_SUBTITLES_STYLE_PRESET", &cfg.SubStylePreset)
	applyEnvInt("VIDEOCUT_SUBTITLES_MAX_CHARS_PER_CUE", &cfg.SubMaxCharsPerCue)
	applyEnvInt("VIDEOCUT_SUBTITLES_MAX_LINES", &cfg.SubMaxLines)
	applyEnvString("VIDEOCUT_SUBTITLES_FONT_NAME", &cfg.SubFontName)
	applyEnvInt("VIDEOCUT_SUBTITLES_FONT_SIZE", &cfg.SubFontSize)
	applyEnvInt("VIDEOCUT_SUBTITLES_BURN_CRF", &cfg.SubBurnCRF)
	applyEnvString("VIDEOCUT_SUBTITLES_BURN_PRESET", &cfg.SubBurnPreset)
	applyEnvString("VIDEOCUT_SUBTITLES_BURN_ENCODER", &cfg.SubBurnEncoder)
	applyEnvString("VIDEOCUT_SUBTITLES_ANIMATE", &cfg.SubAnimate)
	applyEnvInt("VIDEOCUT_SUBTITLES_MAX_AUTO_SKILLS", &cfg.SubMaxAutoSkills)
	applyEnvString("VIDEOCUT_SUBTITLES_KEYWORD_COLOR", &cfg.SubKeywordColor)
	applyEnvInt("VIDEOCUT_SUBTITLES_KEYWORD_MIN_FREQ", &cfg.SubKeywordMinFreq)
}
