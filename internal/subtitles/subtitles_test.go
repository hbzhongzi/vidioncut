package subtitles

// P0 单元测试：重映射表驱动 / 预换行 / SRT+ASS 渲染 / 不变式（字幕功能更新文档 §8）。

import (
	"strings"
	"testing"

	"videocut/internal/edits"
)

// TestRemapCues_IntactSegmentCarriesWords 回归：整段保留的 cue 必须携带
// 词级数据（成片轴）。词级特效（marker-sweep/karaoke-fill/kinetic 的 \kf）
// 对全部 cue 可用，而整段保留是最常见情形——早期实现只在跨段断开时带
// Words，导致这些特效对大多数 cue 静默落空（真实 MCP 全链核对 fx.ass 发现）。
func TestRemapCues_IntactSegmentCarriesWords(t *testing.T) {
	segs := []Segment{{Start: 0.3, End: 2.0, Text: "大家好欢迎使用"}}
	words := []Word{
		{Start: 0.3, End: 0.8, Word: "大家"},
		{Start: 0.8, End: 1.2, Word: "好欢"},
		{Start: 1.2, End: 2.0, Word: "迎使"},
		{Start: 5.0, End: 5.5, Word: "用"}, // 属于后续 segment，不得混入
	}
	keeps := []edits.KeepRange{{Start: 0, End: 3}}
	got, err := RemapCues(segs, words, keeps, 3)
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("cue 数 %d 期望 1", len(got))
	}
	if len(got[0].Words) != 3 {
		t.Fatalf("整段保留应携带 3 个词级 token，实际 %d: %+v", len(got[0].Words), got[0].Words)
	}
	// 词级时间戳已映射到成片轴（此处零删，应与源一致）
	for i, w := range got[0].Words {
		if abs64(w.Start-words[i].Start) > 1e-3 || abs64(w.End-words[i].End) > 1e-3 {
			t.Errorf("词 %d 时间轴 %.3f–%.3f 期望 %.3f–%.3f",
				i, w.Start, w.End, words[i].Start, words[i].End)
		}
	}
	// 无词级数据时 Words 为 nil（omitempty，不发空数组）
	got2, err := RemapCues(segs, nil, keeps, 3)
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if got2[0].Words != nil {
		t.Errorf("无词级数据时 Words 应为 nil，实际 %+v", got2[0].Words)
	}
}

