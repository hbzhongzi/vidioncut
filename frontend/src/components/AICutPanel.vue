<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { storeToRefs } from 'pinia'
import { useTrimStore } from '../store/trim'
import { fmtShort } from '../utils/time'
import type { AIEditRow } from '../store/trim'
import type { Cue } from '../types'

const store = useTrimStore()
const {
  aiSource, aiWorkDir, aiView, aiRows, aiStage, aiBusy, aiMessage, aiProgress,
  aiTaskId, aiLastOutput,
  // M12 字幕
  subMode, subFormat, subPreset, subMaxChars, subMaxLines, subView, subBusy, subBurnTaskId, subBurnProgress, subBurnedOutput,
  subPlan, subPlanBusy,
} = storeToRefs(store)

const typeLabel: Record<string, string> = {
  filler_word: '口水词',
  silence: '静音',
  retake: '重拍',
  manual: '人工',
}

const enabledCount = computed(() => aiRows.value.filter((r) => r.enabled).length)
const summary = computed(() => aiView.value?.summary ?? null)
const sourceDur = computed(() => aiView.value?.source_duration ?? 0)

// 保留段条：accent = 保留段（来自后端 keep_ranges），danger = 启用的删除段
const keepSegs = computed(() =>
  (aiView.value?.keep_ranges ?? []).map((k) => ({
    left: (k.start / sourceDur.value) * 100,
    width: ((k.end - k.start) / sourceDur.value) * 100,
  })),
)
const delSegs = computed(() =>
  aiRows.value
    .filter((r) => r.enabled && sourceDur.value > 0)
    .map((r: AIEditRow) => ({
      left: (r.start / sourceDur.value) * 100,
      width: ((r.end - r.start) / sourceDur.value) * 100,
    })),
)

const outPath = computed(() => {
  if (aiLastOutput.value) return aiLastOutput.value
  if (aiSource.value) {
    const ext = aiSource.value.slice(aiSource.value.lastIndexOf('.'))
    const base = aiSource.value.slice(0, aiSource.value.lastIndexOf('.'))
    return `${base}_cut${ext}`
  }
  return ''
})

// ===== M12 字幕 =====
const subModes: { id: 'sidecar' | 'burn' | 'both'; name: string; desc: string }[] = [
  { id: 'sidecar', name: '外显 Sidecar', desc: 'SRT + ASS 独立文件，可导入剪映 / PR 精修（默认）' },
  { id: 'burn', name: '内嵌 Burn-in', desc: '字幕烧录进画面，适合直接分发（需重编码）' },
  { id: 'both', name: '双输出 Both', desc: '无字幕版 + 烧录版 + 外挂字幕，兼顾两者' },
]
const subPresets: { id: 'default' | 'talking-head' | 'minimal'; name: string; desc: string }[] = [
  { id: 'default', name: '通用 default', desc: '雅黑 48 · 2px 描边' },
  { id: 'talking-head', name: '口播 talking-head', desc: '雅黑 56 · 3px 描边 + 阴影' },
  { id: 'minimal', name: '简洁 minimal', desc: '雅黑 40 · 无描边' },
]

const cueLines = (c: Cue) => c.text.split('\n').length
const hasFinal = computed(() => aiStage.value === 'done' && !!aiLastOutput.value)
const subActionLabel = computed(() => {
  if (subMode.value === 'sidecar') {
    const labels = { srt: 'SRT', ass: 'ASS', both: 'SRT + ASS' } as const
    return `导出字幕（${labels[subFormat.value]}）`
  }
  if (subMode.value === 'burn') return '导出并烧录（重编码）'
  return '导出 + 烧录 + 外挂（全部产物）'
})

onMounted(() => {
  store.loadSubtitleConfig()
})

// 五维分析条（数据单一来源：motion-plan.json 的 analysis，前端纯展示）
const analysisDims = computed(() => {
  const a = subPlan.value?.analysis
  if (!a) return []
  return [
    { k: '关键词密度', v: `${Math.round(a.keyword_density * 100)}%`, w: Math.round(a.keyword_density * 100), kw: a.top_keywords.slice(0, 3).join(' / ') || '—' },
    { k: '内容类型', v: a.content_type, w: 100, kw: '词表命中' },
    { k: '平均句长', v: `${a.avg_chars_per_cue.toFixed(1)} 字/条`, w: Math.min(100, Math.round((a.avg_chars_per_cue / 30) * 100)), kw: '参考区间 10–30' },
    { k: '节奏快慢', v: `${a.avg_cue_duration.toFixed(1)}s/条`, w: Math.min(100, Math.round((a.avg_cue_duration / 6) * 100)), kw: a.avg_cue_duration < 2 ? '快节奏' : a.avg_cue_duration > 4 ? '慢节奏' : '中速' },
    { k: '情感强度', v: `${Math.round(a.emphatic_ratio * 100)}%`, w: Math.round(a.emphatic_ratio * 100), kw: '感叹 / 疑问占比' },
  ]
})

