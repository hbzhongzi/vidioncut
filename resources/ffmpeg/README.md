# FFmpeg 二进制（随程序分发）

将 Windows 版 FFmpeg 静态构建的以下两个文件放在本目录：

- `ffmpeg.exe`
- `ffprobe.exe`

下载地址（静态构建，无需额外 DLL）：

https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip

解压后从 `bin/` 取出 `ffmpeg.exe` 与 `ffprobe.exe` 复制到本目录即可。
构建时由 `scripts/copy-ffmpeg.ps1` 复制到 `build/bin/ffmpeg/`，最终与 `videocut.exe` 一同分发，用户零配置。

> 注意：此目录默认不提交大体积二进制到版本库；CI/构建环境需先获取（或改为构建期下载）。
