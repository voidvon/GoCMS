import { useEffect, useState } from "react"
import {
  AlertTriangle,
  Bug,
  Eye,
  Inbox,
  Link2Off,
  LoaderCircle,
  MonitorSmartphone,
  MousePointerClick,
  FileText,
  UsersRound,
} from "lucide-react"

import { getStats, type AccessLogMetrics, type AdminStats } from "@/lib/api"
import { useSite } from "@/lib/site-context"
import { TimeText } from "@/components/app/time-text"
import { Badge } from "@/components/ui/badge"
import {
  Card,
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
  }, [activeSite?.id])

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
  const topErrors = stats.access?.top_errors ?? []
  const topPages = stats.access?.top_pages ?? []
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
          detail: `24 小时内 ${formatNumber(metrics.recent_404_errors)} 次 · 站内死链 ${formatNumber(metrics.internal_404_errors)} 次`,
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
          value: metrics.internal_404_errors,
          detail: "由站内页面跳转触发的 404，优先修复",
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
            {metrics.internal_404_errors > 0 ? (
              <Badge variant="outline" className="gap-1.5 border-amber-300 text-amber-700 dark:text-amber-300">
                <AlertTriangle className="size-3.5" />
                {formatNumber(metrics.internal_404_errors)} 个站内死链待处理
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
        <Card className="lg:col-span-2">
          <CardHeader>
            <CardTitle className="text-base">错误与死链</CardTitle>
            <CardDescription>按最近出现时间倒序排列，最新触发的错误排在最前</CardDescription>
          </CardHeader>
          <CardContent className="px-0">
            {topErrors.length === 0 ? (
              <p className="px-6 py-6 text-sm text-muted-foreground">当前没有记录到任何 4xx / 5xx 错误。</p>
            ) : (
              <div className="overflow-x-auto">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="w-[70px]">状态</TableHead>
                      <TableHead>请求路径</TableHead>
                      <TableHead className="w-[70px] text-right">命中</TableHead>
                      <TableHead className="w-[80px] text-right">站内</TableHead>
                      <TableHead className="w-[150px]">最近出现</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {topErrors.slice(0, 10).map((item) => (
                      <TableRow key={`${item.status}-${item.path}`}>
                        <TableCell>
                          <span className={`inline-flex rounded px-1.5 py-0.5 font-mono text-xs font-medium ${statusTone(item.status)}`}>
                            {item.status}
                          </span>
                        </TableCell>
                        <TableCell className="max-w-[360px] truncate font-mono text-xs" title={item.path}>
                          {item.path}
                        </TableCell>
                        <TableCell className="text-right font-medium">{formatNumber(item.hits)}</TableCell>
                        <TableCell className="text-right text-muted-foreground">{formatNumber(item.internal_hits)}</TableCell>
                        <TableCell className="text-xs text-muted-foreground"><TimeText value={item.last_visited_at} /></TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            )}
            {statusBreakdown.length > 0 ? (
              <div className="flex flex-wrap gap-2 px-6 pt-4">
                {statusBreakdown.map((item) => (
                  <Badge key={item.status} variant="outline" className="gap-1.5 font-mono">
                    {item.status}
                    <span className="text-muted-foreground">{formatNumber(item.hits)}</span>
                  </Badge>
                ))}
              </div>
            ) : null}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">热门页面</CardTitle>
            <CardDescription>访问量最高的页面（已排除爬虫）</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            {topPages.length === 0 ? (
              <p className="text-sm text-muted-foreground">还没有页面访问记录。</p>
            ) : (
              topPages.map((item, index) => (
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
              ))
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
                  <span className="font-medium">{formatNumber(metrics.internal_404_errors)}</span>
                </div>
              ) : null}
            </CardContent>
          </Card>
        </div>
      </section>
    </div>
  )
}
