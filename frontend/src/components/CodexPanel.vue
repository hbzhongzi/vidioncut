<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { useTrimStore } from '../store/trim'

const store = useTrimStore()
const { asrStatus, callLog, hotwords, codexMessage } = storeToRefs(store)

const newHw = ref('')

const connected = computed(() => !!asrStatus.value && asrStatus.value.healthy && !asrStatus.value.runtime_missing)

function fmtTime(ts: string): string {
  // RFC3339Nano → HH:MM:SS
  const t = ts.split('T')[1] || ts
  return t.split('.')[0]
}

function addHw() {
  store.addHotword(newHw.value)
  newHw.value = ''
}
function reload() {
  store.refreshASRStatus()
  store.refreshCallLog()
}
onMounted(reload)
</script>

<template>
  <div class="codex">
    <div class="toolbar">
      <button class="btn btn-ghost sm" @click="reload">刷新</button>
      <span class="sp"></span>
      <span class="hint">对话轨迹来自 <code>logs/mcp-calls.jsonl</code> · 实时反映 Agent 调用</span>
    </div>

    <div class="work">
      <div class="main">
        <div class="card">
          <h3>Codex 调用轨迹（mcp-serve · stdio）<span class="tag" :class="connected ? 'ok' : 'wait'">{{ connected ? '已连接' : '未连接' }}</span></h3>
          <div class="log">
            <div v-for="(e, i) in callLog" :key="i" class="toolcall">
              <span class="st" :class="e.ok ? 'ok' : 'err'"></span>
              <span class="name">{{ e.tool }}</span>
              <span v-if="e.err" class="err-ms">{{ e.err }}</span>
              <span v-else class="ms">{{ e.ms }}ms</span>
              <span class="ts">{{ fmtTime(e.ts) }}</span>
            </div>
            <div v-if="!callLog.length" class="empty">暂无调用记录 — 运行 <code>videocut.exe mcp-serve</code> 并接入 Codex 后自动累积</div>
          </div>
        </div>
      </div>

      <div class="side">
        <div class="card">
          <h3>MCP Server <span class="tag ok">运行中</span></h3>
          <div class="kv"><span class="k">入口</span><span class="v mono">videocut.exe mcp-serve</span></div>
          <div class="kv"><span class="k">协议</span><span class="v">stdio JSON-RPC</span></div>
          <div class="kv"><span class="k">工具</span><span class="v">15 个已注册</span></div>
          <div class="kv"><span class="k">工具超时</span><span class="v">300s</span></div>
        </div>

        <div class="card">
          <h3>ASR 设置</h3>
          <div class="row"><span class="k">模型</span><span class="v mono">{{ asrStatus?.model || '—' }} · {{ asrStatus?.compute_type || '' }}</span></div>
          <div class="row"><span class="k">词级时间戳</span><span class="v">已启用（字幕重对齐）</span></div>
          <div class="row"><span class="k">sidecar</span><span class="v mono">127.0.0.1:{{ asrStatus?.port || '—' }}</span></div>
          <div class="row"><span class="k">空闲回收</span><span class="v">{{ asrStatus?.idle_timeout_min || '—' }} 分钟</span></div>
          <div class="row"><span class="k">venv</span><span class="v mono" style="font-size:10px">{{ asrStatus?.python || '未找到' }}</span></div>
          <div class="row" style="display:block">
            <span class="k">热词表</span>
            <div class="hotwords">
              <span v-for="(h, i) in hotwords" :key="i" class="hw">{{ h }} <i @click="store.removeHotword(i)">×</i></span>
            </div>
            <div class="hw-add">
              <input v-model="newHw" @keyup.enter="addHw" placeholder="新增热词…" />
              <button class="btn btn-ghost sm" @click="addHw">添加</button>
            </div>
            <button class="btn btn-ghost sm" style="margin-top:8px" :disabled="!hotwords.length" @click="store.applyHotwords">下发 sidecar</button>
          </div>
        </div>

        <div v-if="asrStatus && (asrStatus.runtime_missing || !asrStatus.healthy)" class="warn-box">
          <b>⚠ sidecar 未就绪</b><br />
          调用转录类工具将返回 <code>asr_runtime_missing</code> 与安装指引（<code>scripts/bootstrap-asr.ps1</code>），Agent 不会替代实现。<br />
          <span style="color:var(--text2)">{{ asrStatus.hint }}</span>
        </div>
        <div v-else-if="asrStatus" class="warn-box ok">
          <b style="color:var(--success)">✓ sidecar 就绪</b><br />
          <span style="color:var(--text2)">{{ asrStatus.hint }}</span>
        </div>
      </div>
    </div>

    <div v-if="codexMessage" class="msg">{{ codexMessage }}</div>
  </div>
