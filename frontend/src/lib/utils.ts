export { cn } from "cn"

const pad = (value: number) => String(value).padStart(2, "0")

// Access log timestamps arrive as RFC3339 instants, so they can be rendered in
// the zone of the viewer instead of the zone of the server.
export function formatVisitedAt(value: string) {
  const raw = (value ?? "").trim()
  if (!raw) return ""
  // Without a time part the value was pre-formatted by an older backend; it is
  // already a wall clock, so leave it untouched.
  if (!raw.includes("T")) return raw
  const date = new Date(raw)
  if (Number.isNaN(date.getTime())) return raw
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
}
