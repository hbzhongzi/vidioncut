> 📘 **本文定位：上手向导** —— 5 分钟看懂项目、把它跑起来、用起来。  
> 权威规格（架构/契约/算法/决策依据）见《开发文档》；执行排期与 DoD 见《开发规划》；测试证据见《测试报告》；Codex AI 粗剪操作见《Codex集成指南》。
>
> 文档家族（五类，各司其职）：README（上手）· 开发文档（规格）· 开发规划（台账）· 测试报告（证明）· Codex集成指南（手册）。

# VideoCut · 音视频裁剪器 — 项目说明文档

> Windows 单平台 · 前后端分离（Wails v2）· FFmpeg 媒体引擎 · 随程序分发零配置
>
> 文档版本：v1.10（2026-10-15：M8–M11 落地 + ASR 真实打通 + M12 字幕功能全栈落地与本轮回归修复（重对齐/外显/烧录/二次加工，15 MCP 工具））  
> 配套：`UI设计稿_v2_AI粗剪.html`（AI 粗剪 / Codex 集成界面设计稿，对应 M8–M11 前端规格）、`UI设计稿_v3_字幕.html`（M12 字幕功能设计稿，对应《字幕功能更新文档》）
>
> **功能范围**：单文件裁剪 + 批量裁剪 + 合并/裁剪后合并 + AI 粗剪（ASR 转录 → 删词账本复核 → 保留段剪切 → **成片字幕重对齐/外显/烧录/自动特效**）+ Codex MCP 集成（均已实现）

---

## 1. 项目定位

VideoCut 是一个面向 Windows 桌面的**轻量音视频分割裁剪工具**。用户在原生桌面窗口（WebView2 渲染的前端）中选择本地媒体文件，通过时间轴拖拽裁剪区间并实时预览，由 Go 后端调用本地 FFmpeg 完成分割，结果直接写回本地磁盘。

核心设计取舍：

| 维度   | 决策                               | 理由                                                                                   |
| ---- | -------------------------------- | ------------------------------------------------------------------------------------ |
| 桌面形态 | **Wails v2**（非 Electron）         | 产物单 exe 约 15 MB，Go 原生绑定紧密；Electron 100 MB+ 对"只做裁剪"是过度工程                              |
| 通信方式 | **Wails Binding + 事件总线**（非 HTTP） | 进程内直连，无端口、无 CORS、无防火墙弹窗                                                              |
| 媒体引擎 | FFmpeg（随程序分发）                    | Windows 无包管理器，二进制放 `resources/ffmpeg/`，构建时复制到产物同目录，`os.Executable()` 定位，用户零配置        |
| 裁剪策略 | 三策略（流复制/重编码/仅音频）                 | 流复制快且无损；重编码帧精确；纯音频文件兜底                                                               |
| 任务模型 | 异步任务 + 内存状态机                     | 单机场景无需外部队列；信号量限制并发 ≤ 配置值；单文件/批量/合并统一由 `Task`（`Kind`/`Items`）承载，批量终态含 `partial`（部分成功） |

---

## 2. 技术栈

| 层                 | 选型                             | 版本/说明                                                      |
| ----------------- | ------------------------------ | ---------------------------------------------------------- |
| 桌面框架              | Wails v2                       | WebView2 渲染，`wails build` 产出单 exe                          |
| 前端框架              | Vue 3 + TypeScript             | 响应式状态适合时间轴交互                                               |
| 前端构建              | Vite                           | 开发热更新快                                                     |
| UI 组件             | 无（自绘组件）                        | v1.6 起移除 Element Plus，按钮/表格/开关/进度条等全部自绘，深色主题 CSS 变量        |
| 状态管理              | Pinia                          | 任务 map + 当前区间 + 配置                                         |
| 后端语言              | Go                             | `go.mod` module `videocut`                                 |
| 媒体引擎              | FFmpeg / FFprobe               | gyan.dev release-essentials 静态构建，已随仓库分发                    |
| 配置                | Viper                          | `config.yaml`（可选）+ 环境变量 `VIDEOCUT_*` 覆盖                    |
| 并发控制              | `golang.org/x/sync/semaphore`  | 限制同时运行的 FFmpeg 进程数                                         |
| 日志/进度             | FFmpeg `-progress` 管道          | 结构化 `out_time=`/`progress=` 行，与控制台代码页无关                    |
| ASR 引擎（v1.7+）     | faster-whisper（Python sidecar） | 本地 venv，small+int8 CPU 推理；`scripts/bootstrap-asr.ps1` 一键引导 |
| MCP Server（v1.7+） | mcp-go（stdio）                  | `videocut.exe mcp-serve` 暴露 15 工具（含 M12 字幕族 3），供 Codex CLI 驱动同一套剪切能力    |

