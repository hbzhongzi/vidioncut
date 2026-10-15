package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"videocut/internal/config"
	"videocut/internal/service"
)

func newPreviewSvc(t *testing.T) (*PreviewHandler, string) {
	t.Helper()
	svc := service.NewService(config.Default(), "C:/dummy/ffmpeg.exe", "C:/dummy/ffprobe.exe")
	dir := t.TempDir()
	file := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(file, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc.RegisterPreviewPath(file)
	return NewPreviewHandler(svc), file
}

func TestPreview_NotPreviewPath(t *testing.T) {
	h, _ := newPreviewSvc(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/static/x.js", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("code=%d want 404", rec.Code)
	}
}

func TestPreview_MissingPath(t *testing.T) {
	h, _ := newPreviewSvc(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/preview", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code=%d want 400", rec.Code)
	}
}

func TestPreview_RelativePath(t *testing.T) {
	h, _ := newPreviewSvc(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/preview?path=relative/clip.mp4", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code=%d want 400 for relative path", rec.Code)
	}
}

func TestPreview_Forbidden(t *testing.T) {
	h, _ := newPreviewSvc(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/preview?path=C:/secret/other.mp4", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("code=%d want 403 for non-whitelisted path", rec.Code)
	}
}

func TestPreview_ServeAllowed(t *testing.T) {
	h, file := newPreviewSvc(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/preview?path="+file, nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "video/mp4" {
		t.Errorf("Content-Type=%q want video/mp4", ct)
	}
	if body := rec.Body.String(); body != "0123456789" {
		t.Errorf("body=%q want 0123456789", body)
	}
}

func TestPreview_RangeRequest(t *testing.T) {
	h, file := newPreviewSvc(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/preview?path="+file, nil)
	req.Header.Set("Range", "bytes=0-4")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("code=%d want 206 (Partial Content)", rec.Code)
	}
	if cr := rec.Header().Get("Content-Range"); cr == "" {
		t.Error("expected Content-Range header for range request")
	}
	if body, _ := io.ReadAll(rec.Body); len(body) != 5 {
		t.Errorf("range body len=%d want 5", len(body))
	}
}

func TestContentType(t *testing.T) {
	cases := map[string]string{
		".mp4":  "video/mp4",
		".mkv":  "video/x-matroska",
		".mov":  "video/quicktime",
		".avi":  "video/x-msvideo",
		".webm": "video/webm",
		".mp3":  "audio/mpeg",
		".aac":  "audio/aac",
		".m4a":  "audio/mp4",
		".wav":  "audio/wav",
		".flac": "audio/flac",
		".xyz":  "application/octet-stream",
	}
	for ext, want := range cases {
		if got := contentType(ext); got != want {
			t.Errorf("contentType(%q)=%q want %q", ext, got, want)
		}
	}
}