func TestRemapCues_TableDriven(t *testing.T) {
	cases := []struct {
		name  string
		segs  []Segment
		words []Word
		keeps []edits.KeepRange
		want  []Cue // 只断言 Start/End/Text/SplitAt
	}{
		{
			name:  "零删：全集平移 0",
			segs:  []Segment{{Start: 1, End: 2.5, Text: "大家好"}, {Start: 3, End: 4, Text: "欢迎"}},
			keeps: []edits.KeepRange{{Start: 0, End: 10}},
			want: []Cue{
				{Start: 1, End: 2.5, Text: "大家好"},
				{Start: 3, End: 4, Text: "欢迎"},
			},
		},
		{
			name:  "全删：合法空集",
			segs:  []Segment{{Start: 1, End: 2, Text: "大家好"}},
			keeps: nil,
			want:  []Cue{},
		},
		{
			name: "贴头删除 + 整段平移",
			segs: []Segment{
				{Start: 0, End: 1, Text: "开头静音废话"},
				{Start: 3, End: 5, Text: "正文开始"},
			},
			keeps: []edits.KeepRange{{Start: 2, End: 10}},
			want: []Cue{
				{Start: 1, End: 3, Text: "正文开始"}, // 3-2=1, 5-2=3
			},
		},
		{
			name: "贴尾删除",
			segs: []Segment{
				{Start: 1, End: 2, Text: "正文"},
				{Start: 8, End: 9.5, Text: "结尾废话"},
			},
			keeps: []edits.KeepRange{{Start: 0, End: 7}},
			want: []Cue{
				{Start: 1, End: 2, Text: "正文"},
			},
		},
		{
			name: "跨保留段边界：词级断开",
			segs: []Segment{{Start: 1.5, End: 5.5, Text: "这一段横跨删除区"}},
			words: []Word{
				{Start: 1.5, End: 1.9, Word: "这"},
				{Start: 1.9, End: 2.0, Word: "一"},
				{Start: 4.0, End: 4.5, Word: "段"},
				{Start: 4.5, End: 5.0, Word: "横"},
				{Start: 5.0, End: 5.5, Word: "跨"},
			},
			// 保留 [0,2) 与 [4,10)：拼接点 2.0→成片 2.0；4.0→成片 2.0
			keeps: []edits.KeepRange{{Start: 0, End: 2}, {Start: 4, End: 10}},
			want: []Cue{
				{Start: 1.5, End: 2.0, Text: "这一", SplitAt: true},  // keep0 内
				{Start: 2.0, End: 3.5, Text: "段横跨", SplitAt: true}, // keep1：4.0→2.0, 5.5→3.5
			},
		},
		{
			name:  "跨边界但无词级数据：截断到边界",
			segs:  []Segment{{Start: 1.5, End: 5.5, Text: "无词数据"}},
			words: nil,
			keeps: []edits.KeepRange{{Start: 0, End: 2}, {Start: 4, End: 10}},
			want: []Cue{
				{Start: 1.5, End: 2.0, Text: "无词数据", SplitAt: true},
				{Start: 2.0, End: 3.5, Text: "无词数据", SplitAt: true},
			},
		},
		{
			name:  "零/负时长段跳过",
			segs:  []Segment{{Start: 2, End: 2, Text: "零时长"}, {Start: 5, End: 4, Text: "负时长"}, {Start: 1, End: 1.5, Text: "正常"}},
			keeps: []edits.KeepRange{{Start: 0, End: 10}},
			want: []Cue{
				{Start: 1, End: 1.5, Text: "正常"},
			},
		},
	}

	// 空洞用例单独断言（segment 全落删除段 → 丢弃）
	t.Run("空洞：segment全落删除段", func(t *testing.T) {
		holeCues, err := RemapCues([]Segment{
			{Start: 3, End: 4, Text: "被删"},
			{Start: 6, End: 7, Text: "保留"},
		}, nil, []edits.KeepRange{{Start: 0, End: 2.5}, {Start: 5, End: 10}}, 10)
		if err != nil {
			t.Fatalf("空洞用例失败: %v", err)
		}
		if len(holeCues) != 1 || holeCues[0].Text != "保留" || holeCues[0].Start != 3.5 || holeCues[0].End != 4.5 {
			t.Fatalf("空洞用例: 期望仅 [保留] 3.5→4.5，实际 %+v", holeCues)
		}
	})

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := RemapCues(tc.segs, tc.words, tc.keeps, 10)
			if err != nil {
				t.Fatalf("RemapCues 失败: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("cue 数 %d 期望 %d\n got=%+v", len(got), len(tc.want), got)
			}
			for i := range got {
				g, w := got[i], tc.want[i]
				if abs64(g.Start-w.Start) > 1e-3 || abs64(g.End-w.End) > 1e-3 {
					t.Errorf("cue %d 时间轴 %.3f–%.3f 期望 %.3f–%.3f", i, g.Start, g.End, w.Start, w.End)
				}
				if g.Text != w.Text {
					t.Errorf("cue %d 文本 %q 期望 %q", i, g.Text, w.Text)
				}
				if g.SplitAt != w.SplitAt {
					t.Errorf("cue %d SplitAt=%v 期望 %v", i, g.SplitAt, w.SplitAt)
				}
			}
		})
	}

	// 零/负时长用例（cases 最后一项）
	t.Run("零负时长段跳过", func(t *testing.T) {
		tc := cases[len(cases)-1]
		got, err := RemapCues(tc.segs, nil, tc.keeps, 10)
		if err != nil {
			t.Fatalf("失败: %v", err)
		}
		if len(got) != 1 || got[0].Text != "正常" {
			t.Fatalf("期望仅 [正常]，实际 %+v", got)
		}
	})
}

func abs64(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func TestRemapCues_WordNotDuplicatedAcrossBoundary(t *testing.T) {
	// 边界词（跨保留段拼接点的词）只归一条 cue，不得在相邻两条重复出现
	segs := []Segment{{Start: 1.8, End: 4.2, Text: "跨界词测试用例文本"}}
	words := []Word{
		{Start: 1.8, End: 2.05, Word: "跨界"}, // 跨越 keep0 右端 2.0
		{Start: 2.05, End: 2.4, Word: "词测"}, // 落删除段（应被剪掉）
		{Start: 4.0, End: 4.2, Word: "文本"},  // keep1 内
	}
	keeps := []edits.KeepRange{{Start: 0, End: 2}, {Start: 4, End: 10}}
	got, err := RemapCues(segs, words, keeps, 10)
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	joined := ""
	for _, c := range got {
		joined += c.Text
	}
	if strings.Count(joined, "跨界") != 1 {
		t.Errorf("跨界词出现 %d 次，期望 1 次（cue: %+v）", strings.Count(joined, "跨界"), got)
	}
	if strings.Contains(joined, "词测") {
		t.Errorf("删除段内的词不应出现: %q", joined)
	}
}

func TestRemapCues_ClampAndSelfCheck(t *testing.T) {
	// keeps 早退（总时长 2.5s）但 segment 延伸到 8s——交集裁剪保证不越界；
	// 同时验证自检：任何越界产物必须显式报错而非坏文件
	segs := []Segment{{Start: 0, End: 8, Text: "长段"}}
	keeps := []edits.KeepRange{{Start: 0, End: 2.5}}
	got, err := RemapCues(segs, nil, keeps, 8)
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if len(got) != 1 || got[0].End > 2.5+0.05 {
		t.Fatalf("期望裁剪到 2.5s，实际 %+v", got)
	}
	if err := ValidateCues(got, 2.5); err != nil {
		t.Errorf("自检应通过: %v", err)
	}
}

func TestSegmentCues_Wrap(t *testing.T) {
	cases := []struct {
		name     string
		text     string
		maxChars int
		maxLines int
		want     string
	}{
		{"短句不换行", "大家好", 20, 2, "大家好"},
		{"20字断行", "一二三四五六七八九十一二三四五六七八九二十字整", 20, 2, "一二三四五六七八九十一二三四五六七八九二\n十字整"},
		{"英文词边界", "hello world this is a test", 15, 2, "hello world this\nis a test"},
		{"标点悬挂不计行宽", "你好，世界。和平。", 4, 2, "你好，世界。\n和平。"},
		{"溢出截断加省略号", "一二三四五六七八九十一二三四五六七八九三十四", 10, 2, "一二三四五六七八九十\n一二三四五六七八九三…"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SegmentCues([]Cue{{Text: tc.text}}, tc.maxChars, tc.maxLines)[0].Text
			if got != tc.want {
				t.Errorf("wrap(%q) = %q 期望 %q", tc.text, got, tc.want)
			}
		})
	}
}

