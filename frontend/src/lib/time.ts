// 统一时间显示规则：所有前端时间都按浏览器所在时区呈现。
//
// 后端 API 输出带时区的 RFC3339 瞬时时间，SQLite 里遗留的
// "YYYY-MM-DD HH:MM:SS" 一律按 UTC 解读；content.published_at 这类由用户填写的
// 挂钟时间不带时区，按字面展示，不做换算。
const MINUTE = 60_000
const HOUR = 60 * MINUTE
const WEEKDAYS = ["周日", "周一", "周二", "周三", "周四", "周五", "周六"]

const pad = (value: number) => String(value).padStart(2, "0")

/** 解析后端时间戳为瞬时时间；空值或无法识别的值返回 null。 */
export function parseInstant(value?: string | null): Date | null {
  const raw = (value ?? "").trim()
  if (!raw) return null
  if (/(?:Z|[+-]\d{2}:?\d{2})$/i.test(raw)) {
    const parsed = new Date(raw)
    return Number.isNaN(parsed.getTime()) ? null : parsed
  }
  const match = /^(\d{4}-\d{2}-\d{2})(?:[ T](\d{2}:\d{2}(?::\d{2})?(?:\.\d+)?))?$/.exec(raw)
  if (!match) return null
  const time = match[2] ?? "00:00:00"
  const parsed = new Date(`${match[1]}T${time.length === 5 ? `${time}:00` : time}Z`)
  return Number.isNaN(parsed.getTime()) ? null : parsed
}

/** 浏览器时区下的完整时间：2026-10-18 20:33:24 */
export function formatDateTime(value?: string | null): string {
  const date = parseInstant(value)
  if (!date) return ""
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
}

/** 浏览器时区下的日期：2026-10-18 */
export function formatDate(value?: string | null): string {
  const date = parseInstant(value)
  if (!date) return ""
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}

/** 本地日期（YYYY-MM-DD）当天 00:00 对应的瞬时时间，输出 RFC3339。 */
export function localDayStart(value: string): string {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec((value ?? "").trim())
  if (!match) return value
  const date = new Date(Number(match[1]), Number(match[2]) - 1, Number(match[3]))
  return Number.isNaN(date.getTime()) ? value : date.toISOString()
}

/** 挂钟时间（用户填写、不带时区）按字面展示：2026/10/18 */
export function formatWallDate(value?: string | null): string {
  const raw = (value ?? "").trim()
  if (!raw) return ""
  const date = raw.slice(0, 10)
  return /^\d{4}-\d{2}-\d{2}$/.test(date) ? date.replaceAll("-", "/") : raw
}

/**
 * 相对时间：刚刚、N 分钟前、N 小时前、昨天、前天、本周、上周、本月、上月，
 * 更早的时间显示具体日期，未来时间直接显示具体日期。
 */
export function formatRelativeTime(value?: string | null): string {
  const date = parseInstant(value)
  if (!date) return ""
  const now = new Date()
  const elapsed = now.getTime() - date.getTime()
  const clock = `${pad(date.getHours())}:${pad(date.getMinutes())}`
  const day = `${date.getMonth() + 1}月${date.getDate()}日`
  if (elapsed >= 0 && elapsed < MINUTE) return "刚刚"
  if (elapsed >= MINUTE && elapsed < HOUR) return `${Math.floor(elapsed / MINUTE)} 分钟前`
  const days = dayDiff(now, date)
  if (days === 0 && elapsed >= 0) return `${Math.floor(elapsed / HOUR)} 小时前`
  if (days === 1) return `昨天 ${clock}`
  if (days === 2) return `前天 ${clock}`
  const weeks = dayDiff(startOfWeek(now), startOfWeek(date)) / 7
  if (weeks === 0) return `${WEEKDAYS[date.getDay()]} ${clock}`
  if (weeks === 1) return `上周${WEEKDAYS[date.getDay()].slice(1)} ${clock}`
  const months = (now.getFullYear() - date.getFullYear()) * 12 + (now.getMonth() - date.getMonth())
  if (months === 0) return `本月${date.getDate()}日 ${clock}`
  if (months === 1) return `上月${date.getDate()}日 ${clock}`
  if (date.getFullYear() === now.getFullYear()) return `${day} ${clock}`
  return `${date.getFullYear()}年${day} ${clock}`
}

/** 自然日之差，忽略时分秒。 */
function dayDiff(from: Date, to: Date) {
  const start = new Date(from.getFullYear(), from.getMonth(), from.getDate()).getTime()
  const end = new Date(to.getFullYear(), to.getMonth(), to.getDate()).getTime()
  return Math.round((start - end) / 86_400_000)
}

/** 周一为一周的开始。 */
function startOfWeek(date: Date) {
  const start = new Date(date.getFullYear(), date.getMonth(), date.getDate())
  start.setDate(start.getDate() - ((start.getDay() + 6) % 7))
  return start
}
