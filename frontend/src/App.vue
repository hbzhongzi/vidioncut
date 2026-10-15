<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { useTrimStore } from './store/trim'
import Preview from './components/Preview.vue'
import Timeline from './components/Timeline.vue'
import BatchPanel from './components/BatchPanel.vue'
import MergePanel from './components/MergePanel.vue'
import AICutPanel from './components/AICutPanel.vue'
import CodexPanel from './components/CodexPanel.vue'
import MsgLine from './components/MsgLine.vue'
import { fmtShort } from './utils/time'
import * as api from './wails/bridge'

const store = useTrimStore()
const {
  inputPath, probe, start, end, strategy,
  progress, message, lastOutput, busy,
  batchMessage, batchSummary, batchBusy,
  mergeMessage, mergeNotice, mergeBusy,
} = storeToRefs(store)

// 模式页签：单文件 / 批量 / 合并 / AI 粗剪 / Codex
type Mode = 'single' | 'batch' | 'merge' | 'aicut' | 'codex'
const activeTab = ref<Mode>('single')

// ===== 主题（深色默认，浅色可切换，localStorage 记忆） =====
const theme = ref<'dark' | 'light'>(
  (localStorage.getItem('videocut-theme') as 'dark' | 'light') || 'dark',
)
function applyTheme() {
  document.documentElement.dataset.theme = theme.value
  localStorage.setItem('videocut-theme', theme.value)
}
function toggleTheme() {
  theme.value = theme.value === 'dark' ? 'light' : 'dark'
  applyTheme()
}

// ===== 预览引用与播放头 =====
const preview = ref<InstanceType<typeof Preview> | null>(null)
const playhead = ref(0)

// ===== 键盘快捷键（单文件模式，输入框聚焦时不拦截） =====
function onKey(e: KeyboardEvent) {
  const t = e.target as HTMLElement | null
  if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.isContentEditable)) return
  if (activeTab.value !== 'single' || !probe.value) return
  if (e.code === 'Space') {
    e.preventDefault()
    preview.value?.togglePlay()
  } else if (e.key === 'i' || e.key === 'I') {
    store.start = Math.min(playhead.value, end.value - 0.1)
  } else if (e.key === 'o' || e.key === 'O') {
    store.end = Math.max(playhead.value, start.value + 0.1)
  } else if (e.key === 'ArrowLeft') {
    e.preventDefault()
    preview.value?.seek(playhead.value - 0.5)
  } else if (e.key === 'ArrowRight') {
    e.preventDefault()
    preview.value?.seek(playhead.value + 0.5)
  }
}

// ===== 窗口级文件拖放 =====
let dragDepth = 0

function onDragEnter(e: DragEvent) {
  if (!e.dataTransfer?.types.includes('Files')) return
  dragDepth++
  document.body.classList.add('dragging-files')
}
function onDragOver(e: DragEvent) {
  if (e.dataTransfer?.types.includes('Files')) e.preventDefault()
}
function onDragLeave() {
  dragDepth = Math.max(0, dragDepth - 1)
  if (dragDepth === 0) document.body.classList.remove('dragging-files')
}

/** 拖放路由：多文件 → 批量；单文件 → 单文件模式 */
function onDropPaths(paths: string[]) {
  document.body.classList.remove('dragging-files')
  dragDepth = 0
  if (!paths.length) return
  if (paths.length > 1) {
    activeTab.value = 'batch'
    store.addBatchFiles(paths)
  } else {
    store.loadSingleFile(paths[0])
  }
}

// ===== 状态栏 =====
const statusText = computed(() => {
  if (activeTab.value === 'batch') return batchBusy.value ? batchSummary.value || '批量处理中…' : batchMessage.value || '批量模式就绪'
  if (activeTab.value === 'merge') return mergeBusy.value ? '合并处理中…' : mergeMessage.value || '合并模式就绪'
  if (activeTab.value === 'aicut') return store.aiBusy ? 'AI 粗剪处理中…' : store.aiMessage || 'AI 粗剪就绪'
  if (activeTab.value === 'codex') return store.codexMessage || 'Codex 面板就绪'
  if (busy.value) return '裁剪处理中…'
  return message.value || '就绪'
})
const statusRunning = computed(() => {
  if (activeTab.value === 'batch') return batchBusy.value
  if (activeTab.value === 'merge') return mergeBusy.value
  if (activeTab.value === 'aicut') return store.aiBusy
  if (activeTab.value === 'codex') return false
  return busy.value
})

