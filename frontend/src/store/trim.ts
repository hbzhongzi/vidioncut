import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as api from '../wails/bridge'
import type { ProbeResult, Task, ItemStatus, EditsView, EditItem, ASRStatus, CallEntry, SubtitleMode, SubtitleFormat, StylePreset, SubtitlesView, MotionPlanView } from '../types'

// 批量行（前端本地态；提交时映射为 BatchItem）
export interface BatchRow {
  input_path: string
  start: number
  end: number
  strategy: 'stream_copy' | 'reencode' | 'audio_only'
  duration: number // probe 源时长
  status: ItemStatus
  error: string
  output: string // item_done 事件回填的产物路径
  progress: number // v1.6：单项实时进度（item_percent 事件）
}

// 合并行（前端本地态；has_range=false 表示整段）
export interface MergeRow {
  input_path: string
  start: number
  end: number
  has_range: boolean
  strategy: 'stream_copy' | 'reencode' | 'audio_only'
  duration: number
  status: ItemStatus
}

// M11：AI 粗剪账本行（后端 edits.EditItem + 本地 enabled 开关）
export interface AIEditRow {
  start: number
  end: number
  type: 'filler_word' | 'silence' | 'retake' | 'manual'
  text?: string
  reason?: string
  enabled: boolean
}

