package subtitles

// 字幕二次加工：Codex 自动特效（字幕功能更新文档 §5，v1.2 四个设计点定稿版）。
//
// 设计哲学（与候选扫描 matchFiller/coalesceItems 一致）：确定性规则引擎，
// 表驱动、可回归测试、无 LLM 不确定性；Codex 保留显式覆盖通道（skills 参数）。
// 特效全部由 ASS override tags 承载——同一份 .ass，外显可导入剪映、烧录命令不变。

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// ===== 内容分析（§5.2：数据全部来自既有产物，零新增依赖） =====

// Analysis 五维内容特征（motion-plan.json meta.analysis 单一数据源，
// GUI 只读纯展示、不重复计算——§5.7）。
type Analysis struct {
	KeywordDensity float64  `json:"keyword_density"`   // 最高频关键词词频饱和度（min(1, top1/sat)，sat=5）
	ContentType    string   `json:"content_type"`      // tutorial/ad/narrative/vlog/unknown
	AvgCueDuration float64  `json:"avg_cue_duration"`  // 平均 cue 时长（秒）
	AvgCharsPerCue float64  `json:"avg_chars_per_cue"` // 平均每 cue 字数
	EmphaticRatio  float64  `json:"emphatic_ratio"`    // 感叹/疑问 cue 占比（0–1）
	TopKeywords    []string `json:"top_keywords"`      // 高频关键词（词频 ≥ keyword_min_freq）
}

// Analyze 计算五维特征。cues 为成片时间轴字幕；words 用于词频统计
// （缺省退化为按 cue 文本空格/单字粗分——口播场景 signals.json 必有词级数据）。
func Analyze(cues []Cue, words []Word, hotwords []string, minFreq int) *Analysis {
	a := &Analysis{ContentType: "unknown", TopKeywords: []string{}}
	if len(cues) == 0 {
		return a
	}
	// 节奏与信息密度
	totalDur, totalChars, emphatic := 0.0, 0, 0
	for _, c := range cues {
		totalDur += c.End - c.Start
		totalChars += len([]rune(c.Text))
		if hasEmphaticPunct(c.Text) {
			emphatic++
		}
	}
	a.AvgCueDuration = round2(totalDur / float64(len(cues)))
	a.AvgCharsPerCue = round2(float64(totalChars) / float64(len(cues)))
	a.EmphaticRatio = round2(float64(emphatic) / float64(len(cues)))

	// 关键词提取（§5.5）
	kws, top := extractKeywords(words, hotwords, minFreq)
	a.TopKeywords = kws
	if top > 0 {
		a.KeywordDensity = round2(min2(1.0, float64(top)/keywordFreqSat))
	}
	// 内容类型（词表命中，matchFiller 同款机制）
	a.ContentType = classifyContent(cues)
	return a
}

