# Codex 集成指南（videocut MCP + Plugin）

> 🤖 **本文定位：外部集成操作手册（Integration Runbook）** —— 把 videocut 的 MCP Server 与 Plugin 接进 Codex 的**步骤、配置、故障排查**。
> 规格依据见《开发文档》§19（含 §19.13 本地化决策依据、§19.14 字幕可行性分析）；实施排期见《开发规划》M8–M12；质量证据见《测试报告》§7（M12 见 §7.9）。
>
> 版本：v1.3（2026-10-15）｜对应开发文档 §19 / 开发规划 M8–M12（均已落地；M12 字幕族 3 工具已含于 15 工具）
> 适用：Windows 本地环境，无 GPU，CPU int8 推理

---

## 1. 架构总览

```
┌─────────────┐   stdio JSON-RPC   ┌──────────────────────┐   HTTP 127.0.0.1:4178   ┌─────────────────┐
│   Codex     │ ◄────────────────► │  videocut.exe        │ ◄─────────────────────► │  asr/serve.py   │
│  (Agent)    │   15 个 MCP 工具   │  mcp-serve 子命令    │   /transcribe /hotwords │ faster-whisper  │
└─────────────┘                    │  (Go，复用 service)  │                         │  small + int8   │
                                   └──────────┬───────────┘                         └─────────────────┘
                                              │ ffmpeg（exe 同级自定位）
                                              ▼
                                      剪切 / 合并 / 探测
```

- **零上传**：视频、模型、产物全程本地；sidecar 仅监听回环地址。
- **MCP Server**：`videocut.exe mcp-serve`，协议帧走 stdout，**日志全部走 stderr**（R26）。
- **ASR sidecar**：Python 服务按需拉起（`EnsureRunning` 复用已运行实例），空闲 10 分钟自动回收。

## 2. 安装（三步）

### 2.1 构建 videocut.exe

```powershell
cd E:\videocut
wails build            # 产物：build/bin/videocut.exe（含 ffmpeg，copy-ffmpeg.ps1 已内置）
```

### 2.2 引导 ASR 环境（首次，约 0.5GB 模型下载）

```powershell
powershell -ExecutionPolicy Bypass -File scripts\bootstrap-asr.ps1
```

脚本自动完成：`HF_HOME` → E 盘（R23）→ 定位 Python 3.12 → 建 venv（`asr/.venv`）→ 装 faster-whisper → **依赖兼容自检** → 预热模型（`asr/warmup.py`）。

**网络自适应**：
- huggingface.co 不可达自动切 `hf-mirror.com`；代理对 PyPI 失效自动清除代理环境变量后回退清华/阿里镜像；单策略硬超时（pip 300s / 预热 1200s）。
- **禁用 Xet**：自动设置 `HF_HUB_DISABLE_XET=1`。Xet 存储后端不走 `HF_ENDPOINT`，镜像场景下会直连 `cas-server.xethub.hf.co` 并 401，导致下载必失败。

**幂等自愈**：
- 已装 `faster-whisper` 则跳过联网安装（弱网下 pip 子进程可能整体挂起，重跑不应再冒网络风险）。
- **av 兼容自检**（`asr/compat_check.py`）：faster-whisper 依赖声明里 `av` 无版本上界，pip 会装最新 av（≥15 已移除 `av.open(metadata_errors=...)`），表现为"装完看似成功、一转录就 500"。自检不通过会自动降级到 `av==14.2.0`。

模型已缓存后可用离线模式启动 sidecar，避免每次联网校验 revision：

```powershell
$env:HF_HOME = "E:/models/hf"
$env:HF_HUB_OFFLINE = "1"
asr\.venv\Scripts\python.exe asr\serve.py --port 4178
```

### 2.3 启用 ASR

编辑 exe 同级 `config.yaml`（或环境变量）：

```yaml
asr:
  enabled: true          # 或 VIDEOCUT_ASR_ENABLED=true
  host: 127.0.0.1        # sidecar 仅回环
  port: 4178             # sidecar 回环端口
  model: small           # small | large-v3
  compute_type: int8     # CPU 固定 int8
  hf_home: E:/models/hf  # 模型缓存目录（C 盘空间不足时重定向）
  hotwords: []           # 默认热词表
```

> **注意**：asr 段采用**嵌套块**写法。该段由 `config.Load()` 中的 `applyASRFile` 显式映射
> （viper 的 `Unmarshal` 无法处理带点号的扁平 mapstructure 标签，历史上曾导致该段静默失效）。
> 未配置的键保持默认值，无需写全。