---

## 3. 架构与数据流

```
┌─────────────────────────────────────────────────────────────┐
│  WebView2 前端（Vue3 + TS + 自绘组件，深色主题，嵌入 exe）       │
│                                                             │
│  App.vue ── 页签（单文件/批量/合并/AI粗剪/Codex）+ 快捷键 + 拖放路由 │
│           ── Timeline.vue（刻度尺/手柄拖拽/mm:ss.s 输入）       │
│           ── Preview.vue（<video> 预览 + 控制条 + 选区播放）    │
│           ── BatchPanel.vue / MergePanel.vue（v1.5 新增）     │
│           ── AICutPanel.vue（AI 粗剪四步：账本/剪切/字幕/特效） │
│  store/trim.ts（Pinia）── wails/bridge.ts（window.go 封装）    │
└──────────┬──────────────────────────────────────────────────┘
           │ Wails Binding（方法调用，Promise 化）
           │ Wails Events（trim:progress / trim:item_done / trim:done
           │               trim:error / merge:notice）
           ▼
┌─────────────────────────────────────────────────────────────┐
│  Go 后端（app.go 绑定层）                                     │
│                                                             │
│  App.Trim / TrimBatch / Merge / GetTask / Cancel / Probe /   │
│      SelectFile / RevealInFolder / ConfigGet / ConfigSet +   │
│      app_v17.go：ASR 状态/AI 粗剪绑定（复用 mcpserver 逻辑）  │
│      app_v18.go：字幕绑定（导出/烧录/特效，复用 mcpserver 逻辑）│
│           │                                                 │
│  internal/service                                            │
│    trim.go  ── runItem 单项执行器（单文件/批量/hybrid 共用）    │
│    batch.go ── 原子预校验 / 逐项调度 / 加权聚合进度 / partial  │
│    merge.go ── auto 判定 / ASCII 规范化 / demuxer→filter 回退 │
│    校验 → 探测 → 信号量 → 超时 Kill → 同卷 rename → 事件推送    │
│           │                                                 │
│  internal/ffmpeg（path/executor/probe/builder/progress        │
│                  + concat.go）                              │
│    os.Executable() 定位 → exec.CommandContext → -progress     │
│    concat.go：list 转义 / detectMergeMode / buildConcatFilter │
│           │                                                 │
│  internal/server/preview.go（AssetServer Handler）            │
│    /preview?path=... → http.ServeContent（支持 Range）        │
└────────────────────────────────────────────────────────────┘
           │ os/exec
           ▼
   build/bin/ffmpeg/ffmpeg.exe、ffprobe.exe（随程序分发）
```

**关键实现决策**（详见开发文档 v1.1/v1.3 修订记录）：

- **预览通道**：WebView2 前端无本地文件系统权限，不能用路径字符串 `createObjectURL`。改为 Go 侧 AssetServer 自定义 Handler 拦截 `/preview?path=`，用 `http.ServeContent` 流式返回（自动支持 Range，视频可拖动）。Wails 策略：**先查嵌入资源，未命中再回退 Handler**，故前端静态资源不受影响。
- **进度解析**：`-nostats -progress pipe:1`，从 stdout 读结构化键值行。Windows 控制台 GBK 下 stderr 会乱码，故不解析 stderr 日志；`LANG` 环境变量在 Windows 无效，已弃用。
- **同卷落地**：临时目录建在**输出文件所在卷**（`os.MkdirTemp(filepath.Dir(out), ...)`），先写 `.part` 再同卷 `rename`，规避跨盘 rename 失败；极少数跨卷场景回退 copy。
- **取消语义**：pending/processing 均可取消；`Cancel` 置 `Canceled` 标志并只发一次"已取消"，`run()` 在各失败分支检测该标志后直接收尾，避免二次错误文案。