func min2(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// keywordFreqSat 词频饱和度基准：最高频词达到该次数即密度 1.0（§5.5 词频阈值 3 的
// 对应：密度阈值 0.6 = 最高频词出现 ≥3 次，两处阈值自洽）。
const keywordFreqSat = 5.0

// zhStopwords 中文高频虚词（§5.5，约 100 条覆盖约 90% 误判场景；口播实测后增补）。
var zhStopwords = strings.Fields(`的 了 是 在 和 就 都 而 及 与 着 或 一个 没有 我们 你们 他们 这个 那个 什么 怎么 因为 所以 但是 如果 虽然 而且 并且 然后 就是 这样 那样 一下 一个 一些 不是 不会 不能 可以 应该 需要 已经 还有 但是 或者 以及 对于 关于 通过 进行 相关 目前 现在 时候 问题 情况 方面 方式 方法 东西 事情 大家 自己 其他 每个 之一 之后 之前 以后 以前 起来 出来 进去 一下儿`)

// enStopwords 英文高频虚词（小写比较）。
var enStopwords = map[string]bool{}
var enStopwordsList = strings.Fields(`the a an is are was were be been being have has had do does did will would can could should may might shall must and or but if then so of in on at to for with by from as that this these those it its not no yes`)

func init() {
	for _, w := range enStopwordsList {
		enStopwords[w] = true
	}
}

// extractKeywords 关键词提取（§5.5）：停用词表 + 长度过滤 + 词频阈值；
// 热词表白名单兜底单字专名。返回关键词列表（按词频降序）与本次观察到的最高词频
// （供密度计算；不用包级状态，保证并发调用安全——MCP server 多工具并行）。
func extractKeywords(words []Word, hotwords []string, minFreq int) ([]string, int) {
	if minFreq <= 0 {
		minFreq = 3
	}
	freq := map[string]int{}
	for _, w := range words {
		term := strings.TrimSpace(w.Word)
		if term == "" {
			continue
		}
		if len([]rune(term)) < 2 {
			continue // 单字过滤：中文单字虚词多、专名少
		}
		if isStopword(term) {
			continue
		}
		if isPunctOnly(term) {
			continue // 标点 token（Whisper 词级输出常含纯标点）
		}
		freq[term]++
	}
	// 热词表白名单：词频再低也保留（单字专名兜底通道），不受阈值约束
	for _, hw := range hotwords {
		hw = strings.TrimSpace(hw)
		if hw == "" {
			continue
		}
		if _, exists := freq[hw]; !exists {
			freq[hw] = minFreq // 白名单直通：计入阈值线以上
		}
	}
	type kv struct {
		Term string
		N    int
	}
	list := make([]kv, 0, len(freq))
	top := 0
	for t, n := range freq {
		if n > top {
			top = n
		}
		if n >= minFreq {
			list = append(list, kv{t, n})
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].N != list[j].N {
			return list[i].N > list[j].N
		}
		return list[i].Term < list[j].Term // 同频按字典序，保证确定性
	})
	out := make([]string, 0, 8)
	for i, e := range list {
		if i >= 10 {
			break // 最多 10 个，避免 motion-plan 膨胀
		}
		out = append(out, e.Term)
	}
	return out, top
}

func isStopword(term string) bool {
	if enStopwords[strings.ToLower(term)] {
		return true
	}
	for _, sw := range zhStopwords {
		if term == sw {
			return true
		}
	}
	return false
}

