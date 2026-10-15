// Package subtitles 实现粗剪成片后的字幕层（M12，字幕功能更新文档 v1.2 §4）：
// 源时间轴 → 成片时间轴的重映射（含跨保留段边界词级断开）、
// 程序侧预换行分段、SRT/ASS 外显渲染、二次加工特效规则引擎。
// 本包不依赖 FFmpeg/网络/LLM，全部为可单测的确定性纯函数。
package subtitles

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"

	"videocut/internal/edits"
)

// timeEps 时间浮点容差（毫秒级）。词级 DTW 时间戳存在对齐抖动，
// 比较时统一留 eps，避免"恰好贴界"被误判为无交集。
const timeEps = 1e-3

// Word 词级 token（来自 work/signals.json 的 words[]）。
type Word struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Word  string  `json:"word"`
}

// Segment 自然语句段（signals.json 的 segments[]，重映射的分段粒度）。
type Segment struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

// Cue 一条字幕（重映射 + 预换行后的产物单元，成片时间轴）。
// Words 仅在跨段词级断开时携带（动画特效按词填充需要词级时间戳，已映射到成片轴）；
// SplitAt 标记该条由保留段边界切开（UI 以 ⚡ 标注，提示人工复核断点语义）。
type Cue struct {
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Text    string  `json:"text"`
	Words   []Word  `json:"words,omitempty"`
	SplitAt bool    `json:"split_at,omitempty"`
}

// FinalDuration 保留段总时长（= 成片时长）。
func FinalDuration(keeps []edits.KeepRange) float64 {
	total := 0.0
	for _, k := range keeps {
		if k.End > k.Start {
			total += k.End - k.Start
		}
	}
	return total
}

// timelineMapper 源轴 → 成片轴 的确定性映射（字幕功能更新文档 §4.1）：
// 原时间 t → 所属保留段 [ks,ke) → t' = (t − ks) + Σ(之前各保留段长度)。
type timelineMapper struct {
	keeps   []edits.KeepRange
	offsets []float64 // offsets[i] = Σ_{j<i} keeps[j] 长度
	total   float64
}

func newTimelineMapper(keeps []edits.KeepRange) *timelineMapper {
	ks := make([]edits.KeepRange, len(keeps))
	copy(ks, keeps)
	sort.SliceStable(ks, func(i, j int) bool { return ks[i].Start < ks[j].Start })
	m := &timelineMapper{keeps: ks, offsets: make([]float64, len(ks))}
	for i, k := range ks {
		if k.End < k.Start {
			k.End = k.Start
		}
		if i > 0 {
			m.offsets[i] = m.offsets[i-1] + (ks[i-1].End - ks[i-1].Start)
		}
		m.total += k.End - k.Start
	}
	return m
}

// mapTime 映射源轴时间到成片轴。t 落在保留段 [ks,ke]（含右端，右端即该段
// 在成片轴上的末端）返回 (t', true)；落在删除段内（或负值）返回 (0, false)
// ——调用方只对交集生成 cue，不会命中。两个保留段的拼接点
// （前段 ke == 后段 ks' 被删间隙分隔）映射到同一成片时刻，符合剪切拼接语义。
func (m *timelineMapper) mapTime(t float64) (float64, bool) {
	if t < 0 {
		t = 0 // 时钟抖动可能产出负值起点
	}
	for i, k := range m.keeps {
		if t >= k.Start-timeEps && t <= k.End+timeEps {
			return m.offsets[i] + (t - k.Start), true
		}
	}
	return 0, false
}