---

## 4. 目录结构

```
E:\videocut\
├── internal/
│   ├── config/config.go         # Viper 配置：默认值 + config.yaml（含 asr 段显式映射）+ VIDEOCUT_* 覆盖
│   ├── ffmpeg/
│   │   ├── path.go              # 二进制定位：bundled → system(PATH) → dev 源码树回退
│   │   ├── executor.go          # exec.CommandContext + -progress 解析 + 超时 Kill
│   │   ├── probe.go             # ffprobe JSON 解析（纯音频 width/height 指针兜底）
│   │   ├── builder.go           # 三策略命令构造 + concat list/filter 构造
│   │   ├── concat.go            # list 转义 / detectMergeMode / buildConcatFilter
│   │   └── progress.go          # -progress 协议解析 + 安全缓冲
│   ├── service/
│   │   ├── task.go              # 任务模型 + 状态机 + 输出路径派生/防覆盖（Kind/Items/partial）
│   │   ├── trim.go              # 业务编排：runItem 单项执行器 + 单文件 Trim
│   │   ├── batch.go             # 批量裁剪编排（原子提交/加权进度/partial）
│   │   └── merge.go             # 合并编排（auto 判定/hybrid/回退）
│   ├── server/preview.go        # 预览 Handler（白名单校验 + Range 流）
│   ├── subtitles/               # v1.9 M12：字幕重映射/预换行/SRT+ASS 渲染/特效规则引擎（纯函数）
│   ├── edits/                   # v1.7：删词账本纯逻辑（校验/摘要/保留段补集/SRT 导稿）
│   ├── mcpserver/               # v1.7：MCP Server（15 工具，mcp-go）+ GUI 复用入口
│   └── asr/                     # v1.7：faster-whisper sidecar 客户端（按需拉起/空闲回收）
├── asr/                         # v1.7：Python sidecar（serve.py / bootstrap / compat_check / warmup）
│   ├── serve.py                 #   HTTP 服务（仅回环 127.0.0.1:4178）
│   └── .venv/                   #   bootstrap-asr.ps1 创建（模型缓存在 E:/models/hf）
├── main.go                      # 入口：无参=GUI；mcp-serve 子命令=MCP Server
├── app.go                       # Wails 绑定层（单文件/批量/合并/预览/配置）
├── app_v17.go                   # v1.7：GUI 的 ASR/AI 粗剪绑定层（复用 mcpserver 逻辑）
├── app_v18.go                   # v1.9：GUI 的字幕绑定层（导出/烧录/特效，复用 mcpserver 逻辑）
├── wails.json                   # Wails 构建配置
├── config.yaml                  # 运行期配置（asr 段已启用）
├── go.mod / go.sum
├── frontend/
│   ├── src/
│   │   ├── App.vue              # 主界面：标题栏/页签（单文件/批量/合并/AI粗剪/Codex）/状态栏
│   │   ├── components/Timeline.vue   # 时间轴（刻度尺/拖拽手柄/快捷操作/时间输入）
│   │   ├── components/Preview.vue    # <video> 预览（控制条/seek/选区播放）
│   │   ├── components/BatchPanel.vue # 批量面板（拖放/表格/区间缩略条/汇总进度）
│   │   ├── components/MergePanel.vue # 合并面板（拖拽排序/区间开关/模式卡片）
│   │   ├── components/AICutPanel.vue # AI 粗剪工作台（账本复核/保留段可视化/剪切/导稿/字幕/特效）
│   │   ├── components/CodexPanel.vue # Codex 面板（MCP 调用轨迹/ASR 状态/热词）
│   │   ├── components/MsgLine.vue    # 统一消息行（错误/成功/信息三态）
│   │   ├── utils/time.ts        # mm:ss.s 格式化与解析
│   │   ├── store/trim.ts        # Pinia：全模式任务状态 + 事件订阅 + busy 生命周期
│   │   ├── wails/bridge.ts      # window.go.main.App 封装 + 事件订阅 + OnFileDrop
│   │   └── types.ts             # 与 Go JSON 契约严格对齐的 TS 类型
│   ├── dist/                    # 构建产物（被 Go embed）
│   └── package.json
├── resources/ffmpeg/            # 随仓库分发的 FFmpeg 二进制（源码位置）
│   ├── ffmpeg.exe               # 105 MB
│   └── ffprobe.exe              # 105 MB
├── scripts/
│   ├── copy-ffmpeg.ps1          # 构建期把 resources/ffmpeg 复制到 build/bin/ffmpeg
│   └── bootstrap-asr.ps1        # v1.7：一键引导 ASR venv + 模型（幂等自愈）
├── .agents/plugins/videocut/    # v1.7：Codex 本地 Plugin（4 技能 + marketplace.json）
├── build/bin/                   # 最终产物
│   ├── videocut.exe             # 约 15 MB（双击即用）
│   └── ffmpeg/                  # ffmpeg.exe + ffprobe.exe
├── 开发文档.md                      # 规格文档（架构/契约/算法/决策依据）
├── 开发规划.md                      # 执行台账：里程碑/DoD/依赖/验收
├── 测试报告.md                      # 测试体系/结果/缺陷修复
├── Codex集成指南.md                  # Codex AI 粗剪操作手册
├── UI设计稿_v1.html                    # 界面改版 HTML 设计稿（深色三屏 + 浅色对比）
├── UI设计稿_v2_AI粗剪.html             # AI 粗剪 / Codex 集成 HTML 设计稿
├── UI设计稿_v3_字幕.html               # M12 字幕功能 HTML 设计稿（外显/烧录/Codex 轨迹/二次加工/浅色对比）
├── 字幕功能更新文档.md                  # M12 字幕功能更新文档 v1.3（三模式 + 二次加工自动特效；**P0/P1/P2 已落地**）
└── .workbuddy/memory/           # 项目工作日志
```

