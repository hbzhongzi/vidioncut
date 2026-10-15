package ffmpeg

import (
	"encoding/json"
	"testing"
)

const sampleVideoAudio = `{
  "streams": [
    {
      "index": 0,
      "codec_type": "video",
      "codec_name": "h264",
      "width": 1920,
      "height": 1080
    },
    {
      "index": 1,
      "codec_type": "audio",
      "codec_name": "aac"
    }
  ],
  "format": {
    "duration": "123.456000",
    "format_name": "mov,mp4,m4a,3gp,3g2,mj2"
  }
}`

const sampleAudioOnly = `{
  "streams": [
    {
      "index": 0,
      "codec_type": "audio",
      "codec_name": "mp3"
    }
  ],
  "format": {
    "duration": "200.0",
    "format_name": "mp3"
  }
}`

func TestParseProbeOutput_VideoAudio(t *testing.T) {
	res, err := parseProbeOutput([]byte(sampleVideoAudio))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Duration != 123.456 {
		t.Errorf("Duration=%v want 123.456", res.Duration)
	}
	if res.FormatName != "mov,mp4,m4a,3gp,3g2,mj2" {
		t.Errorf("FormatName=%q", res.FormatName)
	}
	if !res.HasVideo || !res.HasAudio {
		t.Errorf("HasVideo=%v HasAudio=%v want both true", res.HasVideo, res.HasAudio)
	}
	if res.Width != 1920 || res.Height != 1080 {
		t.Errorf("Width=%d Height=%d want 1920x1080", res.Width, res.Height)
	}
	if len(res.Streams) != 2 {
		t.Errorf("Streams=%d want 2", len(res.Streams))
	}
}

func TestParseProbeOutput_AudioOnly(t *testing.T) {
	res, err := parseProbeOutput([]byte(sampleAudioOnly))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.HasVideo {
		t.Error("audio-only file should not HasVideo")
	}
	if !res.HasAudio {
		t.Error("should HasAudio")
	}
	if res.Width != 0 || res.Height != 0 {
		t.Errorf("audio-only should have 0x0, got %dx%d", res.Width, res.Height)
	}
	if res.Duration != 200.0 {
		t.Errorf("Duration=%v want 200.0", res.Duration)
	}
}

func TestParseProbeOutput_InvalidJSON(t *testing.T) {
	_, err := parseProbeOutput([]byte("not json at all {"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !containsStr(err.Error(), "ffprobe parse") {
		t.Errorf("error should indicate parse failure, got %v", err)
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && indexOfStr(s, sub) >= 0
}

func indexOfStr(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// TestProbeResult_JSONContract 锁定 Wails→前端的 JSON 键名契约（snake_case）。
// 前端 types.ts 的 ProbeResult 依赖这些确切键名；缺 tag 会静默破坏元信息与时间轴。
func TestProbeResult_JSONContract(t *testing.T) {
	res := &ProbeResult{
		Duration:   12.5,
		FormatName: "mov,mp4,m4a,3gp,3g2,mj2",
		HasVideo:   true,
		HasAudio:   true,
		Width:      1920,
		Height:     1080,
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{"duration", "format_name", "has_video", "has_audio", "width", "height", "streams"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing snake_case key %q in %s", k, string(b))
		}
	}
	// 反向：不允许出现 PascalCase 泄漏
	for _, k := range []string{"Duration", "HasVideo", "HasAudio", "Width", "Height"} {
		if _, ok := m[k]; ok {
			t.Errorf("PascalCase key %q leaked into JSON contract", k)
		}
	}
}
