package edits

import (
	"strings"
	"testing"
)

func mkLedger(items ...EditItem) *EditsLedger {
	return &EditsLedger{Version: LedgerVersion, Source: "E:/clips/demo.mp4", Items: items}
}

func TestValidateEdits_TableDriven(t *testing.T) {
	cases := []struct {
		name    string
		ledger  *EditsLedger
		dur     float64
		wantErr string // 空=合法
	}{
		{
			name:   "合法账本",
			ledger: mkLedger(EditItem{Start: 1, End: 2, Type: TypeFillerWord}, EditItem{Start: 5, End: 6, Type: TypeSilence}),
			dur:    10,
		},
		{
			name:   "相邻不重叠",
			ledger: mkLedger(EditItem{Start: 1, End: 2, Type: TypeSilence}, EditItem{Start: 2, End: 3, Type: TypeSilence}),
			dur:    10,
		},
		{
			name:    "version 非法",
			ledger:  &EditsLedger{Version: 2, Source: "a.mp4", Items: []EditItem{{Start: 1, End: 2, Type: TypeSilence}}},
			dur:     10,
			wantErr: "version 必须为 1",
		},
		{
			name:    "source 为空",
			ledger:  &EditsLedger{Version: 1, Items: []EditItem{{Start: 1, End: 2, Type: TypeSilence}}},
			dur:     10,
			wantErr: "source 不能为空",
		},
		{
			name:    "items 为空",
			ledger:  mkLedger(),
			dur:     10,
			wantErr: "items 不能为空",
		},
		{
			name:    "类型非法",
			ledger:  mkLedger(EditItem{Start: 1, End: 2, Type: "blah"}),
			dur:     10,
			wantErr: "第 1 项：type",
		},
		{
			name:    "start 为负",
			ledger:  mkLedger(EditItem{Start: -1, End: 2, Type: TypeSilence}),
			dur:     10,
			wantErr: "第 1 项：start(-1.000) 必须为非负数",
		},
		{
			name:    "end 不大于 start",
			ledger:  mkLedger(EditItem{Start: 3, End: 3, Type: TypeSilence}),
			dur:     10,
			wantErr: "第 1 项：end(3.000) 必须大于 start(3.000)",
		},
		{
			name:    "end 越界",
			ledger:  mkLedger(EditItem{Start: 1, End: 11, Type: TypeSilence}),
			dur:     10,
			wantErr: "第 1 项：end(11.000) 超过源时长(10.000)",
		},
		{
			name:    "乱序",
			ledger:  mkLedger(EditItem{Start: 5, End: 6, Type: TypeSilence}, EditItem{Start: 1, End: 2, Type: TypeSilence}),
			dur:     10,
			wantErr: "第 2 项：start(1.000) 小于前一项 start(5.000)",
		},
		{
			name:    "重叠",
			ledger:  mkLedger(EditItem{Start: 1, End: 5, Type: TypeSilence}, EditItem{Start: 4, End: 6, Type: TypeSilence}),
			dur:     10,
			wantErr: "第 2 项：区间 [4.000,6.000) 与前一项 [1.000,5.000) 重叠",
		},
		{
			name:   "无源时长跳过上界",
			ledger: mkLedger(EditItem{Start: 1, End: 999, Type: TypeSilence}),
			dur:    0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateEdits(c.ledger, c.dur)
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("期望合法，得到错误: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("期望错误 %q，得到合法", c.wantErr)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("错误信息不匹配\n得到: %v\n期望包含: %s", err, c.wantErr)
			}
		})
	}
}

