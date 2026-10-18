import { formatDateTime, formatRelativeTime } from "@/lib/time"

type TimeTextProps = {
  value?: string | null
  className?: string
  /** relative 显示"几分钟前"等相对时间，datetime 显示完整时间。 */
  mode?: "relative" | "datetime"
  /** 值为空时展示的占位文案。 */
  placeholder?: string
  /** 附加在悬停提示里的补充信息，例如使用 IP。 */
  hint?: string
}

/** 统一的时间展示：按浏览器时区渲染，鼠标悬停可看到完整时间。 */
export function TimeText({ value, className, mode = "relative", placeholder = "-", hint }: TimeTextProps) {
  const label = mode === "relative" ? formatRelativeTime(value) : formatDateTime(value)
  if (!label) return <span className={className}>{placeholder}</span>
  const exact = formatDateTime(value)
  const title = [exact === label ? "" : exact, hint].filter(Boolean).join(" · ")
  return (
    <span className={className} title={title || undefined}>
      {label}
    </span>
  )
}
