package ffmpeg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func ptrS(s string) *string { return &s }
func ptrI(i int) *int       { return &i }

// probeWith 构造一个带指定参数的 ProbeResult 测试替身。
func probeWith(videoCodec string, w, h int, pixFmt string, audioCodec, sampleRate string, channels int) *ProbeResult {
	p := &ProbeResult{}
	if videoCodec != "" {
		p.HasVideo = true
		p.Width, p.Height = w, h
		p.Streams = append(p.Streams, StreamInfo{
			Index: 0, CodecType: "video", CodecName: videoCodec,
			Width: ptrI(w), Height: ptrI(h), PixFmt: ptrS(pixFmt),
		})
	}
	if audioCodec != "" {
		p.HasAudio = true
		p.Streams = append(p.Streams, StreamInfo{
			Index: 1, CodecType: "audio", CodecName: audioCodec,
			SampleRate: ptrS(sampleRate), Channels: ptrI(channels),
		})
	}
	p.Duration = 5.0
	return p
}

func TestEscapeConcatPath(t *testing.T) {
	cases := []struct{ in, want string }{
		{`C:/clips/1.mp4`, `C:/clips/1.mp4`},
		{`C:\clips\1.mp4`, `C:/clips/1.mp4`},                     // 反斜杠转正斜杠
		{`C:/my clips/a b.mp4`, `C:/my clips/a b.mp4`},           // 空格保留
		{`C:/视频/片段.mp4`, `C:/视频/片段.mp4`},                    // 中文保留
		{`C:/it's/a.mp4`, `C:/it'\''s/a.mp4`},                    // 单引号转义
		{`C:/a'b\c'd.mp4`, `C:/a'\''b/c'\''d.mp4`},               // 组合
	}
	for _, c := range cases {
		if got := escapeConcatPath(c.in); got != c.want {
			t.Errorf("escapeConcatPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestWriteConcatList(t *testing.T) {
	dir := t.TempDir()
	listPath := filepath.Join(dir, "list.txt")
	inputs := []string{`C:/clips/1.mp4`, `C:/my clips/it's 2.mp4`, `D:\视频\3.mp4`}
	if err := WriteConcatList(inputs, listPath); err != nil {
		t.Fatalf("WriteConcatList: %v", err)
	}
	data, err := os.ReadFile(listPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != len(inputs) {
		t.Fatalf("got %d lines, want %d:\n%s", len(lines), len(inputs), data)
	}
	want := []string{
		`file 'C:/clips/1.mp4'`,
		`file 'C:/my clips/it'\''s 2.mp4'`,
		`file 'D:/视频/3.mp4'`,
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("line %d = %q, want %q", i, lines[i], w)
		}
	}
	// UTF-8 无 BOM
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		t.Error("list file should not have BOM")
	}
}

func TestDetectMergeMode(t *testing.T) {
	same := []*ProbeResult{
		probeWith("h264", 640, 360, "yuv420p", "aac", "44100", 2),
		probeWith("h264", 640, 360, "yuv420p", "aac", "44100", 2),
	}
	cases := []struct {
		name   string
		probes []*ProbeResult
		want   string
	}{
		{"同参含音视频→demuxer", same, MergeModeDemuxer},
		{"分辨率不同→filter", []*ProbeResult{
			probeWith("h264", 640, 360, "yuv420p", "aac", "44100", 2),
			probeWith("h264", 1280, 720, "yuv420p", "aac", "44100", 2),
		}, MergeModeFilter},
		{"视频编码不同→filter", []*ProbeResult{
			probeWith("h264", 640, 360, "yuv420p", "aac", "44100", 2),
			probeWith("hevc", 640, 360, "yuv420p", "aac", "44100", 2),
		}, MergeModeFilter},
		{"像素格式不同→filter", []*ProbeResult{
			probeWith("h264", 640, 360, "yuv420p", "aac", "44100", 2),
			probeWith("h264", 640, 360, "yuv444p", "aac", "44100", 2),
		}, MergeModeFilter},
		{"采样率不同→filter", []*ProbeResult{
			probeWith("h264", 640, 360, "yuv420p", "aac", "44100", 2),
			probeWith("h264", 640, 360, "yuv420p", "aac", "48000", 2),
		}, MergeModeFilter},
		{"声道数不同→filter", []*ProbeResult{
			probeWith("h264", 640, 360, "yuv420p", "aac", "44100", 2),
			probeWith("h264", 640, 360, "yuv420p", "aac", "44100", 1),
		}, MergeModeFilter},
		{"音频编码不同→filter", []*ProbeResult{
			probeWith("h264", 640, 360, "yuv420p", "aac", "44100", 2),
			probeWith("h264", 640, 360, "yuv420p", "mp3", "44100", 2),
		}, MergeModeFilter},
		{"音频存在性不同→filter", []*ProbeResult{
			probeWith("h264", 640, 360, "yuv420p", "aac", "44100", 2),
			probeWith("h264", 640, 360, "yuv420p", "", "", 0),
		}, MergeModeFilter},
		{"视频存在性不同→filter", []*ProbeResult{
			probeWith("h264", 640, 360, "yuv420p", "aac", "44100", 2),
			probeWith("", 0, 0, "", "aac", "44100", 2),
		}, MergeModeFilter},
		{"同参纯视频→demuxer", []*ProbeResult{
			probeWith("h264", 640, 360, "yuv420p", "", "", 0),
			probeWith("h264", 640, 360, "yuv420p", "", "", 0),
		}, MergeModeDemuxer},
		{"单项→demuxer", []*ProbeResult{
			probeWith("h264", 640, 360, "yuv420p", "aac", "44100", 2),
		}, MergeModeDemuxer},
	}
	for _, c := range cases {
		if got := DetectMergeMode(c.probes); got != c.want {
			t.Errorf("%s: DetectMergeMode = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestBuildConcatFilter(t *testing.T) {
	t.Run("全部含音视频", func(t *testing.T) {
		probes := []*ProbeResult{
			probeWith("h264", 640, 360, "yuv420p", "aac", "44100", 2),
			probeWith("h264", 640, 360, "yuv420p", "aac", "44100", 2),
		}
		filter, silent, err := BuildConcatFilter(probes, []float64{3, 4})
		if err != nil {
			t.Fatalf("BuildConcatFilter: %v", err)
		}
		if len(silent) != 0 {
			t.Errorf("expected no silent inputs, got %v", silent)
		}
		if !strings.Contains(filter, "[0:v]scale=640:360") || !strings.Contains(filter, "[1:v]scale=640:360") {
			t.Errorf("missing video normalize chains:\n%s", filter)
		}
		if !strings.Contains(filter, "aformat=sample_fmts=fltp:sample_rates=44100:channel_layouts=stereo") {
			t.Errorf("missing audio normalize chain:\n%s", filter)
		}
		if !strings.Contains(filter, "[v0][a0][v1][a1]concat=n=2:v=1:a=1[outv][outa]") {
			t.Errorf("missing concat segment:\n%s", filter)
		}
	})

	t.Run("纯视频", func(t *testing.T) {
		probes := []*ProbeResult{
			probeWith("h264", 640, 360, "yuv420p", "", "", 0),
			probeWith("h264", 640, 360, "yuv420p", "", "", 0),
		}
		filter, silent, err := BuildConcatFilter(probes, []float64{3, 4})
		if err != nil {
			t.Fatalf("BuildConcatFilter: %v", err)
		}
		if len(silent) != 0 {
			t.Errorf("expected no silent inputs, got %v", silent)
		}
		if !strings.Contains(filter, "[v0][v1]concat=n=2:v=1:a=0[outv]") {
			t.Errorf("wrong video-only concat:\n%s", filter)
		}
		if strings.Contains(filter, "[outa]") {
			t.Errorf("video-only merge should not have [outa]:\n%s", filter)
		}
	})

	t.Run("混合音频补静音轨", func(t *testing.T) {
		probes := []*ProbeResult{
			probeWith("h264", 640, 360, "yuv420p", "aac", "48000", 2),
			probeWith("h264", 640, 360, "yuv420p", "", "", 0),
		}
		filter, silent, err := BuildConcatFilter(probes, []float64{3, 4.5})
		if err != nil {
			t.Fatalf("BuildConcatFilter: %v", err)
		}
		if len(silent) != 1 {
			t.Fatalf("expected 1 silent input, got %d", len(silent))
		}
		if silent[0].Duration != 4.5 || silent[0].SampleRate != "48000" {
			t.Errorf("silent input = %+v, want {4.5 48000}", silent[0])
		}
		if !strings.Contains(filter, "[v0][a0][v1][2:a]concat=n=2:v=1:a=1[outv][outa]") {
			t.Errorf("wrong mixed-audio concat:\n%s", filter)
		}
	})

	t.Run("全音频", func(t *testing.T) {
		probes := []*ProbeResult{
			probeWith("", 0, 0, "", "mp3", "44100", 2),
			probeWith("", 0, 0, "", "mp3", "44100", 2),
		}
		filter, silent, err := BuildConcatFilter(probes, []float64{3, 4})
		if err != nil {
			t.Fatalf("BuildConcatFilter: %v", err)
		}
		if len(silent) != 0 {
			t.Errorf("expected no silent inputs, got %v", silent)
		}
		if !strings.Contains(filter, "[a0][a1]concat=n=2:v=0:a=1[outa]") {
			t.Errorf("wrong audio-only concat:\n%s", filter)
		}
		if strings.Contains(filter, "[outv]") {
			t.Errorf("audio-only merge should not have [outv]:\n%s", filter)
		}
	})

	t.Run("视频音频混合存在→报错", func(t *testing.T) {
		probes := []*ProbeResult{
			probeWith("h264", 640, 360, "yuv420p", "", "", 0),
			probeWith("", 0, 0, "", "mp3", "44100", 2),
		}
		if _, _, err := BuildConcatFilter(probes, []float64{3, 4}); err == nil {
			t.Error("expected error for mixed video/audio presence")
		}
	})

	t.Run("无音视频流→报错", func(t *testing.T) {
		probes := []*ProbeResult{
			probeWith("", 0, 0, "", "", "", 0),
			probeWith("", 0, 0, "", "", "", 0),
		}
		if _, _, err := BuildConcatFilter(probes, []float64{3, 4}); err == nil {
			t.Error("expected error for no streams")
		}
	})

	t.Run("混合音频但时长未知→报错", func(t *testing.T) {
		probes := []*ProbeResult{
			probeWith("h264", 640, 360, "yuv420p", "aac", "44100", 2),
			probeWith("h264", 640, 360, "yuv420p", "", "", 0),
		}
		if _, _, err := BuildConcatFilter(probes, []float64{3, 0}); err == nil {
			t.Error("expected error when silent input duration unknown")
		}
	})

	t.Run("空输入→报错", func(t *testing.T) {
		if _, _, err := BuildConcatFilter(nil, nil); err == nil {
			t.Error("expected error for empty input")
		}
	})
}

func TestBuildConcatListArgs(t *testing.T) {
	args := BuildConcatListArgs("C:/tmp/list.txt", "C:/out/merged.mp4")
	joined := strings.Join(args, " ")
	for _, want := range []string{"-f concat", "-safe 0", "-i C:/tmp/list.txt", "-c copy", "-avoid_negative_ts make_zero", "-y"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %v", want, args)
		}
	}
	// 输出必须是 .part 路径（同卷 rename 由调用方负责）
	if got := args[len(args)-1]; got != "C:/out/merged.part.mp4" {
		t.Errorf("last arg = %q, want C:/out/merged.part.mp4", got)
	}
}

func TestBuildConcatFilterArgs(t *testing.T) {
	inputs := []string{"C:/tmp/seg_000.mp4", "C:/tmp/seg_001.mp4"}
	silent := []SilentInput{{Duration: 4.5, SampleRate: "48000"}}
	filter := "[v0][a0][v1][2:a]concat=n=2:v=1:a=1[outv][outa]"
	args := BuildConcatFilterArgs(inputs, silent, filter, "C:/out/merged.mp4")
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"-i C:/tmp/seg_000.mp4", "-i C:/tmp/seg_001.mp4",
		"-f lavfi", "-t 4.500", "-i anullsrc=channel_layout=stereo:sample_rate=48000",
		"-filter_complex", "-map [outv]", "-map [outa]",
		"-c:v libx264", "-preset fast", "-c:a aac",
		"-avoid_negative_ts make_zero", "-y",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %v", want, args)
		}
	}
	if got := args[len(args)-1]; got != "C:/out/merged.part.mp4" {
		t.Errorf("last arg = %q, want .part path", got)
	}

	// 纯视频：无 anullsrc、无 -c:a
	filterV := "[v0][v1]concat=n=2:v=1:a=0[outv]"
	argsV := BuildConcatFilterArgs(inputs, nil, filterV, "C:/out/v.mp4")
	joinedV := strings.Join(argsV, " ")
	if strings.Contains(joinedV, "lavfi") || strings.Contains(joinedV, "-c:a") {
		t.Errorf("video-only args should not contain lavfi/-c:a: %v", argsV)
	}
	if !strings.Contains(joinedV, "-map [outv]") || strings.Contains(joinedV, "-map [outa]") {
		t.Errorf("video-only args mapping wrong: %v", argsV)
	}
}
