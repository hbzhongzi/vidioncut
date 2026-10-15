<script setup lang="ts">
import { computed, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { useTrimStore } from '../store/trim'
import Preview from './Preview.vue'
import MsgLine from './MsgLine.vue'
import { fmtShort } from '../utils/time'

const store = useTrimStore()
const { mergeRows, mergeMode, mergeOutput, mergeBusy, mergeProgress, mergeMessage, mergeNotice, lastPreviewPath, lastOutput } =
  storeToRefs(store)

const modeHint = computed(() => {
  switch (mergeMode.value) {
    case 'demuxer':
      return '流复制：速度快、无损，要求各片段编码参数一致'
    case 'filter':
      return '重编码：兼容不同分辨率/编码，速度较慢'
    default:
      return '自动：参数一致用流复制，否则自动切重编码'
  }
})

// 统一区间：为所有片段开启区间并填充（超出单段时长按后端规则截断提示）
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
  for (const r of mergeRows.value) {
    r.has_range = true
    r.start = s
    r.end = Math.min(e, r.duration)
  }
  uniformMsg.value = `已应用到全部 ${mergeRows.value.length} 个片段`
}

const totalDur = computed(() =>
  mergeRows.value.reduce((sum, r) => sum + (r.has_range ? r.end - r.start : r.duration), 0),
)

// 拖拽排序
const dragFrom = ref(-1)
const dragOver = ref(-1)

function basename(p: string): string {
  return p.split(/[\\/]/).pop() || p
}
function dirname(p: string): string {
  const i = Math.max(p.lastIndexOf('\\'), p.lastIndexOf('/'))
  return i > 0 ? p.slice(0, i) : ''
}
</script>