// RemapCues 把源时间轴字幕重映射到成片时间轴（§4.1，字幕功能前置基石）。
//
// 输入：signals.json 的 segments/words、EditsToKeepRanges 产物、源时长。
// 规则：
//   - 字幕单元完全落在保留段内 → 整体平移；
//   - 完全落在删除段内 → 丢弃（内容已被剪掉）；
//   - 跨越保留段边界 → 词级断开：收集边界内词级 token（跨界词收入并钳位，
//     语义完整优先；每个词只归一个 cue，不会在相邻两条重复出现）；
//     无词级数据时截断到边界；
//   - 相邻 cue 间隙 < 0.2s 保留颗粒度（不合并，分段节奏是内容的一部分）；
//   - 全删 → 空集；零删 → 全集平移 0（均不 panic）；
//   - 时间戳越界 → 钳位到成片时长（自检兜底，属内部缺陷）。
//
// 产物保证通过 ValidateCues（单调不减 / end>start / 不越界 / 文本非空 / 不重叠）。
func RemapCues(segs []Segment, words []Word, keeps []edits.KeepRange, sourceDuration float64) ([]Cue, error) {
	_ = sourceDuration // 保留段已隐含源轴范围；参数留作契约清晰与未来校验
	if len(keeps) == 0 {
		return []Cue{}, nil // 全删：合法空集
	}
	mapper := newTimelineMapper(keeps)

	// 词按 source 序预排序（signals.json 通常已有序，防御性排序保证算法正确）
	sortedWords := make([]Word, len(words))
	copy(sortedWords, words)
	sort.SliceStable(sortedWords, func(i, j int) bool { return sortedWords[i].Start < sortedWords[j].Start })

	cues := make([]Cue, 0, len(segs))
	for _, seg := range segs {
		if seg.End <= seg.Start+timeEps {
			continue // 零/负时长段（标点对齐伪影）
		}
		text := strings.TrimSpace(seg.Text)
		for _, k := range mapper.keeps {
			iStart, iEnd := math.Max(seg.Start, k.Start), math.Min(seg.End, k.End)
			if iEnd <= iStart+timeEps {
				continue // 与本报留段无交集
			}
			ns, okS := mapper.mapTime(iStart)
			ne, okE := mapper.mapTime(iEnd)
			if !okS || !okE {
				continue // 防御：交集端点必在保留段内
			}
			if iStart <= seg.Start+timeEps && iEnd >= seg.End-timeEps {
				// 整段保留：文本用 segment 原文（完整语句，含标点）。
				// 同时携带词级数据——词级特效（marker-sweep/karaoke-fill/
				// kinetic-typography 的 \kf）对全部 cue 可用，而整段保留是
				// 最常见情形；早期实现只在跨段断开时带 Words，导致这些特效
				// 对大多数 cue 静默落空（真实 MCP 全链核对 fx.ass 发现）。
				cues = append(cues, Cue{
					Start: ns, End: ne, Text: text,
					Words: mapWords(pickWordsInRange(sortedWords, seg.Start, seg.End), mapper),
				})
				continue
			}
			// 跨边界：词级断开。收集与交集相交的词（start 落在 [iStart, iEnd)），
			// 已归入更早保留段的词跳过（pickWordsInRange 按 start 归属实现）。
			picked := pickWordsInRange(sortedWords, iStart, iEnd)
			if len(picked) == 0 {
				// 无词级数据：截断到边界（SplitAt 标注供 UI 提示）
				if text == "" {
					continue
				}
				cues = append(cues, Cue{Start: ns, End: ne, Text: text, SplitAt: true})
				continue
			}
			ps, _ := mapper.mapTime(picked[0].Start)
			pe, peOK := mapper.mapTime(picked[len(picked)-1].End)
			if !peOK {
				pe = ne // 防御
			}
			cueText := joinWords(picked)
			if cueText == "" {
				continue
			}
			cues = append(cues, Cue{
				Start: ps, End: pe, Text: cueText,
				Words: mapWords(picked, mapper), SplitAt: true,
			})
		}
	}

	// 排序（多保留段场景下 segment 序未必等于成片轴序）+ 去重（边界抖动可能
	// 产生两条同起点的 cue）+ 钳位
	sort.SliceStable(cues, func(i, j int) bool {
		if cues[i].Start != cues[j].Start {
			return cues[i].Start < cues[j].Start
		}
		return cues[i].End < cues[j].End
	})
	out := make([]Cue, 0, len(cues))
	for _, c := range cues {
		if c.Start < 0 {
			c.Start = 0
		}
		if mapper.total > 0 && c.End > mapper.total {
			c.End = mapper.total // 越界钳位（自检兜底）
		}
		if c.End <= c.Start+timeEps || c.Text == "" {
			continue
		}
		// 去重：与上一条起点相同（边界抖动产生的孪生 cue）时保留文本更长者
		if n := len(out); n > 0 && math.Abs(out[n-1].Start-c.Start) < timeEps {
			if len(c.Text) > len(out[n-1].Text) {
				out[n-1] = c
			}
			continue
		}
		out = append(out, c)
	}
	if err := ValidateCues(out, mapper.total); err != nil {
		return nil, fmt.Errorf("字幕重映射产物自检失败: %w", err)
	}
	return out, nil
}

