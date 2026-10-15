// 时间格式化工具：统一 mm:ss.s 展示与解析（v1.6 UI 改版）

/** 秒 → mm:ss.s（如 65.2 → "01:05.2"） */
export function fmtTime(s: number): string {
  if (!isFinite(s) || s < 0) s = 0
  const m = Math.floor(s / 60)
  const sec = s - m * 60
  return `${String(m).padStart(2, '0')}:${sec.toFixed(1).padStart(4, '0')}`
}

/** 秒 → 紧凑展示（如 65.2 → "1:05.2"），用于行内/表格 */
export function fmtShort(s: number): string {
  if (!isFinite(s) || s < 0) s = 0
  const m = Math.floor(s / 60)
  const sec = s - m * 60
  return `${m}:${sec.toFixed(1).padStart(4, '0')}`
}

/**
 * 解析用户输入的时间，支持：
 *  - "mm:ss.s" / "m:ss.s" / "ss.s" / "ss"
 *  - "hh:mm:ss.s"
 * 返回秒；非法输入返回 null。
 */
export function parseTime(input: string): number | null {
  const t = input.trim()
  if (!t) return null
  const parts = t.split(':')
  if (parts.length > 3) return null
  let total = 0
  for (const p of parts) {
    const v = Number(p)
    if (!isFinite(v) || v < 0 || p.trim() === '') return null
    total = total * 60 + v
  }
  return total
}
