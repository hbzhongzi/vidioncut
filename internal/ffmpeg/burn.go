package ffmpeg

// 内嵌字幕烧录（M12 §4.4）：ass 滤镜 + 重编码 + 音轨直复制。
// 关键实现点均为 2026-10-14 实测结论（可行性分析 §19.14.3）：
//  1. 路径转义：ass 滤镜参数中盘符冒号必须转义（C:/x.ass 的 ":" 会被滤镜图
//     解析为选项分隔符 → 直接报错）。Go 侧 exec 不过 shell，参数精确可控：
//     反斜杠→正斜杠 + ":"→"\:"；
//  2. 兜底：调用方可用 WithWorkDir 设 cmd.Dir 为 ASS 所在目录 + 相对路径（零转义）；
//  3. 编码器：默认 libx264；h264_nvenc/qsv/amf 可选（启动失败由调用方回退 libx264）。

import (
	"strings"
)

// EscapeAssPath 转义 ass 滤镜参数路径：反斜杠→正斜杠（Windows），
// 盘符冒号→"\\:"（双反斜杠）。
//
// 机理（2026-10-14 实测）：ffmpeg 滤镜参数解析有两层——filtergraph 层先把
// "\\" 还原为 "\"，选项分割层再把 "\:" 当字面冒号。因此不过 shell 的
// exec 场景必须传双反斜杠：单层 "\:" 会被选项层误判为"选项分隔符"，
// 导致冒号后的路径被当成 ass 滤镜的 original_size 选项值并报
// "Unable to parse original_size ... as image size"（CLI 文档写单层
// 是因为 shell 先吃掉了一层反斜杠，语义等价）。
// 例：C:\v\final\edited.ass → C\\:/v/final/edited.ass
func EscapeAssPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	// 仅转义盘符冒号（第 2 个字符处的 ":"），避免误伤路径中其他冒号
	if len(p) >= 2 && p[1] == ':' {
		p = p[:1] + `\\:` + p[2:]
	}
	return p
}

// BuildBurnArgs 构造字幕烧录参数切片（不含 ffmpeg 本体）。
// encoder 为空回落 libx264；crf<=0 回落 18；preset 为空回落 fast。
// 输出先写 .part（同 BuildTrimArgs 约定，由调用方 rename 落地）。
//
// 命令形态（附录 §10）：
//
//	ffmpeg -i edited.mp4 -vf "ass=edited.ass" -c:v libx264 -preset fast -crf 18 -c:a copy out.mp4
func BuildBurnArgs(inputPath, assPath, outputPath, encoder string, crf int, preset string) []string {
	if encoder == "" {
		encoder = "libx264"
	}
	if crf <= 0 {
		crf = 18
	}
	if preset == "" {
		preset = "fast"
	}
	partPath := PartPath(outputPath)
	return []string{
		"-i", inputPath,
		"-vf", "ass=" + EscapeAssPath(assPath),
		"-c:v", encoder,
		"-preset", preset,
		"-crf", itoa(crf),
		"-c:a", "copy", // 音轨直复制，不重编码（Q-02 已定）
		"-avoid_negative_ts", "make_zero",
		"-y",
		partPath,
	}
}

// itoa 小整数转字符串（避免引入 strconv 仅为一个场景）。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