func isPunctOnly(s string) bool {
	for _, r := range s {
		if !isPunctRune(r) && !unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

func hasEmphaticPunct(text string) bool {
	return strings.ContainsAny(text, "!！?？")
}

// contentTypeLexicon 内容类型词表（表驱动可扩充，matchFiller 同款机制）。
var contentTypeLexicon = map[string][]string{
	"tutorial":  {"首先", "步骤", "方法", "教程", "如何", "怎么", "介绍", "讲解", "操作", "设置", "配置", "安装", "使用", "示例", "实现", "原理", "注意", "总结"},
	"ad":        {"优惠", "限时", "购买", "下单", "链接", "价格", "免费", "抢购", "活动", "折扣", "广告", "记得关注", "点赞", "订阅", "转发", "评论区", "橱窗"},
	"narrative": {"故事", "有一天", "后来", "当时", "记得", "发生", "经历", "结局", "主人公", "那时候", "没想到", "原来"},
	"vlog":      {"今天", "出门", "旅行", "日常", "生活", "散步", "咖啡", "天气", "周末", "出发", "到家"},
}

// classifyContent 词表命中计数定内容类型（全不命中 → unknown）。
func classifyContent(cues []Cue) string {
	joined := ""
	for _, c := range cues {
		joined += c.Text + "\n"
	}
	best, bestN := "unknown", 0
	types := []string{"tutorial", "ad", "narrative", "vlog"} // 固定顺序保证确定性
	for _, t := range types {
		n := 0
		for _, w := range contentTypeLexicon[t] {
			n += strings.Count(joined, w)
		}
		if n > bestN {
			best, bestN = t, n
		}
	}
	return best
}

// ===== 两级规则引擎（§5.4：表驱动 + 固定顺序，不用权重） =====

// Rule 一条特征→特效规则。
type Rule struct {
	Match func(a *Analysis) bool
	Skill string
}

// Level 1：内容类型（互斥强信号，命中第一个即停，作为主 Skill）。
// marker-sweep 依赖关键词（无关键词可扫时不该选中——否则 reasoning
// 声称生效而实际无视觉效果，审计失真；真实 MCP 全链核对发现）。
var contentTypeRules = []Rule{
	{Match: func(a *Analysis) bool { return a.ContentType == "tutorial" && len(a.TopKeywords) > 0 }, Skill: "marker-sweep"},
	{Match: func(a *Analysis) bool { return a.ContentType == "ad" }, Skill: "bounce-in"},
	{Match: func(a *Analysis) bool { return a.ContentType == "narrative" }, Skill: "karaoke-fill"},
	{Match: func(a *Analysis) bool { return a.ContentType == "vlog" }, Skill: "blur-reveal"}, // ASS 不支持 → 降级 fade-subtle
}

// Level 2：特征规则（可多条命中，按表顺序取前 N-1 个作补充）。
var featureRules = []Rule{
	{Match: func(a *Analysis) bool { return a.KeywordDensity >= 0.6 }, Skill: "keyword-highlight"},
	{Match: func(a *Analysis) bool { return a.AvgCueDuration < 2.0 }, Skill: "kinetic-typography"},
	{Match: func(a *Analysis) bool { return a.AvgCharsPerCue > 15 }, Skill: "slide-in"},
	{Match: func(a *Analysis) bool { return a.EmphaticRatio > 0.2 }, Skill: "pulse-emphasis"},
}

// SelectOptions 自动选择参数（max_auto_skills / disabled_skills / skills 覆盖）。
type SelectOptions struct {
	MaxSkills      int
	DisabledSkills []string
	ExplicitSkills []string // 非空 = Codex 显式覆盖，跳过自动选择
}

// RejectedSkill 未选中的特效及理由（motion-plan 审计）。
type RejectedSkill struct {
	Skill  string `json:"skill"`
	Reason string `json:"reason"`
}

// AutoSkills motion-plan 的 auto_skills 块。
type AutoSkills struct {
	Selected  []string        `json:"selected"`
	Reasoning string          `json:"reasoning"`
	Rejected  []RejectedSkill `json:"rejected"`
	Analysis  *Analysis       `json:"analysis"`
}

// MotionPlan motion-plan.json 顶层结构（§5.6，审计契约）。
type MotionPlan struct {
	Meta struct {
		StyleID     string     `json:"style_id"`
		Orientation string     `json:"orientation"`
		AutoSkills  AutoSkills `json:"auto_skills"`
	} `json:"meta"`
}

// SelectSkills 两级表驱动 + 固定顺序（§5.4 冲突消解 4 步）：
//  1. Level 1 按序匹配，命中第一个即停，作为主 Skill（内容类型是强信号）；
//  2. Level 2 按序扫描，跳过与主 Skill 重复的，取前 N-1 个作补充；
//  3. 总数不超 max_auto_skills；disabled_skills 在最终输出前过滤并记 rejected；
//  4. 无命中 → 纯静态字幕（合法态）；ExplicitSkills 非空时跳过自动选择。
func SelectSkills(a *Analysis, opts SelectOptions) AutoSkills {
	res := AutoSkills{Selected: []string{}, Rejected: []RejectedSkill{}, Analysis: a}
	if opts.MaxSkills <= 0 {
		opts.MaxSkills = 2
	}
	disabled := map[string]bool{}
	for _, d := range opts.DisabledSkills {
		disabled[d] = true
	}

	// 显式覆盖（Codex 通道）：跳过自动选择，仅做禁用于越界过滤
	if len(opts.ExplicitSkills) > 0 {
		for _, s := range opts.ExplicitSkills {
			if disabled[s] {
				res.Rejected = append(res.Rejected, RejectedSkill{s, "配置禁用（disabled_skills）"})
				continue
			}
			if len(res.Selected) >= opts.MaxSkills {
				res.Rejected = append(res.Rejected, RejectedSkill{s, fmt.Sprintf("超出 max_auto_skills=%d 上限", opts.MaxSkills)})
				continue
			}
			res.Selected = append(res.Selected, s)
		}
		res.Reasoning = "Codex 显式指定特效（跳过自动选择）"
		return res
	}

	// Level 1：互斥命中即停
	mainSkill := ""
	for _, r := range contentTypeRules {
		if r.Match(a) {
			mainSkill = r.Skill
			break
		}
	}
	mainReason := ""
	if mainSkill != "" {
		if mainSkill == "blur-reveal" {
			// ASS 不支持文字模糊 → 明确降级并记 rejected（不假装支持）
			res.Rejected = append(res.Rejected, RejectedSkill{"blur-reveal", "libass 无文字模糊能力，降级 fade-subtle"})
			mainSkill = "fade-subtle"
		}
		if disabled[mainSkill] {
			res.Rejected = append(res.Rejected, RejectedSkill{mainSkill, "配置禁用（disabled_skills）"})
		} else {
			res.Selected = append(res.Selected, mainSkill)
			mainReason = fmt.Sprintf("Level 1 内容类型=%s → %s（主技能）", a.ContentType, mainSkill)
		}
	}

	// Level 2：按序取前 N-1
	extra := opts.MaxSkills - len(res.Selected)
	var supplements []string
	var suppReasons []string
	for _, r := range featureRules {
		if extra <= 0 {
			break
		}
		if !r.Match(a) {
			continue
		}
		if contains(res.Selected, r.Skill) {
			continue // 与主技能重复
		}
		if disabled[r.Skill] {
			res.Rejected = append(res.Rejected, RejectedSkill{r.Skill, "配置禁用（disabled_skills）"})
			continue
		}
		supplements = append(supplements, r.Skill)
		suppReasons = append(suppReasons, fmt.Sprintf("Level 2 %s → %s", featureReason(r.Skill, a), r.Skill))
		extra--
	}
	res.Selected = append(res.Selected, supplements...)

	// 记录被上限截断的第一条命中项（审计完整性：为什么选了 A 没选 B）
	if extra <= 0 && len(supplements) > 0 {
		for _, r := range featureRules {
			if !r.Match(a) || contains(supplements, r.Skill) || contains(res.Selected, r.Skill) || disabled[r.Skill] {
				continue
			}
			res.Rejected = append(res.Rejected, RejectedSkill{r.Skill, fmt.Sprintf("Level 2 命中，但按序取前 N-1，超出 max_auto_skills=%d 上限", opts.MaxSkills)})
			break
		}
	}
	if mainReason != "" && len(suppReasons) > 0 {
		res.Reasoning = mainReason + "；" + strings.Join(suppReasons, "；") + "（补充）"
	} else if mainReason != "" {
		res.Reasoning = mainReason
	} else if len(suppReasons) > 0 {
		res.Reasoning = strings.Join(suppReasons, "；")
	} else {
		res.Reasoning = "无特征命中：纯静态字幕"
	}
	return res
}

func featureReason(skill string, a *Analysis) string {
	switch skill {
	case "keyword-highlight":
		return fmt.Sprintf("关键词密度 %.2f", a.KeywordDensity)
	case "kinetic-typography":
		return fmt.Sprintf("平均 cue 时长 %.1fs < 2s", a.AvgCueDuration)
	case "slide-in":
		return fmt.Sprintf("信息密度 %.0f 字/条 > 15", a.AvgCharsPerCue)
	case "pulse-emphasis":
		return fmt.Sprintf("感叹/疑问占比 %.2f > 0.2", a.EmphaticRatio)
	}
	return skill
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}
