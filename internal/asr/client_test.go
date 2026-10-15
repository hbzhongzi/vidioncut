package asr

import (
	"context"
	"errors"
	"testing"

	"videocut/internal/config"
)

func TestNewClient_BaseURL(t *testing.T) {
	cfg := config.Default()
	cfg.ASRHost = "127.0.0.1"
	cfg.ASRPort = 4178
	c := NewClient(cfg)
	if got := c.BaseURL(); got != "http://127.0.0.1:4178" {
		t.Errorf("BaseURL=%s", got)
	}
}

func TestIsRuntimeMissing(t *testing.T) {
	wrapped := errors.Join(errors.New("context"), ErrRuntimeMissing)
	if !IsRuntimeMissing(wrapped) {
		t.Error("IsRuntimeMissing 应识别被包装的 ErrRuntimeMissing")
	}
	if IsRuntimeMissing(errors.New("other")) {
		t.Error("普通错误不应判为 runtime missing")
	}
}

func TestResolvePython_Missing(t *testing.T) {
	cfg := config.Default()
	cfg.ASRPython = `E:/definitely/not/here/python.exe`
	c := NewClient(cfg)
	_, err := c.resolvePython()
	if !IsRuntimeMissing(err) {
		t.Fatalf("配置的 python 不存在应返回 ErrRuntimeMissing，得到 %v", err)
	}
}

func TestEnsureRunning_NoRuntime(t *testing.T) {
	cfg := config.Default()
	cfg.ASRPython = "" // 无 venv、无 PATH python 时（CI 常见）应返回结构化缺失错误
	cfg.ASRHost = "127.0.0.1"
	cfg.ASRPort = 39999 // 无监听端口
	c := NewClient(cfg)
	// 先探测：若端口恰好被占用（健康），跳过本用例
	if err := c.health(context.Background()); err == nil {
		t.Skip("port 39999 已有服务，跳过")
	}
	err := c.EnsureRunning(context.Background())
	if !IsRuntimeMissing(err) {
		t.Fatalf("无运行时应返回 ErrRuntimeMissing，得到 %v", err)
	}
}