function basename(p: string): string {
  return p.split(/[\\/]/).pop() || p
}
</script>

<template>
  <div class="aicut">
    <!-- 工具条 -->
    <div class="toolbar">
      <button class="btn btn-primary" :disabled="aiBusy" @click="store.aiSelectSource">
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/></svg>
        选择视频
      </button>
      <button class="btn btn-ghost" :disabled="aiBusy" @click="store.aiSelectWorkDir">选择输出目录</button>
      <button class="btn btn-primary" :disabled="aiBusy || !aiSource || !aiWorkDir" @click="store.aiGenerate">
        ⚡ 转录并生成候选账本
      </button>
      <button class="btn btn-ghost" :disabled="aiBusy" @click="store.aiLoadLedger">载入已有 edits.json</button>
      <span class="sp"></span>
      <span v-if="aiStage === 'idle'" class="hint">AI 粗剪：转录 → 候选扫描 → 账本复核 → 保留段剪切</span>
    </div>

    <div v-if="aiSource" class="paths">
      <div class="path-chip"><b>{{ basename(aiSource) }}</b><span> · {{ aiSource }}</span></div>
      <div class="path-chip"><i>输出</i>{{ aiWorkDir || '未选择' }}</div>
    </div>

    <!-- 转录中 -->
    <div v-if="aiStage === 'transcribing'" class="card transcribing">
      <div class="asr">
        <div class="spin"></div>
        <div class="info">
          <div class="t1">faster-whisper 正在转录音频轨…</div>
          <div class="t2">small · int8 · VAD 已开启 · 输出 {{ aiWorkDir }}/work/</div>
          <div class="bar"><i :style="{ width: aiProgress + '%' }"></i></div>
        </div>
        <div class="pct">{{ aiProgress }}%</div>
      </div>
      <div class="sub">首次模型加载约 10–60s · 完成后自动生成删词候选</div>
    </div>

    <!-- 账本复核 -->
    <div v-else-if="aiStage === 'review' || aiStage === 'cutting' || aiStage === 'merging' || aiStage === 'done'" class="work">
      <div class="main">
        <div class="card">
          <h3>
            删词账本（edits.json）
            <span class="hint">{{ basename(aiSource) }} · 时长 {{ fmtShort(sourceDur) }} · {{ aiRows.length }} 项删除候选</span>
          </h3>

          <div v-if="aiView && !aiView.valid" class="warn-box">
            <b>⚠ 账本校验未通过</b><br />{{ aiView.validation_error }}
          </div>

          <table class="ledger">
            <thead>
              <tr>
                <th style="width:30px"></th>
                <th style="width:34px">#</th>
                <th>起止</th>
                <th>类型</th>
                <th>内容</th>
                <th>理由 / 来源</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(row, i) in aiRows" :key="i" :class="{ dim: !row.enabled }">
                <td><button class="sw" :class="{ on: row.enabled }" @click="store.aiToggleItem(i)"></button></td>
                <td class="mono" style="color:var(--text3)">{{ String(i + 1).padStart(2, '0') }}</td>
                <td class="mono">{{ fmtShort(row.start) }} → {{ fmtShort(row.end) }}</td>
                <td><span class="tag" :class="row.type">{{ typeLabel[row.type] }}</span></td>
                <td>“{{ row.text || '—' }}”</td>
                <td style="color:var(--text3)">{{ row.reason || '—' }}</td>
              </tr>
              <tr v-if="!aiRows.length"><td colspan="6" class="empty">暂无删除候选</td></tr>
            </tbody>
          </table>

          <div class="keep-strip">
            <div v-for="(s, i) in keepSegs" :key="'k' + i" class="seg-keep" :style="{ left: s.left + '%', width: s.width + '%' }"></div>
            <div v-for="(s, i) in delSegs" :key="'d' + i" class="seg-del" :style="{ left: s.left + '%', width: s.width + '%' }"></div>
          </div>
          <div class="keep-legend">
            <span><i style="background:var(--accent)"></i>保留段（{{ summary?.keep_count ?? 0 }} 段）</span>
            <span><i style="background:rgba(242,96,106,.5)"></i>删除段（已启用 {{ enabledCount }}/{{ aiRows.length }}）</span>
            <span v-if="summary">保留合计 {{ fmtShort(summary.keep_seconds) }} · 压缩 {{ summary.compression_pct.toFixed(1) }}%</span>
          </div>
        </div>

        <!-- M12 第三步：字幕（成片生成后浮现） -->
        <div v-if="hasFinal" class="card">
          <h3>
            ③ 字幕 · 剪后重对齐
            <span class="hint">源轴 → 成片轴 · 跨段边界词级断开</span>
          </h3>

          <div class="submodes">
            <div v-for="m in subModes" :key="m.id" class="submode" :class="{ sel: subMode === m.id }" @click="store.subSetMode(m.id)">
              <div class="t">{{ m.name }}<span class="radio"></span></div>
              <div class="d">{{ m.desc }}</div>
            </div>
          </div>

          <div class="presets">
            <span v-for="p in subPresets" :key="p.id" class="preset" :class="{ sel: subPreset === p.id }"
              :title="p.desc" @click="store.subSetPreset(p.id)">{{ p.name }}</span>
          </div>

          <div class="numrow">
            <label>单条最大字数 <input type="number" min="4" max="100" v-model.number="subMaxChars" :disabled="subBusy" /></label>
            <label>最多行数 <input type="number" min="1" max="5" v-model.number="subMaxLines" :disabled="subBusy" /></label>
            <span class="hint">程序侧预换行（libass 对 CJK 自动换行支持有限）</span>
          </div>

          <table v-if="subView" class="cue-table">
            <thead>
              <tr>
                <th style="width:30px">#</th>
                <th style="width:110px">成片时间轴</th>
                <th>文本</th>
                <th style="width:44px">行数</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(c, i) in subView.cues" :key="i">
                <td class="mono" style="color:var(--text3)">{{ String(i + 1).padStart(2, '0') }}</td>
                <td class="mono">{{ fmtShort(c.start) }} → {{ fmtShort(c.end) }}</td>
                <td :class="{ 'cue-split': c.split_at }">
                  <span v-if="c.split_at" class="split-flag" title="该条由保留段边界切开（词级断开），建议人工复核断点语义">⚡</span>
                  {{ c.text.replace(/\n/g, ' ⏎ ') }}
                </td>
                <td class="mono" style="text-align:center">{{ cueLines(c) }}</td>
              </tr>
              <tr v-if="!subView.cues.length"><td colspan="4" class="empty">重对齐后无字幕条</td></tr>
            </tbody>
          </table>

          <div class="sub-actions">
            <button class="btn btn-primary" style="justify-content:center" :disabled="subBusy || aiBusy" @click="store.subExport">
              ✂ {{ subActionLabel }}
            </button>
            <span v-if="subView" class="hint">
              {{ subView.cue_count }} 条 · 跨段断开 {{ subView.split_count }} 处 · 成片 {{ fmtShort(subView.final_duration) }}
            </span>
          </div>

          <div v-if="subBurnTaskId && (subBusy || subBurnProgress > 0)" class="prog-row">
            <span>烧录中 · 重编码（音轨直复制）</span><b>{{ Math.round(subBurnProgress) }}%</b>
          </div>
          <div v-if="subBurnTaskId && subBurnProgress > 0 && !subBusy" class="pbar"><i :style="{ width: subBurnProgress + '%' }"></i></div>

          <div v-if="subView" class="out-items">
            <div v-if="subView.srt_path" class="out-item"><i>SRT</i><span class="path" :title="subView.srt_path">{{ basename(subView.srt_path) }}</span></div>
            <div v-if="subView.ass_path" class="out-item"><i>ASS</i><span class="path" :title="subView.ass_path">{{ basename(subView.ass_path) }}</span></div>
            <div v-if="subBurnedOutput" class="out-item ok"><i>烧录版</i><span class="path" :title="subBurnedOutput">{{ basename(subBurnedOutput) }}</span></div>
          </div>
          <p v-if="subMode !== 'sidecar'" class="sub-note">内嵌需重编码（耗时随时长线性增长）；音轨直复制不损音质。预览可直接看到烧录字幕。</p>
          <p v-else class="sub-note">外显文件可直接导入剪映 / PR / DaVinci 作为可编辑字幕轨道；WebView2 预览不渲染外挂字幕，请以上方列表核对。</p>
        </div>

        <!-- M12 第四步：二次加工（字幕导出后浮现） -->
        <div v-if="subView" class="card">
          <h3>
            ④ 二次加工 · 自动特效
            <span class="hint">规则引擎自动选择 · 理由可查</span>
          </h3>

          <div class="sub-actions">
            <button class="btn btn-primary" style="justify-content:center" :disabled="subPlanBusy || subBusy" @click="store.subAnimate()">
              ✨ 分析并应用特效
            </button>
            <button class="btn btn-ghost" style="justify-content:center" :disabled="subPlanBusy" @click="store.subDisableAnimate()">全部禁用</button>
          </div>

          <template v-if="subPlan">
            <div v-if="subPlan.static" class="warn-box">
              <b>纯静态字幕</b><br />无特征命中或已全部禁用——合法态，特效不改变内容与时间轴。
            </div>
            <template v-else>
              <div class="dims">
                <div v-for="d in analysisDims" :key="d.k" class="dim">
                  <span class="k">{{ d.k }}</span>
                  <span class="bar"><i :style="{ width: d.w + '%' }"></i></span>
                  <span class="v">{{ d.v }}</span>
                  <span class="kw">{{ d.kw }}</span>
                </div>
              </div>
              <div class="skills-label">已选特效（{{ subPlan.selected.length }} 个）</div>
              <div class="chips">
                <span v-for="s in subPlan.selected" :key="s" class="chip on">{{ s }}</span>
              </div>
              <div v-if="subPlan.rejected.length" class="skills-label">已跳过（理由可查）</div>
              <div class="chips">
                <span v-for="(r, i) in subPlan.rejected" :key="i" class="chip off" :title="r.reason">{{ r.skill }}</span>
              </div>
              <div class="out-items">
                <div class="out-item"><i>FX</i><span class="path" :title="subPlan.fx_path">{{ basename(subPlan.fx_path) }}</span></div>
                <div class="out-item"><i>审计</i><span class="path" :title="subPlan.plan_path">{{ basename(subPlan.plan_path) }}</span></div>
              </div>
              <div class="sub-actions">
                <button v-if="subMode !== 'sidecar'" class="btn btn-primary" style="justify-content:center"
                  :disabled="subBusy || aiBusy" @click="store.subBurnFx()">用特效版重新烧录</button>
                <span v-else class="hint">edited_fx.ass 可导入播放器验证特效；烧录分发时选内嵌模式</span>
              </div>
            </template>
          </template>
          <p v-else class="sub-note">基于成片字幕的五维内容特征（关键词密度 / 内容类型 / 句长 / 节奏 / 情感强度），由确定性规则引擎自动选择 ≤2 个 ASS 特效；选择理由记录于 motion-plan.json，可单项禁用。</p>
        </div>
      </div>

      <div class="side">
        <div class="card">
          <h3>编辑摘要 <span class="tag ok">校验{{ aiView?.valid ? '通过' : '未过' }}</span></h3>
          <div class="kv"><span class="k">删除项</span><span class="v">{{ enabledCount }} 项（已启用 / 共 {{ aiRows.length }}）</span></div>
          <div class="kv"><span class="k">删除总时长</span><span class="v">{{ fmtShort(summary?.deleted_seconds ?? 0) }}</span></div>
          <div class="kv"><span class="k">保留段</span><span class="v">{{ summary?.keep_count ?? 0 }} 段</span></div>
          <div class="kv"><span class="k">预计产物时长</span><span class="v" style="color:var(--accent);font-weight:700">{{ fmtShort(summary?.keep_seconds ?? 0) }}</span></div>
          <div style="display:flex;gap:6px;margin-top:10px">
            <button class="btn sm" style="flex:1" :disabled="aiBusy" @click="store.aiSetAll(true)">全选</button>
            <button class="btn sm" style="flex:1" :disabled="aiBusy" @click="store.aiSetAll(false)">全不选</button>
            <button class="btn sm" style="flex:1" :disabled="aiBusy" @click="store.aiOnlyFillers">仅口水词</button>
          </div>
        </div>

        <div class="card">
          <h3>剪切策略</h3>
          <div class="strategy sel">
            <div class="t">精确重编码 <span class="radio"></span></div>
            <div class="d">reencode · 逐帧精确切分，账本剪切必选（耗时随时长线性增长）</div>
          </div>
        </div>

        <div class="card">
          <h3>输出</h3>
          <div class="output"><span>📁</span><span class="path">{{ outPath }}</span></div>
          <button v-if="aiStage === 'review' || aiStage === 'done'" class="btn btn-primary" style="width:100%;justify-content:center;margin-top:10px;padding:9px"
            :disabled="aiBusy || !aiRows.length" @click="store.aiConfirmCut">
            ✂ 确认剪切（保留段 → 批量任务）
          </button>
          <template v-else-if="aiStage === 'cutting'">
            <div class="prog-row"><span>剪切中 · reencode</span><b>{{ Math.round(aiProgress) }}%</b></div>
            <div class="pbar"><i :style="{ width: aiProgress + '%' }"></i></div>
          </template>
          <button v-if="aiTaskId && aiStage !== 'cutting'" class="btn btn-ghost" style="width:100%;justify-content:center;margin-top:8px;padding:8px"
            :disabled="aiBusy" @click="store.aiMerge">
            🔗 合并为单成片
          </button>
          <div v-if="aiStage === 'merging'">
            <div class="prog-row"><span>合并中</span><b>{{ Math.round(aiProgress) }}%</b></div>
            <div class="pbar"><i :style="{ width: aiProgress + '%' }"></i></div>
          </div>
          <button class="btn btn-ghost sm" style="width:100%;justify-content:center;margin-top:8px" :disabled="aiBusy || !aiWorkDir" @click="store.aiExport('md')">导出文字稿 (.md)</button>
          <button class="btn btn-ghost sm" style="width:100%;justify-content:center;margin-top:6px" :disabled="aiBusy || !aiWorkDir" @click="store.aiExport('txt')">导出文字稿 (.txt)</button>
          <p style="font-size:10.5px;color:var(--text3);margin-top:8px;line-height:1.6">账本先行复核、后物理剪切；映射为 TrimBatch 任务，共享全局信号量与取消能力</p>
        </div>
      </div>
    </div>

    <div v-else class="card empty-card">
      <p>选择视频与输出目录，点击「转录并生成候选账本」开始 AI 粗剪。</p>
      <p class="sub">或点击「载入已有 edits.json」复核既有账本</p>
    </div>

    <div v-if="aiMessage" class="msg">{{ aiMessage }}</div>
  </div>
