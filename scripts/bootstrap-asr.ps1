# videocut ASR sidecar 一键引导（开发文档 v1.7 §19.5）
# 用法：powershell -ExecutionPolicy Bypass -File scripts/bootstrap-asr.ps1
# 作用：① HF_HOME 重定向 E 盘（C 盘空间不足，R23）② 建 venv（Python 3.12 优先，R22）
#       ③ 安装 faster-whisper（候选源经 venv python 实测可达性后排序；代理失效自动绕开）
#       ④ 决策 HF 端点（huggingface.co 不可达切 hf-mirror）⑤ 预热 small+int8 模型
# 设计要点：网络探测一律用 venv python 实测（pip/HF 的真实消费路径）；
#          PowerShell 的 Invoke-WebRequest 走系统代理，与 python 路径可能不一致，不可作数。

param(
    [string]$ProjectRoot = (Split-Path -Parent $PSScriptRoot),
    [string]$HFHome = "E:/models/hf",
    [string]$Model = "small",
    [string]$HFEndpoint = "",          # 留空=自动探测；国内可显式传 https://hf-mirror.com
    [int]$PipTimeoutSec = 300,         # 单条 pip 策略硬超时（挂死自动切下一策略）
    [int]$WarmupTimeoutSec = 1200      # 模型下载+预热硬超时（0.5GB，国内镜像留 20 分钟）
)

$ErrorActionPreference = "Stop"
$asrDir  = Join-Path $ProjectRoot "asr"
$venvDir = Join-Path $asrDir ".venv"
$venvPy  = Join-Path $venvDir "Scripts\python.exe"

# python 探针：argv[1]=URL，argv[2]=代理（空=直连）；200 退出 0，否则 1。
# 只用单引号，避免 PowerShell 传给原生进程时的引号剥离。
$probeCode = @'
import sys, urllib.request
url = sys.argv[1]
proxy = sys.argv[2] if len(sys.argv) > 2 else ""
if proxy:
    handler = urllib.request.ProxyHandler({"http": proxy, "https": proxy})
else:
    handler = urllib.request.ProxyHandler({})
try:
    r = urllib.request.build_opener(handler).open(url, timeout=6)
    sys.exit(0 if r.status == 200 else 1)
except Exception:
    sys.exit(1)
'@

function Invoke-Native($exe, $args, $timeoutMs) {
    # PS5.1 下原生命令写 stderr 会以 ErrorRecord 冒泡，叠加 Stop 首选项会误杀脚本；
    # 且本地代理挂起时 pip 可能无限期阻塞（SSL 握手不受 --timeout 约束）。
    # 用 Process 显式托管：输出合并、硬超时、杀进程、返回退出码（-1=超时）。
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $exe
    $psi.Arguments = (($args | ForEach-Object { '"' + ($_ -replace '"', '\"') + '"' }) -join ' ')
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true
    $p = [System.Diagnostics.Process]::Start($psi)
    $outTask = $p.StandardOutput.ReadToEndAsync()
    $errTask = $p.StandardError.ReadToEndAsync()
    if (-not $p.WaitForExit($timeoutMs)) {
        try { $p.Kill() } catch { }
        Write-Host "    TIMEOUT after $($timeoutMs / 1000)s -> killed"
        return -1
    }
    [void]$p.WaitForExit()
    $out = $outTask.Result
    $err = $errTask.Result
    if ($out) { Write-Host $out }
    if ($err) { Write-Host $err }
    return $p.ExitCode
}

Write-Host "==> [1/5] HF_HOME -> $HFHome (user env)"
[Environment]::SetEnvironmentVariable("HF_HOME", $HFHome, "User")
$env:HF_HOME = $HFHome

# Test-Idx：venv python 实测 URL 可达性（带 1 次重试，抵御沙箱/网络抖动窗口；
# 注意：PowerShell 的 Invoke-WebRequest 走系统代理，与 python 路径可能不一致，不可作数）
function Test-Idx($url, $proxy) {
    for ($attempt = 1; $attempt -le 2; $attempt++) {
        $code = Invoke-Native $venvPy @("-c", $probeCode, $url, $proxy) 20000
        if ($code -eq 0) { return 0 }
        if ($attempt -eq 1) { Start-Sleep -Milliseconds 800 }
    }
    return 1
}

