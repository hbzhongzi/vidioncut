<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { fmtShort, fmtTime, parseTime } from '../utils/time'

const props = defineProps<{
  duration: number
  start: number
  end: number
  current: number // 播放头位置（秒）
}>()

const emit = defineEmits<{
  (evt: 'update:start', v: number): void
  (evt: 'update:end', v: number): void
  (evt: 'play-range', s: number, e: number): void
}>()

const MIN_LEN = 0.1
const track = ref<HTMLDivElement | null>(null)
const dragging = ref<'' | 'start' | 'end'>('')

const startPct = computed(() => (props.duration > 0 ? (props.start / props.duration) * 100 : 0))
const endPct = computed(() => (props.duration > 0 ? (props.end / props.duration) * 100 : 0))
const selLen = computed(() => Math.max(0, props.end - props.start))

// 刻度：按时长选择 5/6 等分
const ticks = computed(() => {
  const n = props.duration <= 6 ? 5 : 6
  return Array.from({ length: n + 1 }, (_, i) => (props.duration * i) / n)
})

// 时间输入框本地态（失焦/回车提交；聚焦中不被外部值覆盖）
const startText = ref(fmtTime(props.start))
const endText = ref(fmtTime(props.end))
const editing = ref<'' | 'start' | 'end'>('')

watch(
  () => [props.start, props.end],
  ([s, t]) => {
    if (editing.value !== 'start') startText.value = fmtTime(s)
    if (editing.value !== 'end') endText.value = fmtTime(t)
  },
)

function pctFromEvent(e: PointerEvent | MouseEvent): number {
  const el = track.value!
  const r = el.getBoundingClientRect()
  return Math.min(100, Math.max(0, ((e.clientX - r.left) / r.width) * 100))
}

function onHandleDown(which: 'start' | 'end') {
  return (e: PointerEvent) => {
    e.preventDefault()
    e.stopPropagation()
    dragging.value = which
    ;(e.target as HTMLElement).setPointerCapture(e.pointerId)
  }
}

function onMove(e: PointerEvent) {
  if (!dragging.value || props.duration <= 0) return
  const t = (pctFromEvent(e) / 100) * props.duration
  if (dragging.value === 'start') {
    emit('update:start', Math.min(t, props.end - MIN_LEN))
  } else {
    emit('update:end', Math.max(t, props.start + MIN_LEN))
  }
}

function onUp() {
  dragging.value = ''
}

// 点击轨道空白：移动较近的手柄
function onTrackDown(e: PointerEvent) {
  if (props.duration <= 0) return
  const t = (pctFromEvent(e) / 100) * props.duration
  if (Math.abs(t - props.start) <= Math.abs(t - props.end)) {
    emit('update:start', Math.min(t, props.end - MIN_LEN))
  } else {
    emit('update:end', Math.max(t, props.start + MIN_LEN))
  }
}

function commitTime(which: 'start' | 'end', raw: string) {
  const v = parseTime(raw)
  if (v === null) {
    // 非法输入：还原为当前值
    if (which === 'start') startText.value = fmtTime(props.start)
    else endText.value = fmtTime(props.end)
    return
  }
  if (which === 'start') {
    emit('update:start', Math.min(v, props.end - MIN_LEN))
  } else {
    emit('update:end', Math.max(v, props.start + MIN_LEN))
  }
}

function setMid() {
  emit('update:end', props.duration / 2)
}
</script>

