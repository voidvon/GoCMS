import { useEffect, useRef, useState } from "react"
import { CodeXml, ExternalLink, FileText, Globe, LoaderCircle, RefreshCw } from "lucide-react"
import { generateLLMS, generateSitemap, getPublication, publishSite, type Publication, type SitemapFormat } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent } from "@/components/ui/card"
import { InlineAlert } from "@/components/app/app-ui"
import { showSuccess } from "@/components/app/admin-notifications"

const publicOrigin = import.meta.env.DEV ? "http://127.0.0.1:18080" : ""

function publicURL(path: string) {
  return `${publicOrigin}${path}`
}

function SitemapRow({
  format,
  title,
  description,
  path,
  generating,
  disabled,
  onGenerate,
}: {
  format: SitemapFormat | "llms"
  title: string
  description: string
  path: string
  generating: boolean
  disabled: boolean
  onGenerate: () => void
}) {
  const Icon = format !== "xml" ? FileText : CodeXml
  return (
    <div className="flex flex-col gap-3 p-4 sm:flex-row sm:items-center sm:justify-between">
      <div className="flex min-w-0 items-start gap-3">
        <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
          <Icon className="size-4" />
        </span>
        <div className="min-w-0">
          <p className="font-medium">{title}</p>
          <p className="text-xs text-muted-foreground">{description} · <code>{path}</code></p>
        </div>
      </div>
      <div className="flex shrink-0 items-center gap-2 sm:pl-4">
        <Button size="sm" variant="outline" render={<a href={publicURL(path)} target="_blank" rel="noreferrer" />}>
          <ExternalLink />
          查看
        </Button>
        <Button size="sm" disabled={disabled} onClick={onGenerate}>
          {generating ? <LoaderCircle className="animate-spin" /> : <RefreshCw />}
          {generating ? "生成中…" : "生成"}
        </Button>
      </div>
    </div>
  )
}

export function PublishPage() {
  const [report, setReport] = useState<Publication | null>(null)
  const [error, setError] = useState("")
  const [sitemapError, setSitemapError] = useState("")
  const [starting, setStarting] = useState(false)
  const [sitemapGenerating, setSitemapGenerating] = useState<SitemapFormat | "llms" | null>(null)
  const [sitemapNotice, setSitemapNotice] = useState("")

  const prevReportStateRef = useRef<string | null>(null)
  const isPublishingRef = useRef(false)

  useEffect(() => {
    let active = true
    const refresh = () =>
      getPublication()
        .then((r) => {
          if (!active) return
          if (
            (isPublishingRef.current || prevReportStateRef.current === "running") &&
            r.state === "success"
          ) {
            showSuccess("发布成功")
            isPublishingRef.current = false
          } else if (
            (isPublishingRef.current || prevReportStateRef.current === "running") &&
            r.state === "failed"
          ) {
            isPublishingRef.current = false
          }
          prevReportStateRef.current = r.state
          setReport(r)
          setError("")
        })
        .catch((e) => {
          if (active) setError(String(e))
        })

    void refresh()
    const timer = setInterval(refresh, 2000)
    return () => {
      active = false
      clearInterval(timer)
    }
  }, [])

  async function publish() {
    setStarting(true)
    setError("")
    isPublishingRef.current = true
    try {
      const next = await publishSite()
      setReport(next)
      if (next.state === "success") {
        showSuccess("发布成功")
        isPublishingRef.current = false
      }
      prevReportStateRef.current = next.state
    } catch (e) {
      isPublishingRef.current = false
      setError(e instanceof Error ? e.message : "发布失败")
    } finally {
      setStarting(false)
    }
  }

  async function generate(format: SitemapFormat | "llms") {
    setSitemapGenerating(format)
    setSitemapError("")
    setSitemapNotice("")
    try {
      if (format === "llms") await generateLLMS()
      else await generateSitemap(format)
      const notice = format === "llms" ? "llms.txt 已生成。" : format === "html" ? "网站地图已生成。" : "Sitemap XML 已生成。"
      setSitemapNotice(notice)
      showSuccess(notice)
    } catch (e) {
      setSitemapError(e instanceof Error ? e.message : "网站地图生成失败")
    } finally {
      setSitemapGenerating(null)
    }
  }

  const running = starting || report?.state === "running"
  const sitemapBusy = running || sitemapGenerating !== null

  return (
    <div className="space-y-4">
      {error && <InlineAlert>{error}</InlineAlert>}
      {report?.error && <InlineAlert>{report.error}</InlineAlert>}
      <Card>
        <CardContent className="space-y-5">
          <div className="flex flex-wrap items-center gap-3">
            <Button size="sm" disabled={!report || sitemapBusy} onClick={publish}>
              {running ? <LoaderCircle className="animate-spin" /> : <RefreshCw />}
              {running ? "生成中…" : "全站重新生成"}
            </Button>
            <Button
              size="sm"
              variant="outline"
              render={<a href={import.meta.env.DEV ? "http://127.0.0.1:18080/" : "/"} target="_blank" rel="noreferrer" />}
            >
              <Globe />
              查看网站
            </Button>
            {report && report.state !== "success" && (
              <Badge variant={report.state === "failed" ? "destructive" : "secondary"}>
                {report.state === "running" ? (
                  <span className="flex items-center gap-1">
                    <LoaderCircle className="size-3 animate-spin" />
                    正在生成
                  </span>
                ) : report.state === "failed" ? (
                  "发布失败"
                ) : (
                  "尚未发布"
                )}
              </Badge>
            )}
          </div>
          <p className="text-sm text-muted-foreground">覆盖主题配置的页面、内容详情、栏目分页、网站地图和 llms.txt。</p>
          <div className="border-t pt-5">
            <div>
              <h2 className="text-sm font-medium">网站索引</h2>
              <p className="mt-1 text-sm text-muted-foreground">全站发布会同时更新以下索引；单独生成时使用已发布页面。</p>
            </div>
            <div className="mt-4 divide-y rounded-lg border">
              <SitemapRow
                format="html"
                title="网站地图"
                description="HTML 页面索引"
                path="/sitemap.html"
                generating={sitemapGenerating === "html"}
                disabled={sitemapBusy}
                onGenerate={() => void generate("html")}
              />
              <SitemapRow
                format="xml"
                title="Sitemap XML"
                description="提交给搜索引擎的 XML 索引"
                path="/Sitemap.xml"
                generating={sitemapGenerating === "xml"}
                disabled={sitemapBusy}
                onGenerate={() => void generate("xml")}
              />
              <SitemapRow
                format="llms"
                title="llms.txt"
                description="供 AI 阅读的页面标题、简介和来源链接"
                path="/llms.txt"
                generating={sitemapGenerating === "llms"}
                disabled={sitemapBusy}
                onGenerate={() => void generate("llms")}
              />
            </div>
            {sitemapError && <InlineAlert className="mt-3">{sitemapError}</InlineAlert>}
            {sitemapNotice && <p className="mt-3 text-sm text-muted-foreground">{sitemapNotice}</p>}
          </div>
          {report?.state === "success" && (
            <div className="grid gap-4 sm:grid-cols-2">
              <div>
                <p className="text-sm text-muted-foreground">生成文件</p>
                <p className="text-2xl font-semibold">{report.files}</p>
              </div>
              <div>
                <p className="text-sm text-muted-foreground">公开内容</p>
                <p className="text-2xl font-semibold">{report.contents}</p>
              </div>
            </div>
          )}
          {report && report.finished && !report.finished.startsWith("0001") && (
            <p className="text-xs text-muted-foreground">最近完成：{new Date(report.finished).toLocaleString("zh-CN")}</p>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