<template>
  <div class="merge-panel">
    <div class="toolbar">
      <button class="btn btn-primary" :disabled="mergeBusy" @click="store.addMergeFile">
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg>
        添加片段
      </button>
      <span class="hint">列表顺序即输出顺序 · 拖动 <b>⠿</b> 可调整</span>
      <span class="sp"></span>
      <button class="btn btn-ghost" :disabled="mergeBusy || mergeRows.length < 2" @click="showUniform = !showUniform">
        统一区间
      </button>
    </div>

    <div class="dropzone">
      将多个视频文件 <b>拖入窗口</b> 即可追加片段（至少 2 个）
    </div>

    <div class="merge-layout">
      <div class="seg-list">
        <div
          v-for="(row, i) in mergeRows"
          :key="row.input_path"
          class="seg-item"
          :class="{ dragging: dragFrom === i, over: dragOver === i && dragFrom !== i }"
          draggable="true"
          @dragstart="dragFrom = i"
          @dragover.prevent="dragOver = i"
          @dragleave="dragOver = -1"
          @drop.prevent="() => { store.moveMergeRow(dragFrom, i); dragFrom = -1; dragOver = -1 }"
          @dragend="dragFrom = -1; dragOver = -1"
        >
          <span class="drag" title="拖动排序">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="currentColor"><circle cx="8" cy="5" r="1.7"/><circle cx="16" cy="5" r="1.7"/><circle cx="8" cy="12" r="1.7"/><circle cx="16" cy="12" r="1.7"/><circle cx="8" cy="19" r="1.7"/><circle cx="16" cy="19" r="1.7"/></svg>
          </span>
          <span class="idx">{{ i + 1 }}</span>
          <div class="thumb"></div>
          <div class="seg-info">
            <div class="nm" :title="row.input_path">{{ basename(row.input_path) }}</div>
            <div class="mt">{{ dirname(row.input_path) }} · 源时长 {{ fmtShort(row.duration) }}</div>
          </div>
          <div class="seg-range">
            <button
              class="sw"
              :class="{ on: row.has_range }"
              :title="row.has_range ? '关闭区间（使用整段）' : '开启区间截取'"
              :disabled="mergeBusy"
              @click="store.toggleMergeRange(i, !row.has_range)"
            ></button>
            <label class="field" :class="{ off: !row.has_range }">
              <input :value="row.has_range ? row.start.toFixed(1) : '整段'" :disabled="!row.has_range || mergeBusy"
                @change="(e) => { const v = parseFloat((e.target as HTMLInputElement).value); if (isFinite(v) && v < row.end) row.start = v }" />
            </label>
            <template v-if="row.has_range">
              <span class="to">至</span>
              <label class="field">
                <input :value="row.end.toFixed(1)" :disabled="mergeBusy"
                  @change="(e) => { const v = parseFloat((e.target as HTMLInputElement).value); if (isFinite(v) && v > row.start) row.end = v }" />
              </label>
            </template>
          </div>
          <button class="rm" :disabled="mergeBusy" @click="store.removeMergeRow(i)">移除</button>
        </div>
        <div v-if="!mergeRows.length" class="seg-empty">尚未添加片段（至少 2 个）</div>
      </div>

      <aside class="sidebar">
        <div class="card">
          <h3>合并模式</h3>
          <button class="opt" :class="{ on: mergeMode === 'auto' }" @click="mergeMode = 'auto'">
            <span class="t">自动<span class="dot"></span></span>
            <span class="d">参数一致走流复制（快/无损），不一致自动切重编码</span>
          </button>
          <button class="opt" :class="{ on: mergeMode === 'demuxer' }" @click="mergeMode = 'demuxer'">
            <span class="t">流复制<span class="dot"></span></span>
            <span class="d">concat demuxer，要求各片段编码参数一致</span>
          </button>
          <button class="opt" :class="{ on: mergeMode === 'filter' }" @click="mergeMode = 'filter'">
            <span class="t">重编码<span class="dot"></span></span>
            <span class="d">concat filter，兼容不同分辨率/编码</span>
          </button>
          <div class="mode-hint">{{ modeHint }}</div>
        </div>

        <div class="card">
          <h3>输出</h3>
          <label class="field full"><span>文件名</span><input v-model="mergeOutput" placeholder="留空派生自首个片段（_merged）" :disabled="mergeBusy" /></label>
          <div class="meta-grid">
            <div><span>片段数</span><b>{{ mergeRows.length }} 个</b></div>
            <div><span>合计时长</span><b>{{ totalDur.toFixed(1) }}s</b></div>
          </div>
        </div>

        <div v-if="mergeBusy" class="card">
          <div class="prog-row"><span>正在合并…</span><b>{{ Math.round(mergeProgress) }}%</b></div>
          <div class="pbar"><i :style="{ width: mergeProgress + '%' }"></i></div>
          <div class="action-row">
            <button class="btn btn-primary lg" disabled>合并中…</button>
            <button class="btn btn-danger" @click="store.cancelMerge">取消</button>
          </div>
        </div>
        <button v-else class="btn btn-primary lg" :disabled="mergeRows.length < 2" @click="store.submitMerge">
          开始合并
        </button>
      </aside>
    </div>

    <div v-if="showUniform" class="uniform">
      <span>统一区间</span>
      <label class="field narrow"><input v-model="uStart" placeholder="开始(秒)" /></label>
      <span class="to">至</span>
      <label class="field narrow"><input v-model="uEnd" placeholder="结束(秒)" /></label>
      <button class="btn btn-ghost sm" :disabled="mergeBusy" @click="applyUniform">应用到全部</button>
      <span class="hint">将为所有片段开启区间并填充</span>
    </div>

    <MsgLine v-if="mergeNotice" :text="mergeNotice" />
    <MsgLine :text="mergeMessage" />
    <MsgLine v-if="uniformMsg" :text="uniformMsg" />

    <div v-if="lastOutput" class="out-ops">
      <button class="btn btn-ghost sm" @click="store.revealFolder">打开输出所在文件夹</button>
    </div>

    <Preview v-if="lastPreviewPath" :path="lastPreviewPath" class="mini-preview" />
  </div>
</template>

