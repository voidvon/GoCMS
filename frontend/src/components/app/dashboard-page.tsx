import { useEffect, useRef, useState, type UIEvent } from "react"
import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  ArrowUpDown,
  Ban,
  Bug,
  Eye,
  EyeOff,
  Inbox,
  Link2Off,
  LoaderCircle,
  MonitorSmartphone,
  MousePointerClick,
  RefreshCw,
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
  type DashboardWindow,
} from "@/lib/api"
import { useSite } from "@/lib/site-context"
import { getErrorMessage } from "@/lib/errors"
import { showError, showSuccess } from "@/components/app/admin-notifications"
import { Skeleton } from "@/components/ui/skeleton"
import { TimeText } from "@/components/app/time-text"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
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
type ErrorSort = "recent" | "hits" | "hits_asc"
const windowOptions: { value: DashboardWindow; label: string }[] = [
  { value: "24h", label: "24小时" },
  { value: "today", label: "今天" },
  { value: "yesterday", label: "昨天" },
]

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
  const [timeWindow, setTimeWindow] = useState<DashboardWindow>("24h")
  const [errorSort, setErrorSort] = useState<ErrorSort>("recent")
  const [loading, setLoading] = useState(true)
  const [payload, setPayload] = useState<{ siteId: number; window: DashboardWindow; stats: AdminStats } | null>(null)
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
  const requestsRef = useRef<AbortController | null>(null)
  const errorRequestsRef = useRef<AbortController | null>(null)

  useEffect(() => {
    if (!activeSite?.id) return
    const siteId = activeSite.id
    const controller = new AbortController()
    requestsRef.current = controller
    const init = { signal: controller.signal, headers: { "X-Site-Id": String(siteId) } }
    pageMoreRef.current = false
    setLoading(true)
    setPayload(null)
    setError("")
    setPageRows([])
    setPageTotal(0)
    setPageLoading(true)
    setPageLoadingMore(false)
    setPageLoadError("")

    // Each block settles independently. Changing the site, range or revision
    // also cancels pending pagination.
    const requests = [
      getStats({ includeTop: false, window: timeWindow, refresh: revision > 0 }, init)
        .then((response) => {
          if (!controller.signal.aborted) setPayload({ siteId, window: timeWindow, stats: response })
        })
        .catch((loadError: unknown) => {
          if (!controller.signal.aborted) setError(getErrorMessage(loadError, "统计加载失败"))
        }),
      getAccessLogPages({ window: timeWindow, limit: PAGE_SIZE, offset: 0 }, init)
        .then((response) => {
          if (controller.signal.aborted) return
          setPageRows(response.items)
          setPageTotal(response.total)
        })
        .catch((loadError: unknown) => {
          if (!controller.signal.aborted) setPageLoadError(getErrorMessage(loadError, "热门页面加载失败"))
        })
        .finally(() => {
          if (!controller.signal.aborted) setPageLoading(false)
        }),
      getAccessLogIgnores(init)
        .then((response) => {
          if (!controller.signal.aborted) setIgnoredPaths(response.items)
        })
        .catch((loadError: unknown) => {
          if (!controller.signal.aborted) setErrorLoadError(getErrorMessage(loadError, "忽略列表加载失败"))
        }),
    ]
    void Promise.allSettled(requests).then(() => {
      if (!controller.signal.aborted) setLoading(false)
    })
    return () => {
      controller.abort()
      requestsRef.current = null
    }
  }, [activeSite?.id, revision, timeWindow])

  useEffect(() => {
    if (!activeSite?.id) return
    const controller = new AbortController()
    errorRequestsRef.current = controller
    errorMoreRef.current = false
    setErrorRows([])
    setErrorTotal(0)
    setErrorLoading(true)
    setErrorLoadingMore(false)
    setErrorLoadError("")
    getAccessLogErrors(
      { window: timeWindow, status_mode: "errors", sort: errorSort, limit: PAGE_SIZE, offset: 0 },
      { signal: controller.signal, headers: { "X-Site-Id": String(activeSite.id) } },
    )
      .then((response) => {
        if (controller.signal.aborted) return
        setErrorRows(response.items)
        setErrorTotal(response.total)
      })
      .catch((loadError: unknown) => {
        if (!controller.signal.aborted) setErrorLoadError(getErrorMessage(loadError, "错误列表加载失败"))
      })
      .finally(() => {
        if (!controller.signal.aborted) setErrorLoading(false)
      })
    return () => {
      controller.abort()
      errorRequestsRef.current = null
    }
  }, [activeSite?.id, revision, timeWindow, errorSort])

  const errorHasMore = errorRows.length < errorTotal
  const pageHasMore = pageRows.length < pageTotal

  function toggleErrorSort() {
    errorRequestsRef.current?.abort()
    setErrorLoading(true)
    setErrorSort((previous) => previous === "hits" ? "hits_asc" : "hits")
  }

  function loadMoreErrors() {
    if (errorMoreRef.current || errorLoading || !errorHasMore) return
    const controller = errorRequestsRef.current
    if (!controller || controller.signal.aborted || !activeSite?.id) return
    errorMoreRef.current = true
    setErrorLoadingMore(true)
    getAccessLogErrors({ window: timeWindow, status_mode: "errors", sort: errorSort, limit: PAGE_SIZE, offset: errorRows.length }, { signal: controller.signal, headers: { "X-Site-Id": String(activeSite.id) } })
      .then((data) => {
        if (controller.signal.aborted) return
        setErrorRows((previous) => {
          const seen = new Set(previous.map((item) => `${item.host}\u0000${item.status}\u0000${item.path}`))
          return [
            ...previous,
            ...data.items.filter((item) => !seen.has(`${item.host}\u0000${item.status}\u0000${item.path}`)),
          ]
        })
        setErrorTotal(data.total)
      })
      .catch((loadError: unknown) => {
        if (!controller.signal.aborted) setErrorLoadError(getErrorMessage(loadError, "错误列表加载失败"))
      })
      .finally(() => {
        if (controller.signal.aborted) return
        errorMoreRef.current = false
        setErrorLoadingMore(false)
      })
  }

  function loadMorePages() {
    if (pageMoreRef.current || pageLoading || !pageHasMore) return
    const controller = requestsRef.current
    if (!controller || controller.signal.aborted || !activeSite?.id) return
    pageMoreRef.current = true
    setPageLoadingMore(true)
    getAccessLogPages({ window: timeWindow, limit: PAGE_SIZE, offset: pageRows.length }, { signal: controller.signal, headers: { "X-Site-Id": String(activeSite.id) } })
      .then((data) => {
        if (controller.signal.aborted) return
        setPageRows((previous) => {
          const seen = new Set(previous.map((item) => item.path))
          return [...previous, ...data.items.filter((item) => !seen.has(item.path))]
        })
        setPageTotal(data.total)
      })
      .catch((loadError: unknown) => {
        if (!controller.signal.aborted) setPageLoadError(getErrorMessage(loadError, "热门页面加载失败"))
      })
      .finally(() => {
        if (controller.signal.aborted) return
        pageMoreRef.current = false
        setPageLoadingMore(false)
      })
  }

  function handleInfiniteScroll(event: UIEvent<HTMLDivElement>, loadMore: () => void, hasMore: boolean) {
    if (!hasMore) return
    // ScrollArea's viewport owns scrolling. Capture its non-bubbling event
    // at the root and measure the viewport rather than the outer container.
    const element = event.target
    if (!(element instanceof HTMLDivElement) || element.dataset.slot !== "scroll-area-viewport") return
    if (element.scrollHeight - element.scrollTop - element.clientHeight < 80) {
      loadMore()
    }
  }

  async function handleIgnore(pathValue: string) {
    if (!pathValue || ignoreBusyPath || !activeSite?.id) return
    setIgnoreBusyPath(pathValue)
    try {
      const result = await addAccessLogIgnore(pathValue, { headers: { "X-Site-Id": String(activeSite.id) } })
      setIgnoredPaths((previous) => [result.item, ...previous.filter((item) => item.path !== result.item.path)])
      const removed = errorRows.filter((item) => item.path === pathValue).length
      setErrorRows((previous) => previous.filter((item) => item.path !== pathValue))
      setErrorTotal((previous) => Math.max(0, previous - removed))
      setRevision((value) => value + 1)
      showSuccess("已忽略该路径")
    } catch (ignoreError) {
      showError(getErrorMessage(ignoreError, "添加忽略失败"))
    } finally {
      setIgnoreBusyPath("")
    }
  }

  async function handleUnignore(pathValue: string) {
    if (!pathValue || ignoreBusyPath || !activeSite?.id) return
    setIgnoreBusyPath(pathValue)
    try {
      await removeAccessLogIgnore(pathValue, { headers: { "X-Site-Id": String(activeSite.id) } })
      setIgnoredPaths((previous) => previous.filter((item) => item.path !== pathValue))
      setRevision((value) => value + 1)
      showSuccess("已取消忽略")
    } catch (ignoreError) {
      showError(getErrorMessage(ignoreError, "取消忽略失败"))
    } finally {
      setIgnoreBusyPath("")
    }
  }

  // Statistics are per site, so a payload from another site must not be shown.
  const stats = payload && payload.siteId === activeSite?.id && payload.window === timeWindow ? payload.stats : null
  const windowLabel = windowOptions.find((option) => option.value === timeWindow)!.label
  const refreshing = loading || errorLoading
  const sortAction = errorSort === "hits" ? "按命中次数从低到高排序" : "按命中次数从高到低排序"
  const SortIcon = errorSort === "hits" ? ArrowDown : errorSort === "hits_asc" ? ArrowUp : ArrowUpDown

  const metrics: AccessLogMetrics | undefined = stats?.access?.metrics
  const statusBreakdown = stats?.access?.status_breakdown ?? []

  const contentCards = stats ? [
    { label: "内容总数", value: stats.contents, detail: `${formatNumber(stats.visible_contents)} 条公开展示`, icon: FileText },
    { label: "客户留言", value: stats.messages, detail: `${formatNumber(stats.pending_messages)} 条待处理`, icon: Inbox },
    { label: "公开内容率", value: `${stats.contents ? Math.round((stats.visible_contents / stats.contents) * 100) : 0}%`, detail: "当前可见内容占比", icon: UsersRound },
  ] : []

  const trafficCards = metrics
    ? [
        {
          label: "页面访问",
          value: metrics.recent_visits,
          detail: `${windowLabel}的页面访问次数`,
          icon: MousePointerClick,
        },
        {
          label: "访客",
          value: metrics.recent_users,
          detail: `${windowLabel}按 IP 去重（已排除爬虫）`,
          icon: Eye,
        },
        {
          label: "新访客",
          value: metrics.recent_new_users,
          detail: "保留记录中首次访问的访客（已排除爬虫）",
          icon: UsersRound,
        },
        {
          label: "404 错误",
          value: metrics.total_404_errors,
          detail: `站内死链地址 ${formatNumber(metrics.internal_404_paths)} 个`,
          icon: Bug,
        },
        {
          label: "错误总数",
          value: metrics.total_errors,
          detail: `${windowLabel}的 4xx / 5xx 错误`,
          icon: AlertTriangle,
        },
        {
          label: "站内死链",
          value: metrics.internal_404_paths,
          detail: `由有效站内页面触发的 404（${formatNumber(metrics.internal_404_errors)} 次命中）`,
          icon: Link2Off,
        },
        {
          label: "访问页面",
          value: metrics.total_pages,
          detail: `成功访问的不同页面（共 ${formatNumber(metrics.total_requests)} 条请求）`,
          icon: FileText,
        },
        {
          label: "访问设备",
          value: `${formatNumber(metrics.recent_pc_users)} / ${formatNumber(metrics.recent_mobile_users)}`,
          detail: "桌面端访客 / 移动端访客",
          icon: MonitorSmartphone,
        },
      ]
    : []

  return (
    <div className="space-y-6">
      <div role="group" aria-label="仪表盘工具栏" className="flex flex-wrap items-center gap-2">
        <Select items={windowOptions} value={timeWindow} onValueChange={(value) => {
          if (value && value !== timeWindow) {
            setLoading(true)
            setTimeWindow(value)
          }
        }}>
          <SelectTrigger size="sm" aria-label="时间范围">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {windowOptions.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}
          </SelectContent>
        </Select>
        <Button variant="outline" size="sm" disabled={refreshing || !activeSite?.id} aria-label="刷新仪表盘" onClick={() => {
          setLoading(true)
          setRevision((value) => value + 1)
        }}>
          <RefreshCw className={refreshing ? "animate-spin" : ""} />
          {refreshing ? "刷新中…" : "刷新"}
        </Button>
      </div>
      {error ? <InlineAlert>{error}</InlineAlert> : null}
      {!stats && !error ? <StatCardsSkeleton count={8} /> : null}
      {metrics ? (
        <section className="space-y-3">
          {metrics.internal_404_paths > 0 ? (
            <div className="flex justify-end">
              <Badge variant="outline" className="gap-1.5 border-amber-300 text-amber-700 dark:text-amber-300">
                <AlertTriangle className="size-3.5" />
                {formatNumber(metrics.internal_404_paths)} 个站内死链地址待处理
              </Badge>
            </div>
          ) : null}
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
            <div className="flex min-w-0 items-center gap-3">
              <CardTitle className="shrink-0 text-base">错误与死链</CardTitle>
              {statusBreakdown.length > 0 ? (
                <ScrollArea orientation="horizontal" className="min-w-0 flex-1" contentClassName="flex items-center gap-3 whitespace-nowrap">
                  <span className="shrink-0 text-xs font-medium text-muted-foreground">错误类型</span>
                  {statusBreakdown.map((item) => (
                    <span key={item.status} className="inline-flex shrink-0 items-center gap-1.5">
                      <span className={`inline-flex rounded px-1.5 py-0.5 font-mono text-xs font-medium ${statusTone(item.status)}`}>
                        {item.status}
                      </span>
                      <span className="font-mono text-xs text-muted-foreground">{formatNumber(item.hits)}</span>
                    </span>
                  ))}
                </ScrollArea>
              ) : null}
            </div>
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
            {errorLoading ? (
              <div className="flex h-40 items-center justify-center text-muted-foreground">
                <LoaderCircle className="size-5 animate-spin" />
              </div>
            ) : errorRows.length === 0 ? (
              <p className="px-6 py-6 text-sm text-muted-foreground">所选时间范围内没有 4xx / 5xx 错误。</p>
            ) : (
              <ScrollArea
                className="min-h-0 flex-1"
                orientation="both"
                onScrollCapture={(event) => handleInfiniteScroll(event, loadMoreErrors, errorHasMore)}
              >
                <Table containerClassName="overflow-visible">
                  <TableHeader className="sticky top-0 z-20 bg-card">
                    <TableRow>
                      <TableHead className="w-[64px]">状态</TableHead>
                      <TableHead>请求地址</TableHead>
                      <TableHead className="w-[80px] text-right" aria-sort={errorSort === "hits" ? "descending" : errorSort === "hits_asc" ? "ascending" : "none"}>
                        <Button variant="ghost" size="xs" className="-mr-2 gap-1" aria-label={sortAction} title={sortAction} onClick={toggleErrorSort}>
                          命中
                          <SortIcon className="size-3.5" />
                        </Button>
                      </TableHead>
                      <TableHead className="w-[64px] text-right">站内</TableHead>
                      <TableHead className="w-[140px]">最近出现</TableHead>
                      <TableHead className="w-[40px] text-right"><span className="sr-only">操作</span></TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {errorRows.map((item) => {
                      const requestHost = item.host || activeSite?.domain || window.location.host
                      const requestAddress = `${requestHost}${item.path}`
                      const requestURL = `${window.location.protocol}//${requestHost}${item.path.split("/").map(encodeURIComponent).join("/")}`
                      return (
                        <TableRow key={`${item.host}-${item.status}-${item.path}`} className="group">
                          <TableCell>
                            <span className={`inline-flex rounded px-1.5 py-0.5 font-mono text-xs font-medium ${statusTone(item.status)}`}>
                              {item.status}
                            </span>
                          </TableCell>
                          <TableCell className="min-w-[240px] max-w-[440px] whitespace-normal break-all font-mono text-xs leading-5" title={requestAddress}>
                            <a
                              href={requestURL}
                              target="_blank"
                              rel="noopener noreferrer"
                              className="rounded-sm underline-offset-4 hover:text-primary hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                              aria-label={`${requestAddress}（在新标签页打开）`}
                            >
                              {requestAddress}
                            </a>
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
                      )
                    })}
                  </TableBody>
                </Table>
              </ScrollArea>
            )}
            {!errorLoading && errorRows.length > 0 ? (
              <ListLoadStatus count={errorRows.length} total={errorTotal} loading={errorLoadingMore} />
            ) : null}
          </CardContent>
        </Card>

        <Card className="flex h-[480px] min-h-0 flex-col">
          <CardHeader>
            <CardTitle className="text-base">热门页面</CardTitle>
            <CardDescription>{windowLabel}访问量最高的页面（已排除爬虫）</CardDescription>
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
              <p className="px-6 py-4 text-sm text-muted-foreground">所选时间范围内没有页面访问记录。</p>
            ) : (
              <ScrollArea
                className="min-h-0 flex-1"
                contentClassName="space-y-3 px-6 py-4"
                onScrollCapture={(event) => handleInfiniteScroll(event, loadMorePages, pageHasMore)}
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
              </ScrollArea>
            )}
            {!pageLoading && pageRows.length > 0 ? (
              <ListLoadStatus count={pageRows.length} total={pageTotal} loading={pageLoadingMore} />
            ) : null}
          </CardContent>
        </Card>
      </section>

      {stats ? <section className="space-y-3">
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
      </section> : !error ? <StatCardsSkeleton title="站点内容" count={4} /> : null}
    </div>
  )
}

