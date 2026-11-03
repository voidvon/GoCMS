import { useCallback, useEffect, useRef, useState } from "react"
import { ArrowUpRight, CodeXml, ExternalLink, FileText, Globe, Languages, LoaderCircle, RefreshCw } from "lucide-react"
import { generatePublishIndexes, getPublication, getPublishTargets, publishSite, type IndexFormat, type Publication, type PublishIndexFile, type PublishTarget } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent } from "@/components/ui/card"
import { ScrollArea } from "@/components/ui/scroll-area"
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

function IndexFileRow({ file, target, generating, disabled, onGenerate }: {
  file: PublishIndexFile
  target: PublishTarget
  generating: boolean
  disabled: boolean
  onGenerate: () => void
}) {
  const { title, description } = indexLabels[file.format]
  const Icon = file.format === "xml" ? CodeXml : FileText
  const canView = file.exists && !target.issue

  return (
    <div className="flex flex-col gap-3 px-4 py-4 sm:flex-row sm:items-center sm:justify-between sm:px-5">
      <div className="flex min-w-0 items-start gap-3">
        <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
          <Icon className="size-4" />
        </span>
        <div className="min-w-0 space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <p className="text-sm font-medium">{title}</p>
            <Badge variant="outline" className="font-normal text-muted-foreground">{file.exists ? "已生成" : "未生成"}</Badge>
          </div>
          <p className="text-xs text-muted-foreground">{description}</p>
          {canView ? (
            <a className="inline-flex items-center gap-1 break-all text-xs text-primary hover:underline" href={file.url} target="_blank" rel="noopener noreferrer">
              {file.url}<ArrowUpRight className="size-3 shrink-0" />
            </a>
          ) : <p className="break-all font-mono text-xs text-muted-foreground">{target.url ? file.url : file.path}</p>}
          {file.aliases?.map(alias => (
            <p key={alias.path} className="text-xs text-muted-foreground">
              兼容地址：{alias.exists && !target.issue ? (
                <a className="inline-flex items-center gap-1 break-all text-primary hover:underline" href={alias.url} target="_blank" rel="noopener noreferrer">
                  {alias.path}<ArrowUpRight className="size-3 shrink-0" />
                </a>
              ) : <span className="font-mono">{alias.path} · 未生成</span>}
            </p>
          ))}
          {file.updated_at && <p className="text-xs text-muted-foreground">更新于 <TimeText value={file.updated_at} mode="datetime" /></p>}
        </div>
      </div>
      <div className="flex shrink-0 items-center gap-2 pl-12 sm:pl-4">
        {canView ? (
          <Button size="sm" variant="outline" nativeButton={false} role="link" aria-label={`查看${target.name}的${title}`} render={<a href={file.url} target="_blank" rel="noopener noreferrer" />}>
            <ExternalLink />查看
          </Button>
        ) : <Button size="sm" variant="outline" disabled><ExternalLink />查看</Button>}
        <Button size="sm" variant="outline" disabled={disabled} onClick={onGenerate} aria-label={`生成${target.name}的${title}`}>
          {generating ? <LoaderCircle className="animate-spin" /> : <RefreshCw />}
          {generating ? "生成中…" : "生成"}
        </Button>
      </div>
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
  const [notice, setNotice] = useState("")
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
    const refresh = async () => {
      try {
        const next = await getPublication(activeSiteId)
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
    const timer = setInterval(refresh, 2000)
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
    setNotice("")
    try {
      const result = await generatePublishIndexes(activeSiteId, target, format)
      if (!mounted.current) return
      setTargets(result.targets)
      const name = target === "all" ? "全部发布目标" : targets.find(item => item.id === target)?.name || "当前发布目标"
      const message = `${name}的${format === "all" ? "全部索引" : indexLabels[format].title}已生成。`
      setNotice(message)
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
          <p className="text-sm text-muted-foreground">主站合并子目录语言；独立语言站点各自生成。单独生成索引时使用已发布页面。</p>
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
      {notice && <p role="status" className="text-sm text-green-600 dark:text-green-400">{notice}</p>}
      {targetsLoading && targets.length === 0 && <p role="status" className="flex items-center gap-2 py-8 text-sm text-muted-foreground"><LoaderCircle className="size-4 animate-spin" />正在读取发布目标…</p>}

      {targets.map(target => {
        const standalone = target.mode === "standalone"
        const Icon = standalone ? Languages : Globe
        const groupGenerating = job?.target === "all" || (job?.target === target.id && job.format === "all")
        const disabled = busy || targetsLoading || !target.published || Boolean(target.issue)
        return (
          <Card key={target.id} className="gap-0 py-0" aria-label={`${target.name}发布目标`}>
            <div className="flex flex-col gap-4 border-b bg-muted/25 px-4 py-4 sm:flex-row sm:items-start sm:justify-between sm:px-5">
              <div className="flex min-w-0 items-start gap-3">
                <span className="flex size-10 shrink-0 items-center justify-center rounded-lg border bg-background"><Icon className="size-5 text-muted-foreground" /></span>
                <div className="min-w-0 space-y-2">
                  <div className="flex flex-wrap items-center gap-2"><h3 className="text-sm font-semibold">{target.name}{standalone ? "独立站" : ""}</h3><Badge variant="outline">{standalone ? "独立域名" : "主站与子目录"}</Badge></div>
                  {target.url ? <a className="inline-flex items-center gap-1 break-all text-sm text-muted-foreground hover:text-foreground hover:underline" href={target.url + "/"} target="_blank" rel="noopener noreferrer">{target.url}<ArrowUpRight className="size-3 shrink-0" /></a> : <p className="text-sm text-muted-foreground">{standalone ? "待配置域名" : "使用当前访问地址"}</p>}
                  <div className="flex flex-wrap gap-2">{target.languages.map(language => <span key={language.code} className="rounded-md border bg-background px-2 py-1 text-xs text-muted-foreground">{language.name} <span className="font-mono">{language.path}</span></span>)}</div>
                  {!target.issue && !target.published && <p className="text-xs text-muted-foreground">尚无已发布页面，请先执行全站发布。</p>}
                </div>
              </div>
              <Button size="sm" variant="outline" disabled={disabled} onClick={() => void generate(target.id, "all")} aria-label={`生成${target.name}的本组索引`}>
                {groupGenerating ? <LoaderCircle className="animate-spin" /> : <RefreshCw />}生成本组索引
              </Button>
            </div>
            {target.issue && <div className="border-b px-4 py-3 sm:px-5"><InlineAlert>{target.issue}。请检查<a className="underline" href={standalone ? "/admin/languages" : "/admin/sites"}>{standalone ? "多语言配置" : "站点配置"}</a>。</InlineAlert></div>}
            <div className="divide-y">{target.files.map(file => <IndexFileRow key={file.path} file={file} target={target} generating={Boolean(groupGenerating || (job?.target === target.id && job.format === file.format))} disabled={disabled} onGenerate={() => void generate(target.id, file.format)} />)}</div>
          </Card>
        )
      })}
    </ScrollArea>
  )
}
