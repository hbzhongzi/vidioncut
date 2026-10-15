---
name: export
description: 精确片段导出与拼接。当用户给出明确的起止时间清单（"保留 0:10-0:35、1:02-1:30，拼成一个视频"）或要求导出单个片段时使用。流程：probe 校验 → trim_batch 批量裁剪 → merge 合并 → 轮询交付。产物：剪切后片段 + 合并成片。
---

# 片段导出（export）

按**明确的时间清单**做物理裁剪与拼接。与 cut-talking-head 的区别：
本技能处理"用户已经知道要哪些段"的场景（时间精确、无需语义判断）；
粗剪决策走 cut-talking-head。

## 前置检查

1. `videocut_probe({input_path})` 逐项核对：
   - 所有 start/end 在 `[0, duration]` 内，start < end；
   - 分辨率/帧率/编码（决定 strategy 选型，见下）。
2. strategy 选择：
   - `stream_copy`：不重编码，秒级完成，但切点会**吸附到最近关键帧**（偏移可达数秒）。
     仅当用户接受粗略切点（如按整段/章节切）时使用。
   - `reencode`：帧精确，耗时 ≈ 片段时长 × 0.3–1。**默认选它**，除非用户明确要快。
   - `audio_only`：只要音频轨。

## 工作流

1. **裁剪**：`videocut_trim_batch({items:[{input_path,start,end,strategy,output_path?}...]})`
   - 原子提交：任一项非法整批拒绝，按错误信息逐项修正。
   - 每段显式给 `output_path`（如 `out/seg01.mp4`），避免自动派生名难追溯。
   - 返回 task_id。
2. **轮询**：`videocut_get_task({task_id})` 直至 `completed/partial/failed`。
   - `partial`：读逐项结果，失败的段单独重提；**已完成项产物保留**，不要整批重来。
   - `failed`：读 error 字段定位（通常是输出已存在——加 `overwrite:true` 或改名）。
3. **合并**（≥2 段才需要）：`videocut_merge({items:[{input_path}...], mode, output_path})`
   - items 顺序即成片顺序；每段可用 start/end 再精修。
   - `mode:"auto"`（默认）：参数一致走 demuxer 流复制（快）；不一致自动重编码（稳）。
   - 再次 `videocut_get_task` 轮询至终态。
4. **交付**：报告成片路径、段数、总时长、各段耗时。

## 边界与注意事项

- **trim_batch 的 items 不能重叠校验源文件**——同一输入的多个不重叠段是合法的
   （如从同一段素材切三刀），这正是批量裁剪的设计场景。
- 输出已存在时默认自动追加序号（`_1`、`_2`），不会覆盖用户文件；
   `overwrite:true` 才会覆盖，慎用。
- 合并前确认所有片段的**分辨率/帧率一致**；不一致时 auto 模式会自动重编码兜底，
  但最好在裁剪阶段就统一（同源素材天然一致）。
- 单段导出（用户只要一个片段）用 `videocut_trim` 即可，不必走批量。