onMounted(() => {
  applyTheme()
  store.setupEvents()
  // Wails 文件拖放（Go 侧已开启 EnableFileDrop）；useDropTarget=false → 整窗可放
  if (typeof window.runtime?.OnFileDrop === 'function') {
    api.OnFileDrop(onDropPaths)
  }
  window.addEventListener('keydown', onKey)
  window.addEventListener('dragenter', onDragEnter)
  window.addEventListener('dragover', onDragOver)
  window.addEventListener('dragleave', onDragLeave)
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKey)
  window.removeEventListener('dragenter', onDragEnter)
  window.removeEventListener('dragover', onDragOver)
  window.removeEventListener('dragleave', onDragLeave)
  document.body.classList.remove('dragging-files')
})
</script>

<template>
  <div class="app">
    <!-- 标题栏 -->
    <header class="titlebar">
      <div class="logo">
        <span class="ic">
          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"><circle cx="6" cy="6" r="2.6"/><circle cx="6" cy="18" r="2.6"/><path d="M8.5 7.6 20 17M8.5 16.4 20 7"/></svg>
        </span>
        VideoCut <small>视频裁剪器</small>
      </div>
      <nav class="seg">
        <button :class="{ on: activeTab === 'single' }" @click="activeTab = 'single'">单文件裁剪</button>
        <button :class="{ on: activeTab === 'batch' }" @click="activeTab = 'batch'">批量裁剪</button>
        <button :class="{ on: activeTab === 'merge' }" @click="activeTab = 'merge'">合并</button>
        <button :class="{ on: activeTab === 'aicut' }" @click="activeTab = 'aicut'">AI 粗剪<span class="badge-new">NEW</span></button>
        <button :class="{ on: activeTab === 'codex' }" @click="activeTab = 'codex'">Codex<span class="badge-new">NEW</span></button>
      </nav>
      <div class="right">
        <button class="theme-btn" :title="theme === 'dark' ? '切换浅色主题' : '切换深色主题'" @click="toggleTheme">
          <svg v-if="theme === 'dark'" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/></svg>
          <svg v-else width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z"/></svg>
        </button>
        <span class="winctl"><i></i><i></i><i></i></span>
      </div>
    </header>

    <!-- 主区域 -->
    <main class="body">
      <!-- ============ 单文件裁剪 ============ -->
      <div v-if="activeTab === 'single'" class="layout">
        <div class="stage">
          <div class="toolbar">
            <button class="btn btn-primary" @click="store.selectFile">
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/></svg>
              选择文件
            </button>
            <div class="path-chip">
              <b v-if="inputPath">{{ inputPath.split(/[\\/]/).pop() }}</b>
              <span v-if="inputPath"> · {{ inputPath }}</span>
              <span v-else>未选择文件 — 也可直接将视频拖入窗口</span>
            </div>
            <button v-if="inputPath" class="btn btn-ghost" @click="store.selectFile">更换</button>
          </div>

          <Preview :path="inputPath" @update:current="playhead = $event" ref="preview" />

          <div v-if="probe" class="meta-row">
            <span class="meta"><i>时长</i>{{ fmtShort(probe.duration) }}</span>
            <span class="meta" v-if="probe.has_video"><i>分辨率</i>{{ probe.width }}×{{ probe.height }}</span>
            <span class="meta"><i>音轨</i>{{ probe.has_audio ? '有' : '无' }}</span>
            <span class="meta"><i>容器</i>{{ probe.format_name }}</span>
          </div>

          <Timeline
            v-if="probe"
            :duration="probe.duration"
            v-model:start="start"
            v-model:end="end"
            :current="playhead"
            @play-range="(s: number, e: number) => preview?.playSelection(s, e)"
          />
          <div v-else class="card empty-card">
            <p>选择文件后，这里会显示可拖拽的裁剪时间轴</p>
            <p class="sub">支持拖放文件到窗口 · 空格播放 · I/O 设入点出点</p>
          </div>

          <MsgLine :text="message" />
        </div>

        <aside class="sidebar">
          <div v-if="probe" class="card">
            <div class="file-card">
              <span class="file-icon">
                <svg width="19" height="19" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="m22 8-6 4 6 4V8z"/><rect x="2" y="6" width="14" height="12" rx="2"/></svg>
              </span>
              <div class="fc-info">
                <div class="nm">{{ inputPath.split(/[\\/]/).pop() }}</div>
                <div class="pth">{{ inputPath }}</div>
              </div>
            </div>
            <div class="meta-grid">
              <div><span>选中区间</span><b>{{ fmtShort(start) }} → {{ fmtShort(end) }}</b></div>
              <div><span>输出时长</span><b>{{ (end - start).toFixed(1) }}s</b></div>
            </div>
          </div>

          <div class="card">
            <h3>裁剪策略</h3>
            <button class="opt" :class="{ on: strategy === 'stream_copy' }" @click="strategy = 'stream_copy'">
              <span class="t">流复制<span class="dot"></span></span>
              <span class="d">速度快、无损失，入点吸附到最近关键帧</span>
            </button>
            <button class="opt" :class="{ on: strategy === 'reencode' }" @click="strategy = 'reencode'">
              <span class="t">重编码<span class="dot"></span></span>
              <span class="d">精确到帧，速度较慢，适合精细剪辑</span>
            </button>
            <button class="opt" :class="{ on: strategy === 'audio_only' }" @click="strategy = 'audio_only'">
              <span class="t">仅音频<span class="dot"></span></span>
              <span class="d">提取选中区间的音轨（m4a）</span>
            </button>
          </div>

          <div class="card">
            <h3>输出</h3>
            <div class="path-chip full">{{ lastOutput || '输出到源文件同目录，自动避让重名' }}</div>
            <div class="out-ops">
              <button class="btn btn-ghost sm" :disabled="!lastOutput" @click="store.revealFolder">打开所在文件夹</button>
            </div>
          </div>

          <div class="card">
            <template v-if="busy">
              <div class="prog-row"><span>正在处理 · {{ { stream_copy: '流复制', reencode: '重编码', audio_only: '仅音频' }[strategy] }}</span><b>{{ Math.round(progress) }}%</b></div>
              <div class="pbar"><i :style="{ width: progress + '%' }"></i></div>
              <div class="action-row">
                <button class="btn btn-primary lg" disabled>裁剪中…</button>
                <button class="btn btn-danger" @click="store.cancel">取消</button>
              </div>
            </template>
            <template v-else>
              <button class="btn btn-primary lg" :disabled="!inputPath" @click="store.submit">开始裁剪</button>
              <div class="tip">完成后可一键打开所在文件夹</div>
            </template>
          </div>
        </aside>
      </div>

      <!-- ============ 批量裁剪 ============ -->
      <BatchPanel v-else-if="activeTab === 'batch'" />

      <!-- ============ 合并 ============ -->
      <MergePanel v-else-if="activeTab === 'merge'" />

      <!-- ============ AI 粗剪 ============ -->
      <AICutPanel v-else-if="activeTab === 'aicut'" />

      <!-- ============ Codex 集成 ============ -->
      <CodexPanel v-else-if="activeTab === 'codex'" />
      <div v-else class="card empty-card">
        <p>未知页面：{{ activeTab }}</p>
      </div>
    </main>

    <!-- 状态栏 -->
    <footer class="statusbar">
      <span class="st">
        <span class="dot" :class="{ run: statusRunning }"></span>
        {{ statusText }}
      </span>
      <span class="ver">VideoCut v2.0 · AI 粗剪 / Codex 集成</span>
    </footer>
  </div>
