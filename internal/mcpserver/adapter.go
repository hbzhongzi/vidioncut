package mcpserver

import (
	"videocut/internal/edits"
	"videocut/internal/service"
)

// keepRangesToBatchItems 保留段 → 批量裁剪子项（§19.4 映射）。
// 删词必须精确切分，strategy 强制 reencode（stream_copy 的关键帧吸附不适用）。
// outputPath 为空时由 service 按命名规则派生（同源多段自动追加序号防覆盖）。
func keepRangesToBatchItems(source string, keeps []edits.KeepRange, strategy string) []service.BatchItem {
	if strategy == "" {
		strategy = "reencode"
	}
	items := make([]service.BatchItem, 0, len(keeps))
	for _, k := range keeps {
		items = append(items, service.BatchItem{
			InputPath: source,
			Start:     k.Start,
			End:       k.End,
			Strategy:  strategy,
		})
	}
	return items
}
