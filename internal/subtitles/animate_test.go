package subtitles

// P2 单元测试：关键词提取 / 两级规则引擎 / 特效 tags / motion-plan 不变式（§5.4–§5.6，§8）。

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExtractKeywords(t *testing.T) {
	words := []Word{
		{Start: 0, End: 0.5, Word: "videocut"}, {Start: 1, End: 1.5, Word: "videocut"},
		{Start: 2, End: 2.5, Word: "videocut"}, {Start: 3, End: 3.5, Word: "videocut"},
		{Start: 4, End: 4.5, Word: "FFmpeg"}, {Start: 5, End: 5.5, Word: "FFmpeg"}, {Start: 5.6, End: 5.9, Word: "FFmpeg"},
		{Start: 6, End: 6.5, Word: "的"}, // 停用词
		{Start: 7, End: 7.5, Word: "了"}, // 停用词
		{Start: 8, End: 8.5, Word: "云"}, // 单字专名（无白名单应过滤）
		{Start: 9, End: 9.5, Word: "，"}, // 标点
		{Start: 10, End: 10.5, Word: "faster-whisper"}, {Start: 11, End: 11.5, Word: "faster-whisper"},
		{Start: 12, End: 12.5, Word: "faster-whisper"},
	}
	got, top := extractKeywords(words, nil, 3)
	if top != 4 {
		t.Errorf("最高词频 = %d 期望 4", top)
	}
	want := []string{"videocut", "FFmpeg", "faster-whisper"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("关键词 = %v 期望 %v", got, want)
	}
	for _, k := range got {
		if k == "的" || k == "了" || k == "云" || k == "，" {
			t.Errorf("停用词/单字/标点不应入选: %q", k)
		}
	}
}

func TestExtractKeywords_HotwordWhitelist(t *testing.T) {
	// 单字专名走热词表白名单兜底（不动过滤规则）
	words := []Word{{Start: 0, End: 0.5, Word: "盾"}, {Start: 1, End: 1.5, Word: "盾"}}
	got, _ := extractKeywords(words, []string{"盾"}, 3)
	if len(got) != 1 || got[0] != "盾" {
		t.Errorf("热词白名单应保留单字专名: %v", got)
	}
}

func TestAnalyze(t *testing.T) {
	cues := []Cue{
		{Start: 0, End: 2, Text: "首先打开设置，配置环境变量。"},
		{Start: 2.2, End: 4, Text: "如何安装？其实很简单！"},
		{Start: 4.3, End: 6, Text: "总结一下三个步骤"},
	}
	words := []Word{
		{Start: 0, End: 0.4, Word: "videocut"}, {Start: 1, End: 1.4, Word: "videocut"},
		{Start: 2, End: 2.4, Word: "videocut"}, {Start: 3, End: 3.4, Word: "videocut"},
	}
	a := Analyze(cues, words, nil, 3)
	if a.AvgCueDuration != 1.83 {
		t.Errorf("AvgCueDuration = %v 期望 1.83", a.AvgCueDuration)
	}
	if a.AvgCharsPerCue != 11 {
		t.Errorf("AvgCharsPerCue = %v 期望 11", a.AvgCharsPerCue)
	}
	if a.EmphaticRatio < 0.32 || a.EmphaticRatio > 0.34 {
		t.Errorf("EmphaticRatio = %v 期望约 0.33", a.EmphaticRatio)
	}
	if a.ContentType != "tutorial" {
		t.Errorf("ContentType = %q 期望 tutorial", a.ContentType)
	}
	if a.KeywordDensity != 0.8 { // top=4, sat=5
		t.Errorf("KeywordDensity = %v 期望 0.8", a.KeywordDensity)
	}
}

// TestSelectSkills_MarkerSweepRequiresKeywords 回归：marker-sweep 无关键词
// 可扫时 Level 1 不命中（否则 reasoning 声称生效而实际无视觉效果）。
func TestSelectSkills_MarkerSweepRequiresKeywords(t *testing.T) {
	a := &Analysis{ContentType: "tutorial", TopKeywords: nil, KeywordDensity: 0.4, AvgCueDuration: 1.5}
	res := SelectSkills(a, SelectOptions{MaxSkills: 2})
	for _, s := range res.Selected {
		if s == "marker-sweep" {
			t.Fatalf("无关键词时不应选 marker-sweep: %v", res.Selected)
		}
	}
	// 有关键词时正常选中
	a2 := &Analysis{ContentType: "tutorial", TopKeywords: []string{"videocut"}, KeywordDensity: 0.8}
	res2 := SelectSkills(a2, SelectOptions{MaxSkills: 2})
	if len(res2.Selected) == 0 || res2.Selected[0] != "marker-sweep" {
		t.Fatalf("有关键词应选 marker-sweep: %v", res2.Selected)
	}
}

