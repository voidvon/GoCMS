import { useCallback, useEffect, useRef, useState } from "react"
import { ExternalLink, LoaderCircle, RefreshCw } from "lucide-react"
import { generatePublishIndexes, getPublication, getPublishTargets, publishSite, type IndexFormat, type Publication, type PublishIndexFile, type PublishTarget } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent } from "@/components/ui/card"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { InlineAlert } from "@/components/app/app-ui"
import { showSuccess } from "@/components/app/admin-notifications"
import { useSite } from "@/lib/site-context"
import { TimeText } from "@/components/app/time-text"

const indexLabels: Record<IndexFormat, { title: string; description: string }> = {
  html: { title: "网站地图", description: "供访客浏览的页面索引" },
  xml: { title: "Sitemap XML", description: "供搜索引擎收录的页面地址" },
  llms: { title: "llms.txt", description: "供 AI 阅读的标题、简介与来源链接" },
}

type IndexJob = { target: string; format: IndexFormat | "all" }

const indexFormats: IndexFormat[] = ["html", "xml", "llms"]

function IndexFileCell({ file, label, generating, disabled, onGenerate }: {
  file?: PublishIndexFile
  label: string
  generating: boolean
  disabled: boolean
  onGenerate: () => void
}) {
  return (
    <div className="flex items-center gap-1">
      {file?.exists ? (
        <Button size="sm" variant="link" className="px-0" nativeButton={false} aria-label={`查看${label}`} render={<a href={file.url} target="_blank" rel="noopener noreferrer" />}>
          查看<ExternalLink className="size-3" />
        </Button>
      ) : <span className="text-xs text-muted-foreground">未生成</span>}
      <Button size="icon-sm" variant="ghost" disabled={disabled || !file} onClick={onGenerate} aria-label={`生成${label}`} title={`生成${label}`}>
        {generating ? <LoaderCircle className="size-3.5 animate-spin" /> : <RefreshCw className="size-3.5" />}
      </Button>
    </div>
  )
}