</template>

<style scoped>
.aicut {
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
  flex-wrap: wrap;
}
.toolbar .sp { flex: 1; }
.hint { font-size: 12px; color: var(--text3); }
.paths { display: flex; gap: 10px; flex-wrap: wrap; }
.path-chip {
  font-size: 12px; color: var(--text2); background: var(--panel2);
  border: 1px solid var(--border); border-radius: 8px; padding: 7px 12px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex: 1; min-width: 0;
}
.path-chip b { color: var(--text); font-weight: 600; }
.path-chip i { color: var(--text3); font-style: normal; margin-right: 7px; font-size: 11px; }

.work { display: grid; grid-template-columns: minmax(0, 1fr) 300px; gap: 16px; align-items: start; }
.main { display: flex; flex-direction: column; gap: 14px; min-width: 0; }
.side { display: flex; flex-direction: column; gap: 14px; }

.card {
  background: var(--panel); border: 1px solid var(--border); border-radius: 12px; padding: 14px 16px;
}
.card > h3 {
  font-size: 13px; font-weight: 600; margin: 0 0 12px;
  display: flex; align-items: center; justify-content: space-between; gap: 8px;
}
.card > h3 .hint { font-size: 11px; color: var(--text3); font-weight: 400; }

.transcribing { display: flex; flex-direction: column; gap: 12px; }
.asr { display: flex; align-items: center; gap: 12px; }
.asr .spin {
  width: 34px; height: 34px; border-radius: 50%; flex: none;
  border: 3px solid var(--border); border-top-color: var(--accent);
  animation: spin 1s linear infinite;
}
@keyframes spin { to { transform: rotate(360deg); } }
.asr .info { flex: 1; min-width: 0; }
.asr .info .t1 { font-size: 13px; font-weight: 600; }
.asr .info .t2 { font-size: 11px; color: var(--text3); margin-top: 3px; }
.bar { height: 7px; border-radius: 5px; background: var(--panel2); overflow: hidden; margin-top: 9px; border: 1px solid var(--border); }
.bar i { display: block; height: 100%; border-radius: 5px; background: linear-gradient(90deg, var(--accent), var(--accent2)); transition: width .2s; }
.asr .pct { font-size: 17px; font-weight: 700; color: var(--accent); font-variant-numeric: tabular-nums; }
.sub { font-size: 11px; color: var(--text3); }