---

## 5. 快速开始

### 5.1 直接运行（已构建产物）

```powershell
# 双击或在命令行启动
E:\videocut\build\bin\videocut.exe
```

无需安装 FFmpeg、无需配置环境变量——二进制已在 `build/bin/ffmpeg/`，程序通过 `os.Executable()` 同目录定位。

### 5.2 开发模式（热更新）

```bash
# 终端 1：Wails dev（自动编译 Go 并启动前端 dev server）
wails dev

# 说明：dev 模式下 ffmpeg 定位有源码树回退（path.go 检测 dev 环境回退到 resources/ffmpeg/）
```

### 5.3 重新构建

```bash
# 1. 构建前端
cd frontend && npm install && npx vite build && cd ..

# 2. 构建 Go + 打包（wails 会重新生成 dist 嵌入）
wails build -platform windows/amd64

# 3. 复制 FFmpeg 到产物目录
powershell -ExecutionPolicy Bypass -File scripts/copy-ffmpeg.ps1 -Mode build
```

### 5.4 运行测试

```bash
go test ./... -count=1                  # 全量（含端到端集成）
go test ./internal/... -race -count=1   # 竞态检测
go test ./... -cover                    # 覆盖率
```

---

## 6. 功能流程（用户视角）

```
① 选择文件 ──► App.SelectFile() 系统对话框，返回绝对路径
       │
② 自动探测 ──► App.Probe(path) → ffprobe 获取时长/分辨率/音视频流
       │        前端展示元信息；路径登记进预览白名单
       │
③ 区间拖拽 ──► Timeline.vue 刻度尺 + 双柄拖拽设定 start/end（自动钳位）；也可直接键入 mm:ss.s
       │
④ 实时预览 ──► <video src="/preview?path=..."> 经 AssetServer Range 流播放
       │
⑤ 提交裁剪 ──► App.Trim(req) 返回 task_id（异步）
       │        校验：文件存在 → end>start → end≤源时长 → 策略合法
       │        信号量排队 → FFmpeg 执行（-progress 推送百分比）
       │
⑥ 进度展示 ──► trim:progress 事件 → 进度条 + 速度
       │
⑦ 完成/失败 ─► trim:done（输出路径+时长）/ trim:error（友好中文文案）
       │
⑧ 定位产物 ──► 「打开所在文件夹」→ App.RevealInFolder(path)（explorer /select）
       │
      随时 ────► 「取消」→ App.Cancel(id)（pending/processing 均可，单次"已取消"文案）
```