export function PublishPage() {
  const { activeSite, activeSiteId } = useSite()
  const [report, setReport] = useState<Publication | null>(null)
  const [targets, setTargets] = useState<PublishTarget[]>([])
  const [targetsLoading, setTargetsLoading] = useState(true)
  const [error, setError] = useState("")
  const [indexError, setIndexError] = useState("")
  const [starting, setStarting] = useState(false)
  const [job, setJob] = useState<IndexJob | null>(null)
  const previousReport = useRef<Publication | null>(null)
  const isPublishing = useRef(false)
  const mounted = useRef(false)

  const refreshTargets = useCallback(async () => {
    setTargetsLoading(true)
    try {
      const result = await getPublishTargets(activeSiteId)
      if (mounted.current) {
        setTargets(result.targets)
        setIndexError("")
      }
    } catch (e) {
      if (mounted.current) setIndexError(e instanceof Error ? e.message : "无法读取发布目标")
    } finally {
      if (mounted.current) setTargetsLoading(false)
    }
  }, [activeSiteId])

  useEffect(() => {
    mounted.current = true
    void refreshTargets()
    let active = true
    const refresh = async (background = false) => {
      try {
        const next = await getPublication(activeSiteId, background)
        if (!active) return
        const previous = previousReport.current
        if ((isPublishing.current || previous?.state === "running") && next.state === "success") {
          showSuccess("发布成功")
          isPublishing.current = false
        } else if (next.state === "failed") {
          isPublishing.current = false
        }
        if (previous && next.state !== "running" && (previous.state !== next.state || previous.finished !== next.finished)) {
          void refreshTargets()
        }
        previousReport.current = next
        setReport(next)
        setError("")
      } catch (e) {
        if (active) setError(e instanceof Error ? e.message : "无法读取发布状态")
      }
    }
    void refresh()
    const timer = setInterval(() => void refresh(true), 2000)
    return () => {
      active = false
      mounted.current = false
      clearInterval(timer)
    }
  }, [activeSiteId, refreshTargets])

  async function publish() {
    setStarting(true)
    setError("")
    isPublishing.current = true
    try {
      const next = await publishSite(activeSiteId)
      if (!mounted.current) return
      setReport(next)
      if (next.state === "success") {
        showSuccess("发布成功")
        isPublishing.current = false
        void refreshTargets()
      }
      previousReport.current = next
    } catch (e) {
      if (mounted.current) {
        isPublishing.current = false
        setError(e instanceof Error ? e.message : "发布失败")
      }
    } finally {
      if (mounted.current) setStarting(false)
    }
  }

  async function generate(target: string, format: IndexFormat | "all") {
    setJob({ target, format })
    setIndexError("")
    try {
      const result = await generatePublishIndexes(activeSiteId, target, format)
      if (!mounted.current) return
      setTargets(result.targets)
      const name = target === "all" ? "全部发布目标" : targets.find(item => item.id === target)?.name || "当前发布目标"
      const message = `${name}的${format === "all" ? "全部索引" : indexLabels[format].title}已生成。`
      showSuccess(message)
    } catch (e) {
      if (mounted.current) {
        setIndexError(e instanceof Error ? e.message : "索引生成失败")
        // A filesystem error may occur after an earlier file was replaced.
        // Refresh actual file status without clearing the generation error.
        void getPublishTargets(activeSiteId).then(result => {
          if (mounted.current) setTargets(result.targets)
        }).catch(() => {})
      }
    } finally {
      if (mounted.current) setJob(null)
    }
  }

  const running = starting || report?.state === "running"
  const busy = running || job !== null
  const configurationIncomplete = targets.some(target => target.issue)
  const allReady = targets.length > 0 && targets.every(target => target.published && !target.issue)
  const mainTarget = targets.find(target => target.id === "main")
  const languagesCount = targets.reduce((count, target) => count + target.languages.length, 0)
  const stateLabel = running ? "正在发布" : report?.state === "success" ? "发布完成" : report?.state === "failed" ? "发布失败" : "尚未发布"

  return (
    <ScrollArea className="h-full" contentClassName="space-y-6 pb-1 pr-3">
      {error && <InlineAlert>{error}</InlineAlert>}
      {report?.error && <InlineAlert>{report.error}</InlineAlert>}
      <Card>
        <CardContent className="space-y-5">
          <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
            <div className="space-y-2">
              <div className="flex flex-wrap items-center gap-2">
                <h2 className="font-semibold">{activeSite?.name || "当前站点"}</h2>
                <Badge variant={report?.state === "failed" ? "destructive" : "secondary"}>{running && <LoaderCircle className="size-3 animate-spin" />}{stateLabel}</Badge>
              </div>
              <p className="max-w-2xl text-sm text-muted-foreground">全站发布会生成所有启用语言的页面、栏目分页，以及每个发布目标的网站地图和 llms.txt。</p>
            </div>
            <div className="flex shrink-0 flex-wrap gap-2">
              {mainTarget && !mainTarget.issue && (
                <Button size="sm" variant="outline" nativeButton={false} role="link" render={<a href={mainTarget.url || "/"} target="_blank" rel="noopener noreferrer" />}>
                  <ExternalLink />查看网站
                </Button>
              )}
              <Button size="sm" disabled={!report || busy || targetsLoading || configurationIncomplete} onClick={() => void publish()}>
                {running ? <LoaderCircle className="animate-spin" /> : <RefreshCw />}
                {running ? "正在发布…" : "全站发布"}
              </Button>
            </div>
          </div>
          <div className="grid grid-cols-2 gap-5 border-t pt-5 lg:grid-cols-4">
            <div><p className="text-xs text-muted-foreground">发布目标</p><p className="mt-1 text-xl font-semibold">{targetsLoading ? "—" : targets.length}</p></div>
            <div><p className="text-xs text-muted-foreground">启用语言</p><p className="mt-1 text-xl font-semibold">{targetsLoading ? "—" : languagesCount}</p></div>
            <div><p className="text-xs text-muted-foreground">最近任务生成文件</p><p className="mt-1 text-xl font-semibold">{report?.state === "success" ? report.files : "—"}</p></div>
            <div><p className="text-xs text-muted-foreground">最近完成</p><p className="mt-2 text-sm">{report?.finished && !report.finished.startsWith("0001") ? <TimeText value={report.finished} mode="datetime" /> : "尚无记录"}</p></div>
          </div>
          {report?.content_id ? <p className="text-xs text-muted-foreground">最近任务为内容 #{report.content_id} 的单条更新；此类更新不会刷新网站索引。</p> : null}
        </CardContent>
      </Card>

      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="space-y-1">
          <h2 className="font-semibold">网站索引</h2>
          <p className="text-sm text-muted-foreground">每种语言一行；子目录语言共用主站索引，生成时会一起更新。</p>
        </div>
        <div className="flex shrink-0 gap-2">
          <Button size="sm" variant="outline" disabled={busy || targetsLoading} onClick={() => void refreshTargets()} aria-label="刷新文件状态">
            <RefreshCw className={targetsLoading ? "animate-spin" : ""} />刷新状态
          </Button>
          <Button size="sm" variant="outline" disabled={busy || targetsLoading || !allReady} onClick={() => void generate("all", "all")}>
            {job?.target === "all" ? <LoaderCircle className="animate-spin" /> : <RefreshCw />}
            生成全部索引
          </Button>
        </div>
      </div>
      {indexError && <InlineAlert>{indexError}</InlineAlert>}
      {targetsLoading && targets.length === 0 && <p role="status" className="flex items-center gap-2 py-8 text-sm text-muted-foreground"><LoaderCircle className="size-4 animate-spin" />正在读取发布目标…</p>}

      <div className="overflow-hidden rounded-lg border">
        <ScrollArea orientation="horizontal" className="w-full">
          <Table containerClassName="overflow-visible" className="min-w-[760px]">
            <TableHeader>
              <TableRow>
                <TableHead className="pl-4">语言</TableHead>
                <TableHead>类型</TableHead>
                <TableHead>发布地址</TableHead>
                {indexFormats.map(format => <TableHead key={format}>{indexLabels[format].title}</TableHead>)}
                <TableHead className="pr-4 text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {targets.flatMap(target => target.languages.map(language => {
                const disabled = busy || targetsLoading || !target.published || Boolean(target.issue)
                const groupGenerating = job?.target === "all" || (job?.target === target.id && job.format === "all")
                const address = `${target.url.replace(/\/$/, "")}/${language.path.replace(/^\//, "")}`
                return (
                  <TableRow key={`${target.id}:${language.code}`}>
                    <TableCell className="pl-4">
                      <p className="font-medium">{language.name} <span className="ml-1 text-xs font-normal text-muted-foreground">{language.code}</span></p>
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">{target.mode === "standalone" ? "独立站点" : "主站索引"}</TableCell>
                    <TableCell className="max-w-64 whitespace-normal">
                      {target.issue ? (
                        <a className="text-xs text-destructive underline" href={target.mode === "standalone" ? "/admin/languages" : "/admin/sites"}>{target.issue}，前往配置</a>
                      ) : <a className="break-all text-xs text-muted-foreground hover:text-foreground hover:underline" href={address} target="_blank" rel="noopener noreferrer">{address}</a>}
                    </TableCell>
                    {indexFormats.map(format => (
                      <TableCell key={format}>
                        {target.issue ? <span className="text-xs text-muted-foreground">待配置</span> : (
                          <IndexFileCell
                            file={target.files.find(file => file.format === format)}
                            label={`${language.name}的${indexLabels[format].title}`}
                            generating={Boolean(groupGenerating || (job?.target === target.id && job.format === format))}
                            disabled={disabled}
                            onGenerate={() => void generate(target.id, format)}
                          />
                        )}
                      </TableCell>
                    ))}
                    <TableCell className="pr-4 text-right">
                      <Button size="sm" variant="outline" disabled={disabled} onClick={() => void generate(target.id, "all")} aria-label={`生成${language.name}的全部索引`}>
                        {groupGenerating && <LoaderCircle className="animate-spin" />}生成索引
                      </Button>
                      {!target.issue && !target.published && <p className="mt-1 text-xs text-muted-foreground">请先全站发布</p>}
                    </TableCell>
                  </TableRow>
                )
              }))}
              {!targetsLoading && targets.every(target => target.languages.length === 0) && (
                <TableRow><TableCell colSpan={7} className="h-24 text-center text-muted-foreground">暂无启用语言</TableCell></TableRow>
              )}
            </TableBody>
          </Table>
        </ScrollArea>
      </div>
    </ScrollArea>
  )
}