export const useTrimStore = defineStore('trim', () => {
  // ===== 单文件模式 =====
  const inputPath = ref('')
  const probe = ref<ProbeResult | null>(null)
  const start = ref(0)
  const end = ref(0)
  const strategy = ref<'stream_copy' | 'reencode' | 'audio_only'>('stream_copy')

  const tasks = ref<Record<string, Task>>({})
  const currentTaskId = ref('')
  const progress = ref(0)
  const message = ref('')
  const lastOutput = ref('')
  const busy = ref(false)

  // ===== 批量模式（v1.4） =====
  const batchRows = ref<BatchRow[]>([])
  const batchTaskId = ref('')
  const batchBusy = ref(false)
  const batchProgress = ref(0)
  const batchMessage = ref('')
  const batchSummary = ref('')
  const lastPreviewPath = ref('') // 批量/合并面板内预览用

  // ===== 合并模式（v1.4） =====
  const mergeRows = ref<MergeRow[]>([])
  const mergeMode = ref<'auto' | 'demuxer' | 'filter'>('auto')
  const mergeOutput = ref('')
  const mergeTaskId = ref('')
  const mergeBusy = ref(false)
  const mergeProgress = ref(0)
  const mergeMessage = ref('')
  const mergeNotice = ref('')

  // ===== M11：AI 粗剪（账本审核 + 保留段剪切） =====
  // 本地态：每项带 enabled 开关（默认 true）；后端只认启用项。
  const aiSource = ref('') // 输入视频
  const aiWorkDir = ref('') // 输出根目录（work/ 下放产物）
  const aiView = ref<EditsView | null>(null) // 后端返回的账本视图（含 summary/keep_ranges）
  const aiRows = ref<AIEditRow[]>([]) // 本地可开关副本
  const aiStage = ref<'idle' | 'transcribing' | 'review' | 'cutting' | 'merging' | 'done'>('idle')
  const aiBusy = ref(false)
  const aiMessage = ref('')
  const aiProgress = ref(0)
  const aiTaskId = ref('') // 当前批量剪切任务
  const aiMergeTaskId = ref('') // 合并任务
  const aiLastOutput = ref('') // 合并后的成片

  // ===== M12：粗剪后字幕（app_v18.go） =====
  const subMode = ref<SubtitleMode>('sidecar')      // 外显/内嵌/双输出
  const subFormat = ref<SubtitleFormat>('both')    // 外显格式（配置默认 both）
  const subPreset = ref<StylePreset>('default')     // ASS 样式预设
  const subMaxChars = ref(20)                       // 单条最大字数（预换行）
  const subMaxLines = ref(2)                        // 最多行数
  const subView = ref<SubtitlesView | null>(null)   // 导出结果 + 字幕条列表
  const subBusy = ref(false)
  const subBurnTaskId = ref('')                     // 烧录任务（复用 trim 事件契约）
  const subBurnProgress = ref(0)
  const subBurnedOutput = ref('')                   // 烧录版成片路径
  const subPlan = ref<MotionPlanView | null>(null)  // 二次加工结果
  const subPlanBusy = ref(false)

  // ===== M11：Codex 面板 =====
  const asrStatus = ref<ASRStatus | null>(null)
  const callLog = ref<CallEntry[]>([])
  const hotwords = ref<string[]>([]) // 可编辑副本
  const codexMessage = ref('')

  // ===== 单文件动作 =====

  async function selectFile() {
    const p = await api.SelectFile()
    if (!p) return
    await loadSingleFile(p)
  }

  /** 载入单个文件（对话框选择 / 拖放共用） */
  async function loadSingleFile(p: string) {
    inputPath.value = p
    message.value = '探测中…'
    try {
      const r = await api.Probe(p)
      probe.value = r
      start.value = 0
      end.value = r.duration
      message.value = ''
    } catch (e) {
      probe.value = null
      message.value = '探测失败：' + String(e)
    }
  }

  async function submit() {
    if (!inputPath.value) {
      message.value = '请先选择文件'
      return
    }
    if (end.value <= start.value) {
      message.value = '结束时间必须大于开始时间'
      return
    }
    busy.value = true
    progress.value = 0
    lastOutput.value = ''
    try {
      const id = await api.Trim({
        input_path: inputPath.value,
        start: start.value,
        end: end.value,
        strategy: strategy.value,
      })
      currentTaskId.value = id
      message.value = '已提交，处理中…'
      // busy 保持 true：裁剪为异步，直到 done/error 事件再复位，避免处理中重复提交。
    } catch (e) {
      message.value = '提交失败：' + String(e)
      busy.value = false
    }
  }

  async function cancel() {
    if (!currentTaskId.value) return
    try {
      await api.Cancel(currentTaskId.value)
    } catch (e) {
      message.value = '取消失败：' + String(e)
    }
  }

  async function revealFolder() {
    if (!lastOutput.value) return
    try {
      await api.RevealInFolder(lastOutput.value)
    } catch (e) {
      message.value = '打开文件夹失败：' + String(e)
    }
  }

  // ===== 批量动作（v1.4） =====

  /** 探测并追加单个文件；返回是否成功（供拖放批量调用统计） */
  async function appendBatchFile(p: string): Promise<boolean> {
    if (batchBusy.value) return false
    try {
      const r = await api.Probe(p)
      batchRows.value.push({
        input_path: p,
        start: 0,
        end: r.duration,
        strategy: strategy.value,
        duration: r.duration,
        status: 'pending',
        error: '',
        output: '',
        progress: 0,
      })
      lastPreviewPath.value = p
      return true
    } catch {
      return false
    }
  }

  async function addBatchFile() {
    const p = await api.SelectFile()
    if (!p) return
    batchMessage.value = '探测中…'
    const ok = await appendBatchFile(p)
    batchMessage.value = ok ? '' : '探测失败：' + p
  }

  /** 拖放多文件批量追加（v1.6） */
  async function addBatchFiles(paths: string[]) {
    if (batchBusy.value || !paths.length) return
    let ok = 0
    for (let i = 0; i < paths.length; i++) {
      batchMessage.value = `正在探测 ${i + 1}/${paths.length}…`
      if (await appendBatchFile(paths[i])) ok++
    }
    batchMessage.value = ok
      ? `已添加 ${ok} 个文件${ok < paths.length ? `，${paths.length - ok} 个探测失败` : ''}`
      : '探测失败：无法读取拖入的文件'
  }

  function removeBatchRow(i: number) {
    if (batchBusy.value) return
    batchRows.value.splice(i, 1)
  }

  // 统一区间：把同一起止填充到所有行（超出单文件时长的行会被后端拒绝，UI 先行提示）
  function setUniformRange(s: number, e: number) {
    for (const row of batchRows.value) {
      row.start = s
      row.end = Math.min(e, row.duration)
    }
  }

  async function submitBatch() {
    if (batchBusy.value) return
    if (batchRows.value.length === 0) {
      batchMessage.value = '请先添加文件'
      return
    }
    batchBusy.value = true
    batchProgress.value = 0
    batchSummary.value = ''
    batchMessage.value = '已提交，处理中…'
    try {
      const id = await api.TrimBatch({
        items: batchRows.value.map((r) => ({
          input_path: r.input_path,
          start: r.start,
          end: r.end,
          strategy: r.strategy,
        })),
      })
      batchTaskId.value = id
      for (const r of batchRows.value) {
        r.status = 'pending'
        r.error = ''
        r.output = ''
        r.progress = 0
      }
      // busy 保持 true：直到 done/error 事件复位（v1.4 沿用 §17.2 修复）
    } catch (e) {
      batchMessage.value = '提交失败：' + String(e)
      batchBusy.value = false
    }
  }

  async function cancelBatch() {
    if (!batchTaskId.value) return
    try {
      await api.Cancel(batchTaskId.value)
    } catch (e) {
      batchMessage.value = '取消失败：' + String(e)
    }
  }

  function revealBatchItem(i: number) {
    const out = batchRows.value[i]?.output
    if (!out) return
    api.RevealInFolder(out).catch((e) => {
      batchMessage.value = '打开文件夹失败：' + String(e)
    })
  }

  // ===== 合并动作（v1.4） =====

  async function appendMergeFile(p: string): Promise<boolean> {
    if (mergeBusy.value) return false
    try {
      const r = await api.Probe(p)
      mergeRows.value.push({
        input_path: p,
        start: 0,
        end: r.duration,
        has_range: false,
        strategy: strategy.value,
        duration: r.duration,
        status: 'pending',
      })
      lastPreviewPath.value = p
      return true
    } catch {
      return false
    }
  }

  async function addMergeFile() {
    const p = await api.SelectFile()
    if (!p) return
    mergeMessage.value = '探测中…'
    const ok = await appendMergeFile(p)
    mergeMessage.value = ok ? '' : '探测失败：' + p
  }

  /** 拖放多片段追加（v1.6） */
  async function addMergeFiles(paths: string[]) {
    if (mergeBusy.value || !paths.length) return
    mergeMessage.value = '正在探测…'
    let ok = 0
    for (const p of paths) {
      if (await appendMergeFile(p)) ok++
    }
    mergeMessage.value = ok
      ? `已添加 ${ok} 个片段${ok < paths.length ? `，${paths.length - ok} 个探测失败` : ''}`
      : '探测失败：无法读取拖入的文件'
  }

  function removeMergeRow(i: number) {
    if (mergeBusy.value) return
    mergeRows.value.splice(i, 1)
  }

  /** 拖拽排序（v1.6）：from 移到 to 位置 */
  function moveMergeRow(from: number, to: number) {
    if (mergeBusy.value) return
    if (from === to || from < 0 || to < 0) return
    if (from >= mergeRows.value.length || to >= mergeRows.value.length) return
    const [row] = mergeRows.value.splice(from, 1)
    mergeRows.value.splice(to, 0, row)
  }

  function toggleMergeRange(i: number, hasRange: boolean) {
    const row = mergeRows.value[i]
    if (!row) return
    row.has_range = hasRange
    if (hasRange) {
      row.start = 0
      row.end = row.duration
    }
  }

  async function submitMerge() {
    if (mergeBusy.value) return
    if (mergeRows.value.length < 2) {
      mergeMessage.value = '合并至少需要 2 个片段'
      return
    }
    for (const r of mergeRows.value) {
      if (r.has_range && r.end <= r.start) {
        mergeMessage.value = '存在结束时间不大于开始时间的片段'
        return
      }
    }
    mergeBusy.value = true
    mergeProgress.value = 0
    mergeNotice.value = ''
    mergeMessage.value = '已提交，处理中…'
    try {
      const id = await api.Merge({
        items: mergeRows.value.map((r) => ({
          input_path: r.input_path,
          start: r.start,
          end: r.end,
          has_range: r.has_range,
          strategy: r.strategy,
        })),
        mode: mergeMode.value,
        output_path: mergeOutput.value || undefined,
      })
      mergeTaskId.value = id
      // busy 保持 true：直到 done/error 事件复位
    } catch (e) {
      mergeMessage.value = '提交失败：' + String(e)
      mergeBusy.value = false
    }
  }

  async function cancelMerge() {
    if (!mergeTaskId.value) return
    try {
      await api.Cancel(mergeTaskId.value)
    } catch (e) {
      mergeMessage.value = '取消失败：' + String(e)
    }
  }

  // ===== M11：AI 粗剪动作 =====

  /** 选择输入视频 */
  async function aiSelectSource() {
    const p = await api.SelectFile()
    if (!p) return
    aiSource.value = p
    aiMessage.value = ''
  }

  /** 选择输出根目录 */
  async function aiSelectWorkDir() {
    const d = await api.SelectOutputDir()
    if (!d) return
    aiWorkDir.value = d
  }

  /** 转录 + 机械扫描候选 + 生成 edits.json（后端编排，§19.5） */
  async function aiGenerate() {
    if (!aiSource.value) {
      aiMessage.value = '请先选择输入视频'
      return
    }
    if (!aiWorkDir.value) {
      aiMessage.value = '请先选择输出目录'
      return
    }
    aiStage.value = 'transcribing'
    aiBusy.value = true
    aiProgress.value = 0
    aiMessage.value = '正在转录并扫描候选（首次模型加载约 10–60s）…'
    try {
      const view = await api.AICutGenerate(aiSource.value, aiWorkDir.value)
      aiView.value = view
      aiRows.value = view.ledger.items.map((it) => ({ ...it, enabled: true }))
      aiStage.value = 'review'
      aiMessage.value = view.valid
        ? `账本校验通过：候选 ${aiRows.value.length} 项`
        : '账本校验未通过：' + view.validation_error
    } catch (e) {
      aiStage.value = 'idle'
      aiMessage.value = '生成失败：' + String(e)
    } finally {
      aiBusy.value = false
    }
  }

  /** 载入已有 edits.json（对话/手动产出） */
  async function aiLoadLedger() {
    const p = await api.LoadEditsFile()
    if (!p) return
    aiMessage.value = '读取中…'
    try {
      const view = await api.ReadEditsLedger(p)
      aiView.value = view
      aiRows.value = view.ledger.items.map((it) => ({ ...it, enabled: true }))
      if (view.ledger.source) aiSource.value = view.ledger.source
      aiStage.value = 'review'
      aiMessage.value = view.valid ? '账本已载入' : '账本校验未通过：' + view.validation_error
    } catch (e) {
      aiMessage.value = '读取失败：' + String(e)
    }
  }

  function aiToggleItem(i: number) {
    const row = aiRows.value[i]
    if (row) row.enabled = !row.enabled
    aiRecompute()
  }
  function aiSetAll(on: boolean) {
    for (const r of aiRows.value) r.enabled = on
    aiRecompute()
  }
  function aiOnlyFillers() {
    for (const r of aiRows.value) r.enabled = r.type === 'filler_word'
    aiRecompute()
  }

  /** 把启用项回传后端重算摘要/保留段（§19.8：逻辑全在 Go 侧） */
  let aiRecomputeTimer: ReturnType<typeof setTimeout> | null = null
  function aiRecompute() {
    if (!aiSource.value || aiStage.value === 'transcribing') return
    const enabled = aiRows.value.filter((r) => r.enabled).map(({ enabled, ...it }) => it)
    if (aiRecomputeTimer) clearTimeout(aiRecomputeTimer)
    aiRecomputeTimer = setTimeout(() => {
      api.AICutCompute(aiSource.value, enabled as EditItem[])
        .then((v) => {
          aiView.value = v
        })
        .catch((e) => {
          aiMessage.value = '重算失败：' + String(e)
        })
    }, 120)
  }

  /** 确认剪切：启用项 → 保留段 → TrimBatch 任务 */
  async function aiConfirmCut() {
    if (!aiSource.value) return
    const enabled = aiRows.value.filter((r) => r.enabled).map(({ enabled, ...it }) => it)
    if (!enabled.length) {
      aiMessage.value = '没有启用的删除项'
      return
    }
    aiStage.value = 'cutting'
    aiBusy.value = true
    aiProgress.value = 0
    aiMessage.value = '已提交保留段批量剪切…'
    try {
      const id = await api.AICutSubmit(aiSource.value, enabled as EditItem[], 'reencode')
      aiTaskId.value = id
    } catch (e) {
      aiStage.value = 'review'
      aiBusy.value = false
      aiMessage.value = '提交失败：' + String(e)
    }
  }

  /** 合并剪切片段为单成片 */
  async function aiMerge() {
    if (!aiTaskId.value) return
    aiStage.value = 'merging'
    aiBusy.value = true
    aiProgress.value = 0
    aiMessage.value = '合并片段中…'
    try {
      const id = await api.MergeCutSegments(aiTaskId.value, '')
      aiMergeTaskId.value = id
    } catch (e) {
      aiStage.value = 'review'
      aiBusy.value = false
      aiMessage.value = '合并提交失败：' + String(e)
    }
  }

  /** 导出文字稿（transcript.srt → md/txt） */
  async function aiExport(format: 'md' | 'txt') {
    if (!aiWorkDir.value) {
      aiMessage.value = '请先选择输出目录'
      return
    }
    try {
      const out = await api.ExportTranscriptFile(aiWorkDir.value, format)
      aiMessage.value = '已导出：' + out
    } catch (e) {
      aiMessage.value = '导出失败：' + String(e)
    }
  }

  // ===== M12：字幕动作 =====

  function subSetMode(m: SubtitleMode) {
    subMode.value = m
  }
  function subSetPreset(p: StylePreset) {
    subPreset.value = p
  }

  /** 读取后端字幕默认值；GUI 仍允许本次会话内覆盖模式和样式。 */
  async function loadSubtitleConfig() {
    try {
      const cfg = await api.ConfigGet()
      const mode = cfg.subtitles_mode
      const format = cfg.subtitles_format
      const preset = cfg.subtitles_style
      if (mode === 'sidecar' || mode === 'burn' || mode === 'both') subMode.value = mode
      if (format === 'srt' || format === 'ass' || format === 'both') subFormat.value = format
      if (preset === 'default' || preset === 'talking-head' || preset === 'minimal') subPreset.value = preset
      const maxChars = Number(cfg.subtitles_max_chars)
      const maxLines = Number(cfg.subtitles_max_lines)
      if (Number.isFinite(maxChars) && maxChars > 0) subMaxChars.value = maxChars
      if (Number.isFinite(maxLines) && maxLines > 0) subMaxLines.value = maxLines
    } catch {
      // 配置读取失败时保留前端默认值，不能阻断 AI 粗剪面板。
    }
  }

  /** 导出字幕（sidecar 秒出文件；burn/both 导出后自动提交烧录任务） */
  async function subExport() {
    if (!aiWorkDir.value) {
      aiMessage.value = '请先选择输出目录'
      return
    }
    if (!aiLastOutput.value) {
      aiMessage.value = '请先生成成片（合并为单成片）'
      return
    }
    subBusy.value = true
    subBurnedOutput.value = ''
    aiMessage.value = '正在重对齐字幕（源轴 → 成片轴）…'
    try {
      // burn/both 必须同时生成 SRT + ASS：ASS 用于烧录，SRT 是外挂交付物。
      // sidecar 才遵守 subtitles.format 的 srt/ass/both 配置。
      const exportFormat = subMode.value === 'sidecar' ? subFormat.value : 'both'
      const view = await api.AICutExportSubtitles(
        aiWorkDir.value, aiLastOutput.value,
        exportFormat,
        subPreset.value, subMaxChars.value, subMaxLines.value,
      )
      subView.value = view
      if (view.cue_count === 0) {
        aiMessage.value = '重对齐后无字幕条（成片可能被全部删除）'
        subBusy.value = false
        return
      }
      aiMessage.value = `字幕已导出：${view.cue_count} 条（跨段断开 ${view.split_count} 处）`
      // 内嵌/双出：导出后自动提交烧录
      if (subMode.value !== 'sidecar') {
        await subBurn()
      } else {
        subBusy.value = false
      }
    } catch (e) {
      subBusy.value = false
      aiMessage.value = '字幕导出失败：' + String(e)
    }
  }

  /** 提交内嵌烧录任务（复用 trim:progress / trim:done 事件契约） */
  async function subBurn() {
    if (!aiLastOutput.value || !subView.value?.ass_path) {
      subBusy.value = false
      aiMessage.value = '缺少成片或 ASS 字幕'
      return
    }
    aiMessage.value = '已提交字幕烧录（重编码，音轨直复制）…'
    try {
      const id = await api.AICutBurnSubtitles(aiLastOutput.value, subView.value.ass_path, '', 0, '')
      subBurnTaskId.value = id
      subBurnProgress.value = 0
    } catch (e) {
      subBusy.value = false
      aiMessage.value = '烧录提交失败：' + String(e)
    }
  }

  /** 用特效版（edited_fx.ass）重新烧录 */
  async function subBurnFx() {
    if (!aiLastOutput.value || !subPlan.value?.fx_path) return
    subBusy.value = true
    aiMessage.value = '已提交特效版烧录…'
    try {
      const id = await api.AICutBurnSubtitles(aiLastOutput.value, subPlan.value.fx_path, '', 0, '')
      subBurnTaskId.value = id
      subBurnProgress.value = 0
    } catch (e) {
      subBusy.value = false
      aiMessage.value = '特效版烧录提交失败：' + String(e)
    }
  }

  /** 二次加工：规则引擎自动选特效（§5.7） */
  async function subAnimate(mode = '') {
    if (!aiWorkDir.value) {
      aiMessage.value = '请先选择输出目录'
      return
    }
    subPlanBusy.value = true
    try {
      const plan = await api.AICutAnimateCaptions(aiWorkDir.value, aiLastOutput.value, mode, [], 0)
      subPlan.value = plan
      aiMessage.value = plan.static
        ? '无特征命中：纯静态字幕（合法态）'
        : `特效已应用：${plan.selected.join(' + ')}（理由见 motion-plan.json）`
    } catch (e) {
      aiMessage.value = '特效分析失败：' + String(e)
    } finally {
      subPlanBusy.value = false
    }
  }

  /** 取消特效（全部禁用）：以 mode=off 重跑 */
  async function subDisableAnimate() {
    await subAnimate('off')
  }

  // ===== M11：Codex 面板动作 =====

  async function refreshASRStatus() {
    try {
      const st = await api.ASRStatus()
      asrStatus.value = st
      // Older configs/backends may serialize an unset slice as null.
      hotwords.value = Array.isArray(st.hotwords) ? [...st.hotwords] : []
    } catch (e) {
      codexMessage.value = 'ASR 状态读取失败：' + String(e)
    }
  }
  async function refreshCallLog() {
    try {
      const entries = await api.ReadMCPCallLog(200)
      // Older backends may serialize an empty Go slice as null.
      callLog.value = Array.isArray(entries) ? entries : []
    } catch {
      /* 静默：无记录时返回空 */
    }
  }
  function addHotword(w: string) {
    const t = w.trim()
    if (t && !hotwords.value.includes(t)) hotwords.value.push(t)
  }
  function removeHotword(i: number) {
    hotwords.value.splice(i, 1)
  }
  async function applyHotwords() {
    try {
      await api.SetASRHotwords(hotwords.value)
      codexMessage.value = '热词已下发 sidecar'
    } catch (e) {
      codexMessage.value = '热词下发失败：' + String(e)
    }
  }

  // ===== 事件路由（v1.4：按 task_id + kind 分流到三种模式；v1.6 增单项进度；M11 增 AI 粗剪） =====

  function setupEvents() {
    api.OnProgress((p) => {
      if (p.task_id === currentTaskId.value) {
        progress.value = p.percent
      } else if (p.task_id === batchTaskId.value) {
        batchProgress.value = p.percent
        // 单项实时进度（v1.6）：带 item_index 时同步到对应行
        if (p.item_index !== undefined) {
          const row = batchRows.value[p.item_index]
          if (row) {
            row.progress = p.item_percent ?? row.progress
            if (row.status === 'pending') row.status = 'processing'
          }
        }
      } else if (p.task_id === mergeTaskId.value) {
        mergeProgress.value = p.percent
      } else if (p.task_id === aiTaskId.value) {
        aiProgress.value = p.percent
      } else if (p.task_id === aiMergeTaskId.value) {
        aiProgress.value = p.percent
      } else if (p.task_id === subBurnTaskId.value) {
        subBurnProgress.value = p.percent
      }
    })
    api.OnItemDone((p) => {
      if (p.task_id !== batchTaskId.value) return
      const row = batchRows.value[p.item_index]
      if (row) {
        row.status = 'completed'
        row.progress = 100
        row.output = p.output_path
      }
    })
    api.OnDone((p) => {
      if (!tasks.value[p.task_id]) tasks.value[p.task_id] = {} as Task
      const t = tasks.value[p.task_id]
      t.status = p.kind === 'batch' && (p.failed ?? 0) > 0 ? 'partial' : 'completed'
      t.output_path = p.output_path ?? ''
      t.duration = p.duration
      if (p.task_id === currentTaskId.value) {
        progress.value = 100
        busy.value = false
        lastOutput.value = p.output_path ?? ''
        message.value = '完成：' + (p.output_path ?? '')
      } else if (p.task_id === batchTaskId.value) {
        batchProgress.value = 100
        batchBusy.value = false
        batchSummary.value = `完成 ${p.completed ?? 0} 项，失败 ${p.failed ?? 0} 项`
        batchMessage.value = (p.failed ?? 0) > 0 ? '批量裁剪结束（部分失败）' : '批量裁剪结束'
      } else if (p.task_id === mergeTaskId.value) {
        mergeProgress.value = 100
        mergeBusy.value = false
        lastOutput.value = p.output_path ?? ''
        mergeMessage.value = '合并完成：' + (p.output_path ?? '')
      } else if (p.task_id === aiTaskId.value) {
        aiProgress.value = 100
        aiBusy.value = false
        aiStage.value = 'review'
        aiMessage.value = '保留段剪切完成，可合并为单成片'
      } else if (p.task_id === aiMergeTaskId.value) {
        aiProgress.value = 100
        aiBusy.value = false
        aiStage.value = 'done'
        aiLastOutput.value = p.output_path ?? ''
        aiMessage.value = '成片生成：' + (p.output_path ?? '')
      } else if (p.task_id === subBurnTaskId.value) {
        subBurnProgress.value = 100
        subBusy.value = false
        subBurnedOutput.value = p.output_path ?? ''
        aiMessage.value = '烧录完成：' + (p.output_path ?? '')
      }
    })
    api.OnError((p) => {
      // 单项失败（批量继续）：带 item_index
      if (p.item_index !== undefined && p.task_id === batchTaskId.value) {
        const row = batchRows.value[p.item_index]
        if (row) {
          row.status = 'failed'
          row.error = p.message
        }
        return
      }
      if (!tasks.value[p.task_id]) tasks.value[p.task_id] = {} as Task
      tasks.value[p.task_id].status = 'failed'
      tasks.value[p.task_id].error = p.message
      if (p.task_id === currentTaskId.value) {
        busy.value = false
        message.value = '错误：' + p.message
      } else if (p.task_id === batchTaskId.value) {
        batchBusy.value = false
        batchMessage.value = '错误：' + p.message
      } else if (p.task_id === mergeTaskId.value) {
        mergeBusy.value = false
        mergeMessage.value = '错误：' + p.message
      } else if (p.task_id === aiTaskId.value || p.task_id === aiMergeTaskId.value) {
        aiBusy.value = false
        aiStage.value = 'review'
        aiMessage.value = '错误：' + p.message
      } else if (p.task_id === subBurnTaskId.value) {
        subBusy.value = false
        aiMessage.value = '烧录失败：' + p.message
      }
    })
    api.OnNotice((p) => {
      if (p.task_id === mergeTaskId.value) mergeNotice.value = p.message
    })
  }

  return {
    // 单文件
    inputPath, probe, start, end, strategy,
    tasks, currentTaskId, progress, message, lastOutput, busy,
    selectFile, loadSingleFile, submit, cancel, revealFolder,
    // 批量
    batchRows, batchTaskId, batchBusy, batchProgress, batchMessage, batchSummary, lastPreviewPath,
    addBatchFile, addBatchFiles, removeBatchRow, setUniformRange, submitBatch, cancelBatch, revealBatchItem,
    // 合并
    mergeRows, mergeMode, mergeOutput, mergeTaskId, mergeBusy, mergeProgress, mergeMessage, mergeNotice,
    addMergeFile, addMergeFiles, removeMergeRow, moveMergeRow, toggleMergeRange, submitMerge, cancelMerge,
    // M11：AI 粗剪
    aiSource, aiWorkDir, aiView, aiRows, aiStage, aiBusy, aiMessage, aiProgress, aiTaskId, aiMergeTaskId, aiLastOutput,
    aiSelectSource, aiSelectWorkDir, aiGenerate, aiLoadLedger, aiToggleItem, aiSetAll, aiOnlyFillers, aiRecompute,
    aiConfirmCut, aiMerge, aiExport,
    // M12：字幕
    subMode, subFormat, subPreset, subMaxChars, subMaxLines, subView, subBusy, subBurnTaskId, subBurnProgress, subBurnedOutput,
    subPlan, subPlanBusy,
    subSetMode, subSetPreset, loadSubtitleConfig, subExport, subBurn, subBurnFx, subAnimate, subDisableAnimate,
    // M11：Codex 面板
    asrStatus, callLog, hotwords, codexMessage,
    refreshASRStatus, refreshCallLog, addHotword, removeHotword, applyHotwords,
    // 事件
    setupEvents,
  }
})