/* 账本表 */
table.ledger { width: 100%; border-collapse: collapse; font-size: 12px; }
table.ledger th {
  text-align: left; color: var(--text3); font-weight: 500; font-size: 11px;
  padding: 7px 8px; border-bottom: 1px solid var(--border); white-space: nowrap;
}
table.ledger td { padding: 8px; border-bottom: 1px solid var(--border); vertical-align: middle; }
table.ledger tr:last-child td { border-bottom: 0; }
table.ledger tr.dim td { opacity: .4; }
table.ledger tr:hover td { background: var(--accent-soft); }
.mono { font-variant-numeric: tabular-nums; font-family: Consolas, monospace; font-size: 11px; }
.tag.filler { background: rgba(242, 96, 106, .14); color: var(--danger); border-color: rgba(242, 96, 106, .35); }
.tag.silence { background: rgba(154, 164, 178, .14); color: var(--text2); }
.tag.retake { background: rgba(124, 92, 255, .15); color: var(--accent2); border-color: rgba(124, 92, 255, .35); }
.tag.manual { background: rgba(63, 185, 111, .14); color: var(--success); border-color: rgba(63, 185, 111, .35); }
.empty { color: var(--text3); font-size: 12px; text-align: center; padding: 18px; }

/* ===== M12 字幕（第三/四步） ===== */
.submodes { display: grid; grid-template-columns: repeat(3, 1fr); gap: 8px; margin-bottom: 12px; }
.submode {
  border: 1px solid var(--border); border-radius: 10px; padding: 9px 11px; cursor: pointer;
  transition: border-color .15s, background .15s;
}
.submode:hover { border-color: var(--accent); }
.submode.sel { border-color: var(--accent); background: var(--accent-soft); }
.submode .t { font-size: 12px; font-weight: 600; display: flex; align-items: center; justify-content: space-between; gap: 6px; }
.submode .d { font-size: 10.5px; color: var(--text3); margin-top: 4px; line-height: 1.5; }

