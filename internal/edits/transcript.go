package edits

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// SrtCue 一条字幕（SRT）。
type SrtCue struct {
	Index int
	Start string // 原始时间戳 H:M:S,mmm
	End   string
	Text  string // 多行已合并为单空格分隔
}

// parseSrtTime 解析 "H:M:S,mmm" / "M:S,mmm" 为秒。
func parseSrtTime(s string) (float64, error) {
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, fmt.Errorf("非法时间戳 %q", s)
	}
	secPart := parts[len(parts)-1]
	hms := parts[:len(parts)-1]
	secF, err := strconv.ParseFloat(strings.Replace(secPart, ",", ".", 1), 64)
	if err != nil {
		return 0, fmt.Errorf("非法秒 %q", secPart)
	}
	var total float64
	for _, p := range hms {
		n, err := strconv.Atoi(p)
		if err != nil {
			return 0, fmt.Errorf("非法时分 %q", p)
		}
		total = total*60 + float64(n)
	}
	return total + secF, nil
}

// ParseSRT 解析 SRT 文本（§19.3 export_transcript 的输入格式）。
// 容忍 CRLF / BOM / 空块 / 缺序号块；多行文本合并为空格分隔单行。
func ParseSRT(data []byte) ([]SrtCue, error) {
	text := strings.TrimPrefix(string(data), "\ufeff")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	blocks := strings.Split(strings.TrimSpace(text), "\n\n")

	cues := make([]SrtCue, 0, len(blocks))
	idx := 0
	for _, block := range blocks {
		lines := strings.Split(strings.TrimSpace(block), "\n")
		if len(lines) == 0 {
			continue
		}
		// 可选序号行
		if _, err := strconv.Atoi(strings.TrimSpace(lines[0])); err == nil {
			lines = lines[1:]
		}
		if len(lines) == 0 {
			continue
		}
		// 时间轴行
		tl := lines[0]
		arrow := strings.Index(tl, "-->")
		if arrow < 0 {
			return nil, fmt.Errorf("块 %d：缺少时间轴行（-->）", idx+1)
		}
		start := strings.TrimSpace(tl[:arrow])
		end := strings.TrimSpace(tl[arrow+3:])
		bodyLines := lines[1:]
		if len(bodyLines) == 0 {
			continue
		}
		idx++
		cues = append(cues, SrtCue{
			Index: idx,
			Start: start,
			End:   end,
			Text:  strings.Join(bodyLines, " "),
		})
	}
	return cues, nil
}

// ExportTranscript 导出文字稿：md=带时间戳的 Markdown；txt=纯文本（§19.3）。
func ExportTranscript(srtPath, format string) (string, error) {
	data, err := os.ReadFile(srtPath)
	if err != nil {
		return "", fmt.Errorf("读取 SRT 失败: %w", err)
	}
	cues, err := ParseSRT(data)
	if err != nil {
		return "", err
	}
	switch format {
	case "", "md":
		return renderMarkdown(cues), nil
	case "txt":
		return renderText(cues), nil
	default:
		return "", fmt.Errorf("未知格式 %q（合法值：md/txt）", format)
	}
}

func renderMarkdown(cues []SrtCue) string {
	var b strings.Builder
	b.WriteString("# 文字稿\n\n")
	b.WriteString(fmt.Sprintf("来源：%s\n\n", "videocut export_transcript"))
	for _, c := range cues {
		start, _ := parseSrtTime(c.Start)
		end, _ := parseSrtTime(c.End)
		b.WriteString(fmt.Sprintf("**[%s → %s]**（%.3fs–%.3fs）%s\n\n", c.Start, c.End, start, end, c.Text))
	}
	return b.String()
}

func renderText(cues []SrtCue) string {
	var b strings.Builder
	for _, c := range cues {
		b.WriteString(fmt.Sprintf("[%s → %s] %s\n", c.Start, c.End, c.Text))
	}
	return b.String()
}
