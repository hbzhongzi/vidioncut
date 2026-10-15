// Package mcpserver 实现 videocut 的 MCP Server（开发文档 v1.7 §19.2/§19.3）：
// `videocut.exe mcp-serve` 子命令以 stdio JSON-RPC 暴露 15 个工具
// （M8 剪切族 6 + M9 ASR 族 3 + M8 编辑族 3 + M12 字幕族 3），
// 剪切/合并/探测 100% 复用 internal/service 既有能力（零业务逻辑重复）。
package mcpserver

import (
	"context"
	"os"
	"time"

	mcpsdk "github.com/mark3labs/mcp-go/server"
	mcp "github.com/mark3labs/mcp-go/mcp"

	"videocut/internal/asr"
	"videocut/internal/config"
	"videocut/internal/service"
)

// serverName/version 出现在 MCP 握手信息中。
const (
	serverName = "videocut"
	version    = "2.0.0"
)

// Server MCP Server 门面。
type Server struct {
	cfg   *config.Config
	svc   *service.Service
	asr   *asr.Client
	srv   *mcpsdk.MCPServer
	calls *CallLogger // 工具调用日志（§19.8 Codex 面板数据源；nil=不记录）
}

// New 构造并注册全部工具。asrClient 为 nil 时 ASR 工具仍注册，
// 调用时返回 asr_runtime_missing（对齐输入文档"Runtime 缺失行为"）。
func New(cfg *config.Config, svc *service.Service, asrClient *asr.Client) *Server {
	s := &Server{cfg: cfg, svc: svc, asr: asrClient}
	if cl, err := NewCallLogger(); err == nil {
		s.calls = cl
	}
	s.srv = mcpsdk.NewMCPServer(serverName, version,
		mcpsdk.WithInstructions("videocut 本地音视频裁剪与 AI 粗剪。工作流：probe 探测 → trim/trim_batch/merge 剪切（异步，返回 task_id）→ get_task 轮询 → cancel 取消。AI 粗剪：process 转录 → suggest_edits 候选 → validate_edits 校验 → get_edits_summary 摘要 → trim_batch 按保留段剪切 → export_transcript 导稿。字幕（M12）：export_subtitles 重对齐外显（剪映/PR 精修入口）→ animate_captions 自动特效 → burn_subtitles 内嵌烧录（分发）。"),
		mcpsdk.WithToolCapabilities(true),
	)
	s.registerTools()
	s.registerASRTools()
	s.registerSubtitleTools()
	return s
}

// MCPServer 暴露底层 SDK server（测试用）。
func (s *Server) MCPServer() *mcpsdk.MCPServer { return s.srv }

// addTool 注册工具并包裹调用日志（工具名取自 tool.Name）。
// 日志失败不影响工具调用本身（calllog.Log 内部已静默容错）。
func (s *Server) addTool(tool mcp.Tool, handler mcpsdk.ToolHandlerFunc) {
	wrapped := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		start := time.Now()
		res, err := handler(ctx, req)
		ok := err == nil && res != nil && !res.IsError
		errMsg := ""
		if err != nil {
			errMsg = err.Error()
		} else if res != nil && res.IsError {
			errMsg = resultErrText(res)
		}
		if s.calls != nil {
			s.calls.Log(CallEntry{Tool: tool.Name, OK: ok, MS: time.Since(start).Milliseconds(), Err: errMsg})
		}
		return res, err
	}
	s.srv.AddTool(tool, wrapped)
}

// Serve 以 stdio 方式服务。
// 注意：stdout 仅允许协议帧——日志一律走 stderr（v1.7 §19.2，R26）。
func (s *Server) Serve(ctx context.Context) error {
	stdio := mcpsdk.NewStdioServer(s.srv)
	return stdio.Listen(ctx, os.Stdin, os.Stdout)
}