func TestSelectSkills_TwoLevel(t *testing.T) {
	// tutorial + 高密度 + 慢节奏（>4s 无 fade 规则命中——fade 只属 Level1 vlog 降级）
	a := &Analysis{
		ContentType:    "tutorial",
		TopKeywords:    []string{"videocut"},
		KeywordDensity: 0.82,
		AvgCueDuration: 2.1,
		AvgCharsPerCue: 16,
		EmphaticRatio:  0.15,
	}
	res := SelectSkills(a, SelectOptions{MaxSkills: 2})
	if strings.Join(res.Selected, ",") != "marker-sweep,keyword-highlight" {
		t.Fatalf("selected = %v 期望 [marker-sweep keyword-highlight]", res.Selected)
	}
	// Level 2 命中 3 条（keyword/kinetic? 2.1s 不 <2 → 不命中；slide-in 16>15 命中）→ slide-in 应记 rejected（超上限）
	foundSlideReject := false
	for _, r := range res.Rejected {
		if r.Skill == "slide-in" && strings.Contains(r.Reason, "上限") {
			foundSlideReject = true
		}
	}
	if !foundSlideReject {
		t.Errorf("slide-in 应因超上限记 rejected: %+v", res.Rejected)
	}
	if !strings.Contains(res.Reasoning, "Level 1 内容类型=tutorial") {
		t.Errorf("reasoning 应含 Level 1 依据: %q", res.Reasoning)
	}
}

func TestSelectSkills_Level1MutualExclusive(t *testing.T) {
	// ad 命中 → bounce-in 主技能（不会同时选 tutorial 的 marker-sweep）；
	// 特征字段取"无额外命中"值，隔离 Level 1 行为
	a := &Analysis{ContentType: "ad", AvgCueDuration: 3, AvgCharsPerCue: 12, KeywordDensity: 0.3, EmphaticRatio: 0.1}
	res := SelectSkills(a, SelectOptions{MaxSkills: 2})
	if len(res.Selected) != 1 || res.Selected[0] != "bounce-in" {
		t.Fatalf("ad 应仅选 bounce-in: %v", res.Selected)
	}
}

func TestSelectSkills_BlurRevealDegrade(t *testing.T) {
	// vlog → blur-reveal 不支持 → 降级 fade-subtle 并记 rejected
	a := &Analysis{ContentType: "vlog", AvgCueDuration: 3, AvgCharsPerCue: 12, KeywordDensity: 0.3, EmphaticRatio: 0.1}
	res := SelectSkills(a, SelectOptions{MaxSkills: 2})
	if len(res.Selected) != 1 || res.Selected[0] != "fade-subtle" {
		t.Fatalf("vlog 应降级 fade-subtle: %v", res.Selected)
	}
	ok := false
	for _, r := range res.Rejected {
		if r.Skill == "blur-reveal" {
			ok = true
		}
	}
	if !ok {
		t.Error("blur-reveal 应记 rejected")
	}
}

func TestSelectSkills_DisabledFilter(t *testing.T) {
	a := &Analysis{ContentType: "tutorial", KeywordDensity: 0.9}
	res := SelectSkills(a, SelectOptions{MaxSkills: 2, DisabledSkills: []string{"marker-sweep"}})
	for _, s := range res.Selected {
		if s == "marker-sweep" {
			t.Fatal("disabled_skills 不应出现在 selected")
		}
	}
	if len(res.Selected) == 0 {
		t.Error("主技能被禁用后 Level 2 应补位（keyword-highlight）")
	}
}