Write-Host "==> [2/5] Locate Python 3.11+ (3.12 preferred; resolves real exe, skips WindowsApps stubs)"
$pyCmd = $null
foreach ($cand in @("python3.12", "python3.11", "python3", "python")) {
    $resolved = Get-Command $cand -ErrorAction SilentlyContinue
    if (-not $resolved) { continue }
    $ver = & $resolved.Source --version 2>&1
    if ($LASTEXITCODE -eq 0 -and ("$ver" -match "Python 3\.(1[1-9]|[2-9]\d)\.")) {
        Write-Host "    using: $($resolved.Source)  ($ver)"
        $pyCmd = $resolved.Source
        break
    }
}
if (-not $pyCmd) { Write-Host "ERROR: no usable python 3.11+ found"; exit 1 }

Write-Host "==> [3/5] Create venv at $venvDir"
if (-not (Test-Path $venvPy)) {
    New-Item -ItemType Directory -Force -Path $asrDir | Out-Null
    if ((Invoke-Native $pyCmd @("-m", "venv", $venvDir) 120000) -ne 0) {
        Write-Host "ERROR: venv creation failed"; exit 1
    }
} else {
    Write-Host "    venv exists, skip"
}

# ---- pip 源策略：venv python 实测可达性排序（探测失败不淘汰，仅排后兜底） ----
Write-Host "==> [4/5] Install faster-whisper (probe indexes with venv python)"

# 幂等：venv 里已有可导入的 faster-whisper 则跳过全部联网安装。
# 沙箱/弱网下 pip 子进程可能整体挂起，重跑时不应因已装包再冒一次网络风险。
# 导入 heavier（faster_whisper 会连带加载 ctranslate2/numpy 的 DLL），首次可能数十秒，
# 故给 90s：这是纯本地操作，宁可慢判也不要误判为"未安装"而白跑一次联网安装。
$fwReady = ((Invoke-Native $venvPy @("-c", "import faster_whisper") 90000) -eq 0)
if ($fwReady) {
    Write-Host "    faster-whisper already present in venv; skipping network install"
} else {
if ($env:PIP_INDEX_URL -and $env:PIP_INDEX_URL -notmatch "^https?://") {
    Write-Host "    clearing invalid PIP_INDEX_URL='$env:PIP_INDEX_URL'"
    $env:PIP_INDEX_URL = $null
}
$mirrorTuna   = "https://pypi.tuna.tsinghua.edu.cn/simple/"
$mirrorAliyun = "https://mirrors.aliyun.com/pypi/simple/"
$candidates = @(
    @{ label = "pypi.org (default)"; url = "https://pypi.org/simple/"; extra = @() },
    @{ label = "tuna mirror";        url = $mirrorTuna;               extra = @("--index-url", $mirrorTuna) },
    @{ label = "aliyun mirror";      url = $mirrorAliyun;             extra = @("--index-url", $mirrorAliyun) }
)

$proxyEnv = $env:HTTPS_PROXY
if (-not $proxyEnv) { $proxyEnv = $env:HTTP_PROXY }
$proxyUsable = $false
if ($proxyEnv) {
    # pip 读 HTTP_PROXY/HTTPS_PROXY（与 PowerShell 系统代理无关），必须显式走代理测
    if ((Test-Idx "https://pypi.org/simple/" $proxyEnv) -eq 0) {
        $proxyUsable = $true
        Write-Host "    proxy $proxyEnv reachable for PyPI; keeping it for default index"
    } else {
        Write-Host "    proxy $proxyEnv unreachable for PyPI; bypassing proxy for pip/HF"
        $env:HTTP_PROXY = $null; $env:HTTPS_PROXY = $null
        $env:http_proxy = $null; $env:https_proxy = $null
    }
}

$reach = @(); $unreach = @()
foreach ($c in $candidates[1..2]) {
    if ((Test-Idx $c.url "") -eq 0) {
        Write-Host "    probe OK   : $($c.label)"
        $reach += $c
    } else {
        Write-Host "    probe FAIL : $($c.label)（排后兜底）"
        $unreach += $c
    }
}
$ordered = @()
if ($proxyUsable) { $ordered += $candidates[0] }   # 代理可用：官方源优先（走代理）
$ordered += $reach + $unreach
if (-not $proxyUsable) { $ordered += $candidates[0] }  # 代理不可用：官方源排最后

$installed = $false
foreach ($st in $ordered) {
    $extra = $st.extra
    # 注意：不要追加 `--proxy ""`——pip 会把空串当作代理地址，触发
    # getaddrinfo failed。绕代理只清环境变量即可（上面已清空 HTTP(S)_PROXY）。
    if (-not $proxyUsable) { $extra = $st.extra }
    Write-Host "    pip install faster-whisper [$($st.label)] ..."
    $pipArgs = @("-m", "pip", "install", "--retries", "1", "--timeout", "15") + $extra + @("faster-whisper")
    $code = Invoke-Native $venvPy $pipArgs ($PipTimeoutSec * 1000)
    if ($code -eq 0) { $installed = $true; break }
    Write-Host "    strategy [$($st.label)] failed (exit=$code)"
}
if (-not $installed) { Write-Host "ERROR: pip install failed (all strategies exhausted)"; exit 1 }
}

