package ffmpeg

import (
	"strings"
	"testing"
)

func containsAll(t *testing.T, args []string, want ...string) {
	t.Helper()
	for _, w := range want {
		found := false
		for _, a := range args {
			if a == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("期望参数包含 %q，实际: %v", w, args)
		}
	}
}

func TestBuildTrimArgs_StreamCopy(t *testing.T) {
	args := BuildTrimArgs(StrategyStreamCopy, "C:/videos/in.mp4", "C:/videos/out.mp4", 0, 10.0)
	containsAll(t, args,
		"-ss", "0.000",
		"-to", "10.000",
		"-i", "C:/videos/in.mp4",
		"-c", "copy",
		"-avoid_negative_ts", "make_zero",
		"-y",
	)
	// 输出应为 .part 临时文件
	if !strings.HasSuffix(args[len(args)-1], "out.part.mp4") {
		t.Errorf("stream_copy 输出应为 .part 文件，实际: %s", args[len(args)-1])
	}
}

func TestBuildTrimArgs_Reencode(t *testing.T) {
	args := BuildTrimArgs(StrategyReencode, "in.mp4", "out.mp4", 5.0, 12.0)
	containsAll(t, args,
		"-ss", "5.000",
		"-i", "in.mp4",
		"-t", "7.000", // 12-5
		"-c:v", "libx264",
		"-preset", "fast",
		"-c:a", "aac",
	)
}

func TestBuildTrimArgs_AudioOnly(t *testing.T) {
	args := BuildTrimArgs(StrategyAudioOnly, "in.mp3", "out.mp3", 1.0, 9.0)
	containsAll(t, args,
		"-ss", "1.000",
		"-to", "9.000",
		"-vn",
		"-c:a", "copy",
	)
}

func TestPartPath(t *testing.T) {
	if got := PartPath("a/b.mp4"); got != "a/b.part.mp4" {
		t.Errorf("PartPath = %q, want a/b.part.mp4", got)
	}
	if got := PartPath("noext"); got != "noext.part" {
		t.Errorf("PartPath = %q, want noext.part", got)
	}
}
