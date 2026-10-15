package mcpserver

import (
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
)

// 错误码（§19.2：统一 {error, code, hint} 结构）。
const (
	codeInvalidParams    = "invalid_params"
	codeInternal         = "internal"
	codeProbeFailed      = "probe_failed"
	codeTaskNotFound     = "task_not_found"
	codeEditsInvalid     = "edits_invalid"
	codeASRRuntimeMiss   = "asr_runtime_missing"
	codeTranscribeFailed = "transcribe_failed"
)

// asrMissingHint sidecar 缺失时的安装指引（§19.5）。
const asrMissingHint = "ASR 运行时未安装。请运行 scripts/bootstrap-asr.ps1（创建 venv、安装 faster-whisper、预热模型），或在 config.yaml 配置 asr.python 指向已有 venv。"

// jsonResult 成功结果（text + structuredContent 双载体，兼容不同 MCP 客户端）。
func jsonResult(v any) (*mcp.CallToolResult, error) {
	res, err := mcp.NewToolResultJSON(v)
	if err != nil {
		return failResult(codeInternal, "", "序列化结果失败: "+err.Error()), nil
	}
	return res, nil
}

// failResult 失败结果：{error, code, hint} 结构 + isError 标志（§19.2）。
func failResult(code, hint, msg string) *mcp.CallToolResult {
	payload := map[string]any{"error": msg, "code": code}
	if hint != "" {
		payload["hint"] = hint
	}
	// fallbackText 用同一 JSON，保证只读 text content 的客户端也能拿到结构
	raw, _ := json.Marshal(payload)
	res := mcp.NewToolResultStructured(payload, string(raw))
	res.IsError = true
	return res
}

// resultErrText 从失败结果中提取错误摘要（调用日志用）：优先 text content，退化到结构化 error 字段。
func resultErrText(res *mcp.CallToolResult) string {
	if res == nil {
		return ""
	}
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok && tc.Text != "" {
			return tc.Text
		}
	}
	if res.StructuredContent != nil {
		if m, ok := res.StructuredContent.(map[string]any); ok {
			if e, ok := m["error"].(string); ok {
				return e
			}
		}
	}
	return "tool error"
}
