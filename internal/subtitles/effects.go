package subtitles

// 特效落地：把规则引擎选中的特效写成 ASS override tags（§5.3）。
// 全部标签由 libass 原生渲染——同一份 .ass，外显可直接导入剪映，
// 烧录命令与静态版完全一致（§5.7"烧录管线零改动"）。

import (
	"fmt"
	"strings"
	"unicode"
)

// EffectOptions 特效应用参数。
type EffectOptions struct {
	Plan         *MotionPlan
	Preset       StylePreset
	VideoWidth   int // probe 宽高（0 时回落 1920×1080）
	VideoHeight  int
	KeywordColor string // "#RRGGBB"
}

// ApplyEffects 按 Plan 选中的特效为每条 cue 生成 override tags。
// 分层：① 词级特效（marker-sweep/karaoke-fill/kinetic-typography）重写整条为词流
// （需 cue.Words 词级时间戳；此时预换行不适用，行边界交还 libass）；
// ② keyword-highlight 对关键词 span 上色；③ cue 级前缀（fad/move/t 系）可叠加。
func ApplyEffects(cues []Cue, opts EffectOptions) []Cue {
	if opts.Plan == nil {
		return cues
	}
	selected := opts.Plan.Meta.AutoSkills.Selected
	if len(selected) == 0 {
		return cues // 纯静态字幕（合法态）
	}
	if opts.KeywordColor == "" {
		opts.KeywordColor = "#FFD700"
	}
	var kws []string
	if opts.Plan.Meta.AutoSkills.Analysis != nil {
		kws = opts.Plan.Meta.AutoSkills.Analysis.TopKeywords
	}

	out := make([]Cue, len(cues))
	for i, c := range cues {
		text := c.Text
		switch {
		case contains(selected, "marker-sweep") && len(c.Words) > 0 && wordCoverageOK(c):
			// 扫描高亮：仅关键词词逐字填充金色（§5.3 定稿 \kf 方案，放弃 \p1）
			text = applyWordFill(c, kws, opts.KeywordColor)
		case contains(selected, "karaoke-fill") && len(c.Words) > 0 && wordCoverageOK(c):
			// 跟读填充：整条逐词（同色系变亮，SecondaryColour 表达）
			text = applyWordFill(c, nil, "")
		case contains(selected, "kinetic-typography") && len(c.Words) > 0 && wordCoverageOK(c):
			// 动态排版：逐词填充 + 淡入快闪前缀
			text = applyWordFill(c, nil, "")
		default:
			// 无词级数据 / 覆盖度不足：keyword-highlight / marker-sweep 退化为 span 上色
			if (contains(selected, "keyword-highlight") || contains(selected, "marker-sweep")) && len(kws) > 0 {
				text = highlightKeywords(text, kws, opts.KeywordColor)
			}
		}
		if prefix := buildPrefix(selected, c, opts); prefix != "" {
			text = "{" + prefix + "}" + text
		}
		out[i] = c
		out[i].Text = text
	}
	return out
}

// wordCoverageOK 词级重建文本是否覆盖原文（防 words 不全时丢内容）。
// Whisper 真实输出（word_timestamps=True）通常覆盖全段；但兜底防御：
// 词级特效会用 words 重建文本，若 words 覆盖不足（手工 signals / DTW
// 缺词 / 纯标点 token），重建会丢文本——这比特效不生效严重得多。
// 覆盖率 < 80% 时该 cue 放弃词级特效，回落 span 上色（文本零丢失）。
func wordCoverageOK(c Cue) bool {
	a := normalizeForCoverage(joinWords(c.Words))
	b := normalizeForCoverage(c.Text)
	if b == "" {
		return true
	}
	if a == "" {
		return false
	}
	pool := map[rune]int{}
	for _, r := range a {
		pool[r]++
	}
	hit := 0
	for _, r := range b {
		if pool[r] > 0 {
			hit++
			pool[r]--
		}
	}
	return float64(hit)/float64(len([]rune(b))) >= 0.8
}

