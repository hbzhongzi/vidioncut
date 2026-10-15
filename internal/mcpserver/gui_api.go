package mcpserver

// GUI 复用入口（M11 Studio 面板）：与 MCP 工具共用同一套内部逻辑，
// 避免业务规则两处实现（开发文档 §19.8）。

import (
	"fmt"
	"os"
	"path/filepath"

	"videocut/internal/edits"
)

// ExportTranscriptFile 渲染文字稿并落盘：<baseDir>/work/transcript.{md|txt}。
// 返回输出文件绝对路径；SRT 不存在时返回错误（GUI 提示"请先转录"）。
func ExportTranscriptFile(baseDir, format string) (string, error) {
	if format != "md" && format != "txt" {
		format = "md"
	}
	srtPath := filepath.Join(baseDir, "work", "transcript.srt")
	text, err := edits.ExportTranscript(srtPath, format)
	if err != nil {
		return "", err
	}
	out := filepath.Join(baseDir, "work", "transcript."+format)
	if err := os.WriteFile(out, []byte(text), 0o644); err != nil {
		return "", fmt.Errorf("写入 %s 失败: %w", out, err)
	}
	return out, nil
}