</template>

<style scoped>
.app {
  height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--bg);
}

/* ---- 标题栏 ---- */
.titlebar {
  height: 54px;
  flex: none;
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 0 14px;
  border-bottom: 1px solid var(--border);
  background: linear-gradient(180deg, var(--panel2), var(--panel));
  -webkit-app-region: drag;
}
.titlebar button,
.titlebar nav {
  -webkit-app-region: no-drag;
}
.logo {
  display: flex;
  align-items: center;
  gap: 9px;
  font-size: 14px;
  font-weight: 700;
}
.logo .ic {
  width: 30px;
  height: 30px;
  border-radius: 8px;
  display: grid;
  place-items: center;
  background: linear-gradient(135deg, var(--accent), var(--accent2));
  color: #fff;
}
.logo small {
  font-size: 11px;
  color: var(--text3);
  font-weight: 500;
  margin-left: 2px;
}
.seg {
  display: flex;
  background: var(--panel2);
  border: 1px solid var(--border);
  border-radius: 9px;
  padding: 3px;
  gap: 2px;
  margin: 0 auto;
}
.seg button {
  border: 0;
  background: transparent;
  color: var(--text2);
  padding: 6px 16px;
  border-radius: 7px;
  font-size: 13px;
  cursor: pointer;
  transition: 0.15s;
  font-family: inherit;
}
.seg button:hover {
  color: var(--text);
}
.seg button.on {
  background: linear-gradient(135deg, var(--accent), var(--accent2));
  color: #fff;
  font-weight: 600;
}
.seg .badge-new {
  font-size: 9px;
  font-weight: 700;
  padding: 1px 5px;
  border-radius: 5px;
  background: var(--accent-soft);
  color: var(--accent);
  letter-spacing: .5px;
}
.seg button.on .badge-new {
  background: rgba(255, 255, 255, .22);
  color: #fff;
}
.right {
  display: flex;
  align-items: center;
  gap: 12px;
}
.theme-btn {
  width: 30px;
  height: 30px;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: var(--panel2);
  color: var(--text2);
  display: grid;
  place-items: center;
  cursor: pointer;
}
.theme-btn:hover {
  color: var(--text);
  border-color: var(--text3);
}
.winctl {
  display: flex;
  gap: 8px;
}
.winctl i {
  width: 11px;
  height: 11px;
  border-radius: 50%;
  background: var(--border);
}