func TestEditsToKeepRanges_Boundaries(t *testing.T) {
	// 贴头（首项 start>0）+ 贴尾（末项 end<duration）+ 中间删除
	l := mkLedger(
		EditItem{Start: 2, End: 3, Type: TypeSilence},
		EditItem{Start: 6, End: 8, Type: TypeFillerWord},
	)
	got := EditsToKeepRanges(l, 10)
	want := []KeepRange{{Start: 0, End: 2}, {Start: 3, End: 6}, {Start: 8, End: 10}}
	if len(got) != len(want) {
		t.Fatalf("保留段数量 %d != %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("保留段 %d: 得到 %+v 期望 %+v", i, got[i], want[i])
		}
	}

	// 贴头删除（首项 start=0）
	l2 := mkLedger(EditItem{Start: 0, End: 2, Type: TypeSilence})
	got2 := EditsToKeepRanges(l2, 10)
	if len(got2) != 1 || got2[0].Start != 2 || got2[0].End != 10 {
		t.Errorf("贴头删除应只产 [2,10]，得到 %+v", got2)
	}

	// 贴尾删除（末项 end=duration）
	l3 := mkLedger(EditItem{Start: 5, End: 10, Type: TypeSilence})
	got3 := EditsToKeepRanges(l3, 10)
	if len(got3) != 1 || got3[0].Start != 0 || got3[0].End != 5 {
		t.Errorf("贴尾删除应只产 [0,5]，得到 %+v", got3)
	}

	// 相邻删除项不产生碎片
	l4 := mkLedger(EditItem{Start: 2, End: 3, Type: TypeSilence}, EditItem{Start: 3, End: 4, Type: TypeSilence})
	got4 := EditsToKeepRanges(l4, 10)
	if len(got4) != 2 {
		t.Fatalf("相邻删除应产 2 段，得到 %+v", got4)
	}
	if got4[0] != (KeepRange{Start: 0, End: 2}) || got4[1] != (KeepRange{Start: 4, End: 10}) {
		t.Errorf("相邻删除结果错误: %+v", got4)
	}

	// 全删
	l5 := mkLedger(EditItem{Start: 0, End: 10, Type: TypeSilence})
	got5 := EditsToKeepRanges(l5, 10)
	if len(got5) != 0 {
		t.Errorf("全删应无保留段，得到 %+v", got5)
	}
}

func TestSummarize(t *testing.T) {
	l := mkLedger(
		EditItem{Start: 12.4, End: 13.1, Type: TypeFillerWord},
		EditItem{Start: 45, End: 47.5, Type: TypeSilence},
		EditItem{Start: 60, End: 61, Type: TypeFillerWord},
	)
	sum := Summarize(l, 100)
	if sum.ItemCount != 3 {
		t.Errorf("ItemCount=%d 期望 3", sum.ItemCount)
	}
	if sum.DeletedSeconds < 4.2-0.001 || sum.DeletedSeconds > 4.2+0.001 {
		t.Errorf("DeletedSeconds=%v 期望 4.2", sum.DeletedSeconds)
	}
	if sum.ByType[TypeFillerWord] != 2 || sum.ByType[TypeSilence] != 1 {
		t.Errorf("ByType=%v", sum.ByType)
	}
	if sum.KeepCount != 4 {
		t.Errorf("KeepCount=%d 期望 4", sum.KeepCount)
	}
	if sum.KeepSeconds < 95.8-0.01 || sum.KeepSeconds > 95.8+0.01 {
		t.Errorf("KeepSeconds=%v 期望 95.8", sum.KeepSeconds)
	}
	if sum.Compression != 4.2 {
		t.Errorf("Compression=%v 期望 4.2", sum.Compression)
	}
}

func TestParseSRT_AndExport(t *testing.T) {
	srt := "1\n00:00:00,120 --> 00:00:03,480\n大家好\n欢迎回来\n\n2\n00:00:03,520 --> 00:00:08,960\n今天聊 #设计* 与_特殊字符_\n"
	cues, err := ParseSRT([]byte(srt))
	if err != nil {
		t.Fatalf("ParseSRT: %v", err)
	}
	if len(cues) != 2 {
		t.Fatalf("cue 数量 %d 期望 2", len(cues))
	}
	if cues[0].Text != "大家好 欢迎回来" {
		t.Errorf("多行合并错误: %q", cues[0].Text)
	}
	if cues[1].Index != 2 {
		t.Errorf("Index=%d", cues[1].Index)
	}

	md := renderMarkdown(cues)
	if !strings.Contains(md, "# 文字稿") || !strings.Contains(md, "#设计*") || !strings.Contains(md, "00:00:03,520") {
		t.Errorf("Markdown 渲染缺内容:\n%s", md)
	}
	txt := renderText(cues)
	if !strings.Contains(txt, "[00:00:00,120 → 00:00:03,480] 大家好 欢迎回来") {
		t.Errorf("TXT 渲染缺内容:\n%s", txt)
	}

	// CRLF + BOM 容忍
	srt2 := "\ufeff1\r\n00:00:01,000 --> 00:00:02,000\r\n测试\r\n"
	cues2, err := ParseSRT([]byte(srt2))
	if err != nil || len(cues2) != 1 {
		t.Fatalf("CRLF/BOM 解析失败: %v cues=%+v", err, cues2)
	}

	// 非法时间轴报错
	if _, err := ParseSRT([]byte("1\n坏行\n")); err == nil {
		t.Error("缺 --> 应报错")
	}
	// 未知格式报错
	if _, err := ExportTranscript("x.srt", "pdf"); err == nil {
		t.Error("未知格式应报错")
	}
}
