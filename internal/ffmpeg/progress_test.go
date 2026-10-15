package ffmpeg

import (
	"strings"
	"testing"
)

func TestParseOutTime(t *testing.T) {
	cases := []struct {
		in      string
		want    float64
		wantOK  bool
	}{
		{"", 0, false},
		{"   ", 0, false},
		{"12.500", 12.5, true},
		{"00:02.000", 2.0, true},            // M:S
		{"00:00:03.250", 3.25, true},        // H:M:S
		{"01:02:03.000", 3723.0, true},      // 1h2m3s
		{"not-a-number", 0, false},
		{"12:xx", 0, false},
	}
	for _, c := range cases {
		got, ok := parseOutTime(c.in)
		if ok != c.wantOK {
			t.Errorf("parseOutTime(%q) ok=%v want %v", c.in, ok, c.wantOK)
			continue
		}
		if ok && got != c.want {
			t.Errorf("parseOutTime(%q)=%v want %v", c.in, got, c.want)
		}
	}
}

func TestSplitKV(t *testing.T) {
	k, v, ok := splitKV("out_time=12.500")
	if !ok || k != "out_time" || v != "12.500" {
		t.Errorf("splitKV = (%q,%q,%v) want (out_time,12.500,true)", k, v, ok)
	}
	k, v, ok = splitKV(" no spaces = yes ")
	if !ok || k != "no spaces" || v != "yes" {
		t.Errorf("splitKV trimmed = (%q,%q,%v)", k, v, ok)
	}
	if _, _, ok := splitKV("noequal"); ok {
		t.Error("splitKV should fail without '='")
	}
}

func TestSafeBufferTail(t *testing.T) {
	b := newSafeBuffer(10)
	// 写入短内容，应完整保留
	_, _ = b.Write([]byte("abc"))
	if b.Tail() != "abc" {
		t.Errorf("Tail=%q want abc", b.Tail())
	}
	// 超出 maxBytes，只保留尾部
	_, _ = b.Write([]byte("defghijklmnop"))
	if len(b.Tail()) != 10 {
		t.Errorf("Tail len=%d want 10 (got %q)", len(b.Tail()), b.Tail())
	}
	if !strings.HasSuffix(b.Tail(), "klmnop") {
		t.Errorf("Tail should end with klmnop, got %q", b.Tail())
	}
}
