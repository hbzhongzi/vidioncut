package subtitles

// 外显字幕渲染（字幕功能更新文档 §4.3）：SRT 通用无样式；ASS 带样式预设，
// 二次加工特效通过 Dialogue 行 override tags 承载（animate.go 生成，不改结构）。

import (
	"fmt"
	"strings"
)

// StylePreset ASS 样式预设（§4.3 预设表）。
type StylePreset struct {
	ID       string  `json:"id"`
	FontName string  `json:"font_name"`
	FontSize int     `json:"font_size"`
	Outline  float64 `json:"outline"`  // 描边宽度（px；0=无描边）
	Shadow   float64 `json:"shadow"`   // 阴影偏移（px）
	Bold     int     `json:"bold"`     // -1 开启 / 0 关闭
	MarginV  int     `json:"margin_v"` // 底边距（PlayResY 坐标）
}

// Presets 三套样式预设（字体统一微软雅黑：Win10/11 默认在位，libass DirectWrite 直通）。
var Presets = map[string]StylePreset{
	"default": {
		ID: "default", FontName: "Microsoft YaHei", FontSize: 48,
		Outline: 2, Shadow: 0, Bold: 0, MarginV: 30,
	},
	"talking-head": {
		ID: "talking-head", FontName: "Microsoft YaHei", FontSize: 56,
		Outline: 3, Shadow: 1, Bold: 0, MarginV: 40,
	},
	"minimal": {
		ID: "minimal", FontName: "Microsoft YaHei", FontSize: 40,
		Outline: 0, Shadow: 0, Bold: 0, MarginV: 24,
	},
}

// GetPreset 取样式预设；未知 ID 回落 default（防御：配置漂移不阻断导出）。
func GetPreset(id string) StylePreset {
	if p, ok := Presets[id]; ok {
		return p
	}
	return Presets["default"]
}

// FormatSRTTime 秒 → "HH:MM:SS,mmm"。
func FormatSRTTime(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	ms := int64(sec*1000 + 0.5)
	h := ms / 3600000
	ms -= h * 3600000
	m := ms / 60000
	ms -= m * 60000
	s := ms / 1000
	ms -= s * 1000
	return fmt.Sprintf("%02d:%02d:%02d,%03d", h, m, s, ms)
}

// formatASSTime 秒 → "H:MM:SS.cc"（ASS 厘秒精度）。
func formatASSTime(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	cs := int64(sec*100 + 0.5)
	h := cs / 360000
	cs -= h * 360000
	m := cs / 6000
	cs -= m * 6000
	s := cs / 100
	cs -= s * 100
	return fmt.Sprintf("%d:%02d:%02d.%02d", h, m, s, cs)
}

// RenderSRT 渲染 SRT（无样式通用格式，剪映/PR/DaVinci 全兼容）。
// 文本中的 "\n"（预换行产物）渲染为真实换行。
func RenderSRT(cues []Cue) string {
	var sb strings.Builder
	for i, c := range cues {
		fmt.Fprintf(&sb, "%d\n", i+1)
		fmt.Fprintf(&sb, "%s --> %s\n", FormatSRTTime(c.Start), FormatSRTTime(c.End))
		sb.WriteString(strings.ReplaceAll(c.Text, "\n", "\n"))
		sb.WriteString("\n\n")
	}
	return sb.String()
}

// assEscape 转义 ASS Dialogue 文本中的特殊字符（花括号为 override tag 定界符，
// 反斜杠为换行/标签前缀）。特效 tags 由 animate.go 显式构造，不在此转义。
func assEscape(s string) string {
	s = strings.ReplaceAll(s, "\n", `\N`)
	return s
}

// RenderASS 渲染带样式预设的 ASS（§4.3）。
// width/height 取视频实际宽高（probe；0 时回落 1920×1080）。
// cues 文本中的 "\n" 转为 ASS \N 换行。
func RenderASS(cues []Cue, preset StylePreset, width, height int) string {
	if width <= 0 || height <= 0 {
		width, height = 1920, 1080
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "[Script Info]\n; 由 VideoCut M12 生成（成片时间轴）\nScriptType: v4.00+\n")
	fmt.Fprintf(&sb, "PlayResX: %d\nPlayResY: %d\nWrapStyle: 0\nScaledBorderAndShadow: yes\n\n", width, height)
	// 颜色：&HAABBGGRR——白字不透明、黑描边、半透明黑底
	fmt.Fprintf(&sb, "[V4+ Styles]\nFormat: Name, Fontname, Fontsize, PrimaryColour, OutlineColour, BackColour, Bold, Italic, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n")
	fmt.Fprintf(&sb, "Style: Default,%s,%d,&H00FFFFFF,&H00000000,&H80000000,%d,0,1,%s,%s,2,10,10,%d,1\n\n",
		preset.FontName, preset.FontSize, preset.Bold,
		trimFloat(preset.Outline), trimFloat(preset.Shadow), preset.MarginV)
	sb.WriteString("[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n")
	for _, c := range cues {
		fmt.Fprintf(&sb, "Dialogue: 0,%s,%s,Default,,0,0,0,,%s\n",
			formatASSTime(c.Start), formatASSTime(c.End), assEscape(c.Text))
	}
	return sb.String()
}

// trimFloat 去掉浮点尾部零（3 → "3"，1.5 → "1.5"）。
func trimFloat(v float64) string {
	s := fmt.Sprintf("%.2f", v)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" {
		return "0"
	}
	return s
}