.presets { display: flex; gap: 6px; flex-wrap: wrap; margin-bottom: 12px; }
.preset {
  font-size: 11px; color: var(--text2); border: 1px solid var(--border); border-radius: 999px;
  padding: 4px 11px; cursor: pointer; transition: all .15s;
}
.preset:hover { border-color: var(--accent); color: var(--text); }
.preset.sel { border-color: var(--accent); background: var(--accent-soft); color: var(--accent); font-weight: 600; }

.numrow { display: flex; align-items: center; gap: 14px; flex-wrap: wrap; margin-bottom: 12px; font-size: 11.5px; color: var(--text2); }
.numrow label { display: flex; align-items: center; gap: 6px; }
.numrow input {
  width: 58px; background: var(--panel2); border: 1px solid var(--border); border-radius: 7px;
  padding: 5px 8px; color: var(--text); font-size: 12px; font-variant-numeric: tabular-nums;
}
.numrow input:disabled { opacity: .5; }

table.cue-table { width: 100%; border-collapse: collapse; font-size: 12px; margin-bottom: 12px; }
table.cue-table th {
  text-align: left; color: var(--text3); font-weight: 500; font-size: 11px;
  padding: 7px 8px; border-bottom: 1px solid var(--border); white-space: nowrap;
}
table.cue-table td { padding: 7px 8px; border-bottom: 1px solid var(--border); vertical-align: middle; }
table.cue-table tr:last-child td { border-bottom: 0; }
table.cue-table tr:hover td { background: var(--accent-soft); }
.cue-split { color: var(--warning); }
.split-flag { margin-right: 5px; }

