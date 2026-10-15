// Package asr 提供 faster-whisper 转录 sidecar（Python）的本地客户端
// （开发文档 v1.7 §19.5）：按需拉起、健康检查、空闲回收。
// venv/脚本缺失时返回 ErrRuntimeMissing，由上层转换为 asr_runtime_missing 结构化错误。
package asr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"videocut/internal/config"
)

// ErrRuntimeMissing 表示 ASR sidecar 运行时缺失（venv 或 serve.py 未找到）。
// 上层应转换为 asr_runtime_missing + 安装指引，不得替代实现。
var ErrRuntimeMissing = errors.New("asr runtime missing")

// TranscribeRequest 转录请求（与 asr/serve.py 的 /transcribe 契约一致）。
type TranscribeRequest struct {
	VideoPath  string   `json:"video_path"`
	OutputBase string   `json:"output_base"`
	Language   string   `json:"language,omitempty"`
	Hotwords   []string `json:"hotwords,omitempty"`
	Model      string   `json:"model,omitempty"`
	// VADFilter 为 nil 时不下发，由 sidecar 取默认 true。
	// 显式置 false 可关闭 VAD——纯音乐/环境音素材下 VAD 会滤空全部片段，
	// 排障与真实环境测试需要能关掉它来暴露静音段。
	VADFilter *bool `json:"vad_filter,omitempty"`
}

// TranscribeResult 转录结果（serve.py 返回 + 路径推导）。
type TranscribeResult struct {
	SRTPath     string  `json:"srt_path"`
	SignalsPath string  `json:"signals_path"`
	Duration    float64 `json:"duration"`
	Segments    int     `json:"segments"`
	Silences    int     `json:"silences"`
}

// Client sidecar 客户端。
type Client struct {
	cfg *config.Config

	mu       sync.Mutex
	proc     *exec.Cmd
	baseURL  string
	lastUsed time.Time
	started  bool

	httpClient *http.Client
}

// NewClient 构造客户端（不立即拉起进程，首次使用时 EnsureRunning）。
func NewClient(cfg *config.Config) *Client {
	return &Client{
		cfg:        cfg,
		baseURL:    fmt.Sprintf("http://%s:%d", cfg.ASRHost, cfg.ASRPort),
		httpClient: &http.Client{Timeout: 0}, // 长任务由 ctx 控制
	}
}

// BaseURL 返回 sidecar 回环地址。
func (c *Client) BaseURL() string { return c.baseURL }

