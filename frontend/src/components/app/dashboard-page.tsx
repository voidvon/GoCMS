import { useEffect, useState } from "react"
import {
  Activity,
  ArrowUpRight,
  Inbox,
  LoaderCircle,
  FileText,
  UsersRound,
} from "lucide-react"

import { getStats, type AdminStats } from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { InlineAlert } from "@/components/app/app-ui"

function formatNumber(value: number) {
  return new Intl.NumberFormat("zh-CN").format(value)
}

export function DashboardPage() {
  const [stats, setStats] = useState<AdminStats | null>(null)
  const [error, setError] = useState("")

  useEffect(() => {
    let active = true
    getStats()
      .then((response) => {
        if (active) setStats(response)
      })
      .catch((loadError) => {
        if (active) setError(loadError instanceof Error ? loadError.message : "统计加载失败")
      })
    return () => {
      active = false
    }
  }, [])

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

  const cards = [
    { label: "内容总数", value: stats.contents, detail: `${formatNumber(stats.visible_contents)} 条公开展示`, icon: FileText },
    { label: "客户留言", value: stats.messages, detail: `${formatNumber(stats.pending_messages)} 条待处理`, icon: Inbox },
    { label: "公开内容率", value: `${stats.contents ? Math.round((stats.visible_contents / stats.contents) * 100) : 0}%`, detail: "当前可见内容占比", icon: UsersRound },
  ]

  return (
    <div className="space-y-6">
      <section className="flex flex-col justify-between gap-4 sm:flex-row sm:items-end">
        <div>
          <p className="text-2xl font-semibold tracking-tight">内容概览</p>
          <p className="mt-1 text-sm text-muted-foreground">今天从这里开始处理站点内容。</p>
        </div>
        <Badge variant="outline" className="w-fit gap-1.5">
          <Activity className="text-emerald-600" />
          SQLite 服务正常
        </Badge>
      </section>

      <section className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        {cards.map((card) => {
          const Icon = card.icon
          return (
            <Card key={card.label} size="sm">
              <CardHeader className="flex flex-row items-start justify-between">
                <CardDescription>{card.label}</CardDescription>
                <Icon className="size-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                <p className="text-2xl font-semibold tracking-tight">{typeof card.value === "number" ? formatNumber(card.value) : card.value}</p>
                <p className="mt-1 text-xs text-muted-foreground">{card.detail}</p>
              </CardContent>
            </Card>
          )
        })}
      </section>

      <section className="grid gap-4 xl:grid-cols-[1.4fr_1fr]">
        <Card>
          <CardHeader className="flex flex-row items-start justify-between border-b">
            <div>
              <CardTitle>迁移状态</CardTitle>
              <CardDescription>当前 Go + SQLite 运行时信息</CardDescription>
            </div>
            <ArrowUpRight className="size-4 text-muted-foreground" />
          </CardHeader>
          <CardContent className="grid gap-4 pt-5 sm:grid-cols-3">
            <div>
              <p className="text-xs text-muted-foreground">数据库</p>
              <p className="mt-1 font-medium">SQLite</p>
            </div>
            <div>
              <p className="text-xs text-muted-foreground">字符集</p>
              <p className="mt-1 font-medium">UTF-8</p>
            </div>
            <div>
              <p className="text-xs text-muted-foreground">公开目录</p>
              <p className="mt-1 font-medium">{formatNumber(stats.visible_contents)} 条内容</p>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>待处理事项</CardTitle>
            <CardDescription>按优先级查看后台工作</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="flex items-center justify-between border-b pb-3 text-sm">
              <span className="text-muted-foreground">未读留言</span>
              <span className="font-medium">{formatNumber(stats.pending_messages)}</span>
            </div>
            <div className="flex items-center justify-between text-sm">
              <span className="text-muted-foreground">隐藏内容</span>
              <span className="font-medium">{formatNumber(stats.contents - stats.visible_contents)}</span>
            </div>
          </CardContent>
        </Card>
      </section>
    </div>
  )
}