func TestSelectSkills_ExplicitOverride(t *testing.T) {
	a := &Analysis{ContentType: "tutorial"}
	res := SelectSkills(a, SelectOptions{MaxSkills: 2, ExplicitSkills: []string{"karaoke-fill", "bounce-in", "pulse-emphasis"}})
	if strings.Join(res.Selected, ",") != "karaoke-fill,bounce-in" {
		t.Fatalf("显式覆盖应按序取 2 个: %v", res.Selected)
	}
	if res.Rejected[0].Skill != "pulse-emphasis" {
		t.Errorf("第三个应记 rejected: %+v", res.Rejected)
	}
}

func TestSelectSkills_NoHitStatic(t *testing.T) {
	a := &Analysis{ContentType: "unknown", KeywordDensity: 0.1, AvgCueDuration: 5, AvgCharsPerCue: 10, EmphaticRatio: 0.05}
	res := SelectSkills(a, SelectOptions{MaxSkills: 2})
	if len(res.Selected) != 0 {
		t.Fatalf("无特征命中应为空 selected（纯静态合法态）: %v", res.Selected)
	}
	if res.Reasoning != "无特征命中：纯静态字幕" {
		t.Errorf("reasoning = %q", res.Reasoning)
	}
}

func TestApplyEffects_Tags(t *testing.T) {
	// cue0 无词级数据：marker-sweep 退化为 keyword-highlight（span 上色）
	// cue1 有词级数据：marker-sweep 逐词 \kf + 关键词金色
	cues := []Cue{
		{Start: 0, End: 2, Text: "大家好，欢迎使用 videocut 工具"},
		{Start: 2, End: 3, Text: "videocut 很简单", Words: []Word{
			{Start: 2, End: 2.4, Word: "videocut"}, {Start: 2.4, End: 2.7, Word: "很"}, {Start: 2.7, End: 3, Word: "简单"},
		}},
	}
	plan := &MotionPlan{}
	plan.Meta.StyleID = "talking-head"
	plan.Meta.AutoSkills.Selected = []string{"marker-sweep", "fade-subtle"}
	plan.Meta.AutoSkills.Analysis = &Analysis{TopKeywords: []string{"videocut"}}

	out := ApplyEffects(cues, EffectOptions{Plan: plan, Preset: Presets["talking-head"], VideoWidth: 1920, VideoHeight: 1080, KeywordColor: "#FFD700"})
	// cue0：无词级 → keyword-highlight 退化路径 + fade 前缀
	if !strings.Contains(out[0].Text, "\\c&H0000D7FF") {
		t.Errorf("关键词应上金色 &H0000D7FF: %q", out[0].Text)
	}
	if !strings.Contains(out[0].Text, "\\fad(200,200)") {
		t.Errorf("fade-subtle 应生成 \\fad: %q", out[0].Text)
	}
	// cue1：有词级 → 关键词词 \kf + 金色，非关键词词原样
	if !strings.Contains(out[1].Text, "{\\kf40}{\\c&H0000D7FF}videocut{\\r}") {
		t.Errorf("marker-sweep 关键词应 \\kf+金色: %q", out[1].Text)
	}
	if !strings.Contains(out[1].Text, "很简单") && !strings.Contains(out[1].Text, "很简单") {
		t.Errorf("非关键词词应原样保留: %q", out[1].Text)
	}
}

// TestApplyEffects_WordCoverageFallback 回归：词级数据覆盖不足时放弃词级
// 特效（词级模式会用 words 重建文本，丢内容比特效弱更严重），回落 span 上色。
func TestApplyEffects_WordCoverageFallback(t *testing.T) {
	// words 只覆盖"大家 videocut"，原文还有"好，欢迎使用…工具"——覆盖率 < 80%
	cues := []Cue{{Start: 0, End: 2, Text: "大家好，欢迎使用 videocut 工具", Words: []Word{
		{Start: 0, End: 0.8, Word: "大家"},
		{Start: 0.8, End: 2, Word: "videocut"},
	}}}
	plan := &MotionPlan{}
	plan.Meta.AutoSkills.Selected = []string{"marker-sweep"}
	plan.Meta.AutoSkills.Analysis = &Analysis{TopKeywords: []string{"videocut"}}
	out := ApplyEffects(cues, EffectOptions{Plan: plan, KeywordColor: "#FFD700"})
	if strings.Contains(out[0].Text, `\kf`) {
		t.Errorf("覆盖不足不应走词级特效: %q", out[0].Text)
	}
	// 回落 span 上色且文本完整（一个字节都不丢）
	for _, frag := range []string{"大家好，欢迎使用", "工具", `\c&H0000D7FF`} {
		if !strings.Contains(out[0].Text, frag) {
			t.Errorf("回落结果应含 %q: %q", frag, out[0].Text)
		}
	}

	// 覆盖充分时词级特效生效（不回归；文本须含关键词，marker-sweep 只扫关键词词）
	full := []Cue{{Start: 0, End: 2, Text: "大家好欢迎使用 videocut", Words: []Word{
		{Start: 0, End: 0.5, Word: "大家"}, {Start: 0.5, End: 1, Word: "好欢"},
		{Start: 1, End: 1.5, Word: "迎使"}, {Start: 1.5, End: 2, Word: "videocut"},
	}}}
	out2 := ApplyEffects(full, EffectOptions{Plan: plan, KeywordColor: "#FFD700"})
	if !strings.Contains(out2[0].Text, `\kf`) {
		t.Errorf("覆盖充分应走词级特效: %q", out2[0].Text)
	}
}