// resolvePython 定位 venv python.exe：配置优先，其次 exe 同级 asr/.venv，最后 PATH。
func (c *Client) resolvePython() (string, error) {
	if c.cfg.ASRPython != "" {
		if fileExists(c.cfg.ASRPython) {
			return c.cfg.ASRPython, nil
		}
		return "", fmt.Errorf("%w: 配置的 asr.python 不存在: %s", ErrRuntimeMissing, c.cfg.ASRPython)
	}
	// 便携布局：<dir>/asr/.venv/Scripts/python.exe（相对 exe 向上找两级项目根）
	if exe, err := os.Executable(); err == nil {
		// build/bin/videocut.exe → ../../asr/.venv
		root := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
		cand := filepath.Join(root, "asr", ".venv", "Scripts", "python.exe")
		if fileExists(cand) {
			return cand, nil
		}
		// 源码树布局：<repo>/asr/.venv
		cand2 := filepath.Join(filepath.Dir(exe), "asr", ".venv", "Scripts", "python.exe")
		if fileExists(cand2) {
			return cand2, nil
		}
	}
	// PATH 兜底
	if p, err := exec.LookPath("python"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("%w: 未找到 ASR venv（asr.python 未配置，asr/.venv/Scripts/python.exe 不存在）。请运行 scripts/bootstrap-asr.ps1", ErrRuntimeMissing)
}

// resolveScript 定位 serve.py。
func (c *Client) resolveScript() (string, error) {
	if exe, err := os.Executable(); err == nil {
		root := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
		cand := filepath.Join(root, "asr", "serve.py")
		if fileExists(cand) {
			return cand, nil
		}
		cand2 := filepath.Join(filepath.Dir(exe), "asr", "serve.py")
		if fileExists(cand2) {
			return cand2, nil
		}
	}
	if fileExists("asr/serve.py") {
		return "asr/serve.py", nil
	}
	return "", fmt.Errorf("%w: 未找到 asr/serve.py", ErrRuntimeMissing)
}

// EnsureRunning 确保 sidecar 进程在运行且健康；缺失时返回 ErrRuntimeMissing。
// 复用策略：端口已健康（外部启动或上次遗留实例）→ 直接复用，不重复拉起。
func (c *Client) EnsureRunning(ctx context.Context) error {
	c.mu.Lock()
	proc := c.proc
	c.mu.Unlock()
	if proc == nil {
		// 无自有进程：先探测端口是否已有健康实例
		if err := c.health(ctx); err == nil {
			c.mu.Lock()
			c.started = true
			c.mu.Unlock()
			return nil
		}
	} else if proc.ProcessState == nil || !proc.ProcessState.Exited() {
		// 自有进程仍在：健康检查失败视为已退出，重启
		if err := c.health(ctx); err == nil {
			return nil
		}
		c.mu.Lock()
		c.proc = nil
		c.mu.Unlock()
	}

	python, err := c.resolvePython()
	if err != nil {
		return err
	}
	script, err := c.resolveScript()
	if err != nil {
		return err
	}

	// HF_HOME 重定向（C 盘空间不足，v1.7 §19.5）
	env := os.Environ()
	if c.cfg.ASRHFHome != "" {
		env = append(env, "HF_HOME="+c.cfg.ASRHFHome)
	}
	cmd := exec.CommandContext(ctx, python, script,
		"--host", c.cfg.ASRHost, "--port", fmt.Sprint(c.cfg.ASRPort))
	cmd.Env = env
	// sidecar 日志走 stderr（其 stdout 不属于 MCP 协议帧，但同样避免污染）
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%w: 拉起 sidecar 失败: %v", ErrRuntimeMissing, err)
	}

	c.mu.Lock()
	c.proc = cmd
	c.mu.Unlock()

	// 等待健康就绪（模型加载可能耗时）
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if err := c.health(ctx); err == nil {
			c.mu.Lock()
			c.started = true
			c.mu.Unlock()
			return nil
		}
		select {
		case <-ctx.Done():
			c.Stop()
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	c.Stop()
	return fmt.Errorf("%w: sidecar 60s 内未就绪", ErrRuntimeMissing)
}

func (c *Client) health(ctx context.Context) error {
	hctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(hctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health status %d", resp.StatusCode)
	}
	return nil
}

// Health 暴露健康检查（MCP 工具/前端可查询）。
//
// 修复（v1.8）：此前只看 c.proc 是否为 nil，导致**外部已启动的 sidecar
// （如用户手工 `python serve.py --port 4178`）被误判为"未启动"**，
// 与 EnsureRunning"端口已监听则复用"的语义自相矛盾。
// 正确顺序：先探端口（不管谁拉起的），不通再看自启进程。
func (c *Client) Health(ctx context.Context) error {
	if err := c.health(ctx); err == nil {
		return nil // 端口健康：可能是复用外部实例，也可能是自启实例
	}
	c.mu.Lock()
	proc := c.proc
	c.mu.Unlock()
	if proc == nil {
		return fmt.Errorf("%w: sidecar 未启动（端口 %s 无响应）", ErrRuntimeMissing, c.baseURL)
	}
	return fmt.Errorf("%w: sidecar 进程已退出或端口无响应", ErrRuntimeMissing)
}

// PortHealthy 直接探测回环端口健康状态（不管 sidecar 由谁拉起；GUI 状态面板用）。
func (c *Client) PortHealthy(ctx context.Context) error {
	return c.health(ctx)
}

// PythonPath 暴露 venv python 解析结果（GUI 状态面板展示；空串+error=未找到）。
func (c *Client) PythonPath() (string, error) {
	return c.resolvePython()
}

// Transcribe 转录视频（确保 sidecar 运行后调用 /transcribe）。
func (c *Client) Transcribe(ctx context.Context, req TranscribeRequest) (*TranscribeResult, error) {
	if err := c.EnsureRunning(ctx); err != nil {
		return nil, err
	}
	if req.Language == "" {
		req.Language = "zh"
	}
	if req.Model == "" {
		req.Model = c.cfg.ASRModel
	}
	if len(req.Hotwords) == 0 {
		req.Hotwords = c.cfg.ASRHotwords
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/transcribe", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(hreq)
	if err != nil {
		return nil, fmt.Errorf("转录请求失败: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sidecar 返回 %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}

	var out struct {
		SRTPath     string  `json:"srt_path"`
		SignalsPath string  `json:"signals_path"`
		Duration    float64 `json:"duration"`
		Segments    int     `json:"segments"`
		Silences    int     `json:"silences"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("解析 sidecar 响应失败: %w", err)
	}
	// 路径兜底：按约定 <output_base>/work/ 推导
	if out.SRTPath == "" {
		out.SRTPath = filepath.Join(req.OutputBase, "work", "transcript.srt")
	}
	if out.SignalsPath == "" {
		out.SignalsPath = filepath.Join(req.OutputBase, "work", "signals.json")
	}

	c.mu.Lock()
	c.lastUsed = time.Now()
	c.mu.Unlock()
	return &TranscribeResult{
		SRTPath:     out.SRTPath,
		SignalsPath: out.SignalsPath,
		Duration:    out.Duration,
		Segments:    out.Segments,
		Silences:    out.Silences,
	}, nil
}

// SetHotwords 透传热词表到 sidecar（MCP videocut_set_hotwords）。
func (c *Client) SetHotwords(ctx context.Context, hotwords []string) error {
	if err := c.EnsureRunning(ctx); err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{"hotwords": hotwords})
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/hotwords", bytes.NewReader(body))
	if err != nil {
		return err
	}
	hreq.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(hreq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("set hotwords 返回 %d", resp.StatusCode)
	}
	c.mu.Lock()
	c.lastUsed = time.Now()
	c.mu.Unlock()
	return nil
}

// StartIdleReaper 空闲回收：超过 asr.idle_timeout_min 未使用则关闭进程。
func (c *Client) StartIdleReaper(ctx context.Context) {
	idle := time.Duration(c.cfg.ASRIdleTimeoutMin) * time.Minute
	if idle <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.mu.Lock()
				proc := c.proc
				last := c.lastUsed
				c.mu.Unlock()
				if proc != nil && !last.IsZero() && time.Since(last) > idle {
					c.Stop()
				}
			}
		}
	}()
}

// Stop 关闭 sidecar 进程。
func (c *Client) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.proc != nil && c.proc.Process != nil {
		_ = c.proc.Process.Kill()
		_, _ = c.proc.Process.Wait()
	}
	c.proc = nil
	c.started = false
}

// IsRuntimeMissing 判断错误是否为 sidecar 缺失。
func IsRuntimeMissing(err error) bool {
	return errors.Is(err, ErrRuntimeMissing)
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
