"""下载并预热 faster-whisper 模型（bootstrap 的第 5 步，独立可重跑）。

用法：
    $env:HF_HOME = "E:/models/hf"
    $env:HF_ENDPOINT = "https://hf-mirror.com"   # 国内镜像
    python asr/warmup.py [model] [compute_type]
"""
import os
import sys
import time

model = sys.argv[1] if len(sys.argv) > 1 else "small"
compute_type = sys.argv[2] if len(sys.argv) > 2 else "int8"

print(f"HF_HOME     = {os.environ.get('HF_HOME')}")
print(f"HF_ENDPOINT = {os.environ.get('HF_ENDPOINT', '(default)')}")
print(f"model       = {model} / {compute_type}")
print("loading (first run downloads ~0.5GB) ...", flush=True)

t0 = time.time()
from faster_whisper import WhisperModel  # noqa: E402

m = WhisperModel(model, device="cpu", compute_type=compute_type)
print(f"model ready in {time.time() - t0:.1f}s", flush=True)

# 极短音频冒烟：确认 transcribe 路径可用（不需要真实音频，用静音 numpy 帧）
import numpy as np  # noqa: E402

sr = 16000
audio = np.zeros(sr * 2, dtype=np.float32)  # 2 秒静音
t1 = time.time()
segments, info = m.transcribe(audio, beam_size=1, language="zh")
_ = list(segments)
print(f"transcribe smoke ok in {time.time() - t1:.1f}s (language={info.language})")
print("WARMUP DONE")