## 3. 接入 Codex（二选一）

### 3.1 方式 A：config.toml 直连 MCP（推荐，改路径即可）

在 `~/.codex/config.toml` 加：

```toml
[mcp_servers.videocut]
command = "E:/videocut/build/bin/videocut.exe"
args = ["mcp-serve"]
enabled = true
startup_timeout_ms = 30000
tool_timeout_sec = 300
```

| 参数 | 值 | 说明 |
|---|---|---|
| `startup_timeout_ms` | 30000 | Go 二进制 <1s 启动，留足余量 |
| `tool_timeout_sec` | 300 | CPU 无 GPU，长片转录/重编码需要 |
| 路径 | 全正斜杠 | 规避 TOML 反斜杠转义坑 |

### 3.2 方式 B：本地 Plugin marketplace（ skills 随插件分发）

本仓库已按 Codex 插件规范组织（`.agents/plugins/`）：

```
.agents/plugins/
├── marketplace.json              # 市场清单（name: videocut-local）
└── videocut/
    ├── plugin.json               # 插件清单（.codex-plugin/plugin.json 为兼容镜像）
    ├── .mcp.json                 #  bundled MCP（command 指向本机 build/bin/videocut.exe）
    └── skills/                   # 4 个技能（自包含，安装时整体复制）
        ├── cut-talking-head/     # 口播 AI 粗剪
        ├── transcript/           # 转录 + 文字稿
        ├── export/               # 精确片段导出拼接
        └── subtitle/             # SRT 字幕稿清理
```

在 Codex 中执行：

```text
/plugin marketplace add E:/videocut/.agents/plugins
/plugin install videocut@videocut-local
```

> `.mcp.json` 里的 `command` 是**本机绝对路径**；换机器构建需同步修改
> （方式 A 的 config.toml 同理）。

## 4. 技能与触发词

| 技能 | 触发词示例 | 产物 |
|---|---|---|
| `cut-talking-head` | "粗剪这个口播""删掉嗯呃啊""去静音" | edits.json（人工复核）+ 剪切后视频 |
| `transcript` | "转文字""出稿子""视频里说了什么" | transcript.srt + md/txt 文字稿 |
| `export` | "保留 0:10–0:35 和 1:02–1:30 拼起来" | 片段 + 合并成片 |
| `subtitle` | "顺一下字幕""清理错别字""交付 srt" | subtitle_clean.srt |

**核心安全约定**：cut-talking-head 强制"先复核账本、后物理剪切"——Codex 生成
edits.json 后必须经人工确认才执行 trim_batch（对齐 chengfeng-videocut 理念）。

## 5. 工具速查（15 个）

| 工具 | 关键参数 | 备注 |
|---|---|---|
| `videocut_probe` | input_path | 元数据 + 登记预览白名单 |
| `videocut_trim` | input_path, start, end, strategy? | 异步，返回 task_id |
| `videocut_trim_batch` | items[] | 原子提交；strategy 默认 reencode |
| `videocut_merge` | items[], mode? | auto/demuxer/filter |
| `videocut_get_task` | task_id | 轮询至 completed/partial/failed |
| `videocut_cancel` | task_id | 未完成项取消，已完成产物保留 |
| `videocut_process` | video_path, output_base, hotwords? | ASR 转录 + 静音检测；sidecar 固定输出词级时间戳 `signals.json.words[]` |
| `videocut_suggest_edits` | base_dir, min_silence? | 机械候选（口水词/静音） |
| `videocut_set_hotwords` | hotwords[] | 提升专名识别 |
| `videocut_export_subtitles` | base_dir, edits_json?, output_dir?, format?, style_preset?, max_chars_per_cue?, max_lines? | 字幕重对齐 + 外显导出；`format=srt\|ass\|both` 由调用方指定 |
| `videocut_burn_subtitles` | video_path, ass_path, output_path?, encoder?, crf?, preset? | 内嵌烧录（ass 滤镜重编码，音轨直复制；异步 task_id）；硬件编码失败自动回退 `libx264` |
| `videocut_animate_captions` | base_dir, edits_json?, output_dir?, mode?, skills?, max_skills? | 二次加工：规则引擎自动选特效 → edited_fx.ass + motion-plan.json |
| `videocut_validate_edits` | edits_json, source_duration? | 合法性 + 统计 |
| `videocut_get_edits_summary` | edits_path, source_duration? | 删除统计 + 保留段补集 |
| `videocut_export_transcript` | base_dir, format? | SRT → md/txt |

