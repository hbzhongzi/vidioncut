#!/usr/bin/env python
# -*- coding: utf-8 -*-
"""videocut ASR sidecar（faster-whisper 本地转录服务）。

开发文档 v1.7 §19.5：
- 仅监听 127.0.0.1（本地回环，不暴露网络）
- GET  /health           健康检查
- POST /transcribe       转录 + 静音检测 → <output_base>/work/transcript.srt + signals.json
- POST /hotwords         设置热词表（initial_prompt）

由 Go 侧 internal/asr/client.go 按需拉起；也可手工启动调试：
    python serve.py --host 127.0.0.1 --port 4178
"""

import argparse
import json
import os
import sys
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

_lock = threading.Lock()
_model = None
_model_name = None
_hotwords = []


def get_model(name, compute_type):
    """延迟加载 WhisperModel（首次调用才 import，启动 <1s）。"""
    global _model, _model_name
    with _lock:
        if _model is None or _model_name != name:
            from faster_whisper import WhisperModel  # noqa: PLC0415

            _model = WhisperModel(name, device="cpu", compute_type=compute_type)
            _model_name = name
        return _model


def fmt_time(t):
    """秒 → SRT 时间戳 H:MM:SS,mmm。"""
    ms = int(round(max(t, 0) * 1000))
    h, ms = divmod(ms, 3600000)
    m, ms = divmod(ms, 60000)
    s, ms = divmod(ms, 1000)
    return "%02d:%02d:%02d,%03d" % (h, m, s, ms)


def do_transcribe(video_path, output_base, language, hotwords, model, compute_type, vad_filter=True):
    if not os.path.isfile(video_path):
        raise FileNotFoundError("视频文件不存在: %s" % video_path)
    m = get_model(model, compute_type)

    prompt = " ".join(w for w in (hotwords or []) if w.strip()) or None
    segments, info = m.transcribe(
        video_path,
        language=language or "zh",
        vad_filter=vad_filter,
        word_timestamps=True,
        initial_prompt=prompt,
        beam_size=5,
    )

    work = os.path.join(output_base, "work")
    os.makedirs(work, exist_ok=True)

    srt = []
    seg_list = []
    words = []
    silences = []
    prev_end = 0.0
    idx = 0
    for seg in segments:
        idx += 1
        text = (seg.text or "").strip()
        srt.append(str(idx))
        srt.append("%s --> %s" % (fmt_time(seg.start), fmt_time(seg.end)))
        srt.append(text)
        srt.append("")
        seg_list.append({"start": round(seg.start, 3), "end": round(seg.end, 3), "text": text})
        # 片段间隙 ≥1s 视为静音段（VAD 之外的兜底检测）
        if seg.start - prev_end >= 1.0:
            silences.append({"start": round(prev_end, 3), "end": round(seg.start, 3)})
        prev_end = max(prev_end, seg.end)
        for w in (getattr(seg, "words", None) or []):
            words.append({"start": round(w.start, 3), "end": round(w.end, 3), "word": w.word})

    srt_path = os.path.join(work, "transcript.srt")
    with open(srt_path, "w", encoding="utf-8") as f:
        f.write("\n".join(srt))

    signals = {
        "source": video_path.replace("\\", "/"),
        "duration": round(float(getattr(info, "duration", 0.0) or 0.0), 3),
        "language": getattr(info, "language", language or "zh"),
        "segments": seg_list,
        "words": words,
        "silences": silences,
    }
    signals_path = os.path.join(work, "signals.json")
    with open(signals_path, "w", encoding="utf-8") as f:
        json.dump(signals, f, ensure_ascii=False, indent=2)

    return {
        "srt_path": srt_path.replace("\\", "/"),
        "signals_path": signals_path.replace("\\", "/"),
        "duration": signals["duration"],
        "segments": len(seg_list),
        "silences": len(silences),
        "words": len(words),
    }


class Handler(BaseHTTPRequestHandler):
    server_version = "videocut-asr/1.0"

    def log_message(self, fmt, *args):  # 日志走 stderr，不污染调用方 stdout
        sys.stderr.write("[asr-sidecar] %s\n" % (fmt % args))

    def _json(self, obj, status=200):
        body = json.dumps(obj, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _body(self):
        n = int(self.headers.get("Content-Length") or 0)
        raw = self.rfile.read(n) if n else b""
        if not raw:
            return {}
        try:
            return json.loads(raw.decode("utf-8"))
        except Exception:
            return {}

    def do_GET(self):
        if self.path == "/health":
            self._json({"ok": True, "model_loaded": _model is not None})
            return
        self._json({"error": "not found"}, 404)

    def do_POST(self):
        try:
            if self.path == "/hotwords":
                global _hotwords
                body = self._body()
                _hotwords = [str(w) for w in (body.get("hotwords") or [])]
                self._json({"ok": True, "hotwords": _hotwords})
                return
            if self.path == "/transcribe":
                body = self._body()
                video = body.get("video_path") or ""
                base = body.get("output_base") or ""
                if not video or not base:
                    self._json({"error": "video_path/output_base 不能为空"}, 400)
                    return
                result = do_transcribe(
                    video_path=video,
                    output_base=base,
                    language=body.get("language") or "zh",
                    hotwords=body.get("hotwords") if body.get("hotwords") is not None else _hotwords,
                    model=body.get("model") or os.environ.get("VIDEOCUT_ASR_MODEL", "small"),
                    compute_type=body.get("compute_type") or os.environ.get("VIDEOCUT_ASR_COMPUTE_TYPE", "int8"),
                    # 默认开启 VAD；对纯音乐/环境音素材可显式关闭以暴露静音段（测试与排障用）
                    vad_filter=bool(body.get("vad_filter", True)),
                )
                self._json(result)
                return
            self._json({"error": "not found"}, 404)
        except FileNotFoundError as e:
            self._json({"error": str(e)}, 404)
        except Exception as e:  # noqa: BLE001
            self._json({"error": "%s: %s" % (type(e).__name__, e)}, 500)


def main():
    ap = argparse.ArgumentParser(description="videocut ASR sidecar (faster-whisper)")
    ap.add_argument("--host", default="127.0.0.1", help="监听地址（仅本地回环）")
    ap.add_argument("--port", type=int, default=4178)
    args = ap.parse_args()

    if args.host not in ("127.0.0.1", "localhost", "::1"):
        sys.stderr.write("安全限制：sidecar 仅允许监听 127.0.0.1/localhost\n")
        sys.exit(2)

    srv = ThreadingHTTPServer((args.host, args.port), Handler)
    sys.stderr.write("[asr-sidecar] listening on http://%s:%d (model lazy-loaded)\n" % (args.host, args.port))
    try:
        srv.serve_forever()
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