// mapWords 把词级 token 映射到成片轴（起点/终点分别映射；无词级数据返回 nil，
// 使 Cue.Words 走 omitempty，不向 JSON 消费者发空数组）。
func mapWords(picked []Word, mapper *timelineMapper) []Word {
	if len(picked) == 0 {
		return nil
	}
	mapped := make([]Word, len(picked))
	for j, w := range picked {
		ws, _ := mapper.mapTime(w.Start)
		we, weOK := mapper.mapTime(w.End)
		if !weOK {
			we = ws // 防御：词尾越界时至少保起点
		}
		if we <= ws {
			we = ws + 0.01 // 零/负时长保底
		}
		mapped[j] = Word{Start: ws, End: we, Word: w.Word}
	}
	return mapped
}

// pickWordsInRange 收集 start 落在 [lo, hi) 内的词（signals.json words 已按 start 排序）。
// 边界处的跨界词（start<hi 且 end>lo）收入——钳位在调用方映射时自然处理。
func pickWordsInRange(words []Word, lo, hi float64) []Word {
	out := make([]Word, 0, 8)
	for _, w := range words {
		if w.Start >= hi-timeEps {
			break // 已排序：后续词 start 更大
		}
		if w.Start < lo-timeEps {
			continue // 属于更早保留段（或更早 segment 的残余）
		}
		if w.End <= w.Start {
			continue // 零/负时长 token
		}
		if w.Start >= hi+timeEps {
			continue
		}
		out = append(out, w)
	}
	return out
}

// joinWords 拼接词级 token 为可读文本：CJK 直接相接，中英之间补空格，
// 标点不前置空格（Whisper 词 token 常粘连标点，如"那个，"）。
func joinWords(ws []Word) string {
	var sb strings.Builder
	var prev string
	for _, w := range ws {
		t := strings.TrimSpace(w.Word)
		if t == "" {
			continue
		}
		if sb.Len() > 0 && needsSpace(prev, t) {
			sb.WriteByte(' ')
		}
		sb.WriteString(t)
		prev = t
	}
	return strings.TrimSpace(sb.String())
}

// needsSpace 判断两个相邻 token 之间是否需要空格（仅中英混排时需要；
// 标点后不补——标点自身已提供视觉分隔）。
func needsSpace(prev, next string) bool {
	pr := []rune(prev)
	nr := []rune(next)
	if len(pr) == 0 || len(nr) == 0 {
		return false
	}
	pl, nl := pr[len(pr)-1], nr[0]
	if isPunctRune(pl) {
		return false
	}
	return (unicode.IsLetter(pl) && pl < 128) || (unicode.IsLetter(nl) && nl < 128)
}

// SegmentCues 程序侧预换行（§4.2）：libass 对 CJK 自动换行支持有限，
// 必须在写入 ASS/SRT 前按 maxCharsPerCue/maxLines 断行，确保任何播放器一致。
// 词边界优先（中文按字、英文按词、标点不置行首）；超出 maxLines 的余量
// 截断并追加"…"（明确有损，由调用方统计提示，见 TruncatedCount）。
func SegmentCues(cues []Cue, maxCharsPerCue, maxLines int) []Cue {
	if maxCharsPerCue <= 0 {
		maxCharsPerCue = 20
	}
	if maxLines <= 0 {
		maxLines = 2
	}
	out := make([]Cue, len(cues))
	for i, c := range cues {
		out[i] = c
		out[i].Text = wrapText(c.Text, maxCharsPerCue, maxLines)
	}
	return out
}

