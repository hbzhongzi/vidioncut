#!/usr/bin/env python
# -*- coding: utf-8 -*-
"""ASR 运行时依赖兼容性自检（bootstrap-asr.ps1 第 4b 步调用）。

背景（实测缺陷）：
    faster-whisper（截至 1.2.1）在 audio.py 中调用
        av.open(input_file, mode="r", metadata_errors="ignore")
    但其依赖声明里 `av` **无版本上界**。pip 会装上最新的 av（如 19.x），
    而 PyAV 自 15.0 起移除了 `metadata_errors` 参数，导致真实转录直接抛
        TypeError: open() got an unexpected keyword argument 'metadata_errors'
    即"装完看似成功、一转录就 500"。

本脚本在装完包后立即探测该签名，提前暴露不兼容并给出可安装版本建议。

退出码：
    0  = 兼容
    10 = av 与 faster-whisper 不兼容（stdout 末行输出建议版本，形如 NEED_AV=<spec>）
    11 = faster-whisper 未安装
"""
import inspect
import sys

try:
    import av  # noqa: PLC0415
except Exception as e:
    print("faster-whisper/av 未就绪: %s: %s" % (type(e).__name__, e))
    sys.exit(11)

try:
    import faster_whisper  # noqa: F401,PLC0415
except Exception as e:
    print("faster-whisper 未安装: %s: %s" % (type(e).__name__, e))
    sys.exit(11)

av_ver = getattr(av, "__version__", "?")
print("av              = %s" % av_ver)
print("faster-whisper  = %s" % getattr(faster_whisper, "__version__", "?"))

try:
    params = inspect.signature(av.open).parameters
except Exception as e:
    print("无法探测 av.open 签名: %s" % e)
    sys.exit(10)

if "metadata_errors" in params:
    print("compat: OK (av.open 支持 metadata_errors)")
    sys.exit(0)

print("compat: BROKEN (av %s 移除了 metadata_errors，faster-whisper 转录会抛 TypeError)" % av_ver)
# av 15.0 起移除该参数；14.2.0 是仍带 cp312 Windows 预编译包的最后一个 14.x。
print("NEED_AV=av<15")
sys.exit(10)
