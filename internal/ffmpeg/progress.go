package ffmpeg

import (
	"strconv"
	"strings"
)

// Progress 表示一次裁剪任务的进度快照。
type Progress struct {
	Percent float64 // 0-100
	OutTime float64 // 已处理时长（秒）
	Speed   string  // 如 "12.3x"
	Done    bool    // progress=end
}

// parseOutTime 解析 FFmpeg -progress 输出的 out_time 字段。
// 格式可能是 HH:MM:SS.microseconds 或纯秒数字符串，两者都兼容。
func parseOutTime(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if strings.Contains(s, ":") {
		parts := strings.Split(s, ":")
		var sec float64
		// 支持 H:M:S 或 M:S
		for _, p := range parts {
			f, err := strconv.ParseFloat(p, 64)
			if err != nil {
				return 0, false
			}
			sec = sec*60 + f
		}
		return sec, true
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// splitKV 解析 "key=value" 行。
func splitKV(line string) (string, string, bool) {
	idx := strings.Index(line, "=")
	if idx < 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:idx]), strings.TrimSpace(line[idx+1:]), true
}

// safeBuffer 保留最后 maxBytes 的写入内容，用于错误时回显 FFmpeg stderr 尾部。
type safeBuffer struct {
	buf      []byte
	maxBytes int
}

func newSafeBuffer(max int) *safeBuffer {
	return &safeBuffer{maxBytes: max}
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	if len(p) >= b.maxBytes {
		b.buf = append(b.buf[:0], p[len(p)-b.maxBytes:]...)
		return len(p), nil
	}
	b.buf = append(b.buf, p...)
	if len(b.buf) > b.maxBytes {
		b.buf = b.buf[len(b.buf)-b.maxBytes:]
	}
	return len(p), nil
}

func (b *safeBuffer) Tail() string {
	return strings.TrimSpace(string(b.buf))
}