**拖放与快捷键（v1.6）**：

| 操作     | 方式                              |
| ------ | ------------------------------- |
| 添加文件   | 选择文件按钮；或**将文件拖入窗口**（多文件自动进批量模式） |
| 播放/暂停  | 预览控制条按钮或 `空格`                   |
| 设入点/出点 | 时间轴按钮或 `I` / `O`（取当前播放头）        |
| 微调播放头  | `←` / `→`（±0.5s）                |
| 播放选区   | 时间轴「播放选区」按钮（到点自停）               |
| 调整顺序   | 合并模式拖动片段卡片的 `⠿` 手柄              |

### 6.1 界面与交互（v1.6）

深色专业工具风（默认深色，标题栏可切浅色，`localStorage` 记忆）：标题栏（logo + 分段模式页签 + 主题切换）→ 双栏工作台（左：预览/元信息/时间轴；右：文件信息/策略卡片/输出/操作）→ 底部状态栏。设计稿见 `UI设计稿_v1.html`（三屏 + 浅色对比，浏览器可直接打开）。

**裁剪策略选择**：

| 策略            | 适用场景        | FFmpeg 行为                                                | 精度            |
| ------------- | ----------- | -------------------------------------------------------- | ------------- |
| `stream_copy` | 快速分割、无需帧精确  | `-c copy` 流复制 + `-avoid_negative_ts make_zero`           | 关键帧吸附（可能偏移数秒） |
| `reencode`    | 帧精确、区间不在关键帧 | 重编码 `-c:v libx264 -preset fast -c:a aac`，`-t <duration>` | 精确            |
| `audio_only`  | 纯音频输入       | `-c:a copy`（或重编码）                                        | 关键帧吸附         |


**防覆盖**：默认输出为 `<name>_trimmed.<ext>`；若已存在则自动追加 `_1`、`_2` 序号。

### 6.2 批量裁剪与合并（v1.5 已实现）

```
批量模式：添加 N 个文件 → 每项设定区间/策略（可"统一区间"）→ TrimBatch 一次性提交
        → 原子预校验（任一项不过整批拒绝）→ 逐项执行（共享并发信号量）
        → 整体加权进度 + 每项状态 → 终态 completed / partial（部分成功）/ failed
        → 可整体取消（未完成项取消，已成功项产物保留）

合并模式：添加 N 个片段（起止可空=整段）→ Merge 提交
        → auto 判定：输入参数一致走 concat demuxer（-c copy 流复制，快/无损）
                     参数不一致走 concat filter（重编码，无音频输入自动补 anullsrc 静音轨）
        → demuxer 失败且为 auto 模式时自动回退 filter 重试（merge:notice 提示）
        → 带起止的项先裁剪到临时目录（ASCII 规范化）再合并 = "分割+合并"一条龙
```

关键设计：批量/合并**复用既有单文件执行管线**（同卷临时目录、`.part`+rename、超时 Kill、信号量、事件），Task 模型仅扩展 `Kind`/`Items`/`partial` 终态，不引入外部队列。详见开发文档 §18 / §17。

### 6.3 AI 粗剪与 Codex（v1.7 已落地）

新增两个页签（设计稿 `UI设计稿_v2_AI粗剪.html`）：