// wrapText 按行宽上限断行（\n 分隔由渲染层写入 ASS \N）。词边界优先，
// 行首禁标点、标点不计行宽（悬挂）；超出 maxLines 的最后一行截断加"…"。
func wrapText(text string, maxChars, maxLines int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	tokens := tokenizeForWrap(text)
	lines := make([]string, 0, maxLines)
	cur := make([]string, 0, 8)
	curW := 0
	ti := 0
	for ; ti < len(tokens); ti++ {
		tk := tokens[ti]
		w := tokenWidth(tk)
		if curW+w > maxChars && len(cur) > 0 {
			lines = append(lines, joinTokens(cur))
			cur = cur[:0]
			curW = 0
			if len(lines) == maxLines {
				break // 还有未装 token → 走溢出截断
			}
		}
		cur = append(cur, tk)
		curW += w
	}
	if len(lines) < maxLines && len(cur) > 0 {
		lines = append(lines, joinTokens(cur))
		cur = nil
	}
	// 溢出截断：仍有 token 未装完（或尾行被 maxLines 截留）→ 末行硬截 + "…"
	if ti < len(tokens) && len(lines) == maxLines {
		lr := []rune(lines[maxLines-1])
		if len(lr) > maxChars {
			lr = lr[:maxChars]
		}
		lines[maxLines-1] = string(lr) + "…"
	}
	return strings.Join(lines, "\n")
}

// joinTokens 拼接行内 token：英文词间补空格，CJK 直接相接（同 joinWords 规则）。
func joinTokens(tokens []string) string {
	var sb strings.Builder
	var prev string
	for _, t := range tokens {
		if sb.Len() > 0 && needsSpace(prev, t) {
			sb.WriteByte(' ')
		}
		sb.WriteString(t)
		prev = t
	}
	return sb.String()
}

// tokenWidth token 的行宽：尾部标点不计（悬挂），纯标点 token 保底 1。
func tokenWidth(tk string) int {
	r := []rune(tk)
	n := len(r)
	for n > 0 && isPunctRune(r[n-1]) {
		n--
	}
	if n == 0 {
		return 1
	}
	return n
}

// tokenizeForWrap 把文本切成换行 token：连续 ASCII 字母/数字为一个 token
// （英文词整体换行），单个 CJK 字一个 token，标点并入前一 token（不出行首）。
func tokenizeForWrap(text string) []string {
	var tokens []string
	var asciiBuf []rune
	flush := func() {
		if len(asciiBuf) > 0 {
			tokens = append(tokens, string(asciiBuf))
			asciiBuf = nil
		}
	}
	for _, r := range text {
		switch {
		case unicode.IsLetter(r) && r < 128, unicode.IsDigit(r) && r < 128:
			asciiBuf = append(asciiBuf, r)
		case unicode.IsSpace(r):
			flush()
		default:
			// CJK 单字或标点：先冲刷 ASCII 缓冲，再作为独立 token（标点形态）
			flush()
			tokens = append(tokens, string(r))
		}
	}
	flush()
	// 标点并入前一 token（标点不出现在行首）
	merged := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if len(merged) > 0 && isPunctRune([]rune(t)[0]) && len([]rune(t)) == 1 {
			merged[len(merged)-1] += t
			continue
		}
		merged = append(merged, t)
	}
	return merged
}

func isPunctRune(r rune) bool {
	return unicode.IsPunct(r) || unicode.IsSymbol(r)
}

// ValidateCues 产物不变式自检（§4.1）：成片时间轴上 Start 非降序、end>start、
// 不越界（end ≤ total+eps）、文本非空、相邻不重叠。
// 任一违反即显式报错——不产出坏文件（与候选扫描自检同哲学）。
func ValidateCues(cues []Cue, finalDuration float64) error {
	prevEnd := 0.0
	for i, c := range cues {
		pos := fmt.Sprintf("第 %d 条", i+1)
		if c.Text == "" {
			return fmt.Errorf("%s：文本为空", pos)
		}
		if c.End <= c.Start+timeEps {
			return fmt.Errorf("%s：end(%.3f) 必须大于 start(%.3f)", pos, c.End, c.Start)
		}
		if c.Start < prevEnd-timeEps {
			return fmt.Errorf("%s：start(%.3f) 与上一条 end(%.3f) 重叠", pos, c.Start, prevEnd)
		}
		if finalDuration > 0 && c.End > finalDuration+0.05 {
			return fmt.Errorf("%s：end(%.3f) 超过成片时长(%.3f)", pos, c.End, finalDuration)
		}
		prevEnd = c.End
	}
	return nil
}