func TestApplyEffects_Karaoke(t *testing.T) {
	cues := []Cue{{Start: 0, End: 1, Text: "逐词填充", Words: []Word{
		{Start: 0, End: 0.5, Word: "逐词"}, {Start: 0.5, End: 1, Word: "填充"},
	}}}
	plan := &MotionPlan{}
	plan.Meta.AutoSkills.Selected = []string{"karaoke-fill"}
	out := ApplyEffects(cues, EffectOptions{Plan: plan})
	if strings.Count(out[0].Text, "\\kf") != 2 {
		t.Errorf("karaoke 应逐词 \\kf ×2: %q", out[0].Text)
	}

	// 词级重建的中英边界必须补空格（防"使用videocut"粘连；真实全链核对发现）
	mixed := []Cue{{Start: 0, End: 2, Text: "使用 videocut 工具", Words: []Word{
		{Start: 0, End: 0.5, Word: "使用"}, {Start: 0.5, End: 1.2, Word: "videocut"}, {Start: 1.2, End: 2, Word: "工具"},
	}}}
	out2 := ApplyEffects(mixed, EffectOptions{Plan: plan})
	if !strings.Contains(out2[0].Text, "使用 {\\kf70}videocut") {
		t.Errorf("中英边界应补空格: %q", out2[0].Text)
	}
}

func TestMotionPlan_JSON(t *testing.T) {
	// 序列化形状与不变式（§5.6）：selected ⊆ 预设库、reasoning 非空、analysis 齐全
	a := &Analysis{KeywordDensity: 0.82, ContentType: "tutorial", AvgCueDuration: 2.1, AvgCharsPerCue: 16, EmphaticRatio: 0.15, TopKeywords: []string{"videocut"}}
	res := SelectSkills(a, SelectOptions{MaxSkills: 2})
	plan := &MotionPlan{}
	plan.Meta.StyleID = "talking-head"
	plan.Meta.Orientation = "landscape"
	plan.Meta.AutoSkills = res
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var back MotionPlan
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.Meta.AutoSkills.Reasoning == "" {
		t.Error("reasoning 不能为空")
	}
	if back.Meta.AutoSkills.Analysis == nil || len(back.Meta.AutoSkills.Analysis.TopKeywords) != 1 {
		t.Error("analysis 字段应齐全")
	}
	for _, s := range back.Meta.AutoSkills.Selected {
		if !knownSkill(s) {
			t.Errorf("selected 含未知特效 %q", s)
		}
	}
}

func knownSkill(s string) bool {
	switch s {
	case "marker-sweep", "bounce-in", "karaoke-fill", "fade-subtle", "keyword-highlight",
		"kinetic-typography", "slide-in", "pulse-emphasis", "blur-reveal":
		return true
	}
	return false
}

func TestAssColor(t *testing.T) {
	cases := map[string]string{
		"#FFD700": "&H0000D7FF",
		"FFD700":  "&H0000D7FF",
		"#ffffff": "&H00FFFFFF",
		"bad":     "&H0000D7FF", // 回落金
	}
	for in, want := range cases {
		if got := assColor(in); got != want {
			t.Errorf("assColor(%q) = %q 期望 %q", in, got, want)
		}
	}
}
