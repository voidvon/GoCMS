import { useEffect, useMemo, useState } from "react"
import { LoaderCircle, RefreshCw, Search, Trash2 } from "lucide-react"

import {
  clearAccessLogs,
  getAccessLogs,
  type AccessLogFilters,
  type AccessLogItem,
  type AdminUser,
} from "@/lib/api"
import { useSite } from "@/lib/site-context"
import { TimeText } from "@/components/app/time-text"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { InlineAlert } from "@/components/app/app-ui"
import { PaginatedTable } from "@/components/app/paginated-table"

const pageSize = 30

const windowOptions = [
  { value: "today", label: "今天" },
  { value: "24h", label: "最近 24 小时" },
  { value: "3d", label: "最近 3 天" },
  { value: "all", label: "全部时间" },
]

const kindOptions = [
  { value: "all", label: "全部类型" },
  { value: "page", label: "页面访问" },
  { value: "error", label: "错误请求" },
]

const statusOptions = [
  { value: "all", label: "全部状态" },
  { value: "errors", label: "仅错误（4xx/5xx）" },
  { value: "404", label: "仅 404" },
  { value: "4xx", label: "4xx" },
  { value: "5xx", label: "5xx" },
  { value: "200", label: "仅 200" },
]

const agentOptions = [
  { value: "all", label: "全部客户端" },
  { value: "browser", label: "浏览器" },
  { value: "bot", label: "搜索引擎 / 爬虫" },
  { value: "tool", label: "工具 / 脚本" },
  { value: "other", label: "其他" },
]

const deviceOptions = [
  { value: "all", label: "全部设备" },
  { value: "desktop", label: "桌面端" },
  { value: "mobile", label: "手机" },
  { value: "tablet", label: "平板" },
  { value: "other", label: "其他" },
]

const refererOptions = [
  { value: "all", label: "全部来源" },
  { value: "internal", label: "站内跳转" },
  { value: "external", label: "站外来源" },
  { value: "none", label: "直接访问" },
  { value: "invalid", label: "无效 Referer" },
]

function hasPermission(user: AdminUser, key: string, activeSiteId?: number) {
  if (user.is_super) return true
  const sitePermissions = activeSiteId ? user.site_permissions?.[activeSiteId] : undefined
  if (sitePermissions && sitePermissions.length > 0) {
    return sitePermissions.includes(key) || sitePermissions.includes("*")
  }
  return user.permissions.includes(key)
}

function statusTone(status: number) {
  if (status >= 500) return "bg-red-100 text-red-700 dark:bg-red-950 dark:text-red-300"
  if (status >= 400) return "bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-300"
  if (status >= 300) return "bg-sky-100 text-sky-700 dark:bg-sky-950 dark:text-sky-300"
  return "bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300"
}

function StatusBadge({ status }: { status: number }) {
  return <span className={`inline-flex rounded px-1.5 py-0.5 font-mono text-xs font-medium ${statusTone(status)}`}>{status}</span>
}

function refererLabel(kind: string) {
  if (kind === "internal") return "站内"
  if (kind === "external") return "站外"
  if (kind === "invalid") return "无效"
  return "直接"
}

