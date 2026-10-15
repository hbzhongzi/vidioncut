<script setup lang="ts">
import { computed, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { useTrimStore } from '../store/trim'
import Preview from './Preview.vue'
import MsgLine from './MsgLine.vue'
import { fmtShort } from '../utils/time'

const store = useTrimStore()
const { batchRows, batchBusy, batchProgress, batchMessage, batchSummary, lastPreviewPath } =
  storeToRefs(store)

// 统一区间（本地编辑态）
const showUniform = ref(false)
const uniformMsg = ref('')
const uStart = ref('0')
const uEnd = ref('')

function applyUniform() {
  const s = parseFloat(uStart.value)
  const e = parseFloat(uEnd.value)
  if (!isFinite(s) || !isFinite(e) || e <= s) {
    uniformMsg.value = '统一区间不合法（需满足 结束 > 开始）'
    return
  }
  store.setUniformRange(s, e)
  uniformMsg.value = `已应用到全部 ${batchRows.value.length} 行`
}

const doneCount = computed(() => batchRows.value.filter((r) => r.status === 'completed').length)
const failCount = computed(() => batchRows.value.filter((r) => r.status === 'failed').length)

function basename(p: string): string {
  return p.split(/[\\/]/).pop() || p
}
function dirname(p: string): string {
  const i = Math.max(p.lastIndexOf('\\'), p.lastIndexOf('/'))
  return i > 0 ? p.slice(0, i) : ''
}

const statusText: Record<string, string> = {
  pending: '等待',
  processing: '处理中',
  completed: '完成',
  failed: '失败',
  canceled: '已取消',
}
function statusClass(s: string): string {
  if (s === 'completed') return 'ok'
  if (s === 'failed') return 'err'
  if (s === 'processing') return 'run'
  return 'wait'
}
</script>

<template>
  <div class="batch-panel">
    <div class="toolbar">
      <button class="btn btn-primary" :disabled="batchBusy" @click="store.addBatchFile">
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg>
        添加文件
      </button>
      <span class="hint">支持一次拖入多个文件 · 逐项执行，共享并发上限</span>
      <span class="sp"></span>
      <button class="btn btn-ghost" :disabled="batchBusy || !batchRows.length" @click="showUniform = !showUniform">
        统一区间
      </button>
    </div>

    <div class="dropzone">
      将多个视频文件 <b>拖入窗口</b> 即可批量添加，或点击「添加文件」逐个选择
    </div>

    <div class="table">
      <div class="thead">
        <span>#</span>
        <span>文件</span>
        <span>裁剪区间</span>
        <span>策略</span>
        <span>状态</span>
        <span>操作</span>
      </div>
      <div v-for="(row, i) in batchRows" :key="row.input_path" class="trow">
        <span class="idx">{{ i + 1 }}</span>
        <div class="cell-file">
          <div class="nm" :title="row.input_path">{{ basename(row.input_path) }}</div>
          <div class="sub">{{ dirname(row.input_path) }} · 源时长 {{ fmtShort(row.duration) }}</div>
        </div>
        <div class="cell-range">
          <div class="mini">
            <i :style="{ left: (row.start / row.duration) * 100 + '%', right: 100 - (row.end / row.duration) * 100 + '%' }"></i>
          </div>
          <span class="rng">{{ row.start.toFixed(1) }}–{{ row.end.toFixed(1) }}s</span>
        </div>
        <div>
          <select v-model="row.strategy" class="sel" :disabled="batchBusy">
            <option value="stream_copy">流复制</option>
            <option value="reencode">重编码</option>
            <option value="audio_only">仅音频</option>
          </select>
        </div>
        <div>
          <span class="tag" :class="statusClass(row.status)" :title="row.error || ''">
            {{ row.status === 'processing' ? `${Math.round(row.progress)}%` : statusText[row.status] }}
          </span>
        </div>
        <div class="row-ops">
          <button :disabled="!row.output" @click="store.revealBatchItem(i)">定位</button>
          <button class="del" :disabled="batchBusy" @click="store.removeBatchRow(i)">移除</button>
        </div>
      </div>
      <div v-if="!batchRows.length" class="trow empty">尚未添加文件</div>
    </div>

    <div v-if="showUniform" class="uniform">
      <span>统一区间</span>
      <label class="field narrow"><input v-model="uStart" placeholder="开始(秒)" /></label>
      <span class="to">至</span>
      <label class="field narrow"><input v-model="uEnd" placeholder="结束(秒)" /></label>
      <button class="btn btn-ghost sm" :disabled="batchBusy" @click="applyUniform">应用到全部</button>
      <span class="hint">超出单文件时长的行会被后端拒绝</span>
    </div>

    <div class="sum-bar">
      <span class="txt">
        <b>{{ doneCount }}/{{ batchRows.length }}</b> 项已完成
        <template v-if="failCount"> · <b class="err">{{ failCount }}</b> 项失败</template>
        · 整体进度 <b>{{ Math.round(batchProgress) }}%</b>
      </span>
      <div class="pbar"><i :style="{ width: batchProgress + '%' }"></i></div>
      <button v-if="!batchBusy" class="btn btn-primary" :disabled="!batchRows.length" @click="store.submitBatch">
        开始批量裁剪
      </button>
      <template v-else>
        <button class="btn btn-primary" disabled>处理中…</button>
        <button class="btn btn-danger" @click="store.cancelBatch">取消</button>
      </template>
    </div>

    <MsgLine :text="batchMessage" />
    <MsgLine v-if="uniformMsg" :text="uniformMsg" />
    <div v-if="batchSummary" class="summary">{{ batchSummary }}</div>

    <Preview v-if="lastPreviewPath" :path="lastPreviewPath" class="mini-preview" />
  </div>
</template>

<style scoped>
.batch-panel {
  max-width: 1180px;
  margin: 0 auto;
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
}
.toolbar .sp {
  flex: 1;
}
.hint {
  font-size: 12px;
  color: var(--text3);
}
.dropzone {
  border: 1.5px dashed var(--border);
  border-radius: 11px;
  padding: 16px;
  text-align: center;
  font-size: 12.5px;
  color: var(--text3);
}
.dropzone b {
  color: var(--text2);
}

.table {
  border: 1px solid var(--border);
  border-radius: 11px;
  overflow: hidden;
  background: var(--panel);
}
.thead,
.trow {
  display: grid;
  align-items: center;
  gap: 10px;
  padding: 10px 13px;
  grid-template-columns: 34px minmax(0, 1.7fr) minmax(0, 1.5fr) 108px 76px 104px;
}
.thead {
  background: var(--panel2);
  font-size: 11.5px;
  color: var(--text3);
  border-bottom: 1px solid var(--border);
}
.trow {
  border-bottom: 1px solid var(--border);
  font-size: 12.5px;
}
.trow:last-child {
  border-bottom: 0;
}
.trow:hover:not(.empty) {
  background: var(--panel2);
}
.trow.empty {
  color: var(--text3);
  font-size: 12px;
  justify-content: center;
  display: block;
  text-align: center;
  padding: 22px;
}
.idx {
  color: var(--text3);
  font-variant-numeric: tabular-nums;
}
.cell-file {
  min-width: 0;
}
.cell-file .nm {
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.cell-file .sub {
  font-size: 10.5px;
  color: var(--text3);
  margin-top: 2px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.cell-range {
  display: flex;
  align-items: center;
  gap: 9px;
  min-width: 0;
}
.mini {
  position: relative;
  height: 6px;
  flex: 1;
  min-width: 36px;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 4px;
}
.mini > i {
  position: absolute;
  top: 0;
  bottom: 0;
  background: var(--accent);
  border-radius: 3px;
}
.cell-range .rng {
  font-size: 11px;
  color: var(--text2);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
.row-ops {
  display: flex;
  gap: 4px;
}
.row-ops button {
  background: none;
  border: 0;
  color: var(--text3);
  font-size: 12px;
  cursor: pointer;
  padding: 3px 6px;
  border-radius: 6px;
  font-family: inherit;
}
.row-ops button:hover:not(:disabled) {
  color: var(--accent);
  background: var(--accent-soft);
}
.row-ops button:disabled {
  opacity: 0.35;
  cursor: not-allowed;
}
.row-ops button.del:hover:not(:disabled) {
  color: var(--danger);
  background: rgba(242, 96, 106, 0.1);
}

.uniform {
  display: flex;
  align-items: center;
  gap: 10px;
  background: var(--panel2);
  border: 1px solid var(--border);
  border-radius: 11px;
  padding: 11px 14px;
  font-size: 12px;
  color: var(--text2);
}
.uniform .to {
  color: var(--text3);
  font-size: 11px;
}
.field.narrow {
  width: 110px;
  flex: none;
  padding: 5px 9px;
}
.field.narrow input {
  font-size: 12px;
}

.sum-bar {
  display: flex;
  align-items: center;
  gap: 14px;
  background: var(--panel2);
  border: 1px solid var(--border);
  border-radius: 11px;
  padding: 13px 16px;
}
.sum-bar .txt {
  font-size: 12px;
  color: var(--text2);
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
}
.sum-bar .txt b {
  color: var(--text);
}
.sum-bar .txt b.err {
  color: var(--danger);
}
.summary {
  font-size: 12px;
  color: var(--success);
}
.mini-preview {
  max-width: 520px;
}
</style>