<template>
  <div class="timeline">
    <div class="tl-head">
      <h3>裁剪区间</h3>
      <span class="badge">已选 {{ selLen.toFixed(1) }} 秒</span>
    </div>

    <div class="tl-ruler">
      <span v-for="(t, i) in ticks" :key="i">{{ fmtShort(t) }}</span>
    </div>

    <div
      ref="track"
      class="tl-track"
      @pointerdown="onTrackDown"
      @pointermove="onMove"
      @pointerup="onUp"
      @pointercancel="onUp"
    >
      <div class="ticks"><i v-for="(t, i) in ticks" :key="i"></i></div>
      <div class="tl-sel" :style="{ left: startPct + '%', right: 100 - endPct + '%' }">
        <span class="lab">{{ fmtTime(start) }} → {{ fmtTime(end) }}</span>
      </div>
      <div
        class="tl-handle l"
        :style="{ left: startPct + '%' }"
        title="拖动调整开始时间"
        @pointerdown="onHandleDown('start')"
      ></div>
      <div
        class="tl-handle r"
        :style="{ left: endPct + '%' }"
        title="拖动调整结束时间"
        @pointerdown="onHandleDown('end')"
      ></div>
    </div>

    <div class="tl-ops">
      <button class="btn btn-ghost sm" @click="emit('play-range', start, end)">
        <svg width="10" height="10" viewBox="0 0 24 24" fill="currentColor"><path d="M8 5v14l11-7z"/></svg>
        播放选区
      </button>
      <button class="btn btn-ghost sm" @click="emit('update:start', current)">设入点 <span class="kbd">I</span></button>
      <button class="btn btn-ghost sm" @click="emit('update:end', current)">设出点 <span class="kbd">O</span></button>
      <button class="btn btn-ghost sm" title="将结束点设为总时长中点" @click="setMid">中点</button>
      <span class="kbd-hint">
        <span><span class="kbd">空格</span> 播放/暂停</span>
        <span><span class="kbd">←→</span> 播放头 ±0.5s</span>
      </span>
    </div>

    <div class="tl-inputs">
      <label class="field">
        <span>开始</span>
        <input
          v-model="startText"
          spellcheck="false"
          @focus="editing = 'start'"
          @blur="editing = ''; commitTime('start', startText)"
          @keydown.enter="commitTime('start', startText)"
        />
      </label>
      <label class="field">
        <span>结束</span>
        <input
          v-model="endText"
          spellcheck="false"
          @focus="editing = 'end'"
          @blur="editing = ''; commitTime('end', endText)"
          @keydown.enter="commitTime('end', endText)"
        />
      </label>
    </div>
  </div>
</template>

<style scoped>
.timeline {
  background: var(--panel2);
  border: 1px solid var(--border);
  border-radius: 12px;
  padding: 14px 16px;
}
.tl-head {
  display: flex;
  align-items: center;
  margin-bottom: 12px;
}
.tl-head h3 {
  font-size: 13px;
  font-weight: 600;
  margin: 0;
}
.tl-head .badge {
  font-size: 11px;
  color: var(--accent);
  background: var(--accent-soft);
  border: 1px solid rgba(79, 140, 255, 0.35);
  border-radius: 20px;
  padding: 3px 10px;
  margin-left: auto;
  font-variant-numeric: tabular-nums;
}
.tl-ruler {
  display: flex;
  justify-content: space-between;
  font-size: 10.5px;
  color: var(--text3);
  margin-bottom: 5px;
  font-variant-numeric: tabular-nums;
}
.tl-track {
  position: relative;
  height: 36px;
  background: var(--panel);
  border: 1px solid var(--border);
  border-radius: 9px;
  touch-action: none;
  user-select: none;
}
.tl-track .ticks {
  position: absolute;
  inset: 0;
  display: flex;
  justify-content: space-between;
  padding: 0 1px;
  pointer-events: none;
}
.tl-track .ticks i {
  width: 1px;
  background: var(--border);
  height: 7px;
  align-self: flex-end;
}
.tl-sel {
  position: absolute;
  top: 0;
  bottom: 0;
  background: var(--accent-soft);
  border-left: 2px solid var(--accent);
  border-right: 2px solid var(--accent);
  border-radius: 7px;
  pointer-events: none;
}
.tl-sel .lab {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  font-size: 11px;
  color: var(--accent);
  font-weight: 600;
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
}
.tl-handle {
  position: absolute;
  top: 50%;
  transform: translate(-50%, -50%);
  width: 13px;
  height: 27px;
  border-radius: 5px;
  background: var(--accent);
  box-shadow: 0 0 0 3px rgba(79, 140, 255, 0.22);
  cursor: ew-resize;
  display: grid;
  place-items: center;
  touch-action: none;
}
.tl-handle::before,
.tl-handle::after {
  content: '';
  width: 1px;
  height: 11px;
  background: rgba(255, 255, 255, 0.75);
}
.tl-handle::before {
  margin-right: 2px;
}
.tl-handle::after {
  margin-left: 2px;
}
.tl-ops {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 13px;
  flex-wrap: wrap;
}
.kbd-hint {
  font-size: 11px;
  color: var(--text3);
  margin-left: auto;
  display: flex;
  gap: 12px;
  align-items: center;
}
.tl-inputs {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 12px;
}
</style>
