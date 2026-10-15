package service

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Status 任务状态（开发文档 v1.1 §3.2，v1.4 §18.2 增加 partial）。
type Status string

const (
	StatusPending    Status = "pending"
	StatusProcessing Status = "processing"
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
	StatusPartial    Status = "partial" // v1.4：批量部分成功终态（≥1 成功且 ≥1 失败）
)

// ItemStatus 子项状态（v1.4 §18.2）。
type ItemStatus string

const (
	ItemStatusPending    ItemStatus = "pending"
	ItemStatusProcessing ItemStatus = "processing"
	ItemStatusCompleted  ItemStatus = "completed"
	ItemStatusFailed     ItemStatus = "failed"
	ItemStatusCanceled   ItemStatus = "canceled"
)

// 任务类型（v1.4 §18.1）。
const (
	KindTrim  = "trim"  // 单文件裁剪（默认，兼容旧行为）
	KindBatch = "batch" // 批量裁剪
	KindMerge = "merge" // 合并
)

// 合并模式（v1.4 §18.4）。
const (
	MergeModeAuto    = "auto"
	MergeModeDemuxer = "demuxer"
	MergeModeFilter  = "filter"
)

// TrimItem 表示一个可执行单元（v1.4 §18.2）。
// trim=1 项，batch=N 项，merge=N 段；merge 的中间段为临时 ASCII 路径。
type TrimItem struct {
	InputPath  string     `json:"input_path"`
	OutputPath string     `json:"output_path"`
	Start      float64    `json:"start"`
	End        float64    `json:"end"`
	Strategy   string     `json:"strategy"`
	Status     ItemStatus `json:"status"`
	Progress   float64    `json:"progress"`
	Error      string     `json:"error"`
	HasRange   bool       `json:"has_range"` // merge 项是否带区间（false=整段）

	// Subtitle 非空 = 字幕烧录项（M12 §4.4）：runItem 分流到 ass 滤镜命令。
	Subtitle *SubtitleBurn `json:"subtitle,omitempty"`
}

// Task 表示一个裁剪/批量/合并任务（开发文档 v1.1 §3.1，v1.4 §18.2 扩展）。
// JSON tag 一律 snake_case，与前端 types.ts 严格一致（契约测试锁死）。
type Task struct {
	TaskID     string    `json:"task_id"`
	Kind       string    `json:"kind"` // trim | batch | merge
	InputPath  string    `json:"input_path"`
	OutputPath string    `json:"output_path"`
	Start      float64   `json:"start"`
	End        float64   `json:"end"`
	Strategy   string    `json:"strategy"`
	Status     Status    `json:"status"`
	Progress   float64   `json:"progress"`
	Duration   float64   `json:"duration"`
	Error      string    `json:"error"`
	Canceled   bool      `json:"canceled"`
	CreatedAt  time.Time `json:"created_at"`

	// v1.4 批量/合并扩展字段
	Items          []TrimItem `json:"items"` // trim=1 项，batch/merge=N 项
	CompletedCount int        `json:"completed_count"`
	FailedCount    int        `json:"failed_count"`
	MergeMode      string     `json:"merge_mode"` // merge 专用：auto/demuxer/filter
}

// deriveOutput 由输入路径派生默认输出路径：<name><suffix>.<ext>。
func deriveOutput(inputPath, suffix string) string {
	ext := filepath.Ext(inputPath)
	base := strings.TrimSuffix(inputPath, ext)
	return base + suffix + ext
}

// appendSeq 若 path 已存在，插入序号 _1/_2 ... 避免覆盖。
func appendSeq(path string) string {
	return appendSeqAvoiding(path, nil)
}

// appendSeqAvoiding 在 appendSeq 基础上额外避开 taken 集合中的路径
// （批量预校验时多个输出尚未落盘，仅靠文件存在性无法防重，v1.4 §18.3）。
func appendSeqAvoiding(path string, taken map[string]bool) string {
	isTaken := func(p string) bool {
		if taken != nil && taken[p] {
			return true
		}
		return fileExists(p)
	}
	if !isTaken(path) {
		return path
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	for i := 1; ; i++ {
		candidate := base + "_" + strconv.Itoa(i) + ext
		if !isTaken(candidate) {
			return candidate
		}
	}
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
