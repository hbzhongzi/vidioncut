package server

import (
	"net/http"
	"os"
	"path/filepath"

	"videocut/internal/service"
)

// PreviewHandler 通过 Wails AssetServer 提供本地文件预览（开发文档 v1.1 §5.4）。
// 前端使用相对 URL `/preview?path=<绝对路径>`，由 Go 侧流式返回，
// http.ServeContent 自动处理 Range 请求，<video> 可拖动进度。
type PreviewHandler struct {
	svc *service.Service
}

// NewPreviewHandler 构造预览 Handler。
func NewPreviewHandler(svc *service.Service) *PreviewHandler {
	return &PreviewHandler{svc: svc}
}

// ServeHTTP 仅处理 /preview，其余返回 404。
func (h *PreviewHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/preview" {
		http.NotFound(w, r)
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "missing path", http.StatusBadRequest)
		return
	}
	if !filepath.IsAbs(path) {
		http.Error(w, "path must be absolute", http.StatusBadRequest)
		return
	}
	// 安全边界：仅允许白名单内的路径（由用户选择/处理的文件）
	if !h.svc.IsPreviewAllowed(path) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "cannot open file", http.StatusNotFound)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		http.Error(w, "stat failed", http.StatusInternalServerError)
		return
	}
	if info.IsDir() {
		http.Error(w, "not a file", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", contentType(path))
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), f)
}

func contentType(path string) string {
	switch filepath.Ext(path) {
	case ".mp4":
		return "video/mp4"
	case ".mkv":
		return "video/x-matroska"
	case ".mov":
		return "video/quicktime"
	case ".avi":
		return "video/x-msvideo"
	case ".webm":
		return "video/webm"
	case ".mp3":
		return "audio/mpeg"
	case ".aac":
		return "audio/aac"
	case ".m4a":
		return "audio/mp4"
	case ".wav":
		return "audio/wav"
	case ".flac":
		return "audio/flac"
	default:
		return "application/octet-stream"
	}
}