- **AI 粗剪**：选视频 → 转录（ASR sidecar，可跳过手动导入 `edits.json`）→ 机械候选（口水词/静音）逐项开关复核 → 保留段可视化 → 一键剪切（默认 `reencode`，精确切分）→ 合并为一个成片 → 导出文字稿（SRT→MD/TXT）。强制「先复核账本、后物理剪切」。
- **字幕（M12 新增，成片后两步）**：③ 字幕——成片时间轴自动重对齐（删除段剔除/保留段平移/跨段词级断开）→ 外显导出 SRT+ASS（默认，剪映/PR 精修入口）或内嵌烧录（重编码，音轨直复制）或双输出；④ 二次加工——规则引擎按五维内容特征自动选 ≤2 个 ASS 特效（`edited_fx.ass` + `motion-plan.json` 理由可查）。GUI 的 `burn/both` 始终保留 `edited.srt` + `edited.ass` 后再烧录；WebView2 不渲染外挂字幕，烧录版才可直接在预览中看到字幕。规格见《字幕功能更新文档》。
- **Codex**：MCP 调用轨迹（15 工具时间线）+ ASR 设置/热词管理 + `asr_runtime_missing` 降级提示。底层是 `videocut.exe mcp-serve` 暴露的 15 个 MCP 工具（剪切族 6 + ASR 族 3 + 编辑族 3 + 字幕族 3），供 Codex CLI 驱动同一套剪切能力（详见《Codex集成指南》与开发文档 §19）。

> AI 粗剪依赖 ASR 运行时：首次需运行 `scripts/bootstrap-asr.ps1` 安装 venv 与模型（约 0.5GB）；`config.yaml` 中 `asr.enabled=false` 时仅显示「未启用」，不拉起进程。

> ASR sidecar 固定启用 faster-whisper `word_timestamps=True`，前台没有关闭开关；词级时间戳会写入 `work/signals.json` 的 `words[]`，供候选扫描、剪后字幕重对齐和字幕特效使用。`subtitles.style_preset=custom` 当前安全回退 `default`，三套内置样式可直接使用。

---

## 7. 配置说明

按优先级（高→低）：**环境变量 > config.yaml > 内置默认值**。配置文件放可执行文件同目录或 `resources/` 下，缺失时静默使用默认值。

| 配置项                     | 环境变量                                      | 默认值                  | 说明                                                    |
| ----------------------- | ----------------------------------------- | -------------------- | ----------------------------------------------------- |
| `ffmpeg.mode`           | `VIDEOCUT_FFMPEG_MODE`                    | `bundled`            | `bundled`=随程序分发；`system`=从 PATH 查找                    |
| `ffmpeg.dir`            | `VIDEOCUT_FFMPEG_DIR`                     | （空）                  | `bundled` 模式下自定义 ffmpeg 所在目录                          |
| `concurrency`           | `VIDEOCUT_CONCURRENCY`                    | CPU 核心数              | 并发 FFmpeg 进程上限（信号量）                                   |
| `trim.default_strategy` | `VIDEOCUT_TRIM_DEFAULT_STRATEGY`          | `stream_copy`        | 前端未指定策略时的默认值                                          |
| `trim.timeout_seconds`  | `VIDEOCUT_TRIM_TIMEOUT_SECONDS`           | `600`                | 单任务超时（秒），超时自动 Kill                                    |
| `output.suffix`         | `VIDEOCUT_OUTPUT_SUFFIX`                  | `_trimmed`           | 默认输出文件名后缀                                             |
| `webview2.fallback`     | `VIDEOCUT_WEBVIEW2_FALLBACK`              | `embed`              | `embed`=内嵌引导器（离线可用）                                   |
| `merge.default_mode`    | `VIDEOCUT_MERGE_DEFAULT_MODE`             | `auto`               | **v1.4 新增**：合并默认模式（auto/demuxer/filter）               |
| `batch.max_items`       | `VIDEOCUT_BATCH_MAX_ITEMS`                | `50`                 | **v1.4 新增**：单批最大项数上限                                  |
| `asr.enabled`           | `VIDEOCUT_ASR_ENABLED`                    | `false`              | **v1.7 新增**：ASR 总开关（关闭时 AI 粗剪/Codex 面板显示「未启用」，不拉起进程）  |
| `asr.host` / `asr.port` | `VIDEOCUT_ASR_HOST` / `VIDEOCUT_ASR_PORT` | `127.0.0.1` / `4178` | **v1.7 新增**：sidecar 回环地址与端口                           |
| `asr.model`             | `VIDEOCUT_ASR_MODEL`                      | `small`              | **v1.7 新增**：`small` / `large-v3`（无 GPU，默认 small+int8） |
| `asr.compute_type`      | `VIDEOCUT_ASR_COMPUTE_TYPE`               | `int8`               | **v1.7 新增**：CPU 量化推理                                  |
| `asr.hf_home`           | `VIDEOCUT_ASR_HF_HOME`                    | `E:/models/hf`       | **v1.7 新增**：模型缓存（重定向到 E 盘，C 盘空间不足）                    |
| `asr.idle_timeout_min`  | `VIDEOCUT_ASR_IDLE_TIMEOUT_MIN`           | `10`                 | **v1.7 新增**：sidecar 空闲回收分钟数                           |
| `asr.hotwords`          | `VIDEOCUT_ASR_HOTWORDS`                   | `[]`                 | **v1.7 新增**：默认热词表（提升专名命中）                             |
| `subtitles.*`           | `VIDEOCUT_SUBTITLES_*`                    | 见 config.yaml       | **v1.9 新增**：字幕模式（sidecar/burn/both）/ 格式 / 样式预设 / 预换行参数 / 烧录编码器 / 自动特效开关与上限（`config.yaml` 内有逐键注释） |

