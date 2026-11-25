import { CircleCheck, CircleAlert, LoaderCircle } from "lucide-react"
import { usePublication } from "@/lib/publication-context"
import { Button } from "@/components/ui/button"
import { Popover, PopoverContent, PopoverTitle, PopoverTrigger } from "@/components/ui/popover"
import { TimeText } from "@/components/app/time-text"

export function PublicationStatus() {
  const { report, syncError } = usePublication()
  if ((!report || report.state === "idle") && !syncError) return null

  const progress = report?.progress
  let label = "准备发布"
  if (syncError) label = "状态待同步"
  else if (report?.state === "success") label = "发布完成"
  else if (report?.state === "failed") label = "发布失败"
  else if (progress?.stage === "rendering") label = `已渲染 ${progress.completed} 页`
  else if (progress?.stage === "assembling") label = "整理发布文件"
  else if (progress?.stage === "writing") label = `写入文件 ${progress.completed}/${progress.total ?? 0}`
  else if (progress?.stage === "installing") label = "正在切换线上文件"

  const running = report?.state === "running"
  return (
    <Popover>
      <PopoverTrigger render={<Button size="sm" variant="outline" />}>
        {syncError || report?.state === "failed" ? <CircleAlert className="text-destructive" /> :
          running ? <LoaderCircle className="animate-spin" /> : <CircleCheck />}
        {label}{report?.queued && !syncError && " · 有更新排队"}
      </PopoverTrigger>
      <PopoverContent align="end" className="max-w-[calc(100vw-2rem)] p-3">
        <PopoverTitle>当前站点发布进度</PopoverTitle>
        <p>{label}</p>
        {syncError ? <p className="break-words text-muted-foreground">{syncError}，正在重试。</p> : (
          <>
            {report?.queued && <p>当前任务结束后，将继续处理排队的更新。</p>}
            {running && progress?.stage === "writing" && !!progress.total && (
              <progress aria-label="文件写入进度" value={progress.completed} max={progress.total} className="h-1.5 w-full" />
            )}
            {report?.state === "success" && <p>已更新 {report.files} 个文件</p>}
            {report?.error && <p className="break-words text-destructive">{report.error}</p>}
            {report && report.state !== "idle" && (
              <p className="text-xs text-muted-foreground">{running ? "开始时间：" : report.state === "success" ? "完成时间：" : "失败时间："}
                <TimeText value={running ? report.started : report.finished} />
              </p>
            )}
          </>
        )}
      </PopoverContent>
    </Popover>
  )
}