## 6. 典型对话（Codex 端到端）

```text
用户：把 E:/media/talk.mp4 粗剪一下，删掉口水词

Codex：[videocut_probe] 05:12, 1920x1080
       [videocut_process] 转录中（small+int8，CPU 约 0.6×实时）…
       [videocut_suggest_edits] 候选：口水词 23 处 / 静音 8 处
       [videocut_validate_edits] + [videocut_get_edits_summary]
       → 呈现账本表格，等待确认
用户：确认
Codex：[videocut_trim_batch] keep_ranges×N（reencode）→ task_id
      [videocut_get_task] 轮询至 completed
       → 产物 E:/media/talk_cut.mp4（04:37，压缩 15.4%）
```

字幕交付可在成片完成后继续执行：

```text
Codex：[videocut_export_subtitles format=both] → edited.srt + edited.ass
       [videocut_animate_captions mode=auto] → edited_fx.ass + motion-plan.json
       [videocut_burn_subtitles ass_path=edited_fx.ass] → task_id
       [videocut_get_task] 轮询至 completed
```

MCP 的 `format` 由调用方决定；GUI 的 `burn/both` 为保证烧录输入和交付物完整，会强制 `both`。字幕工具应在剪切并合并成片之后调用，否则字幕时间轴仍对应源视频。

## 7. 故障排查

| 现象 | 原因 | 处理 |
|---|---|---|
| 转录工具返回 `asr_runtime_missing` | venv/模型未就绪 | 运行 `scripts\bootstrap-asr.ps1`；确认 `asr.enabled=true` |
| MCP 无响应 / 协议解析错误 | stdout 被日志污染 | 确认以 `mcp-serve` 启动（GUI 模式不暴露 MCP） |
| 长视频工具超时 | 300s 不够 | 上调 `tool_timeout_sec`；或先用 export 技能分段 |
| `partial` 任务状态 | 批量中个别项失败 | `videocut_get_task` 看逐项 error，只重提失败项 |
| 模型下载卡住 | 网络/镜像 | bootstrap 已内置 hf-mirror 回退；手动设 `HF_ENDPOINT=https://hf-mirror.com` |
| 下载报 `401 ... cas-server.xethub.hf.co` | Xet 后端绕过镜像 | 设 `HF_HUB_DISABLE_XET=1`（bootstrap 已自动设置） |
| 转录返回 500，日志 `TypeError: open() got an unexpected keyword argument 'metadata_errors'` | `av` 版本过高 | 运行 `asr\compat_check.py`；执行 `pip install "av==14.2.0"`（bootstrap 第 4b 步会自动修） |
| 模型已缓存却仍联网校验失败 | `snapshot_download` 会先查 revision | 启动时设 `HF_HUB_OFFLINE=1` |
| 剪切点偏移数秒 | 误用 stream_copy | 删词场景必须 `reencode`（关键帧吸附） |
| `合并至少需要 2 个片段` | 保留段只剩 1 段 | v1.8 已修复：单段自动 remux 成终片，不再报错 |

## 8. 真实环境验证（可选）

引导完成后，可用 opt-in 端到端测试验证"真实 Whisper 输出 → 候选扫描 → 保留段剪切 → 合并成片"这条接缝：

```powershell
# 终端 A：起真实 sidecar（离线加载缓存模型）
$env:HF_HOME = "E:/models/hf"; $env:HF_HUB_OFFLINE = "1"
asr\.venv\Scripts\python.exe asr\serve.py --port 4178

# 终端 B：跑测试
$env:VIDEOCUT_REAL_ASR = "1"
go test . -run TestRealASR -v -timeout 15m
```

默认素材 `asr/testdata/sample.mp4` 为合成片（音调段 + 静音段、无人声），
测试会显式关闭 VAD（否则 Whisper 的 VAD 会把合成音整段滤空、0 片段 0 候选）。
真实人声素材可用 `VIDEOCUT_ASR_TEST_VIDEO=<路径>` 指定。

## 9. 与输入文档 v2.2.0 的差异

见开发文档 §19.12（载体 Go 单体、15 工具、300s 超时、HF_HOME→E 盘、
Studio 审核改编辑器/M11 可选）与 §19.13（本地化决策依据）。
