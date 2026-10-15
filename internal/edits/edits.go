// Package edits 实现 AI 粗剪的删词账本（edits.json）纯逻辑：
// 校验、统计摘要、保留段补集（开发文档 v1.7 §19.4）。
// 本包不依赖 FFmpeg/网络，全部为可单测纯函数。
package edits

import (
	"fmt"
	"math"
	"sort"
)

// LedgerVersion 当前账本格式版本。
const LedgerVersion = 1

// 删除项类型（§19.4）。
const (
	TypeFillerWord = "filler_word" // 口水词（嗯/呃/然后…）
	TypeSilence    = "silence"     // 静音段
	TypeRetake     = "retake"      // 重拍/口误重说
	TypeManual     = "manual"      // 人工标记
)

// validTypes 合法类型集合。
var validTypes = map[string]bool{
	TypeFillerWord: true,
	TypeSilence:    true,
	TypeRetake:     true,
	TypeManual:     true,
}

// EditItem 单个删除段。
type EditItem struct {
	Start  float64 `json:"start"`
	End    float64 `json:"end"`
	Type   string  `json:"type"`
	Text   string  `json:"text,omitempty"`
	Reason string  `json:"reason,omitempty"`
}

// EditsLedger 删词账本（edits.json 顶层结构）。
type EditsLedger struct {
	Version int        `json:"version"`
	Source  string     `json:"source"`
	Items   []EditItem `json:"items"`
}

// KeepRange 保留段（删除项的补集）。
type KeepRange struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// EditsSummary 账本统计摘要（§19.4）。
type EditsSummary struct {
	ItemCount      int            `json:"item_count"`       // 删除项数
	DeletedSeconds float64        `json:"deleted_seconds"`  // 删除总时长
	ByType         map[string]int `json:"by_type"`          // 按类型分组计数
	KeepCount      int            `json:"keep_count"`       // 保留段数
	KeepSeconds    float64        `json:"keep_seconds"`     // 保留总时长（=产物时长）
	SourceSeconds  float64        `json:"source_seconds"`   // 源时长
	Compression    float64        `json:"compression_pct"`  // 压缩比例（删除/源）
}

// durationTolerance 时长浮点容差（探测时长与手工填写存在小数误差）。
const durationTolerance = 0.05

// ValidateEdits 校验账本合法性。sourceDuration<=0 时跳过时长上界校验。
// 规则（§19.4）：version=1；source 非空；items≥1 且按 start 升序；
// 每项 start≥0、end>start、end≤源时长+容差、type 合法；同源区间不重叠。
// 错误信息逐项定位（第 N 项：违反的规则）。
func ValidateEdits(l *EditsLedger, sourceDuration float64) error {
	if l == nil {
		return fmt.Errorf("账本不能为空")
	}
	if l.Version != LedgerVersion {
		return fmt.Errorf("version 必须为 %d（当前 %d）", LedgerVersion, l.Version)
	}
	if l.Source == "" {
		return fmt.Errorf("source 不能为空")
	}
	if len(l.Items) == 0 {
		return fmt.Errorf("items 不能为空（至少 1 条删除项）")
	}

	for i, it := range l.Items {
		pos := fmt.Sprintf("第 %d 项", i+1)
		if !validTypes[it.Type] {
			return fmt.Errorf("%s：type %q 非法（合法值：filler_word/silence/retake/manual）", pos, it.Type)
		}
		if math.IsNaN(it.Start) || math.IsNaN(it.End) {
			return fmt.Errorf("%s：start/end 不能为 NaN", pos)
		}
		if it.Start < 0 {
			return fmt.Errorf("%s：start(%.3f) 必须为非负数", pos, it.Start)
		}
		if it.End <= it.Start {
			return fmt.Errorf("%s：end(%.3f) 必须大于 start(%.3f)", pos, it.End, it.Start)
		}
		if sourceDuration > 0 && it.End > sourceDuration+durationTolerance {
			return fmt.Errorf("%s：end(%.3f) 超过源时长(%.3f)", pos, it.End, sourceDuration)
		}
		// 升序 + 不重叠（与前一项比较；首项与前项 0 比较无意义，跳过）
		if i > 0 {
			prev := l.Items[i-1]
			if it.Start < prev.Start {
				return fmt.Errorf("%s：start(%.3f) 小于前一项 start(%.3f)，items 必须按 start 升序", pos, it.Start, prev.Start)
			}
			if it.Start < prev.End {
				return fmt.Errorf("%s：区间 [%.3f,%.3f) 与前一项 [%.3f,%.3f) 重叠", pos, it.Start, it.End, prev.Start, prev.End)
			}
		}
	}
	return nil
}

// sortedItems 返回按 start 升序的副本（不改动原账本）。
func sortedItems(l *EditsLedger) []EditItem {
	out := make([]EditItem, len(l.Items))
	copy(out, l.Items)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

// EditsToKeepRanges 计算保留段补集（§19.4）。
// 边界：贴头（首项 start>0 才产出 [0,start]）、贴尾（末项 end<duration 才产出 [end,duration]）、
// 相邻删除项（prev.End==next.Start）不产生碎片。调用前应先 ValidateEdits（未排序输入内部排序）。
func EditsToKeepRanges(l *EditsLedger, sourceDuration float64) []KeepRange {
	if l == nil || len(l.Items) == 0 {
		if sourceDuration > 0 {
			return []KeepRange{{Start: 0, End: sourceDuration}}
		}
		return nil
	}
	items := sortedItems(l)
	ranges := make([]KeepRange, 0, len(items)+1)
	prev := 0.0
	for _, it := range items {
		if it.Start > prev+durationTolerance {
			ranges = append(ranges, KeepRange{Start: prev, End: it.Start})
		}
		if it.End > prev {
			prev = it.End
		}
	}
	if sourceDuration > 0 && prev < sourceDuration-durationTolerance {
		ranges = append(ranges, KeepRange{Start: prev, End: sourceDuration})
	}
	return ranges
}

// Summarize 统计摘要（§19.4）。sourceDuration<=0 时保留段按删除项末端推导。
func Summarize(l *EditsLedger, sourceDuration float64) EditsSummary {
	sum := EditsSummary{ByType: map[string]int{}}
	if l == nil {
		return sum
	}
	for _, it := range l.Items {
		sum.ItemCount++
		sum.DeletedSeconds += it.End - it.Start
		sum.ByType[it.Type]++
	}
	dur := sourceDuration
	if dur <= 0 {
		// 未提供源时长：以最后删除项末端为近似终点（摘要用途）
		for _, it := range l.Items {
			if it.End > dur {
				dur = it.End
			}
		}
	}
	keeps := EditsToKeepRanges(l, dur)
	sum.KeepCount = len(keeps)
	for _, k := range keeps {
		sum.KeepSeconds += k.End - k.Start
	}
	sum.SourceSeconds = dur
	if dur > 0 {
		sum.Compression = round2(sum.DeletedSeconds / dur * 100)
	}
	return sum
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