</template>

<style scoped>
.codex { max-width: 1180px; margin: 0 auto; display: flex; flex-direction: column; gap: 14px; }
.toolbar { display: flex; align-items: center; gap: 10px; }
.toolbar .sp { flex: 1; }
.hint { font-size: 12px; color: var(--text3); }
.hint code { font-family: Consolas, monospace; background: var(--panel2); border: 1px solid var(--border); border-radius: 5px; padding: 1px 5px; color: var(--accent); font-size: 11px; }

.work { display: grid; grid-template-columns: minmax(0, 1fr) 300px; gap: 16px; align-items: start; }
.main { display: flex; flex-direction: column; gap: 14px; min-width: 0; }
.side { display: flex; flex-direction: column; gap: 14px; }

.card { background: var(--panel); border: 1px solid var(--border); border-radius: 12px; padding: 14px 16px; }
.card > h3 { font-size: 13px; font-weight: 600; margin: 0 0 12px; display: flex; align-items: center; justify-content: space-between; gap: 8px; }

.log { display: flex; flex-direction: column; gap: 8px; max-height: 460px; overflow-y: auto; }
.toolcall { display: flex; align-items: center; gap: 9px; background: var(--panel2); border: 1px solid var(--border); border-radius: 8px; padding: 8px 11px; font-size: 11.5px; }
.toolcall .st { width: 8px; height: 8px; border-radius: 50%; flex: none; }
.toolcall .st.ok { background: var(--success); }
.toolcall .st.err { background: var(--danger); }
.toolcall .st.run { background: var(--warning); animation: pulse 1.2s infinite; }
@keyframes pulse { 50% { opacity: .35; } }
.toolcall .name { font-family: Consolas, monospace; color: var(--accent); font-size: 11px; }
.toolcall .err-ms { color: var(--danger); font-size: 10.5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex: 1; }
.toolcall .ms { margin-left: auto; color: var(--text3); font-variant-numeric: tabular-nums; }
.toolcall .ts { color: var(--text3); font-size: 10px; font-variant-numeric: tabular-nums; flex: none; }
.empty { color: var(--text3); font-size: 12px; text-align: center; padding: 22px; line-height: 1.7; }
.empty code { font-family: Consolas, monospace; background: var(--panel2); border: 1px solid var(--border); border-radius: 5px; padding: 1px 5px; color: var(--accent); font-size: 11px; }

.kv { display: flex; justify-content: space-between; font-size: 12px; padding: 5px 0; }
.kv .k { color: var(--text3); }
.kv .v { color: var(--text); font-variant-numeric: tabular-nums; }
.mono { font-family: Consolas, monospace; font-size: 10.5px; }

.row { padding: 8px 0; border-bottom: 1px solid var(--border); font-size: 12px; }
.row:last-child { border-bottom: 0; }
.row .k { color: var(--text2); }
.row .v { color: var(--text3); font-size: 11px; }
.hotwords { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 6px; }
.hw { font-size: 11px; background: var(--accent-soft); color: var(--accent); border-radius: 6px; padding: 3px 9px; display: inline-flex; align-items: center; gap: 6px; }
.hw i { font-style: normal; cursor: pointer; opacity: .6; }
.hw i:hover { opacity: 1; }
.hw-add { display: flex; gap: 6px; margin-top: 8px; }
.hw-add input { flex: 1; background: var(--panel2); border: 1px solid var(--border); color: var(--text); border-radius: 7px; padding: 5px 9px; font-size: 12px; outline: none; min-width: 0; }
.hw-add input:focus { border-color: var(--accent); }

.warn-box { border: 1px solid rgba(232, 163, 61, .35); background: rgba(232, 163, 61, .09); border-radius: 9px; padding: 10px 12px; font-size: 11.5px; color: var(--warning); line-height: 1.6; }
.warn-box b { color: var(--warning); }
.warn-box code { font-family: Consolas, monospace; background: rgba(232, 163, 61, .14); padding: 1px 5px; border-radius: 4px; font-size: 10.5px; }
.warn-box.ok { border-color: rgba(63, 185, 111, .35); background: rgba(63, 185, 111, .09); }

.msg { font-size: 12px; color: var(--text2); background: var(--panel2); border: 1px solid var(--border); border-radius: 8px; padding: 9px 12px; }
</style>