<style scoped>
.merge-panel {
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
.hint b {
  color: var(--text2);
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

.merge-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 330px;
  gap: 16px;
  align-items: start;
}
.seg-list {
  min-width: 0;
}
.seg-item {
  display: flex;
  align-items: center;
  gap: 12px;
  background: var(--panel2);
  border: 1px solid var(--border);
  border-radius: 11px;
  padding: 10px 13px;
  margin-bottom: 10px;
}
.seg-item:hover {
  border-color: var(--text3);
}
.seg-item.dragging {
  opacity: 0.45;
}
.seg-item.over {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-soft);
}
.drag {
  color: var(--text3);
  cursor: grab;
  display: grid;
  place-items: center;
  flex: none;
}
.drag:active {
  cursor: grabbing;
}
.idx {
  width: 23px;
  height: 23px;
  border-radius: 7px;
  flex: none;
  display: grid;
  place-items: center;
  background: var(--accent-soft);
  color: var(--accent);
  font-size: 12px;
  font-weight: 700;
}
.thumb {
  width: 68px;
  aspect-ratio: 16 / 9;
  border-radius: 7px;
  flex: none;
  background: linear-gradient(135deg, #2a3140, #171b22);
  border: 1px solid var(--border);
}
.seg-info {
  flex: 1;
  min-width: 0;
}
.seg-info .nm {
  font-size: 12.5px;
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.seg-info .mt {
  font-size: 10.5px;
  color: var(--text3);
  margin-top: 3px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.seg-range {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: none;
}
.seg-range .field {
  width: 76px;
  flex: none;
  padding: 5px 9px;
}
.seg-range .field input {
  font-size: 12px;
}
.seg-range .to {
  font-size: 11px;
  color: var(--text3);
}
.rm {
  background: none;
  border: 0;
  color: var(--text3);
  font-size: 12px;
  cursor: pointer;
  padding: 3px 6px;
  border-radius: 6px;
  font-family: inherit;
  flex: none;
}
.rm:hover:not(:disabled) {
  color: var(--danger);
  background: rgba(242, 96, 106, 0.1);
}
.rm:disabled {
  opacity: 0.35;
  cursor: not-allowed;
}
.seg-empty {
  text-align: center;
  color: var(--text3);
  font-size: 12px;
  padding: 26px;
  border: 1px dashed var(--border);
  border-radius: 11px;
}

.sidebar {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.card {
  background: var(--panel2);
  border: 1px solid var(--border);
  border-radius: 12px;
  padding: 14px 16px;
}
.card > h3 {
  font-size: 13px;
  font-weight: 600;
  margin: 0 0 12px;
}
.opt {
  display: block;
  width: 100%;
  text-align: left;
  border: 1px solid var(--border);
  background: var(--panel);
  border-radius: 10px;
  padding: 10px 12px;
  margin-bottom: 8px;
  cursor: pointer;
  font-family: inherit;
  transition: 0.15s;
}
.opt:last-of-type {
  margin-bottom: 0;
}
.opt:hover {
  border-color: var(--text3);
}
.opt.on {
  border-color: var(--accent);
  background: var(--accent-soft);
}
.opt .t {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 13px;
  font-weight: 600;
  color: var(--text);
}
.opt .t .dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  border: 1.5px solid var(--text3);
}
.opt.on .t .dot {
  border-color: var(--accent);
  background: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-soft);
}
.opt .d {
  font-size: 11px;
  color: var(--text3);
  margin-top: 4px;
  line-height: 1.5;
  display: block;
}
.mode-hint {
  font-size: 11px;
  color: var(--text3);
  margin-top: 10px;
  line-height: 1.5;
}
.field.full {
  width: 100%;
}
.meta-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
  margin-top: 12px;
}
.meta-grid > div {
  background: var(--panel);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 8px 11px;
}
.meta-grid span {
  font-size: 10.5px;
  color: var(--text3);
}
.meta-grid b {
  display: block;
  font-size: 13px;
  margin-top: 3px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}
.prog-row {
  display: flex;
  justify-content: space-between;
  font-size: 12px;
  margin-bottom: 9px;
  color: var(--text2);
}
.prog-row b {
  color: var(--accent);
  font-variant-numeric: tabular-nums;
}
.action-row {
  display: flex;
  gap: 8px;
  margin-top: 12px;
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
.out-ops {
  display: flex;
  justify-content: flex-end;
}
.mini-preview {
  max-width: 520px;
}
</style>
