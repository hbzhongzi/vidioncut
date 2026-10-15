package ffmpeg

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
)

// ProbeResult 是 ffprobe 输出解析后的结构化元数据（开发文档 v1.1 §4.1）。
// JSON tag 必须与前端 types.ts 的 ProbeResult（snake_case）严格一致，
// 否则 Wails 序列化后前端取不到值（duration/has_video/width 等）。
type ProbeResult struct {
	Duration   float64      `json:"duration"`
	FormatName string       `json:"format_name"`
	HasVideo   bool         `json:"has_video"`
	HasAudio   bool         `json:"has_audio"`
	Width      int          `json:"width"`
	Height     int          `json:"height"`
	Streams    []StreamInfo `json:"streams"`
}

// StreamInfo 单条流信息。
type StreamInfo struct {
	Index      int     `json:"index"`
	CodecType  string  `json:"codec_type"`
	CodecName  string  `json:"codec_name"`
	Width      *int    `json:"width"`
	Height     *int    `json:"height"`
	Duration   *string `json:"duration"`
	// v1.4：合并模式判定所需字段（detectMergeMode）
	PixFmt     *string `json:"pix_fmt"`
	SampleRate *string `json:"sample_rate"`
	Channels   *int    `json:"channels"`
}

type ffprobeOutput struct {
	Streams []StreamInfo `json:"streams"`
	Format  struct {
		Duration   string `json:"duration"`
		FormatName string `json:"format_name"`
	} `json:"format"`
}

// Probe 调用 ffprobe 获取媒体元数据。
// 纯音频流无 width/height 字段，用指针兜底（开发文档 v1.1 §4.1）。
func Probe(ctx context.Context, ffprobePath, inputPath string) (*ProbeResult, error) {
	args := []string{
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		"-i", inputPath,
	}
	cmd := exec.CommandContext(ctx, ffprobePath, args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w", err)
	}

	res, err := parseProbeOutput(out)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// parseProbeOutput 将 ffprobe 的 JSON 输出解析为结构化元数据（开发文档 v1.1 §4.1）。
// 纯音频流无 width/height 字段，用指针兜底。抽出为独立函数便于单测。
func parseProbeOutput(out []byte) (*ProbeResult, error) {
	var parsed ffprobeOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("ffprobe parse: %w", err)
	}

	res := &ProbeResult{FormatName: parsed.Format.FormatName}
	if d, perr := strconv.ParseFloat(parsed.Format.Duration, 64); perr == nil {
		res.Duration = d
	}

	for _, s := range parsed.Streams {
		res.Streams = append(res.Streams, s)
		switch s.CodecType {
		case "video":
			res.HasVideo = true
			if s.Width != nil {
				res.Width = *s.Width
			}
			if s.Height != nil {
				res.Height = *s.Height
			}
		case "audio":
			res.HasAudio = true
		}
	}
	return res, nil
}