// normalizeForCoverage 去标点空白（覆盖度比较忽略排版差异）。
func normalizeForCoverage(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if unicode.IsPunct(r) || unicode.IsSpace(r) {
			continue
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// applyWordFill 词级 \kf 填充。golden 非空时仅关键词词上金色（marker-sweep）；
// 为空时整条同色系填充（karaoke-fill / kinetic-typography）。
// 词间空格按 joinTokens 同款规则（中英边界补空格，防"使用videocut"粘连）。
func applyWordFill(c Cue, kws []string, golden string) string {
	var sb strings.Builder
	var prev string
	for _, w := range c.Words {
		t := strings.TrimSpace(w.Word)
		if t == "" {
			continue
		}
		if sb.Len() > 0 && needsSpace(prev, t) {
			sb.WriteByte(' ')
		}
		cs := int((w.End-w.Start)*100 + 0.5) // 四舍五入到厘秒（防 0.3999→39 抖动）
		if cs < 1 {
			cs = 1 // 零/负时长 token 保底 1 厘秒（DTW 抖动）
		}
		if golden != "" {
			if matchesKeyword(t, kws) {
				sb.WriteString(fmt.Sprintf("{\\kf%d}{\\c%s}%s{\\r}", cs, assColor(golden), t))
				prev = t
				continue
			}
			sb.WriteString(t)
			prev = t
			continue
		}
		sb.WriteString(fmt.Sprintf("{\\kf%d}%s", cs, t))
		prev = t
	}
	return sb.String()
}

// highlightKeywords 关键词 span 上色（非词级模式的 keyword-highlight）。
func highlightKeywords(text string, kws []string, color string) string {
	cc := assColor(color)
	for _, kw := range kws {
		if kw == "" || !strings.Contains(text, kw) {
			continue
		}
		text = strings.ReplaceAll(text, kw, fmt.Sprintf("{\\c%s}%s{\\r}", cc, kw))
	}
	return text
}

// buildPrefix cue 级前缀标签（可叠加，按优先级拼装）。
func buildPrefix(selected []string, c Cue, opts EffectOptions) string {
	var tags []string
	if contains(selected, "slide-in") {
		cx, by := opts.VideoWidth/2, opts.VideoHeight-opts.Preset.MarginV
		if cx <= 0 {
			cx = 960
		}
		if by <= 0 {
			by = 1050
		}
		tags = append(tags, fmt.Sprintf("\\move(%d,%d,%d,%d,0,300)", cx, by+30, cx, by), "\\fad(250,0)")
	}
	if contains(selected, "fade-subtle") {
		tags = append(tags, "\\fad(200,200)")
	}
	if contains(selected, "bounce-in") {
		tags = append(tags, "\\t(0,150,\\fscx110\\fscy110)\\t(150,300,\\fscx100\\fscy100)")
	}
	if contains(selected, "pulse-emphasis") && hasEmphaticPunct(c.Text) {
		tags = append(tags, "\\t(0,180,\\fscx112\\fscy112)")
	}
	if contains(selected, "kinetic-typography") {
		tags = append(tags, "\\fad(120,120)")
	}
	return strings.Join(tags, "")
}

// matchesKeyword 词是否命中关键词（包含匹配，容忍"然后呢"式后缀与标点粘连）。
func matchesKeyword(word string, kws []string) bool {
	for _, kw := range kws {
		if kw != "" && strings.Contains(word, kw) {
			return true
		}
	}
	return false
}

// assColor "#RRGGBB" → ASS "&H00BBGGRR"（&H + AA=00 不透明 + BBGGRR 8 位十六进制）；
// 非法输入回落金色 #FFD700（&H0000D7FF）。
func assColor(hex string) string {
	h := strings.TrimPrefix(hex, "#")
	if len(h) != 6 {
		return "&H0000D7FF"
	}
	r, g, b := h[0:2], h[2:4], h[4:6]
	if !isHex(r) || !isHex(g) || !isHex(b) {
		return "&H0000D7FF"
	}
	return "&H00" + strings.ToUpper(b) + strings.ToUpper(g) + strings.ToUpper(r)
}

func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
