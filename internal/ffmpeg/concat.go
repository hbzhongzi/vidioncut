package ffmpeg

import (
	"fmt"
	"os"
	"strings"
)

// 合并模式（开发文档 v1.4 §18.4；与 service.KindMerge 侧的常量保持一致的值）。
const (
	MergeModeDemuxer = "demuxer" // 流复制：concat demuxer，快/无损，要求编码参数一致
	MergeModeFilter  = "filter"  // 重编码：concat filter，兼容异构输入
)

// SilentInput 描述为"无音频输入"补充的 anullsrc 静音轨（v1.4 §18.4 B 路径）。
type SilentInput struct {
	Duration   float64 // 对应片段时长（秒），用于 -t 限制无限静音流
	SampleRate string  // 采样率，取自有音频输入
}

// WriteConcatList 写入 concat demuxer 列表文件（v1.4 §18.4）。
// 格式：UTF-8 无 BOM，每行 file '<path>'；单引号转义为 '\''；
// 反斜杠统一转正斜杠（demuxer 将 \ 视为转义符）。
// 设计上游已保证路径经临时目录 ASCII 规范化，转义逻辑作为纵深防御保留。
func WriteConcatList(inputs []string, listPath string) error {
	var sb strings.Builder
	for _, in := range inputs {
		sb.WriteString("file '")
		sb.WriteString(escapeConcatPath(in))
		sb.WriteString("'\n")
	}
	if err := os.WriteFile(listPath, []byte(sb.String()), 0o644); err != nil {
		return fmt.Errorf("write concat list: %w", err)
	}
	return nil
}

// escapeConcatPath 转义 concat 列表中的路径（v1.4 §18.4）。
func escapeConcatPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	return strings.ReplaceAll(p, "'", `'\''`)
}

// firstVideoStream / firstAudioStream 取首个视频/音频流（无则返回零值）。
func firstVideoStream(p *ProbeResult) *StreamInfo {
	for i := range p.Streams {
		if p.Streams[i].CodecType == "video" {
			return &p.Streams[i]
		}
	}
	return nil
}

func firstAudioStream(p *ProbeResult) *StreamInfo {
	for i := range p.Streams {
		if p.Streams[i].CodecType == "audio" {
			return &p.Streams[i]
		}
	}
	return nil
}

// DetectMergeMode auto 判定（v1.4 §18.4，纯函数）：
// 逐项比较音视频存在性、video（codec_name/width/height/pix_fmt）与
// audio（codec_name/sample_rate/channels），全部一致 → demuxer，否则 filter。
func DetectMergeMode(probes []*ProbeResult) string {
	if len(probes) <= 1 {
		return MergeModeDemuxer
	}
	first := probes[0]
	fv, fa := firstVideoStream(first), firstAudioStream(first)
	for _, p := range probes[1:] {
		if p.HasVideo != first.HasVideo || p.HasAudio != first.HasAudio {
			return MergeModeFilter
		}
		if first.HasVideo {
			v := firstVideoStream(p)
			if v == nil || v.CodecName != fv.CodecName ||
				p.Width != first.Width || p.Height != first.Height ||
				ptrStr(v.PixFmt) != ptrStr(fv.PixFmt) {
				return MergeModeFilter
			}
		}
		if first.HasAudio {
			a := firstAudioStream(p)
			if a == nil || a.CodecName != fa.CodecName ||
				ptrStr(a.SampleRate) != ptrStr(fa.SampleRate) ||
				ptrInt(a.Channels) != ptrInt(fa.Channels) {
				return MergeModeFilter
			}
		}
	}
	return MergeModeDemuxer
}

func ptrStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func ptrInt(p *int) int {
	if p == nil {
		return -1
	}
	return *p
}

// BuildConcatFilter 构造 concat filter 表达式（v1.4 §18.4，纯函数）：
//   - 全部无音频 → concat=n=N:v=1:a=0
//   - 全部有音频 → [i:v][i:a] 直接配对
//   - 混合（部分输入无音频）→ 无音频输入补 anullsrc 静音轨（经 SilentInput 返回，
//     由 BuildConcatFilterArgs 追加 lavfi 输入）
//   - 分辨率/像素格式归一：逐视频输入加 scale+pad+setsar+format 归一到首项
//   - 采样率/声道归一：aformat 归一到首个有音频输入的参数
//
// segDurations 为各路输入进入合并时的实际时长（hybrid 为裁剪后时长）。
func BuildConcatFilter(probes []*ProbeResult, segDurations []float64) (string, []SilentInput, error) {
	n := len(probes)
	if n == 0 {
		return "", nil, fmt.Errorf("concat filter: 输入不能为空")
	}

	anyVideo, allVideo, anyAudio := false, true, false
	for _, p := range probes {
		if p.HasVideo {
			anyVideo = true
		} else {
			allVideo = false
		}
		if p.HasAudio {
			anyAudio = true
		}
	}
	if anyVideo && !allVideo {
		return "", nil, fmt.Errorf("合并输入必须全部包含视频或全部为纯音频（当前混合）")
	}
	if !anyVideo && !anyAudio {
		return "", nil, fmt.Errorf("合并输入无可识别的音视频流")
	}

	// 参考参数：首项分辨率；首个有音频输入的采样率
	refW, refH := probes[0].Width, probes[0].Height
	refRate := "44100"
	for _, p := range probes {
		if a := firstAudioStream(p); a != nil && a.SampleRate != nil && *a.SampleRate != "" {
			refRate = *a.SampleRate
			break
		}
	}

	var chains []string   // 各输入归一链
	var labels []string   // concat 的输入标签（按 [v][a] 成对顺序）
	var silent []SilentInput
	nextInput := n // anullsrc 追加输入的索引从 n 起

	for i, p := range probes {
		if p.HasVideo {
			if refW > 0 && refH > 0 {
				chains = append(chains, fmt.Sprintf(
					"[%d:v]scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1,format=yuv420p[v%d]",
					i, refW, refH, refW, refH, i))
			} else {
				chains = append(chains, fmt.Sprintf("[%d:v]format=yuv420p[v%d]", i, i))
			}
			labels = append(labels, fmt.Sprintf("[v%d]", i))
		}
		if p.HasAudio {
			chains = append(chains, fmt.Sprintf(
				"[%d:a]aformat=sample_fmts=fltp:sample_rates=%s:channel_layouts=stereo[a%d]",
				i, refRate, i))
			labels = append(labels, fmt.Sprintf("[a%d]", i))
		} else if anyAudio {
			// 无音频输入：补 anullsrc 静音轨（-t 限制为片段时长，避免无限流挂起）
			dur := 0.0
			if i < len(segDurations) {
				dur = segDurations[i]
			}
			if dur <= 0 {
				return "", nil, fmt.Errorf("第 %d 个输入无音频且时长未知，无法补静音轨合并", i+1)
			}
			silent = append(silent, SilentInput{Duration: dur, SampleRate: refRate})
			labels = append(labels, fmt.Sprintf("[%d:a]", nextInput))
			nextInput++
		}
	}

	vFlag, aFlag := 0, 0
	if anyVideo {
		vFlag = 1
	}
	if anyAudio {
		aFlag = 1
	}
	concat := fmt.Sprintf("%sconcat=n=%d:v=%d:a=%d", strings.Join(labels, ""), n, vFlag, aFlag)
	if vFlag == 1 {
		concat += "[outv]"
	}
	if aFlag == 1 {
		concat += "[outa]"
	}
	chains = append(chains, concat)
	return strings.Join(chains, ";"), silent, nil
}
