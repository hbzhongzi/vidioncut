package ffmpeg

// M12 内嵌字幕烧录参数测试（字幕功能更新文档 §4.4 要点 1：路径转义）。

import (
	"strings"
	"testing"
)

func TestEscapeAssPath(t *testing.T) {
	cases := []struct{ in, want string }{
		{`C:\Users\x\edited.ass`, `C\\:/Users/x/edited.ass`},
		{`E:\videocut\out\edited_fx.ass`, `E\\:/videocut/out/edited_fx.ass`},
		{`C:/already/forward.ass`, `C\\:/already/forward.ass`},
		{`relative\path.ass`, `relative/path.ass`},
		{"", ""},
	}
	for _, tc := range cases {
		if got := EscapeAssPath(tc.in); got != tc.want {
			t.Errorf("EscapeAssPath(%q) = %q 期望 %q", tc.in, got, tc.want)
		}
	}
}

func TestBuildBurnArgs_Defaults(t *testing.T) {
	args := BuildBurnArgs(`E:\v\edited.mp4`, `E:\v\edited.ass`, `E:\v\edited_burned.mp4`, "", 0, "")
	joined := strings.Join(args, " ")
	wants := []string{
		"-i E:\\v\\edited.mp4",
		`-vf ass=E\\:/v/edited.ass`,     // 双反斜杠冒号转义 + 正斜杠（Go exec 不过 shell）
		"-c:v libx264",                  // 编码器回落
		"-preset fast",                  // preset 回落
		"-crf 18",                       // crf 回落
		"-c:a copy",                     // 音轨直复制（Q-02）
		"E:\\v\\edited_burned.part.mp4", // .part 原子落地约定
	}
	for _, w := range wants {
		if !strings.Contains(joined, w) {
			t.Errorf("缺少 %q\n完整: %s", w, joined)
		}
	}
}

func TestBuildBurnArgs_Explicit(t *testing.T) {
	args := BuildBurnArgs("in.mp4", "sub.ass", "out.mp4", "h264_nvenc", 23, "p4")
	joined := strings.Join(args, " ")
	for _, w := range []string{"-c:v h264_nvenc", "-preset p4", "-crf 23"} {
		if !strings.Contains(joined, w) {
			t.Errorf("显式参数应生效，缺少 %q: %s", w, joined)
		}
	}
}

func TestBuildBurnArgs_NoAvoidNegativeTS(t *testing.T) {
	// 与裁剪一致：避免 Windows 播放器时间戳问题
	args := BuildBurnArgs("in.mp4", "sub.ass", "out.mp4", "", 0, "")
	if !contains(args, "-avoid_negative_ts") {
		t.Error("应含 -avoid_negative_ts make_zero")
	}
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}
