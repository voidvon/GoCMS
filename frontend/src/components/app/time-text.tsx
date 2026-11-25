import { formatDateTime, formatRelativeTime, type TimeSource } from "@/lib/time"

type TimeTextProps = {
  value?: string | null
  className?: string
  /** relative 显示"几分钟前"等相对时间，datetime 显示完整时间。 */
  mode?: "relative" | "datetime"
  /** wall 用于编辑填写的本地日期或时间。 */
  source?: TimeSource
  /** 值为空时展示的占位文案。 */
  placeholder?: string
  /** 附加在悬停提示里的补充信息，例如使用 IP。 */
  hint?: string
}

/** 统一的时间展示：按浏览器时区渲染，鼠标悬停可看到完整时间。 */
export function TimeText({ value, className, mode = "relative", source = "instant", placeholder = "-", hint }: TimeTextProps) {
  const exact = formatDateTime(value, source)
  const label = mode === "relative" ? formatRelativeTime(value, source) : exact
  if (!label) return <span className={className}>{placeholder}</span>
  const title = [exact === label ? "" : exact, hint].filter(Boolean).join(" · ")
  return (
    <span className={className} title={title || undefined}>
      {label}
    </span>
  )
}
