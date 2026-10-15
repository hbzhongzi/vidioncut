package config

// asr 段从 config.yaml 加载的回归测试。
//
// 背景（v1.8 修复的真实缺陷）：
//   结构体字段的 mapstructure 标签是扁平点号键（mapstructure:"asr.enabled"），
//   而 viper.Unmarshal 把点号当作嵌套分隔符——YAML 里无论写嵌套 `asr:` 块还是
//   扁平点号键，都无法映射到扁平字段，导致 config.yaml 的 asr 配置**静默失效**、
//   全部回落默认值。此前环境变量覆盖（applyEnvOverrides）是单独实现的，掩盖了该缺陷。
//   修复方式：Load() 中新增 applyASRFile 显式读取 asr.* 子键。
//
// 本测试锁定该行为，防止再次回归。

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

// newViperFromYAML 从 YAML 字符串构造 viper（不依赖进程 CWD）。
func newViperFromYAML(t *testing.T, yaml string) *viper.Viper {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(p, []byte(yaml), 0o644); err != nil {
		t.Fatalf("写入临时配置失败: %v", err)
	}
	v := viper.New()
	v.SetConfigFile(p)
	v.SetConfigType("yaml")
	if err := v.ReadInConfig(); err != nil {
		t.Fatalf("读取临时配置失败: %v", err)
	}
	return v
}

func TestApplyASRFile_NestedBlock(t *testing.T) {
	v := newViperFromYAML(t, `
asr:
  enabled: true
  host: 127.0.0.9
  port: 4999
  model: medium
  compute_type: int8_float16
  hf_home: E:/models/hf-test
  python: C:/venv/py.exe
  idle_timeout_min: 42
  hotwords:
    - videocut
    - AI粗剪
`)
	cfg := Default()
	applyASRFile(v, cfg)

	if !cfg.ASREnabled {
		t.Error("asr.enabled 未生效（应 true）")
	}
	if cfg.ASRHost != "127.0.0.9" {
		t.Errorf("asr.host = %q", cfg.ASRHost)
	}
	if cfg.ASRPort != 4999 {
		t.Errorf("asr.port = %d", cfg.ASRPort)
	}
	if cfg.ASRModel != "medium" {
		t.Errorf("asr.model = %q", cfg.ASRModel)
	}
	if cfg.ASRComputeType != "int8_float16" {
		t.Errorf("asr.compute_type = %q", cfg.ASRComputeType)
	}
	if cfg.ASRHFHome != "E:/models/hf-test" {
		t.Errorf("asr.hf_home = %q", cfg.ASRHFHome)
	}
	if cfg.ASRPython != "C:/venv/py.exe" {
		t.Errorf("asr.python = %q", cfg.ASRPython)
	}
	if cfg.ASRIdleTimeoutMin != 42 {
		t.Errorf("asr.idle_timeout_min = %d", cfg.ASRIdleTimeoutMin)
	}
	if len(cfg.ASRHotwords) != 2 || cfg.ASRHotwords[1] != "AI粗剪" {
		t.Errorf("asr.hotwords = %+v", cfg.ASRHotwords)
	}
}

// TestApplyASRFile_PartialKeepsDefaults 未配置的键必须保留默认值（IsSet 保护）。
func TestApplyASRFile_PartialKeepsDefaults(t *testing.T) {
	v := newViperFromYAML(t, `
asr:
  enabled: false
  port: 4180
`)
	def := Default()
	cfg := Default()
	applyASRFile(v, cfg)

	if cfg.ASREnabled {
		t.Error("asr.enabled 应为 false")
	}
	if cfg.ASRPort != 4180 {
		t.Errorf("asr.port = %d", cfg.ASRPort)
	}
	// 其余应保持默认
	if cfg.ASRHost != def.ASRHost {
		t.Errorf("asr.host 被意外覆盖: %q != 默认 %q", cfg.ASRHost, def.ASRHost)
	}
	if cfg.ASRModel != def.ASRModel {
		t.Errorf("asr.model 被意外覆盖: %q != 默认 %q", cfg.ASRModel, def.ASRModel)
	}
	if cfg.ASRHFHome != def.ASRHFHome {
		t.Errorf("asr.hf_home 被意外覆盖: %q != 默认 %q", cfg.ASRHFHome, def.ASRHFHome)
	}
	if cfg.ASRIdleTimeoutMin != def.ASRIdleTimeoutMin {
		t.Errorf("asr.idle_timeout_min 被意外覆盖: %d", cfg.ASRIdleTimeoutMin)
	}
}

// TestApplyASRFile_NoASRSection 无 asr 段时不应改动任何字段。
func TestApplyASRFile_NoASRSection(t *testing.T) {
	v := newViperFromYAML(t, "concurrency: 3\n")
	def := Default()
	cfg := Default()
	applyASRFile(v, cfg)

	// 结构体含 slice，不可直接比较：逐项比对 asr 相关字段
	if cfg.ASREnabled != def.ASREnabled ||
		cfg.ASRHost != def.ASRHost ||
		cfg.ASRPort != def.ASRPort ||
		cfg.ASRModel != def.ASRModel ||
		cfg.ASRComputeType != def.ASRComputeType ||
		cfg.ASRHFHome != def.ASRHFHome ||
		cfg.ASRPython != def.ASRPython ||
		cfg.ASRIdleTimeoutMin != def.ASRIdleTimeoutMin {
		t.Errorf("无 asr 段时配置被改动:\n got %+v\nwant %+v", cfg, def)
	}
	if len(cfg.ASRHotwords) != len(def.ASRHotwords) {
		t.Errorf("asr.hotwords 被改动: %+v != %+v", cfg.ASRHotwords, def.ASRHotwords)
	}
}

// TestApplyASRFile_EmptyNestedBlock asr 段存在但为空时同样保持默认（IsSet 对空块为 true，
// 但子键 IsSet 为 false，故不得把字符串字段清零）。
func TestApplyASRFile_EmptyNestedBlock(t *testing.T) {
	v := newViperFromYAML(t, "asr:\n")
	def := Default()
	cfg := Default()
	applyASRFile(v, cfg)

	if cfg.ASRHost != def.ASRHost || cfg.ASRModel != def.ASRModel {
		t.Errorf("空 asr 块不应覆盖: host=%q model=%q", cfg.ASRHost, cfg.ASRModel)
	}
}
