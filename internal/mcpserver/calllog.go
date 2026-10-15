package mcpserver

// MCP 工具调用日志（JSONL，开发文档 §19.8 Codex 面板"对话轨迹"数据源）。
// 位置：<exe_dir>/logs/mcp-calls.jsonl；GUI 的 Codex 页签轮询读取展示。
// 注意：写日志只走文件，绝不碰 stdout（协议帧纯净，R26）。

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CallEntry 一条工具调用记录。
type CallEntry struct {
	TS     string `json:"ts"`     // RFC3339Nano 本地时间
	Tool   string `json:"tool"`   // 工具名（videocut_*）
	OK     bool   `json:"ok"`     // 是否成功（err==nil 且结果非 isError）
	MS     int64  `json:"ms"`     // 耗时毫秒
	Err    string `json:"err"`    // 错误摘要（成功为空）
	Detail string `json:"detail"` // 预留：关键参数摘要（当前留空）
}

// CallLogger 并发安全的 JSONL 追加写入器。
type CallLogger struct {
	mu   sync.Mutex
	path string
}

// NewCallLogger 定位日志文件：<exe_dir>/logs/mcp-calls.jsonl。
// exe 路径不可用时退避到可执行目录（测试环境走 os.TempDir 由调用方注入）。
func NewCallLogger() (*CallLogger, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(filepath.Dir(exe), "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &CallLogger{path: filepath.Join(dir, "mcp-calls.jsonl")}, nil
}

// NewCallLoggerAt 测试注入点：指定日志文件路径。
func NewCallLoggerAt(path string) (*CallLogger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return &CallLogger{path: path}, nil
}

// Log 追加一条调用记录（失败不阻塞主流程，仅静默跳过）。
func (l *CallLogger) Log(e CallEntry) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	e.TS = time.Now().Format(time.RFC3339Nano)
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	b = append(b, '\n')
	_, _ = f.Write(b)
}

// ReadCallLog 读取最近 limit 条（倒序返回，最新在前；limit<=0 时全量）。
// 文件不存在/不可读时返回空切片（GUI 显示"暂无记录"而非报错）。
func ReadCallLog(limit int) []CallEntry {
	exe, err := os.Executable()
	if err != nil {
		return []CallEntry{}
	}
	path := filepath.Join(filepath.Dir(exe), "logs", "mcp-calls.jsonl")
	return readCallLogFile(path, limit)
}

func readCallLogFile(path string, limit int) []CallEntry {
	f, err := os.Open(path)
	if err != nil {
		return []CallEntry{}
	}
	defer f.Close()

	var all []CallEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 单行上限 1MB
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e CallEntry
		if err := json.Unmarshal(line, &e); err != nil {
			continue // 容忍半行/坏行：跳过继续
		}
		all = append(all, e)
	}
	// 倒序（最新在前）+ 截断
	out := make([]CallEntry, 0, len(all))
	for i := len(all) - 1; i >= 0; i-- {
		out = append(out, all[i])
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}
