import { useEffect, useRef, useState, type UIEvent } from "react"
import {
  AlertTriangle,
  Ban,
  Bug,
  Eye,
  EyeOff,
  Inbox,
  Link2Off,
  LoaderCircle,
  MonitorSmartphone,
  MousePointerClick,
  FileText,
  UsersRound,
} from "lucide-react"

import {
  addAccessLogIgnore,
  getAccessLogErrors,
  getAccessLogIgnores,
  getAccessLogPages,
  getStats,
  removeAccessLogIgnore,
  type AccessLogErrorStat,
  type AccessLogIgnore,
  type AccessLogMetrics,
  type AccessLogPageStat,
  type AdminStats,
} from "@/lib/api"
import { useSite } from "@/lib/site-context"
import { TimeText } from "@/components/app/time-text"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { ScrollArea } from "@/components/ui/scroll-area"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { InlineAlert } from "@/components/app/app-ui"

const PAGE_SIZE = 20

function formatNumber(value: number) {
  return new Intl.NumberFormat("zh-CN").format(value)
}

function statusTone(status: number) {
  if (status >= 500) return "bg-red-100 text-red-700 dark:bg-red-950 dark:text-red-300"
  if (status >= 400) return "bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-300"
  return "bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300"
}

export function DashboardPage() {
  const { activeSite } = useSite()
  const [payload, setPayload] = useState<{ siteId?: number; stats: AdminStats } | null>(null)
  const [error, setError] = useState("")
  const [errorRows, setErrorRows] = useState<AccessLogErrorStat[]>([])
  const [errorTotal, setErrorTotal] = useState(0)
  const [errorLoading, setErrorLoading] = useState(true)
  const [errorLoadingMore, setErrorLoadingMore] = useState(false)
  const [pageRows, setPageRows] = useState<AccessLogPageStat[]>([])
  const [pageTotal, setPageTotal] = useState(0)
  const [pageLoading, setPageLoading] = useState(true)
  const [pageLoadingMore, setPageLoadingMore] = useState(false)
  const [errorLoadError, setErrorLoadError] = useState("")
  const [pageLoadError, setPageLoadError] = useState("")
  const [ignoredPaths, setIgnoredPaths] = useState<AccessLogIgnore[]>([])
  const [ignoreBusyPath, setIgnoreBusyPath] = useState("")
  const [revision, setRevision] = useState(0)
  const errorMoreRef = useRef(false)
  const pageMoreRef = useRef(false)
  const siteIdRef = useRef(activeSite?.id)

  useEffect(() => {
    siteIdRef.current = activeSite?.id
    errorMoreRef.current = false
    pageMoreRef.current = false
    setIgnoredPaths([])
    setIgnoreBusyPath("")
  }, [activeSite?.id])

  useEffect(() => {
    let active = true
    getStats()
      .then((response) => {
        if (!active) return
        setPayload({ siteId: activeSite?.id, stats: response })
        setError("")
      })
      .catch((loadError) => {
        if (active) setError(loadError instanceof Error ? loadError.message : "统计加载失败")
      })
    return () => {
      active = false
    }
  }, [activeSite?.id, revision])

  // The dashboard cards are independently paginated so users can scroll for
  // more than the fixed top-N snapshot returned by /api/admin/stats. The stats
  // request still supplies the headline cards and status breakdown.
  useEffect(() => {
    if (!activeSite?.id) return
    let active = true
    setErrorRows([])
    setErrorTotal(0)
    setErrorLoading(true)
    setErrorLoadingMore(false)
    setPageRows([])
    setPageTotal(0)
    setPageLoading(true)
    setPageLoadingMore(false)
    setErrorLoadError("")
    setPageLoadError("")
    Promise.allSettled([
      getAccessLogErrors({ status_mode: "errors", sort: "recent", limit: PAGE_SIZE, offset: 0 }),
      getAccessLogPages({ limit: PAGE_SIZE, offset: 0 }),
      getAccessLogIgnores(),
    ])
      .then(([errorResult, pageResult, ignoreResult]) => {
        if (!active) return
        if (errorResult.status === "fulfilled") {
          setErrorRows(errorResult.value.items)
          setErrorTotal(errorResult.value.total)
        } else {
          setErrorLoadError(errorResult.reason instanceof Error ? errorResult.reason.message : "错误列表加载失败")
        }
        if (pageResult.status === "fulfilled") {
          setPageRows(pageResult.value.items)
          setPageTotal(pageResult.value.total)
        } else {
          setPageLoadError(pageResult.reason instanceof Error ? pageResult.reason.message : "热门页面加载失败")
        }
        if (ignoreResult.status === "fulfilled") {
          setIgnoredPaths(ignoreResult.value.items)
        } else {
          setErrorLoadError(ignoreResult.reason instanceof Error ? ignoreResult.reason.message : "忽略列表加载失败")
        }
      })
      .finally(() => {
        if (!active) return
        setErrorLoading(false)
        setPageLoading(false)
      })
    return () => {
      active = false
    }
  }, [activeSite?.id, revision])

  const errorHasMore = errorRows.length < errorTotal
  const pageHasMore = pageRows.length < pageTotal

  function loadMoreErrors() {
    if (errorMoreRef.current || errorLoading || !errorHasMore) return
    const siteId = activeSite?.id
    errorMoreRef.current = true
    setErrorLoadingMore(true)
    getAccessLogErrors({ status_mode: "errors", sort: "recent", limit: PAGE_SIZE, offset: errorRows.length })
      .then((data) => {
        if (siteIdRef.current !== siteId) return
        setErrorRows((previous) => {
          const seen = new Set(previous.map((item) => `${item.host}\u0000${item.status}\u0000${item.path}`))
          return [
            ...previous,
            ...data.items.filter((item) => !seen.has(`${item.host}\u0000${item.status}\u0000${item.path}`)),
          ]
        })
        setErrorTotal(data.total)
      })
      .catch((loadError: Error) => {
        if (siteIdRef.current === siteId) setErrorLoadError(loadError.message)
      })
      .finally(() => {
        errorMoreRef.current = false
        if (siteIdRef.current === siteId) setErrorLoadingMore(false)
      })
  }

  function loadMorePages() {
    if (pageMoreRef.current || pageLoading || !pageHasMore) return
    const siteId = activeSite?.id
    pageMoreRef.current = true
    setPageLoadingMore(true)
    getAccessLogPages({ limit: PAGE_SIZE, offset: pageRows.length })
      .then((data) => {
        if (siteIdRef.current !== siteId) return
        setPageRows((previous) => {
          const seen = new Set(previous.map((item) => item.path))
          return [...previous, ...data.items.filter((item) => !seen.has(item.path))]
        })
        setPageTotal(data.total)
      })
      .catch((loadError: Error) => {
        if (siteIdRef.current === siteId) setPageLoadError(loadError.message)
      })
      .finally(() => {
        pageMoreRef.current = false
        if (siteIdRef.current === siteId) setPageLoadingMore(false)
      })
  }

  function handleInfiniteScroll(event: UIEvent<HTMLDivElement>, loadMore: () => void, hasMore: boolean) {
    if (!hasMore) return
    const element = event.currentTarget
    if (element.scrollHeight - element.scrollTop - element.clientHeight < 80) {
      loadMore()
    }
  }

  async function handleIgnore(pathValue: string) {
    if (!pathValue || ignoreBusyPath) return
    setIgnoreBusyPath(pathValue)
    try {
      const result = await addAccessLogIgnore(pathValue)
      setIgnoredPaths((previous) => [result.item, ...previous.filter((item) => item.path !== result.item.path)])
      const removed = errorRows.filter((item) => item.path === pathValue).length
      setErrorRows((previous) => previous.filter((item) => item.path !== pathValue))
      setErrorTotal((previous) => Math.max(0, previous - removed))
      setRevision((value) => value + 1)
    } catch (ignoreError) {
      setErrorLoadError(ignoreError instanceof Error ? ignoreError.message : "添加忽略失败")
    } finally {
      setIgnoreBusyPath("")
    }
  }

  async function handleUnignore(pathValue: string) {
    if (!pathValue || ignoreBusyPath) return
    setIgnoreBusyPath(pathValue)
    try {
      await removeAccessLogIgnore(pathValue)
      setIgnoredPaths((previous) => previous.filter((item) => item.path !== pathValue))
      setRevision((value) => value + 1)
    } catch (ignoreError) {
      setErrorLoadError(ignoreError instanceof Error ? ignoreError.message : "取消忽略失败")
    } finally {
      setIgnoreBusyPath("")
    }
  }

  // Statistics are per site, so a payload from another site must not be shown.
  const stats = payload && payload.siteId === activeSite?.id ? payload.stats : null

  if (error) {
    return <InlineAlert className="p-4">{error}</InlineAlert>
  }

  if (!stats) {
    return (
      <div className="flex min-h-64 items-center justify-center text-muted-foreground">
        <LoaderCircle className="size-5 animate-spin" />
      </div>
    )
  }

  const metrics: AccessLogMetrics | undefined = stats.access?.metrics
  const statusBreakdown = stats.access?.status_breakdown ?? []

  const contentCards = [
    { label: "内容总数", value: stats.contents, detail: `${formatNumber(stats.visible_contents)} 条公开展示`, icon: FileText },
    { label: "客户留言", value: stats.messages, detail: `${formatNumber(stats.pending_messages)} 条待处理`, icon: Inbox },
    { label: "公开内容率", value: `${stats.contents ? Math.round((stats.visible_contents / stats.contents) * 100) : 0}%`, detail: "当前可见内容占比", icon: UsersRound },
  ]

  const trafficCards = metrics
    ? [
        {
          label: "今日访问",
          value: metrics.today_visits,
          detail: `今日错误 ${formatNumber(metrics.today_errors)} 次 · ${metrics.day_key}`,
          icon: MousePointerClick,
        },
        {
          label: "24 小时访问",
          value: metrics.recent_visits,
          detail: `24 小时错误 ${formatNumber(metrics.recent_errors)} 次`,
          icon: Eye,
        },
        {
          label: "24 小时访客",
          value: metrics.recent_users,
          detail: `其中新访客 ${formatNumber(metrics.recent_new_users)} 位（已排除爬虫）`,
          icon: UsersRound,
        },
        {
          label: "404 错误",
          value: metrics.total_404_errors,
          detail: `24 小时内 ${formatNumber(metrics.recent_404_errors)} 次 · 站内死链地址 ${formatNumber(metrics.internal_404_paths)} 个`,
          icon: Bug,
        },
        {
          label: "错误总数",
          value: metrics.total_errors,
          detail: `含 4xx / 5xx，保留 ${metrics.retention_days} 天`,
          icon: AlertTriangle,
        },
        {
          label: "站内死链",
          value: metrics.internal_404_paths,
          detail: `由有效站内页面触发的 404（累计 ${formatNumber(metrics.internal_404_errors)} 次命中），优先修复`,
          icon: Link2Off,
        },
        {
          label: "累计页面",
          value: metrics.total_pages,
          detail: `成功访问过的页面数（共 ${formatNumber(metrics.total_requests)} 条记录）`,
          icon: FileText,
        },
        {
          label: "24 小时设备",
          value: `${formatNumber(metrics.recent_pc_users)} / ${formatNumber(metrics.recent_mobile_users)}`,
          detail: "桌面端访客 / 移动端访客",
          icon: MonitorSmartphone,
        },
      ]
    : []

  return (
    <div className="space-y-6">
      {metrics ? (
        <section className="space-y-3">
          <div className="flex items-center justify-between">
            <h2 className="text-sm font-medium text-muted-foreground">访问与错误</h2>
            {metrics.internal_404_paths > 0 ? (
              <Badge variant="outline" className="gap-1.5 border-amber-300 text-amber-700 dark:text-amber-300">
                <AlertTriangle className="size-3.5" />
                {formatNumber(metrics.internal_404_paths)} 个站内死链地址待处理
              </Badge>
            ) : null}
          </div>
          <div className="grid grid-cols-2 gap-3 sm:gap-4 xl:grid-cols-4">
            {trafficCards.map((card) => {
              const Icon = card.icon
              return (
                <Card key={card.label} size="sm">
                  <CardHeader className="flex flex-row items-start justify-between">
                    <CardDescription>{card.label}</CardDescription>
                    <Icon className="size-4 text-muted-foreground" />
                  </CardHeader>
                  <CardContent>
                    <p className="text-xl font-semibold tracking-tight sm:text-2xl">
                      {typeof card.value === "number" ? formatNumber(card.value) : card.value}
                    </p>
                    <p className="mt-1 text-xs text-muted-foreground">{card.detail}</p>
                  </CardContent>
                </Card>
              )
            })}
          </div>
        </section>
      ) : null}

      <section className="grid gap-4 lg:grid-cols-3">
        <Card className="flex h-[480px] min-h-0 flex-col lg:col-span-2">
          <CardHeader>
            <CardTitle className="text-base">错误与死链</CardTitle>
            <CardAction>
              <Popover>
                <PopoverTrigger
                  render={
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      title="忽略列表"
                      aria-label={`忽略列表（${ignoredPaths.length} 条）`}
                    />
                  }
                >
                  <Ban />
                </PopoverTrigger>
                <PopoverContent side="bottom" align="end" className="w-80 p-0">
                  <div className="border-b px-3 py-2">
                    <p className="text-sm font-medium">已忽略路径</p>
                    <p className="mt-0.5 text-xs text-muted-foreground">按路径匹配，所有域名生效</p>
                  </div>
                  <ScrollArea className="max-h-64" contentClassName="p-2">
                    {ignoredPaths.length === 0 ? (
                      <p className="px-2 py-4 text-center text-xs text-muted-foreground">暂无忽略路径</p>
                    ) : (
                      ignoredPaths.map((item) => (
                        <div key={item.path} className="flex items-center justify-between gap-2 rounded-md px-2 py-1.5 hover:bg-muted">
                          <span className="min-w-0 flex-1 truncate font-mono text-xs" title={item.path}>{item.path}</span>
                          <Button
                            variant="ghost"
                            size="xs"
                            disabled={ignoreBusyPath === item.path}
                            onClick={() => void handleUnignore(item.path)}
                          >
                            取消忽略
                          </Button>
                        </div>
                      ))
                    )}
                  </ScrollArea>
                </PopoverContent>
              </Popover>
            </CardAction>
          </CardHeader>
          <CardContent className="flex min-h-0 flex-1 flex-col px-0">
            {errorLoadError ? (
              <p className="px-6 pb-3 text-xs text-destructive">{errorLoadError}</p>
            ) : null}
            {statusBreakdown.length > 0 ? (
              <div className="flex flex-wrap items-center gap-x-4 gap-y-2 border-b px-6 pb-3">
                <span className="text-xs font-medium text-muted-foreground">错误类型</span>
                {statusBreakdown.map((item) => (
                  <span key={item.status} className="inline-flex items-center gap-1.5">
                    <span className={`inline-flex rounded px-1.5 py-0.5 font-mono text-xs font-medium ${statusTone(item.status)}`}>
                      {item.status}
                    </span>
                    <span className="font-mono text-xs text-muted-foreground">{formatNumber(item.hits)}</span>
                  </span>
                ))}
              </div>
            ) : null}
            {errorLoading ? (
              <div className="flex h-40 items-center justify-center text-muted-foreground">
                <LoaderCircle className="size-5 animate-spin" />
              </div>
            ) : errorRows.length === 0 ? (
              <p className="px-6 py-6 text-sm text-muted-foreground">当前没有记录到任何 4xx / 5xx 错误。</p>
            ) : (
              <ScrollArea
                className="min-h-0 flex-1"
                orientation="both"
                onScroll={(event) => handleInfiniteScroll(event, loadMoreErrors, errorHasMore)}
              >
                <Table containerClassName="overflow-visible">
                  <TableHeader className="sticky top-0 z-20 bg-card">
                    <TableRow>
                      <TableHead className="w-[64px]">状态</TableHead>
                      <TableHead className="w-[150px]">域名</TableHead>
                      <TableHead>请求路径</TableHead>
                      <TableHead className="w-[64px] text-right">命中</TableHead>
                      <TableHead className="w-[64px] text-right">站内</TableHead>
                      <TableHead className="w-[140px]">最近出现</TableHead>
                      <TableHead className="w-[40px] text-right"><span className="sr-only">操作</span></TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {errorRows.map((item) => (
                      <TableRow key={`${item.host}-${item.status}-${item.path}`} className="group">
                        <TableCell>
                          <span className={`inline-flex rounded px-1.5 py-0.5 font-mono text-xs font-medium ${statusTone(item.status)}`}>
                            {item.status}
                          </span>
                        </TableCell>
                        <TableCell className="max-w-[150px] truncate text-xs" title={item.host || activeSite?.domain || ""}>
                          {item.host || activeSite?.domain || "—"}
                        </TableCell>
                        <TableCell className="max-w-[280px] truncate font-mono text-xs" title={item.path}>
                          {item.path}
                        </TableCell>
                        <TableCell className="text-right font-medium">{formatNumber(item.hits)}</TableCell>
                        <TableCell className="text-right text-muted-foreground">{formatNumber(item.internal_hits)}</TableCell>
                        <TableCell className="text-xs text-muted-foreground"><TimeText value={item.last_visited_at} /></TableCell>
                        <TableCell className="w-[40px] text-right">
                          <Button
                            variant="ghost"
                            size="icon-xs"
                            className="opacity-100 transition-opacity sm:opacity-0 sm:group-hover:opacity-100 sm:group-focus-within:opacity-100 focus-visible:opacity-100"
                            title="忽略此路径（全域名）"
                            aria-label={`忽略 ${item.path}`}
                            disabled={ignoreBusyPath === item.path}
                            onClick={() => void handleIgnore(item.path)}
                          >
                            {ignoreBusyPath === item.path ? <LoaderCircle className="animate-spin" /> : <EyeOff />}
                          </Button>
                        </TableCell>
                      </TableRow>
                    ))}
                    {errorLoadingMore ? (
                      <TableRow>
                        <TableCell colSpan={7} className="py-4 text-center text-xs text-muted-foreground">
                          <LoaderCircle className="mr-1.5 inline size-3.5 animate-spin" />加载更多…
                        </TableCell>
                      </TableRow>
                    ) : null}
                    {!errorLoadingMore && !errorHasMore && errorRows.length > PAGE_SIZE ? (
                      <TableRow>
                        <TableCell colSpan={7} className="py-4 text-center text-xs text-muted-foreground">
                          已加载全部
                        </TableCell>
                      </TableRow>
                    ) : null}
                  </TableBody>
                </Table>
              </ScrollArea>
            )}
          </CardContent>
        </Card>

        <Card className="flex h-[480px] min-h-0 flex-col">
          <CardHeader>
            <CardTitle className="text-base">热门页面</CardTitle>
            <CardDescription>访问量最高的页面（已排除爬虫），滚动加载更多</CardDescription>
          </CardHeader>
          <CardContent className="flex min-h-0 flex-1 flex-col p-0">
            {pageLoadError ? (
              <p className="px-6 pb-3 pt-4 text-xs text-destructive">{pageLoadError}</p>
            ) : null}
            {pageLoading ? (
              <div className="flex h-40 items-center justify-center text-muted-foreground">
                <LoaderCircle className="size-5 animate-spin" />
              </div>
            ) : pageRows.length === 0 ? (
              <p className="px-6 py-4 text-sm text-muted-foreground">还没有页面访问记录。</p>
            ) : (
              <ScrollArea
                className="min-h-0 flex-1"
                contentClassName="space-y-3 px-6 py-4"
                onScroll={(event) => handleInfiniteScroll(event, loadMorePages, pageHasMore)}
              >
                {pageRows.map((item, index) => (
                  <div key={item.path} className="flex items-start justify-between gap-3">
                    <div className="flex min-w-0 items-start gap-2">
                      <span className="mt-0.5 w-4 shrink-0 text-xs text-muted-foreground">{index + 1}</span>
                      <div className="min-w-0">
                        <p className="truncate font-mono text-xs" title={item.path}>{item.path}</p>
                        <p className="mt-0.5 text-xs text-muted-foreground">
                          {formatNumber(item.visits)} 次访问 · {formatNumber(item.unique_ips)} 位访客
                        </p>
                      </div>
                    </div>
                  </div>
                ))}
                {pageLoadingMore ? (
                  <p className="py-2 text-center text-xs text-muted-foreground">
                    <LoaderCircle className="mr-1.5 inline size-3.5 animate-spin" />加载更多…
                  </p>
                ) : null}
                {!pageLoadingMore && !pageHasMore && pageRows.length > PAGE_SIZE ? (
                  <p className="py-2 text-center text-xs text-muted-foreground">已加载全部</p>
                ) : null}
              </ScrollArea>
            )}
          </CardContent>
        </Card>
      </section>

      <section className="space-y-3">
        <h2 className="text-sm font-medium text-muted-foreground">站点内容</h2>
        <div className="grid grid-cols-2 gap-3 sm:gap-4 xl:grid-cols-4">
          {contentCards.map((card) => {
            const Icon = card.icon
            return (
              <Card key={card.label} size="sm">
                <CardHeader className="flex flex-row items-start justify-between">
                  <CardDescription>{card.label}</CardDescription>
                  <Icon className="size-4 text-muted-foreground" />
                </CardHeader>
                <CardContent>
                  <p className="text-xl font-semibold tracking-tight sm:text-2xl">
                    {typeof card.value === "number" ? formatNumber(card.value) : card.value}
                  </p>
                  <p className="mt-1 text-xs text-muted-foreground">{card.detail}</p>
                </CardContent>
              </Card>
            )
          })}
          <Card size="sm">
            <CardHeader>
              <CardTitle className="text-base">待处理事项</CardTitle>
              <CardDescription>按优先级查看后台工作</CardDescription>
            </CardHeader>
            <CardContent className="space-y-2">
              <div className="flex items-center justify-between text-sm">
                <span className="text-muted-foreground">未读留言</span>
                <span className="font-medium">{formatNumber(stats.pending_messages)}</span>
              </div>
              <div className="flex items-center justify-between text-sm">
                <span className="text-muted-foreground">隐藏内容</span>
                <span className="font-medium">{formatNumber(stats.contents - stats.visible_contents)}</span>
              </div>
              {metrics ? (
                <div className="flex items-center justify-between text-sm">
                  <span className="text-muted-foreground">站内死链</span>
                  <span className="font-medium">{formatNumber(metrics.internal_404_paths)}</span>
                </div>
              ) : null}
            </CardContent>
          </Card>
        </div>
      </section>
    </div>
  )
}