# ---- 4b) 依赖兼容性自检：faster-whisper × av 版本陷阱 ----
# faster-whisper 依赖声明里 av 无版本上界，pip 会装最新 av（>=15 已移除
# av.open(metadata_errors=...)），导致"装完看似成功、一转录就 500"。
$compatPy = Join-Path $asrDir "compat_check.py"
if (Test-Path $compatPy) {
    $compatCode = Invoke-Native $venvPy @($compatPy) 60000
    if ($compatCode -eq 10) {
        Write-Host "    av 与 faster-whisper 不兼容，尝试降级 av ..."
        $avFixed = $false
        # 优先精确版（有 cp312 Windows 预编译包，避免源码构建）；再退到范围约束
        foreach ($spec in @("av==14.2.0", "av<15")) {
            $code = Invoke-Native $venvPy @("-m", "pip", "install", "--only-binary", ":all:",
                "--no-deps", "--retries", "1", "--timeout", "15", $spec) ($PipTimeoutSec * 1000)
            if ($code -ne 0) { continue }
            if ((Invoke-Native $venvPy @($compatPy) 60000) -eq 0) {
                Write-Host "    av 降级成功 -> $spec"
                $avFixed = $true
                break
            }
        }
        if (-not $avFixed) {
            Write-Host "ERROR: av 降级失败；请手动执行: $venvPy -m pip install `"av==14.2.0`""
            exit 1
        }
    } elseif ($compatCode -ne 0) {
        Write-Host "ERROR: ASR 依赖自检未通过 (exit=$compatCode)"
        exit 1
    }
}

# ---- HF 端点：python 实测 huggingface.co，不可达切 hf-mirror ----
if (-not $HFEndpoint) {
    if ((Test-Idx "https://huggingface.co" "") -eq 0) {
        $HFEndpoint = "https://huggingface.co"
    } elseif ((Test-Idx "https://hf-mirror.com" "") -eq 0) {
        $HFEndpoint = "https://hf-mirror.com"
    } else {
        $HFEndpoint = "https://huggingface.co"  # 均不可达：保持默认，让预热报错暴露网络问题
    }
}
Write-Host "==> [5/5] HF endpoint -> $HFEndpoint ; warm up model ($Model / int8, first run ~0.5GB)"
$env:HF_ENDPOINT = $HFEndpoint
# Xet 存储后端不走 HF_ENDPOINT，镜像场景下会直连 cas-server.xethub.hf.co 并 401；
# 禁用后回退到镜像的普通 CDN 路径（实测必需）。
$env:HF_HUB_DISABLE_XET = "1"

# 预热用独立脚本（asr/warmup.py）：避免 `-c` 传参在部分宿主下的引号/转义问题，
# 且可单独重跑。若模型已在缓存中，可加 HF_HUB_OFFLINE=1 完全离线。
$warmPy = Join-Path $asrDir "warmup.py"
if (-not (Test-Path $warmPy)) {
    Write-Host "ERROR: 缺少 $warmPy"
    exit 1
}
$sw = [System.Diagnostics.Stopwatch]::StartNew()
$warmCode = Invoke-Native $venvPy @($warmPy, $Model, "int8") ($WarmupTimeoutSec * 1000)
if ($warmCode -ne 0) { Write-Host "ERROR: model warmup failed (exit=$warmCode)"; exit 1 }
$sw.Stop()

Write-Host ""
Write-Host "DONE in $($sw.Elapsed.TotalSeconds.ToString('0.0'))s (warmup)"
Write-Host "  venv python : $venvPy"
Write-Host "  model cache : $env:HF_HOME (endpoint: $HFEndpoint)"
Write-Host "  next        : config.yaml set asr.enabled=true (or env VIDEOCUT_ASR_ENABLED=true)"
Write-Host "                 then run: videocut.exe mcp-serve"
