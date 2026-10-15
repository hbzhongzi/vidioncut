---
name: cut-talking-head
description: 中文口播视频粗剪。当用户要求"粗剪/剪口播/删词/去静音/去口水词"一段说话视频时使用。流程：转录 → 候选扫描 → 生成删词账本 → 校验 → 摘要评估 → 人工复核 → 按保留段精确剪切 → 合并成片 →（可选）字幕重对齐/特效/烧录 → 导稿。产物：edits.json（人工复核过）+ 剪切后视频 + 成片字幕。
---

# 剪口播（cut-talking-head）

对中文口播视频做"删词式"粗剪：删掉口水词（嗯/呃/那个/然后…）、静音段、重拍段，
保留段重新拼接成片。**先复核账本、后物理剪切**——账本未经人工确认不得执行 cut。

## 前置检查

1. 确认 `videocut` MCP 工具可用（`videocut_probe` 能返回元数据）。
   若转录类工具返回 `asr_runtime_missing`：**停下**，提示用户运行
   `powershell -File scripts/bootstrap-asr.ps1`（首次下载模型约 0.5GB），不要自行替代转录实现。
2. `videocut_probe({input_path})` 确认时长/分辨率，把时长记下来（账本校验要用）。

## 工作流（严格按序）

1. **设热词**（可选）：`videocut_set_hotwords({hotwords: ["专名1","专名2"]})`
   —— 频道名、产品名、人名等，提升 ASR 识别。
2. **转录**：`videocut_process({video_path, output_base})`
   → 产出 `<output_base>/work/transcript.srt` + `signals.json`。
   长视频（>10 分钟）先告知用户预计耗时（CPU 无 GPU 时约 0.5–1.5× 实时）。
3. **候选扫描**：`videocut_suggest_edits({base_dir})`
   → 返回候选 items（silence / filler_word）与统计。**这是机械候选，不是最终决定。**
4. **生成账本**：读 transcript.srt 与候选，逐条判断后写出 `<output_base>/work/edits.json`：

   ```json
   {"version":1,"source":"<video_path>","items":[
     {"start":12.4,"end":13.1,"type":"filler_word","text":"嗯","reason":"口水词"},
     {"start":45.0,"end":47.5,"type":"silence","reason":"静音>1.5s"}
   ]}
   ```

   规则：items 按 start 升序、不重叠、不越界；type ∈ filler_word/silence/retake/manual。
   可以比 suggest 删得更少（保守）或更多（retake 整段重说），但**必须来自你的语义判断**。
5. **校验**：`videocut_validate_edits({edits_json, source_duration})`
   返回 `edits_invalid` 时按逐项定位错误修正后重试。
6. **摘要评估**：`videocut_get_edits_summary({edits_path, source_duration})`
   向用户汇报：删除 N 项 / 删除 Xs / 保留段 M 段 / 产物时长 Ys / 压缩比 Z%。
7. **人工复核**：把账本表格呈现给用户（起止/类型/文本/理由），**等待确认**。
   用户要求调整时改 edits.json 并重跑 5–6 步。**未确认不得进入下一步。**
8. **剪切**：确认后把 summary 返回的 keep_ranges 逐段作为 items 调
   `videocut_trim_batch`（strategy 固定 `reencode`——删词必须精确切分，
   stream_copy 的关键帧吸附会让删除点偏移数秒）。
   返回 task_id 后用 `videocut_get_task` 轮询至 completed/partial/failed。
9. **合并成片**（多保留段时）：用剪切产物调 `videocut_merge`（mode=auto）得单成片。
10. **字幕**（成片后可选，默认外显——核心场景是剪映/PR 精修）：
    - `videocut_export_subtitles({base_dir})` → 成片时间轴的 `edited.srt` + `edited.ass`
      （删除段内容已剔除、保留段已平移、跨段边界词级断开；跨段条目建议人工复核断点语义）。
    - `videocut_animate_captions({base_dir})` → 规则引擎自动选特效（≤2 个），
      产出 `edited_fx.ass` + `motion-plan.json`（选择理由可查；不想要的特效由用户在
      config `subtitles.disabled_skills` 禁用，或以 `skills` 参数显式覆盖）。
    - `videocut_burn_subtitles({video_path, ass_path})` → 内嵌烧录（重编码，
      音轨直复制）；**仅确需直接分发时才烧录**，精修一律走外显。
11. **导稿**：`videocut_export_transcript({base_dir, format:"md"})` 生成文字稿。

## 边界与注意事项

- **区间越界/重叠**：账本校验会逐项报错；修正时优先保障"不重叠"。
- **partial 终态**：批量 cut 出现 partial 说明部分保留段失败——好项产物已保留，
  把失败项定位给用户，可只对失败段重提 trim。
- **取消**：用户喊停时 `videocut_cancel({task_id})`，仅取消未完成段。
- **transcript.srt 可直接读**：Codex 有文件读取能力，不需要把内容贴进对话。