function ListLoadStatus({ count, total, loading }: { count: number; total: number; loading: boolean }) {
  return (
    <div role="status" className="flex shrink-0 flex-wrap items-center justify-between gap-2 border-t px-6 pt-3 text-xs text-muted-foreground">
      <span>已加载 {formatNumber(count)} / {formatNumber(total)} 条</span>
      <span className="inline-flex items-center gap-1.5">
        {loading ? <LoaderCircle className="size-3.5 animate-spin" /> : null}
        {loading ? "加载更多…" : count < total ? "下滑加载更多" : "已加载全部"}
      </span>
    </div>
  )
}


function StatCardsSkeleton({ title, count }: { title?: string; count: number }) {
  return (
    <section className="space-y-3" aria-busy="true" aria-label={`${title ?? "统计"}加载中`}>
      {title ? <h2 className="text-sm font-medium text-muted-foreground">{title}</h2> : null}
      <div className="grid grid-cols-2 gap-3 sm:gap-4 xl:grid-cols-4">
        {Array.from({ length: count }, (_, index) => (
          <Card key={index} size="sm">
            <CardHeader><Skeleton className="h-4 w-20" /></CardHeader>
            <CardContent className="space-y-2">
              <Skeleton className="h-7 w-24" />
              <Skeleton className="h-3 w-full" />
            </CardContent>
          </Card>
        ))}
      </div>
    </section>
  )
}
