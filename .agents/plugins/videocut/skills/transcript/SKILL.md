---
name: transcript
description: 视频转录与文字稿。当用户要求"转文字/出稿子/提取台词/看看视频里说了什么"时使用。流程：转录（videocut_process）→ 导稿（videocut_export_transcript，md/txt）→ 交付。产物：transcript.srt + 文字稿文件。
---

# 转录与导稿（transcript）

把视频语音转成文字稿。**只读不剪**——本技能不动画面，只产出文字；
若用户随后要"按稿子剪"，转交 cut-talking-head 技能。

## 前置检查

1. `videocut_probe({input_path})` 确认文件存在、有音频流。
   若 `videocut_process` 返回 `asr_runtime_missing`：**停下**，提示用户先运行
   `powershell -ExecutionPolicy Bypass -File scripts/bootstrap-asr.ps1`
   （首次下载模型约 0.5GB，国内自动走 hf-mirror），不要自行替代转录实现。

## 工作流

1. **设热词**（推荐）：`videocut_set_hotwords({hotwords: ["专名1","专名2"]})`
   —— 人名、产品名、频道名，显著提升识别准确率。
2. **转录**：`videocut_process({video_path, output_base, hotwords?})`
   → 产出 `<output_base>/work/transcript.srt`（带时间轴）+ `signals.json`（静音/口水词信号）。
   - 长视频（>10 分钟）先告知预计耗时：CPU int8 约 0.5–1.5× 实时。
   - 转录失败时把错误原文报告给用户，**不要重试超过 2 次**（重试不解决模型/显存问题）。
3. **导稿**：`videocut_export_transcript({base_dir, format})`
   - `format:"md"`（默认）：Markdown，按段落合并、带说话停顿标记，适合阅读/二次编辑。
   - `format:"txt"`：纯文本，适合粘贴进其他工具。
4. **质量自查**：读一遍稿子，检查专名是否识别正确（如"ChatGPT"→"查特基皮蒂"）。
   错误较多时：补热词重跑第 1–2 步，或在交付稿中附"已知识别错误"清单。
5. **交付**：告知用户稿子路径 + 字数/时长概览。

## 边界与注意事项

- 转录结果是**带时间轴的 SRT**；导稿只是渲染，不修改 SRT 本身。
- 无音频流的文件：probe 阶段就发现并拒绝，不要调 process。
- 本技能不裁剪、不烧字幕；需要成片走 cut-talking-head，需要字幕文件走 subtitle。
