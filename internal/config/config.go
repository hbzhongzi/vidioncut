package config

import (
	"os"
	"strconv"

	"github.com/spf13/viper"
)

// Config 保存运行时可配置项，字段与开发文档 v1.1 §7、v1.4 §18.5、v1.7 §19.6 一致。
type Config struct {
	FFmpegMode       string `mapstructure:"ffmpeg.mode"` // bundled | system
	FFmpegDir        string `mapstructure:"ffmpeg.dir"`  // bundled 模式相对 exe 的子目录
	TempDir          string `mapstructure:"temp.dir"`    // 任务临时文件根（空=系统临时）
	Concurrency      int    `mapstructure:"concurrency"` // 同时运行 FFmpeg 进程上限
	DefaultStrategy  string `mapstructure:"trim.default_strategy"`
	TimeoutSeconds   int    `mapstructure:"trim.timeout_seconds"`
	OutputSuffix     string `mapstructure:"output.suffix"`
	MergeDefaultMode string `mapstructure:"merge.default_mode"` // v1.4：合并默认模式 auto/demuxer/filter
	BatchMaxItems    int    `mapstructure:"batch.max_items"`    // v1.4：批量单次提交上限
	Webview2Fallback bool   `mapstructure:"webview2.fallback"`

	// v1.7 §19.6：ASR sidecar（faster-whisper，本地回环）
	ASREnabled        bool     `mapstructure:"asr.enabled"`          // sidecar 总开关
	ASRHost           string   `mapstructure:"asr.host"`             // 仅 127.0.0.1
	ASRPort           int      `mapstructure:"asr.port"`             // 默认 4178
	ASRModel          string   `mapstructure:"asr.model"`            // small | large-v3
	ASRComputeType    string   `mapstructure:"asr.compute_type"`     // int8（CPU）
	ASRHFHome         string   `mapstructure:"asr.hf_home"`          // 模型缓存目录（C 盘空间不足时重定向）
	ASRPython         string   `mapstructure:"asr.python"`           // venv python.exe（空=自动探测）
	ASRIdleTimeoutMin int      `mapstructure:"asr.idle_timeout_min"` // 空闲回收分钟数
	ASRHotwords       []string `mapstructure:"asr.hotwords"`         // 默认热词表

	// M12：粗剪后字幕（字幕功能更新文档 §4.5 / §5.8；applySubtitlesFile 显式映射）
	SubMode           string   `mapstructure:"subtitles.mode"`              // sidecar | burn | both（默认外显）
	SubFormat         string   `mapstructure:"subtitles.format"`            // srt | ass | both
	SubStylePreset    string   `mapstructure:"subtitles.style_preset"`      // default | talking-head | minimal | custom
	SubMaxCharsPerCue int      `mapstructure:"subtitles.max_chars_per_cue"` // 单条最大字数（预换行）
	SubMaxLines       int      `mapstructure:"subtitles.max_lines"`         // 最多行数
	SubFontName       string   `mapstructure:"subtitles.font_name"`         // custom 预设生效（Q-04 P2）
	SubFontSize       int      `mapstructure:"subtitles.font_size"`         // custom 预设生效
	SubOutlineWidth   float64  `mapstructure:"subtitles.outline_width"`     // custom 预设生效
	SubBurnCRF        int      `mapstructure:"subtitles.burn_crf"`          // 烧录画质（越小越好）
	SubBurnPreset     string   `mapstructure:"subtitles.burn_preset"`       // 烧录编码预设
	SubBurnEncoder    string   `mapstructure:"subtitles.burn_encoder"`      // libx264 | h264_nvenc | h264_qsv | h264_amf（失败回退 libx264）
	SubAnimate        string   `mapstructure:"subtitles.animate"`           // auto | off（二次加工特效开关）
	SubMaxAutoSkills  int      `mapstructure:"subtitles.max_auto_skills"`   // 自动特效上限
	SubDisabledSkills []string `mapstructure:"subtitles.disabled_skills"`   // 特效禁用清单
	SubKeywordColor   string   `mapstructure:"subtitles.keyword_color"`     // 关键词高亮色 #RRGGBB
	SubKeywordMinFreq int      `mapstructure:"subtitles.keyword_min_freq"`  // 关键词词频阈值
}

// Default 返回带默认值的配置。
func Default() *Config {
	return &Config{
		FFmpegMode:        "bundled",
		FFmpegDir:         "ffmpeg",
		TempDir:           "",
		Concurrency:       0, // 0 表示运行时按 CPU 核心数
		DefaultStrategy:   "stream_copy",
		TimeoutSeconds:    300,
		OutputSuffix:      "_trimmed",
		MergeDefaultMode:  "auto", // v1.4 §18.4
		BatchMaxItems:     50,     // v1.4 §18.7
		Webview2Fallback:  true,
		ASREnabled:        false,
		ASRHost:           "127.0.0.1",
		ASRPort:           4178,
		ASRModel:          "small",
		ASRComputeType:    "int8",
		ASRHFHome:         "",
		ASRPython:         "",
		ASRIdleTimeoutMin: 10,
		// Keep collection-valued settings non-nil so JSON consumers receive []
		// instead of null when no hotwords are configured.
		ASRHotwords: []string{},
		// M12 字幕默认值（字幕功能更新文档 §4.5：默认外显——核心场景是剪映/PR 精修）
		SubMode:           "sidecar",
		SubFormat:         "both",
		SubStylePreset:    "default",
		SubMaxCharsPerCue: 20,
		SubMaxLines:       2,
		SubFontName:       "Microsoft YaHei",
		SubFontSize:       48,
		SubOutlineWidth:   2,
		SubBurnCRF:        18,
		SubBurnPreset:     "fast",
		SubBurnEncoder:    "libx264",
		SubAnimate:        "auto",
		SubMaxAutoSkills:  2,
		SubDisabledSkills: []string{},
		SubKeywordColor:   "#FFD700",
		SubKeywordMinFreq: 3,
	}
}