export function TrafficPage({ user }: { user: AdminUser }) {
  const { activeSite } = useSite()
  const canClear = hasPermission(user, "traffic.clear", activeSite?.id)

  const [timeWindow, setTimeWindow] = useState("today")
  const [kind, setKind] = useState("all")
  const [statusMode, setStatusMode] = useState("all")
  const [agentKind, setAgentKind] = useState("all")
  const [deviceKind, setDeviceKind] = useState("all")
  const [refererKind, setRefererKind] = useState("all")
  const [path, setPath] = useState("")
  const [ip, setIp] = useState("")
  const [search, setSearch] = useState("")
  const [page, setPage] = useState(1)
  const [revision, setRevision] = useState(0)

  const [items, setItems] = useState<AccessLogItem[]>([])
  const [total, setTotal] = useState(0)
  const [loadedKey, setLoadedKey] = useState("")
  const [clearing, setClearing] = useState(false)
  const [error, setError] = useState("")

  const filters = useMemo<AccessLogFilters>(
    () => ({
      window: timeWindow,
      kind: kind === "all" ? undefined : kind,
      status_mode: statusMode === "all" ? undefined : statusMode,
      agent_kind: agentKind === "all" ? undefined : agentKind,
      device_kind: deviceKind === "all" ? undefined : deviceKind,
      referer_kind: refererKind === "all" ? undefined : refererKind,
      path: path || undefined,
      ip: ip || undefined,
      q: search || undefined,
    }),
    [timeWindow, kind, statusMode, agentKind, deviceKind, refererKind, path, ip, search]
  )

  // Loading is derived from the request key so switching filters or pages never
  // shows the previous result set.
  const requestKey = useMemo(
    () => `${page}|${revision}|${JSON.stringify(filters)}`,
    [page, revision, filters]
  )
  const loading = loadedKey !== requestKey

  useEffect(() => {
    let active = true
    getAccessLogs({ ...filters, page, page_size: pageSize })
      .then((data) => {
        if (!active) return
        setItems(data.items)
        setTotal(data.total)
        setError("")
      })
      .catch((loadError: Error) => {
        if (!active) return
        setItems([])
        setTotal(0)
        setError(loadError.message)
      })
      .finally(() => {
        if (active) setLoadedKey(requestKey)
      })
    return () => {
      active = false
    }
  }, [filters, page, revision, requestKey])

  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  async function handleClear() {
    if (!window.confirm("确认清空当前筛选条件下的访问日志吗？该操作不可撤销。")) return
    setClearing(true)
    setError("")
    try {
      await clearAccessLogs(filters)
      setRevision((value) => value + 1)
    } catch (clearError) {
      setError(clearError instanceof Error ? clearError.message : "访问日志清理失败")
    } finally {
      setClearing(false)
    }
  }

  const activeFilterCount = [kind, statusMode, agentKind, deviceKind, refererKind].filter((value) => value !== "all").length + (path ? 1 : 0) + (ip ? 1 : 0) + (search ? 1 : 0)

  return (
    <div className="space-y-4 lg:flex lg:h-full lg:min-h-0 lg:flex-col lg:space-y-0 lg:gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <Select items={windowOptions} value={timeWindow} onValueChange={(value) => { setTimeWindow(value ?? "today"); setPage(1) }}>
          <SelectTrigger size="sm" className="w-[150px]">
            <SelectValue placeholder="时间范围" />
          </SelectTrigger>
          <SelectContent>
            {windowOptions.map((option) => (
              <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select items={kindOptions} value={kind} onValueChange={(value) => { setKind(value ?? "all"); setPage(1) }}>
          <SelectTrigger size="sm" className="w-[130px]">
            <SelectValue placeholder="类型" />
          </SelectTrigger>
          <SelectContent>
            {kindOptions.map((option) => (
              <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select items={statusOptions} value={statusMode} onValueChange={(value) => { setStatusMode(value ?? "all"); setPage(1) }}>
          <SelectTrigger size="sm" className="w-[160px]">
            <SelectValue placeholder="状态" />
          </SelectTrigger>
          <SelectContent>
            {statusOptions.map((option) => (
              <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select items={agentOptions} value={agentKind} onValueChange={(value) => { setAgentKind(value ?? "all"); setPage(1) }}>
          <SelectTrigger size="sm" className="w-[150px]">
            <SelectValue placeholder="客户端" />
          </SelectTrigger>
          <SelectContent>
            {agentOptions.map((option) => (
              <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select items={deviceOptions} value={deviceKind} onValueChange={(value) => { setDeviceKind(value ?? "all"); setPage(1) }}>
          <SelectTrigger size="sm" className="w-[120px]">
            <SelectValue placeholder="设备" />
          </SelectTrigger>
          <SelectContent>
            {deviceOptions.map((option) => (
              <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select items={refererOptions} value={refererKind} onValueChange={(value) => { setRefererKind(value ?? "all"); setPage(1) }}>
          <SelectTrigger size="sm" className="w-[130px]">
            <SelectValue placeholder="来源" />
          </SelectTrigger>
          <SelectContent>
            {refererOptions.map((option) => (
              <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <div className="relative">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={path}
            onChange={(event) => { setPath(event.target.value); setPage(1) }}
            placeholder="路径包含…"
            className="h-8 w-[180px] pl-8"
          />
        </div>
        <Input
          value={ip}
          onChange={(event) => { setIp(event.target.value); setPage(1) }}
          placeholder="访客 IP"
          className="h-8 w-[140px]"
        />
        <Input
          value={search}
          onChange={(event) => { setSearch(event.target.value); setPage(1) }}
          placeholder="搜索 UA / 来源"
          className="h-8 w-[160px]"
        />
        <Button variant="outline" size="sm" onClick={() => setRevision((value) => value + 1)} disabled={loading}>
          <RefreshCw className={loading ? "animate-spin" : undefined} />
          刷新
        </Button>
        {canClear ? (
          <Button variant="outline" size="sm" onClick={handleClear} disabled={clearing}>
            {clearing ? <LoaderCircle className="animate-spin" /> : <Trash2 />}
            清空{activeFilterCount > 0 ? `（${activeFilterCount} 项筛选）` : "日志"}
          </Button>
        ) : null}
      </div>

      {error ? <InlineAlert className="p-3">{error}</InlineAlert> : null}

      <PaginatedTable
        page={page}
        totalPages={totalPages}
        total={total}
        pageSize={pageSize}
        loading={loading}
        onPageChange={setPage}
      >
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-[160px]">时间</TableHead>
              <TableHead className="w-[70px]">状态</TableHead>
              <TableHead className="w-[170px]">域名</TableHead>
              <TableHead>请求路径</TableHead>
              <TableHead className="w-[130px]">客户端</TableHead>
              <TableHead className="w-[90px]">设备</TableHead>
              <TableHead className="w-[130px]">访客 IP</TableHead>
              <TableHead className="w-[150px]">来源</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.length === 0 ? (
              <TableRow>
                <TableCell colSpan={8} className="py-10 text-center text-sm text-muted-foreground">
                  {loading ? "加载中…" : "当前筛选条件下没有访问记录。"}
                </TableCell>
              </TableRow>
            ) : (
              items.map((item) => (
                <TableRow key={item.id}>
                  <TableCell className="text-xs text-muted-foreground"><TimeText value={item.visited_at} /></TableCell>
                  <TableCell><StatusBadge status={item.status} /></TableCell>
                  <TableCell className="max-w-[170px] truncate text-xs" title={item.host || activeSite?.domain || ""}>
                    {item.host || activeSite?.domain || "—"}
                  </TableCell>
                  <TableCell className="max-w-[360px] truncate font-mono text-xs" title={`${item.path}${item.query ? `?${item.query}` : ""}`}>
                    {item.path}
                    {item.query ? <span className="text-muted-foreground">?{item.query}</span> : null}
                  </TableCell>
                  <TableCell className="text-xs">
                    <span className="flex items-center gap-1.5">
                      <span className="truncate" title={item.user_agent}>{item.agent_label || item.agent_kind}</span>
                      {item.agent_kind === "bot" ? <Badge variant="outline" className="shrink-0 text-[10px]">爬虫</Badge> : null}
                      {item.agent_kind === "tool" ? <Badge variant="outline" className="shrink-0 text-[10px]">工具</Badge> : null}
                    </span>
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {item.device_kind === "desktop" ? "桌面" : item.device_kind === "mobile" ? "手机" : item.device_kind === "tablet" ? "平板" : "其他"}
                  </TableCell>
                  <TableCell className="font-mono text-xs text-muted-foreground">{item.ip}</TableCell>
                  <TableCell className="max-w-[180px] truncate text-xs text-muted-foreground" title={item.referer}>
                    {item.referer_host ? (
                      <span className="flex items-center gap-1.5">
                        <Badge variant="outline" className="shrink-0 text-[10px]">{refererLabel(item.referer_kind)}</Badge>
                        <span className="truncate">{item.referer_host}</span>
                      </span>
                    ) : (
                      <span>直接访问</span>
                    )}
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </PaginatedTable>
    </div>
  )
}
