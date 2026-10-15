<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { fmtTime } from '../utils/time'

const props = defineProps<{ path: string }>()
const emit = defineEmits<{ (e: 'update:current', v: number): void }>()

const video = ref<HTMLVideoElement | null>(null)
const playing = ref(false)
const cur = ref(0)
const dur = ref(0)
const muted = ref(false)
// 选区播放终点（>0 时到点自动暂停）
let stopAt = 0

// 经 Go 侧 AssetServer 流式提供（支持 Range），前端用相对 URL（开发文档 v1.1 §5.4）。
const src = computed(() => (props.path ? '/preview?path=' + encodeURIComponent(props.path) : ''))
const pct = computed(() => (dur.value > 0 ? (cur.value / dur.value) * 100 : 0))

watch(src, () => {
  stopAt = 0
  cur.value = 0
})

function togglePlay() {
  const v = video.value
  if (!v) return
  if (v.paused) v.play().catch(() => {})
  else v.pause()
}

function seek(t: number) {
  const v = video.value
  if (!v) return
  v.currentTime = Math.max(0, Math.min(t, v.duration || t))
}

/** 播放 [s, e) 选区，到点自动暂停 */
function playSelection(s: number, e: number) {
  const v = video.value
  if (!v) return
  stopAt = e
  v.currentTime = s
  v.play().catch(() => {})
}

function toggleMute() {
  const v = video.value
  if (!v) return
  v.muted = !v.muted
  muted.value = v.muted
}

function onTime() {
  const v = video.value
  if (!v) return
  cur.value = v.currentTime
  emit('update:current', v.currentTime)
  if (stopAt > 0 && v.currentTime >= stopAt) {
    v.pause()
    stopAt = 0
  }
}

function onMeta() {
  const v = video.value
  if (v) dur.value = v.duration || 0
}

function onEnded() {
  stopAt = 0
}

function onBarClick(e: MouseEvent) {
  const v = video.value
  if (!v || dur.value <= 0) return
  const bar = e.currentTarget as HTMLElement
  const r = bar.getBoundingClientRect()
  seek(((e.clientX - r.left) / r.width) * dur.value)
}

defineExpose({ togglePlay, seek, playSelection })
</script>

<template>
  <div class="preview">
    <video
      v-if="src"
      ref="video"
      :src="src"
      preload="metadata"
      class="video"
      @play="playing = true"
      @pause="playing = false"
      @timeupdate="onTime"
      @loadedmetadata="onMeta"
      @ended="onEnded"
    />
    <div v-else class="empty">
      <svg width="30" height="30" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><rect x="2" y="5" width="20" height="14" rx="2"/><path d="m10 9 5 3-5 3z"/></svg>
      <span>选择文件后可在此预览</span>
    </div>

    <div v-if="src" class="ctrlbar">
      <div class="pline" @click="onBarClick"><i :style="{ width: pct + '%' }"></i></div>
      <div class="cbar">
        <button class="ico" :title="playing ? '暂停（空格）' : '播放（空格）'" @click="togglePlay">
          <svg v-if="!playing" width="13" height="13" viewBox="0 0 24 24" fill="#fff"><path d="M8 5v14l11-7z"/></svg>
          <svg v-else width="13" height="13" viewBox="0 0 24 24" fill="#fff"><path d="M6 5h4v14H6zM14 5h4v14h-4z"/></svg>
        </button>
        <span class="t">{{ fmtTime(cur) }} / {{ fmtTime(dur) }}</span>
        <span class="sp"></span>
        <button class="ico" :title="muted ? '取消静音' : '静音'" @click="toggleMute">
          <svg v-if="!muted" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="#dfe3ea" stroke-width="2"><path d="M11 5 6 9H2v6h4l5 4z"/><path d="M15.5 8.5a5 5 0 0 1 0 7"/></svg>
          <svg v-else width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="#dfe3ea" stroke-width="2"><path d="M11 5 6 9H2v6h4l5 4z"/><path d="m16 9 5 6M21 9l-5 6"/></svg>
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.preview {
  position: relative;
  width: 100%;
  aspect-ratio: 16 / 9;
  background: #000;
  border-radius: 12px;
  overflow: hidden;
  border: 1px solid var(--border);
}
.video {
  width: 100%;
  height: 100%;
  display: block;
}
.empty {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  align-content: center;
  gap: 10px;
  color: var(--text3);
  font-size: 13px;
  background: radial-gradient(60% 80% at 70% 20%, rgba(79, 140, 255, 0.1), transparent 60%), #101318;
}
.ctrlbar {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  background: linear-gradient(transparent, rgba(0, 0, 0, 0.85));
  padding: 16px 12px 8px;
}
.pline {
  height: 3px;
  background: rgba(255, 255, 255, 0.18);
  border-radius: 2px;
  margin-bottom: 7px;
  cursor: pointer;
}
.pline > i {
  display: block;
  height: 100%;
  background: var(--accent);
  border-radius: 2px;
  position: relative;
}
.pline > i::after {
  content: '';
  position: absolute;
  right: -5px;
  top: -2.5px;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #fff;
}
.cbar {
  display: flex;
  align-items: center;
  gap: 12px;
  color: #dfe3ea;
  font-size: 11.5px;
}
.cbar .t {
  font-variant-numeric: tabular-nums;
  color: #aeb6c2;
}
.cbar .sp {
  flex: 1;
}
.ico {
  background: none;
  border: 0;
  padding: 2px;
  cursor: pointer;
  display: grid;
  place-items: center;
  border-radius: 5px;
}
.ico:hover {
  background: rgba(255, 255, 255, 0.14);
}
</style>