// Load 从当前目录 / resources 的 config.yaml 读取（可选），叠加默认值，
// 再应用 VIDEOCUT_* 环境变量覆盖。配置文件不存在时直接用默认值，不视为错误。
//
// 说明：本配置为扁平结构体，但文档以点号 key（trim.default_strategy 等）描述。
// viper 对带点号的嵌套 key 在 Unmarshal 时覆盖不可靠，故环境变量直接落到结构体字段，
// 保证覆盖确定生效；config.yaml 走 viper.Unmarshal（best-effort）。
func Load() (*Config, error) {
	cfg := Default()

	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("./resources")

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			// 配置文件存在但解析失败，视为致命
			return nil, err
		}
		// 不存在则使用默认值
	}

	if err := v.Unmarshal(cfg); err != nil {
		return nil, err
	}

	// asr 子键因 viper 的 Unmarshal 无法映射带点号 mapstructure 标签（asr.enabled 等）
	// 到扁平字段，需显式读取（与 applyEnvOverrides 同一手法）。仅当键存在时覆盖，保留默认值。
	applyASRFile(v, cfg)

	// subtitles 子键同理（M12：viper 对点号嵌套 key 不可靠的既有教训）。
	applySubtitlesFile(v, cfg)

	applyEnvOverrides(cfg)
	return cfg, nil
}

// applyASRFile 显式读取 config.yaml 的 asr.* 嵌套键，映射到扁平结构体字段。
// 仅当键存在时覆盖（IsSet 保护），未配置的键保留 Default() 默认值。
func applyASRFile(v *viper.Viper, cfg *Config) {
	if v.IsSet("asr.enabled") {
		cfg.ASREnabled = v.GetBool("asr.enabled")
	}
	if v.IsSet("asr.host") {
		cfg.ASRHost = v.GetString("asr.host")
	}
	if v.IsSet("asr.port") {
		cfg.ASRPort = v.GetInt("asr.port")
	}
	if v.IsSet("asr.model") {
		cfg.ASRModel = v.GetString("asr.model")
	}
	if v.IsSet("asr.compute_type") {
		cfg.ASRComputeType = v.GetString("asr.compute_type")
	}
	if v.IsSet("asr.hf_home") {
		cfg.ASRHFHome = v.GetString("asr.hf_home")
	}
	if v.IsSet("asr.python") {
		cfg.ASRPython = v.GetString("asr.python")
	}
	if v.IsSet("asr.idle_timeout_min") {
		cfg.ASRIdleTimeoutMin = v.GetInt("asr.idle_timeout_min")
	}
	if v.IsSet("asr.hotwords") {
		cfg.ASRHotwords = v.GetStringSlice("asr.hotwords")
	}
}

// applyEnvOverrides 应用 VIDEOCUT_* 环境变量覆盖（直接写结构体字段，确定生效）。
func applyEnvOverrides(cfg *Config) {
	if val := os.Getenv("VIDEOCUT_FFMPEG_MODE"); val != "" {
		cfg.FFmpegMode = val
	}
	if val := os.Getenv("VIDEOCUT_FFMPEG_DIR"); val != "" {
		cfg.FFmpegDir = val
	}
	if val := os.Getenv("VIDEOCUT_CONCURRENCY"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.Concurrency = n
		}
	}
	if val := os.Getenv("VIDEOCUT_TRIM_DEFAULT_STRATEGY"); val != "" {
		cfg.DefaultStrategy = val
	}
	if val := os.Getenv("VIDEOCUT_TRIM_TIMEOUT_SECONDS"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.TimeoutSeconds = n
		}
	}
	if val := os.Getenv("VIDEOCUT_OUTPUT_SUFFIX"); val != "" {
		cfg.OutputSuffix = val
	}
	if val := os.Getenv("VIDEOCUT_MERGE_DEFAULT_MODE"); val != "" {
		cfg.MergeDefaultMode = val
	}
	if val := os.Getenv("VIDEOCUT_BATCH_MAX_ITEMS"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.BatchMaxItems = n
		}
	}
	if val := os.Getenv("VIDEOCUT_WEBVIEW2_FALLBACK"); val != "" {
		if b, err := strconv.ParseBool(val); err == nil {
			cfg.Webview2Fallback = b
		}
	}
	if val := os.Getenv("VIDEOCUT_ASR_ENABLED"); val != "" {
		if b, err := strconv.ParseBool(val); err == nil {
			cfg.ASREnabled = b
		}
	}
	if val := os.Getenv("VIDEOCUT_ASR_HOST"); val != "" {
		cfg.ASRHost = val
	}
	if val := os.Getenv("VIDEOCUT_ASR_PORT"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.ASRPort = n
		}
	}
	if val := os.Getenv("VIDEOCUT_ASR_MODEL"); val != "" {
		cfg.ASRModel = val
	}
	if val := os.Getenv("VIDEOCUT_ASR_COMPUTE_TYPE"); val != "" {
		cfg.ASRComputeType = val
	}
	if val := os.Getenv("VIDEOCUT_ASR_HF_HOME"); val != "" {
		cfg.ASRHFHome = val
	}
	if val := os.Getenv("VIDEOCUT_ASR_PYTHON"); val != "" {
		cfg.ASRPython = val
	}
	if val := os.Getenv("VIDEOCUT_ASR_IDLE_TIMEOUT_MIN"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.ASRIdleTimeoutMin = n
		}
	}
	// M12 字幕段环境变量覆盖（VIDEOCUT_SUBTITLES_*）
	applySubtitleEnvOverrides(cfg)
}