`config.yaml` 示例：

```yaml
ffmpeg:
  mode: bundled
concurrency: 4
trim:
  default_strategy: reencode
  timeout_seconds: 900
output:
  suffix: "_cut"
# asr 段为嵌套块（viper 对带点号 key 的扁平映射有缺陷，由 config.Load 显式读取）
asr:
  enabled: false          # 启用前先跑 scripts/bootstrap-asr.ps1
  host: "127.0.0.1"
  port: 4178
  model: "small"
  compute_type: "int8"
  hf_home: "E:/models/hf"
  idle_timeout_min: 10
  hotwords: []
```

---

## 8. 测试体系（摘要）

四层设计，全自动运行（`go test ./...`；集成测试缺 ffmpeg 时自动 Skip，不误报）：

| 层       | 覆盖对象                                                         | 手段                    |
| ------- | ------------------------------------------------------------ | --------------------- |
| L1 纯逻辑  | 命令构造、进度解析、元数据解析、路径解析、edits 账本、JSON 契约                        | 直接调用 + 表驱动            |
| L2 边界校验 | 输入校验、任务状态机、防覆盖序号、配置加载/环境变量覆盖                                 | 临时文件 + 表驱动            |
| L3 假替身  | executor 进度解析 + 超时 Kill + 启动失败                               | 自制 `.cmd` 假 ffmpeg    |
| L4 端到端  | Probe→Trim→产物校验；批量/合并/hybrid/中文路径/auto 回退；AI 粗剪全链（假 sidecar） | 真实 ffmpeg + lavfi 合成片 |

**当前状态**：`go build` / `go vet` / `go test ./...` 全绿；`-race` 无竞争；`vue-tsc` + `vite build` 通过。AI 粗剪另有 opt-in 真实环境端到端测试（`VIDEOCUT_REAL_ASR=1`，真实 Whisper 模型 + 真实 ffmpeg）。具体包数和本轮回归结果以《测试报告》§7.9 为准。

完整用例清单、执行结果、覆盖率与缺陷修复记录见《测试报告》；Codex 侧手动 QA 见《Codex集成指南》。

---

## 9. 打包与分发

**当前产物**（便携版）：

```
build/bin/
├── videocut.exe     # 约 15 MB
└── ffmpeg/
    ├── ffmpeg.exe   # 105 MB
    └── ffprobe.exe  # 105 MB
```

zip 打包后约 **225 MB**，解压即用，零配置。AI 粗剪另需一次性引导 ASR 运行时（`scripts/bootstrap-asr.ps1`，约 0.5GB 模型），详见《Codex集成指南》§2。

**可选增强**（未实施，按需再做）：

- NSIS 安装包：`wails.json` 中 `"nsis": true`
- 代码签名：避免 SmartScreen 警告
- FFmpeg 瘦身：`ffmpeg.exe` 可用 `--enable-small` 或按需裁剪解码器，体积可降至 ~30 MB

---

## 10. 已知限制与后续决策

**已知限制**：