.sub-actions { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-bottom: 10px; }
.sub-actions .btn { flex: 0 1 auto; }
.sub-actions .hint { flex: 1; min-width: 120px; }

.out-items { display: flex; gap: 8px; flex-wrap: wrap; }
.out-item {
  display: flex; align-items: center; gap: 7px; font-size: 11px; background: var(--panel2);
  border: 1px solid var(--border); border-radius: 8px; padding: 6px 10px; min-width: 0; max-width: 100%;
}
.out-item i { color: var(--text3); font-style: normal; font-size: 10px; letter-spacing: .4px; }
.out-item .path { color: var(--text2); font-family: Consolas, monospace; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.out-item.ok { border-color: rgba(63, 185, 111, .4); }
.out-item.ok i { color: var(--success); }

.sub-note { font-size: 10.5px; color: var(--text3); line-height: 1.6; margin: 8px 0 0; }

.dims { display: flex; flex-direction: column; gap: 6px; margin-bottom: 12px; }
.dim { display: grid; grid-template-columns: 72px 1fr 76px 92px; align-items: center; gap: 8px; font-size: 11px; }
.dim .k { color: var(--text3); }
.dim .bar { height: 6px; border-radius: 4px; background: var(--panel2); border: 1px solid var(--border); overflow: hidden; }
.dim .bar i { display: block; height: 100%; background: linear-gradient(90deg, var(--accent), var(--accent2)); }
.dim .v { color: var(--text); font-variant-numeric: tabular-nums; text-align: right; }
.dim .kw { color: var(--text3); font-size: 10px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.skills-label { font-size: 10.5px; color: var(--text3); margin: 8px 0 6px; }
.chips { display: flex; gap: 6px; flex-wrap: wrap; }
.chip {
  font-size: 11px; border-radius: 999px; padding: 3px 11px; font-family: Consolas, monospace;
  border: 1px solid var(--border); color: var(--text2);
}
.chip.on { border-color: rgba(63, 185, 111, .45); color: var(--success); background: rgba(63, 185, 111, .1); }
.chip.off { opacity: .55; text-decoration: line-through; }

.keep-strip {
  height: 22px; border-radius: 6px; background: var(--panel2); border: 1px solid var(--border);
  position: relative; overflow: hidden; margin-top: 12px;
}
.keep-strip .seg-keep { position: absolute; top: 0; bottom: 0; background: linear-gradient(135deg, var(--accent), var(--accent2)); opacity: .85; border-radius: 4px; }
.keep-strip .seg-del { position: absolute; top: 0; bottom: 0; background: rgba(242, 96, 106, .28); border-left: 1px solid var(--danger); border-right: 1px solid var(--danger); }
.keep-legend { display: flex; gap: 14px; font-size: 10px; color: var(--text3); margin-top: 7px; flex-wrap: wrap; }
.keep-legend i { display: inline-block; width: 9px; height: 9px; border-radius: 3px; margin-right: 5px; vertical-align: -1px; }

.kv { display: flex; justify-content: space-between; font-size: 12px; padding: 5px 0; }
.kv .k { color: var(--text3); }
.kv .v { color: var(--text); font-variant-numeric: tabular-nums; }

.strategy { border: 1px solid var(--accent); border-radius: 10px; padding: 10px 11px; background: var(--accent-soft); }
.strategy .t { font-size: 12.5px; font-weight: 600; display: flex; align-items: center; justify-content: space-between; }
.strategy .d { font-size: 11px; color: var(--text3); margin-top: 4px; line-height: 1.5; }
.radio { width: 14px; height: 14px; border-radius: 50%; border: 2px solid var(--accent); background: var(--accent); box-shadow: inset 0 0 0 2.5px var(--panel); flex: none; }

.output { display: flex; align-items: center; gap: 8px; background: var(--panel2); border: 1px solid var(--border); border-radius: 8px; padding: 8px 10px; font-size: 11px; color: var(--text2); }
.output .path { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: Consolas, monospace; }
.prog-row { display: flex; justify-content: space-between; font-size: 12px; margin: 10px 0 8px; color: var(--text2); }
.prog-row b { color: var(--accent); font-variant-numeric: tabular-nums; }

.warn-box { border: 1px solid rgba(232, 163, 61, .35); background: rgba(232, 163, 61, .09); border-radius: 9px; padding: 10px 12px; font-size: 11.5px; color: var(--warning); line-height: 1.6; margin-bottom: 12px; }
.warn-box b { color: var(--warning); }

.empty-card { text-align: center; color: var(--text3); padding: 30px 16px; }
.empty-card p { margin: 0; font-size: 13px; }
.empty-card .sub { font-size: 11.5px; margin-top: 6px; }
.msg { font-size: 12px; color: var(--text2); background: var(--panel2); border: 1px solid var(--border); border-radius: 8px; padding: 9px 12px; }
</style>
