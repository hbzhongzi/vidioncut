// 与 Go 侧 JSON 契约保持一致（开发文档 v1.1 §3.1 / §8.1；v1.4 §18.2 批量/合并扩展）

export type TaskStatus = 'pending' | 'processing' | 'completed' | 'partial' | 'failed'
export type ItemStatus = 'pending' | 'processing' | 'completed' | 'failed' | 'canceled'
export type TaskKind = 'trim' | 'batch' | 'merge'

export interface TrimRequest {
  input_path: string
  start: number
  end: number
  strategy: string
  output_path?: string
  overwrite?: boolean
}

// v1.4：可执行单元（trim=1 项，batch=N 项，merge=N 段）
export interface TrimItem {
  input_path: string
  output_path: string
  start: number
  end: number
  strategy: string
  status: ItemStatus
  progress: number
  error: string
  has_range: boolean
}

export interface Task {
  task_id: string
  kind: TaskKind
  input_path: string
  output_path: string
  start: number
  end: number
  strategy: string
  status: TaskStatus
  progress: number
  duration: number
  error: string
  canceled: boolean
  created_at: string
  // v1.4 批量/合并扩展
  items?: TrimItem[]
  completed_count: number
  failed_count: number
  merge_mode: string
}

export interface ProbeResult {
  duration: number
  format_name: string
  has_video: boolean
  has_audio: boolean
  width: number
  height: number
  streams: Array<{
    index: number
    codec_type: string
    codec_name: string
    width?: number
    height?: number
  }>
}

// v1.4 §18.6：批量裁剪
export interface BatchItem {
  input_path: string
  start: number
  end: number
  strategy: string
  output_path?: string
}

export interface BatchRequest {
  items: BatchItem[]
}

// v1.4 §18.4：合并
export interface MergeItem {
  input_path: string
  start: number
  end: number
  has_range: boolean
  strategy: string
}

export interface MergeRequest {
  items: MergeItem[]
  mode: 'auto' | 'demuxer' | 'filter'
  output_path?: string
  overwrite?: boolean
}

// ===== M11：AI 粗剪 / Codex 面板（对应 app_v17.go 绑定） =====

// 删除项类型（与后端 edits.Type* 一致）
export type EditType = 'filler_word' | 'silence' | 'retake' | 'manual'

export interface EditItem {
  start: number
  end: number
  type: EditType
  text?: string
  reason?: string
}

export interface EditsLedger {
  version: number
  source: string
  items: EditItem[]
}

export interface EditsSummary {
  item_count: number
  deleted_seconds: number
  by_type: Record<string, number>
  keep_count: number
  keep_seconds: number
  source_seconds: number
  compression_pct: number
}

export interface KeepRange {
  start: number
  end: number
}

export interface EditsView {
  path: string
  ledger: EditsLedger
  valid: boolean
  validation_error: string
  summary: EditsSummary | null
  keep_ranges: KeepRange[]
  source_duration: number
}

// ASR sidecar 状态（对应 ASRStatusInfo）
export interface ASRStatus {
  enabled: boolean
  host: string
  port: number
  model: string
  compute_type: string
  hf_home: string
  idle_timeout_min: number
  hotwords: string[]
  python: string
  healthy: boolean
  runtime_missing: boolean
  hint: string
}

// MCP 工具调用记录（对应 mcpserver.CallEntry）
export interface CallEntry {
  ts: string
  tool: string
  ok: boolean
  ms: number
  err: string
  detail: string
}

// ===== M12：粗剪后字幕（对应 app_v18.go 绑定 / 字幕功能更新文档 §3.2） =====

export type SubtitleMode = 'sidecar' | 'burn' | 'both'
export type SubtitleFormat = 'srt' | 'ass' | 'both'
export type StylePreset = 'default' | 'talking-head' | 'minimal'

// 一条字幕（成片时间轴；split_at = 跨保留段边界词级断开）
export interface Cue {
  start: number
  end: number
  text: string
  split_at?: boolean
}

// 外显导出结果（SubtitlesView）
export interface SubtitlesView {
  srt_path: string
  ass_path: string
  cue_count: number
  split_count: number
  final_duration: number
  output_dir: string
  cues: Cue[]
}

// 五维内容特征（motion-plan.json meta.analysis——GUI 单一数据源，纯展示）
export interface CaptionAnalysis {
  keyword_density: number
  content_type: string
  avg_cue_duration: number
  avg_chars_per_cue: number
  emphatic_ratio: number
  top_keywords: string[]
}

export interface RejectedSkill {
  skill: string
  reason: string
}

// 二次加工结果（MotionPlanView）
export interface MotionPlanView {
  fx_path: string
  plan_path: string
  selected: string[]
  reasoning: string
  rejected: RejectedSkill[]
  analysis: CaptionAnalysis | null
  static: boolean
  style_id: string
  orientation: string
}
