package ffmpeg

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Strategy 裁剪策略（开发文档 v1.1 §4.2）。
type Strategy string

const (
	StrategyStreamCopy Strategy = "stream_copy" // 流复制，速度快、无损，按关键帧粗定位
	StrategyReencode   Strategy = "reencode"    // 重编码，可精确到帧
	StrategyAudioOnly  Strategy = "audio_only"  // 纯音频/无视频流，仅处理音频
)

// BuildTrimArgs 构造 FFmpeg 裁剪参数切片（不含 ffmpeg 本体）。
// 返回的参数可直接 append 到 exec.Command 的 args。
//
// 要点：
//   - 流复制：-ss/-to 置于 -i 之前（关键帧粗定位），-to 为绝对时间。
//   - 重编码：-ss 置于 -i 之后（精确裁切），用 -t <duration> 表示裁切长度，避免 -to 语义歧义。
//   - 所有路径均加 -avoid_negative_ts make_zero，兼容 Windows 播放器。
func BuildTrimArgs(strategy Strategy, inputPath, outputPath string, start, end float64) []string {
	// 输出先写 .part，由调用方在同卷 rename 为最终文件（规避跨盘失败）。
	partPath := outputPath
	if ext := filepath.Ext(outputPath); ext != "" {
		// 保持扩展名，仅改主名尾部，避免歧义
		partPath = outputPath[:len(outputPath)-len(ext)] + ".part" + ext
	}

	args := []string{}

	switch strategy {
	case StrategyReencode:
		dur := end - start
		args = append(args,
			"-ss", fmt.Sprintf("%.3f", start),
			"-i", inputPath,
			"-t", fmt.Sprintf("%.3f", dur),
			"-c:v", "libx264",
			"-preset", "fast",
			"-c:a", "aac",
		)
	case StrategyAudioOnly:
		args = append(args,
			"-ss", fmt.Sprintf("%.3f", start),
			"-to", fmt.Sprintf("%.3f", end),
			"-i", inputPath,
			"-vn", // 丢弃视频
			"-c:a", "copy",
		)
	case StrategyStreamCopy:
		fallthrough
	default:
		args = append(args,
			"-ss", fmt.Sprintf("%.3f", start),
			"-to", fmt.Sprintf("%.3f", end),
			"-i", inputPath,
			"-c", "copy",
		)
	}

	args = append(args,
		"-avoid_negative_ts", "make_zero",
		"-y",
		partPath,
	)
	return args
}

// PartPath 由最终输出路径推导 .part 临时路径（与 BuildTrimArgs 保持一致）。
func PartPath(outputPath string) string {
	if ext := filepath.Ext(outputPath); ext != "" {
		return outputPath[:len(outputPath)-len(ext)] + ".part" + ext
	}
	return outputPath + ".part"
}

// BuildConcatListArgs 构造 concat demuxer 合并参数（v1.4 §18.4 A 路径：流复制，快/无损）。
// 输出先写 .part，由调用方在同卷 rename 为最终文件。
func BuildConcatListArgs(listFile, output string) []string {
	partPath := PartPath(output)
	return []string{
		"-f", "concat",
		"-safe", "0",
		"-i", listFile,
		"-c", "copy",
		"-avoid_negative_ts", "make_zero",
		"-y",
		partPath,
	}
}

// BuildConcatFilterArgs 构造 concat filter 合并参数（v1.4 §18.4 B 路径：重编码，兼容异构输入）。
// silent 为无音频输入补充的 anullsrc 静音轨（Duration/SampleRate，来自 BuildConcatFilter）；
// filter 为 BuildConcatFilter 产出的 filter_complex 表达式。
func BuildConcatFilterArgs(inputs []string, silent []SilentInput, filter, output string) []string {
	partPath := PartPath(output)
	args := []string{}
	for _, in := range inputs {
		args = append(args, "-i", in)
	}
	for _, si := range silent {
		args = append(args,
			"-f", "lavfi",
			"-t", fmt.Sprintf("%.3f", si.Duration),
			"-i", fmt.Sprintf("anullsrc=channel_layout=stereo:sample_rate=%s", si.SampleRate),
		)
	}
	args = append(args, "-filter_complex", filter)
	// 按 filter 输出的 [outv]/[outa] 标签决定 map 与编码器（纯视频/纯音频合并均兼容）
	if strings.Contains(filter, "[outv]") {
		args = append(args, "-map", "[outv]", "-c:v", "libx264", "-preset", "fast")
	}
	if strings.Contains(filter, "[outa]") {
		args = append(args, "-map", "[outa]", "-c:a", "aac")
	}
	args = append(args,
		"-avoid_negative_ts", "make_zero",
		"-y",
		partPath,
	)
	return args
}