1. 任务表为内存态，重启后历史丢失（单机轻工具可接受）
2. 预览白名单仅在 Probe 后登记；直接输入未探测路径的预览请求会被拒绝（安全设计）
3. 前端组件级 QA（选文件→拖区间→预览→裁剪）依赖 GUI 环境，已由后端集成测试覆盖等价逻辑，建议发布前人工过一遍（《测试报告》§7.7 清单）
4. 批量/合并顺序调整（UI 排序）未做，`items` 数组顺序即输出顺序（开发文档 §18.10 R11）
5. 前端组件自动化测试（Vitest）未落地，以手动 QA 清单替代
6. 中文识别质量/口水词召回率未经真实人声素材量化（链路已验证，词表可随使用反馈扩充）
7. WebView2 的 `<video>` 不渲染外挂 SRT/ASS；需要看字幕时使用面板字幕列表、外部播放器或烧录版视频
8. `subtitles.style_preset=custom` 尚未实现，当前安全回退 `default`；词级时间戳则固定开启，不提供前台关闭开关

**待产品决策**（完整决策台账见《开发规划》§9）：

| # | 决策项                  | 当前默认                            |
| - | -------------------- | ------------------------------- |
| 1 | 交付形态                 | 便携版单 exe（未做 NSIS）               |
| 2 | 是否保留 HTTP 形态（方案 B）   | 否，不引入 Gin                       |
| 3 | 默认策略 / 关键帧吸附提示       | `stream_copy`，UI 未显式提示吸附偏移      |
| 4 | FFmpeg 更新机制          | 锁版本，随程序分发                       |
| 5 | 跨格式导出（如 mp4→gif/mp3） | 否，输出容器跟随输入                      |
| 6 | 合并默认模式               | `auto`（已实现，可运行期 `ConfigSet` 调整） |

> v1.7 的 5 项决策（Codex 载体 / ASR 选型 / Plugin 范围 / Studio GUI / 复核策略）已全部落地，记录于开发文档 §16 修订记录。

---

## 11. 配套文档索引

| 文档             | 内容                                                                                                 |
| -------------- | -------------------------------------------------------------------------------------------------- |
| `开发文档.md`      | 📐 权威规格：架构、模块、API 契约、FFmpeg 命令、错误矩阵、决策依据；**§18 批量裁剪与合并 + §19 Codex 集成与 AI 粗剪本地化规格（含 §19.13 决策依据、§19.14 字幕可行性分析）** |
| `开发规划.md`      | 🗂 执行台账：M0–M12 实施计划（全部完成）+ DAG/DoD/验收清单/风险登记/决策与偏差记录                       |
| `测试报告.md`      | 🔬 质量证明：四层测试体系、用例、真实 ffmpeg 端到端结果、真实 ASR 验证、缺陷修复记录                                                 |
| `Codex集成指南.md` | 🤖 操作手册：让 Codex 驱动 videocut 做 AI 粗剪的安装/接入/技能/故障排查                                                  |
| `字幕功能更新文档.md` | 📝 功能更新说明：M12 粗剪后字幕（重对齐 + 外显/内嵌/双出三模式 + 二次加工自动特效，含两级规则引擎/停用词表/\kf 扫描/analysis 定稿；**已实施**：`internal/subtitles` + MCP 三工具 + GUI 第三/四步，测试报告 §7.9） |
| 本文档            | 📘 上手向导：定位、架构、快速开始、功能流程、配置、打包                                                                      |

**界面设计稿**：`UI设计稿_v1.html`（v1.6 界面改版）· `UI设计稿_v2_AI粗剪.html`（M8–M11 AI 粗剪/Codex）· `UI设计稿_v3_字幕.html`（M12 字幕工作台/烧录/Codex 轨迹/二次加工/浅色对比，已落地为 AI 粗剪面板第三/四步）



---

*文档版本：v1.10（2026-10-15：M8–M11 落地 + ASR 真实打通 + M12 字幕功能全栈落地与本轮回归修复（重对齐/外显/烧录/二次加工，15 MCP 工具）；文档家族各司其职，测试详情移入《测试报告》、决策台账移入《开发规划》§9） · 平台：Windows 单平台 · 基线：Wails v2 + FFmpeg 随程序分发 · 产物：build/bin/videocut.exe（约 15 MB，内置 ffmpeg 105 MB×2）*
