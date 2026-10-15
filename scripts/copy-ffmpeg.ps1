# 构建后复制 FFmpeg 二进制到输出目录（开发文档 v1.1 §11.2）
# 用法：
#   开发期：  pwsh scripts/copy-ffmpeg.ps1 -Mode dev
#   打包后：  pwsh scripts/copy-ffmpeg.ps1 -Mode build
param(
    [string]$Mode = "build"   # dev | build
)

$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
# 上溯到项目根（scripts/ 的上一级）
$projectRoot = Split-Path -Parent $root

$src = Join-Path $projectRoot "resources/ffmpeg"
if (-not (Test-Path $src)) {
    Write-Warning "未找到 $src，请先放置 ffmpeg.exe / ffprobe.exe"
    exit 0
}

if ($Mode -eq "dev") {
    # wails dev 下 exe 在 build/bin，复制过去以便 dev 也能跑
    $dst = Join-Path $projectRoot "build/bin/ffmpeg"
} else {
    $dst = Join-Path $projectRoot "build/bin/ffmpeg"
}

if (-not (Test-Path $dst)) { New-Item -ItemType Directory -Path $dst | Out-Null }

Copy-Item -Path (Join-Path $src "ffmpeg.exe") -Destination $dst -Force
Copy-Item -Path (Join-Path $src "ffprobe.exe") -Destination $dst -Force

Write-Host "已复制 FFmpeg 到 $dst"