/* ---- 主区域 ---- */
.body {
  flex: 1;
  overflow-y: auto;
  padding: 16px;
}
.layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 330px;
  gap: 16px;
  max-width: 1180px;
  margin: 0 auto;
}
.stage {
  display: flex;
  flex-direction: column;
  gap: 14px;
  min-width: 0;
}
.sidebar {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
}
.path-chip {
  font-size: 12px;
  color: var(--text2);
  background: var(--panel2);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 7px 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
  min-width: 0;
}
.path-chip b {
  color: var(--text);
  font-weight: 600;
}
.path-chip.full {
  width: 100%;
  flex: none;
}

.meta-row {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
}
.meta {
  font-size: 12px;
  color: var(--text);
  background: var(--panel2);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 5px 11px;
  font-variant-numeric: tabular-nums;
}
.meta i {
  font-style: normal;
  color: var(--text3);
  margin-right: 7px;
  font-size: 11px;
}

.empty-card {
  text-align: center;
  color: var(--text3);
  padding: 26px 16px;
}
.empty-card p {
  margin: 0;
  font-size: 13px;
}
.empty-card .sub {
  font-size: 11.5px;
  margin-top: 6px;
}

/* ---- 卡片 ---- */
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

.file-card {
  display: flex;
  gap: 12px;
  align-items: center;
}
.file-icon {
  width: 42px;
  height: 42px;
  border-radius: 10px;
  flex: none;
  background: var(--accent-soft);
  color: var(--accent);
  display: grid;
  place-items: center;
}
.fc-info {
  min-width: 0;
}
.fc-info .nm {
  font-size: 13px;
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.fc-info .pth {
  font-size: 11px;
  color: var(--text3);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  margin-top: 2px;
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
.opt:last-child {
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

.out-ops {
  margin-top: 10px;
  display: flex;
  justify-content: flex-end;
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
.tip {
  font-size: 11px;
  color: var(--text3);
  margin-top: 9px;
  text-align: center;
}

/* ---- 状态栏 ---- */
.statusbar {
  flex: none;
  border-top: 1px solid var(--border);
  padding: 9px 16px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 11.5px;
  color: var(--text3);
  background: var(--panel);
}
.statusbar .st {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.statusbar .dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--success);
  box-shadow: 0 0 8px var(--success);
  flex: none;
}
.statusbar .dot.run {
  background: var(--accent);
  box-shadow: 0 0 8px var(--accent);
  animation: blink 1s infinite;
}
@keyframes blink {
  50% {
    opacity: 0.35;
  }
}
.ver {
  flex: none;
  margin-left: 12px;
}
</style>
