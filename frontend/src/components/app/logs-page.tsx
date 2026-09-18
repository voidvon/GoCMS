import { useEffect, useState } from "react"
import { LogIn, ScrollText } from "lucide-react"

import {
  clearLogs,
  getLoginLogs,
  getOperationLogs,
  type AdminUser,
  type LoginLog,
  type OperationLog,
} from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { InlineAlert } from "@/components/app/app-ui"
import { PaginatedTable } from "@/components/app/paginated-table"

const pageSize = 30

export function LogsPage({ user }: { user: AdminUser }) {
  const canReadOperations = user.is_super || user.permissions.includes("logs")
  const canReadLogins = user.is_super || user.permissions.includes("login_logs")
  const [tab, setTab] = useState<"operations" | "logins">(
    canReadOperations ? "operations" : "logins"
  )
  const [items, setItems] = useState<OperationLog[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [username, setUsername] = useState("")
  const [filter, setFilter] = useState("")
  const [revision, setRevision] = useState(0)
  const [busy, setBusy] = useState(true)
  const [error, setError] = useState("")
  const [loginItems, setLoginItems] = useState<LoginLog[]>([])
  const [before, setBefore] = useState("")
  const [clearing, setClearing] = useState(false)

  useEffect(() => {
    let active = true
    if (tab === "logins") {
      setBusy(true)
      getLoginLogs()
        .then((data) => {
          if (active) {
            setLoginItems(data.items)
            setError("")
          }
        })
        .catch((e: Error) => {
          if (active) {
            setError(e.message)
            setLoginItems([])
          }
        })
        .finally(() => {
          if (active) setBusy(false)
        })
      return () => {
        active = false
      }
    }
    getOperationLogs(page, filter)
      .then((data) => {
        if (active) {
          setItems(data.items)
          setTotal(data.total)
          setError("")
        }
      })
      .catch((e: Error) => {
        if (active) {
          setError(e.message)
          setItems([])
        }
      })
      .finally(() => {
        if (active) setBusy(false)
      })
    return () => {
      active = false
    }
  }, [page, filter, revision, tab])

  async function handleClear() {
    if (!before || !window.confirm(`确认删除 ${before} 之前的操作和登录日志吗？`)) return
    setClearing(true)
    setError("")
    try {
      await clearLogs(before)
      setRevision((value) => value + 1)
      setBefore("")
    } catch (e) {
      setError(e instanceof Error ? e.message : "日志清理失败")
    } finally {
      setClearing(false)
    }
  }

  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  return (
    <div className="space-y-4 lg:flex lg:h-full lg:min-h-0 lg:flex-col lg:space-y-0 lg:gap-4">
      <Tabs
        value={tab}
        onValueChange={(val) => {
          if (val === tab) return
          setBusy(true)
          if (val === "operations") setPage(1)
          setTab(val as "operations" | "logins")
        }}
        className="space-y-4 lg:flex lg:min-h-0 lg:flex-1 lg:flex-col lg:space-y-0 lg:gap-4"
      >
        <div className="flex shrink-0 flex-wrap items-center justify-between gap-2">
          {tab === "operations" && canReadOperations ? (
            <form
              className="flex flex-wrap items-center gap-2"
              onSubmit={(e) => {
                e.preventDefault()
                setBusy(true)
                setPage(1)
                setFilter(username.trim())
                setRevision((v) => v + 1)
              }}
            >
              <Input
                size="sm"
                className="w-[200px]"
                aria-label="按账号筛选日志"
                placeholder="账号名称（精确匹配）"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
              />
              <Button disabled={busy} type="submit" size="sm">
                查询 / 刷新
              </Button>
            </form>
          ) : tab === "logins" && canReadLogins ? (
            <div className="flex flex-wrap items-center gap-2">
              <Button
                variant="outline"
                size="sm"
                disabled={busy}
                onClick={() => {
                  setBusy(true)
                  setRevision((v) => v + 1)
                }}
              >
                刷新
              </Button>
            </div>
          ) : (
            <div />
          )}

          <TabsList size="sm" className="ml-auto">
            {canReadOperations && (
              <TabsTrigger value="operations" className="gap-2">
                <ScrollText className="size-4" />
                操作日志
              </TabsTrigger>
            )}
            {canReadLogins && (
              <TabsTrigger value="logins" className="gap-2">
                <LogIn className="size-4" />
                登录日志
              </TabsTrigger>
            )}
          </TabsList>
        </div>

        <p className="shrink-0 text-sm text-muted-foreground">
          时间为 UTC。操作日志记录通过权限检查后的写操作；登录日志记录成功、失败和限流结果。发布任务结果请到网站发布查看。
        </p>

        {error ? <InlineAlert className="shrink-0">{error}</InlineAlert> : null}

        <TabsContent value="operations" className="min-h-0 flex-1 lg:flex lg:flex-col">
          <PaginatedTable
            page={page}
            totalPages={totalPages}
            total={total}
            pageSize={pageSize}
            loading={busy}
            onPageChange={(nextPage) => {
              setBusy(true)
              setPage(nextPage)
            }}
          >
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="pl-4 whitespace-nowrap">时间（UTC）</TableHead>
                  <TableHead className="whitespace-nowrap">账号</TableHead>
                  <TableHead className="whitespace-nowrap">操作</TableHead>
                  <TableHead className="whitespace-nowrap">对象</TableHead>
                  <TableHead className="whitespace-nowrap">结果</TableHead>
                  <TableHead className="pr-4 whitespace-nowrap">IP</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {busy ? (
                  <TableRow>
                    <TableCell colSpan={6} className="h-28 text-center text-muted-foreground">
                      正在读取日志…
                    </TableCell>
                  </TableRow>
                ) : items.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={6} className="h-28 text-center text-muted-foreground">
                      暂无操作日志
                    </TableCell>
                  </TableRow>
                ) : (
                  items.map((item) => (
                    <TableRow key={item.id}>
                      <TableCell className="pl-4 whitespace-nowrap">{item.created_at}</TableCell>
                      <TableCell className="whitespace-nowrap font-medium">{item.username}</TableCell>
                      <TableCell className="whitespace-nowrap">
                        {({ POST: "提交", PUT: "修改", PATCH: "修改", DELETE: "删除" } as Record<string, string>)[item.method] ?? item.method}
                      </TableCell>
                      <TableCell className="break-all font-mono text-xs">{item.path}</TableCell>
                      <TableCell className="whitespace-nowrap">
                        <span className={item.status < 400 ? "text-emerald-600 dark:text-emerald-400" : "text-destructive"}>
                          {item.status < 400 ? "成功" : "失败"}（{item.status}）
                        </span>
                      </TableCell>
                      <TableCell className="pr-4 whitespace-nowrap font-mono text-xs">{item.ip}</TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </PaginatedTable>
        </TabsContent>

        <TabsContent value="logins" className="min-h-0 flex-1 lg:flex lg:flex-col">
          <div className="flex min-h-0 flex-1 flex-col rounded-md border bg-background lg:overflow-hidden">
            <div className="relative min-h-0 flex-1 overflow-auto [scrollbar-color:color-mix(in_oklab,var(--muted-foreground)_38%,transparent)_transparent] [scrollbar-width:thin] [&::-webkit-scrollbar]:size-2 [&::-webkit-scrollbar-thumb]:rounded-full [&::-webkit-scrollbar-thumb]:bg-muted-foreground/35 [&::-webkit-scrollbar-thumb:hover]:bg-muted-foreground/55 [&>[data-slot=table-container]]:overflow-visible [&>[data-slot=table-container]]:static">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="pl-4 whitespace-nowrap">时间（UTC）</TableHead>
                    <TableHead className="whitespace-nowrap">账号</TableHead>
                    <TableHead className="whitespace-nowrap">结果</TableHead>
                    <TableHead className="pr-4 whitespace-nowrap">IP</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {busy ? (
                    <TableRow>
                      <TableCell colSpan={4} className="h-28 text-center text-muted-foreground">
                        正在读取日志…
                      </TableCell>
                    </TableRow>
                  ) : loginItems.length === 0 ? (
                    <TableRow>
                      <TableCell colSpan={4} className="h-28 text-center text-muted-foreground">
                        暂无登录日志
                      </TableCell>
                    </TableRow>
                  ) : (
                    loginItems.map((item) => (
                      <TableRow key={item.id}>
                        <TableCell className="pl-4 whitespace-nowrap">{item.created_at}</TableCell>
                        <TableCell className="whitespace-nowrap font-medium">{item.username}</TableCell>
                        <TableCell className="whitespace-nowrap">
                          <span className={item.success ? "text-emerald-600 dark:text-emerald-400" : "text-destructive"}>
                            {item.success ? "登录成功" : "登录失败 / 受限"}
                          </span>
                        </TableCell>
                        <TableCell className="pr-4 whitespace-nowrap font-mono text-xs">{item.ip}</TableCell>
                      </TableRow>
                    ))
                  )}
                </TableBody>
              </Table>
            </div>
          </div>
        </TabsContent>
      </Tabs>

      {user.is_super && (
        <div className="flex shrink-0 flex-wrap items-end gap-2 rounded-md border p-3">
          <label className="space-y-1 text-sm">
            <span className="block">清理该日期之前的日志（UTC）</span>
            <Input type="date" value={before} onChange={(e) => setBefore(e.target.value)} />
          </label>
          <Button variant="destructive" disabled={clearing || !before} onClick={() => void handleClear()}>
            {clearing ? "正在清理…" : "清理日志"}
          </Button>
        </div>
      )}
    </div>
  )
}
