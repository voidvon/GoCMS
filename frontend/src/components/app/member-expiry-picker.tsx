import { useId, useState } from "react"
import { addDays, addMonths, format } from "date-fns"
import { zhCN } from "react-day-picker/locale"
import { CalendarIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Calendar } from "@/components/ui/calendar"
import { Input } from "@/components/ui/input"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"

const durations = [
  { label: "1天", days: 1 },
  { label: "7天", days: 7 },
  { label: "1个月", months: 1 },
  { label: "3个月", months: 3 },
  { label: "6个月", months: 6 },
  { label: "1年", months: 12 },
] as const

export function MemberExpiryPicker({ value, onChange, base, disabled = false }: {
  value: string
  base: Date
  onChange: (value: string) => void
  disabled?: boolean
}) {
  const [open, setOpen] = useState(false)
  const id = useId()
  const date = value ? new Date(value) : undefined
  const presets = durations.map((duration) => ({
    label: duration.label,
    value: format("days" in duration ? addDays(base, duration.days) : addMonths(base, duration.months), "yyyy-MM-dd'T'HH:mm"),
  }))
  const selected = value ? presets.find((preset) => preset.value === value)?.label ?? "自定义" : "永久"

  return <div className="space-y-3">
    <fieldset disabled={disabled} className="space-y-2">
      <legend className="text-sm font-medium">有效期</legend>
      <div className="flex flex-wrap gap-2">
        {[...presets, { label: "永久", value: "" }].map((preset) =>
          <Button key={preset.label} type="button" size="sm" disabled={disabled}
            variant={selected === preset.label ? "default" : "outline"}
            aria-pressed={selected === preset.label} onClick={() => onChange(preset.value)}>
            {preset.label}
          </Button>
        )}
      </div>
    </fieldset>
    <div className="grid gap-3 sm:grid-cols-[1fr_8rem]">
      <div className="space-y-1">
        <label htmlFor={`${id}-date`} className="text-sm">到期日期（本地时间）</label>
        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger render={<Button id={`${id}-date`} type="button" variant="outline" disabled={disabled} className="w-full justify-start font-normal" />}>
            <CalendarIcon className="size-4" />{date ? format(date, "yyyy年MM月dd日") : "永久（可选择日期）"}
          </PopoverTrigger>
          <PopoverContent align="start" className="w-auto p-0">
            <Calendar mode="single" required selected={date} defaultMonth={date ?? base} locale={zhCN}
              captionLayout="dropdown" startMonth={new Date(1970, 0)} endMonth={new Date(2100, 11)}
              disabled={disabled} onSelect={(day) => {
                if (!day) return
                const time = date ?? base
                day.setHours(time.getHours(), time.getMinutes(), 0, 0)
                onChange(format(day, "yyyy-MM-dd'T'HH:mm"))
                setOpen(false)
              }} />
          </PopoverContent>
        </Popover>
      </div>
      <div className="space-y-1">
        <label htmlFor={`${id}-time`} className="text-sm">到期时间</label>
        <Input id={`${id}-time`} type="time" required={!!value} disabled={disabled || !date}
          value={date ? format(date, "HH:mm") : ""}
          onChange={(event) => {
            if (date && event.target.value) onChange(`${format(date, "yyyy-MM-dd")}T${event.target.value}`)
          }} />
      </div>
    </div>
    <p role="status" className="text-sm text-muted-foreground">当前：{selected}{date ? ` · ${format(date, "yyyy-MM-dd HH:mm")} 到期` : " · 无到期时间"}</p>
    <p className="text-xs text-muted-foreground">快捷期限从打开弹窗时起算，按自然日、自然月计算。可通过日历和时间调整为自定义有效期。</p>
  </div>
}