func TestSegmentCues_Defaults(t *testing.T) {
	// 非法参数回落默认值（20/2），不得 panic
	got := SegmentCues([]Cue{{Text: "一二三四五六七八九十一二三四五六七八九二十"}}, 0, 0)
	if strings.Count(got[0].Text, "\n") != 1 {
		t.Errorf("缺省 20 字应断一行: %q", got[0].Text)
	}
}

func TestRenderSRT(t *testing.T) {
	cues := []Cue{
		{Start: 1.0, End: 2.5, Text: "第一行\n第二行"},
		{Start: 3600.25, End: 3602, Text: "跨小时"},
	}
	got := RenderSRT(cues)
	want := "1\n00:00:01,000 --> 00:00:02,500\n第一行\n第二行\n\n2\n01:00:00,250 --> 01:00:02,000\n跨小时\n\n"
	if got != want {
		t.Errorf("SRT 渲染不符:\n got=%q\nwant=%q", got, want)
	}
}

func TestRenderASS(t *testing.T) {
	cues := []Cue{{Start: 0.5, End: 2.25, Text: "中文字幕\n第二行"}}
	got := RenderASS(cues, Presets["talking-head"], 1920, 1080)
	for _, want := range []string{
		"PlayResX: 1920",
		"PlayResY: 1080",
		"Style: Default,Microsoft YaHei,56,",
		"Dialogue: 0,0:00:00.50,0:00:02.25,Default,,0,0,0,,中文字幕\\N第二行",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("ASS 缺少 %q\n完整:\n%s", want, got)
		}
	}
}

func TestRenderASS_FallbackSize(t *testing.T) {
	// 0 宽高回落 1920×1080（probe 失败不阻断导出）
	got := RenderASS(nil, Presets["default"], 0, 0)
	if !strings.Contains(got, "PlayResX: 1920") {
		t.Error("0 宽高应回落 1920×1080")
	}
}

func TestValidateCues_Invariants(t *testing.T) {
	cases := []struct {
		name string
		cues []Cue
		dur  float64
		bad  bool
	}{
		{"合法", []Cue{{Start: 0, End: 1, Text: "a"}, {Start: 1, End: 2, Text: "b"}}, 2, false},
		{"空文本", []Cue{{Start: 0, End: 1, Text: ""}}, 2, true},
		{"end不大于start", []Cue{{Start: 1, End: 1, Text: "a"}}, 2, true},
		{"重叠", []Cue{{Start: 0, End: 2, Text: "a"}, {Start: 1, End: 3, Text: "b"}}, 5, true},
		{"越界", []Cue{{Start: 0, End: 3, Text: "a"}}, 2, true},
		{"空集合法", nil, 2, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateCues(tc.cues, tc.dur)
			if tc.bad && err == nil {
				t.Error("期望报错但通过")
			}
			if !tc.bad && err != nil {
				t.Errorf("期望通过但报错: %v", err)
			}
		})
	}
}

func TestJoinWords_CJKSpace(t *testing.T) {
	got := joinWords([]Word{{Word: "大家"}, {Word: "好"}, {Word: "，"}, {Word: "welcome"}, {Word: "to"}, {Word: "videocut"}})
	want := "大家好，welcome to videocut"
	if got != want {
		t.Errorf("joinWords = %q 期望 %q", got, want)
	}
}

func TestFinalDuration(t *testing.T) {
	got := FinalDuration([]edits.KeepRange{{Start: 0, End: 2.5}, {Start: 5, End: 7.5}})
	if got != 5.0 {
		t.Errorf("FinalDuration = %v 期望 5.0", got)
	}
}
