import { useEffect, useState } from "react"
import { clearLogs, getLoginLogs, getOperationLogs, type AdminUser, type LoginLog, type OperationLog } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"

export function LogsPage({ user }: { user: AdminUser }) {
  const canReadOperations = user.is_super || user.permissions.includes("logs")
  const canReadLogins = user.is_super || user.permissions.includes("login_logs")
  const [tab, setTab] = useState<"operations" | "logins">(canReadOperations ? "operations" : "logins")
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
      getLoginLogs().then((data) => {
        if (active) { setLoginItems(data.items); setError("") }
      }).catch((e: Error) => { if (active) { setError(e.message); setLoginItems([]) } }).finally(() => { if (active) setBusy(false) })
      return () => { active = false }
    }
    getOperationLogs(page, filter).then((data) => {
      if (active) { setItems(data.items); setTotal(data.total); setError("") }
    }).catch((e: Error) => { if (active) { setError(e.message); setItems([]) } }).finally(() => { if (active) setBusy(false) })
    return () => { active = false }
  }, [page, filter, revision, tab])
  async function handleClear() {
    if (!before || !window.confirm(`确认删除 ${before} 之前的操作和登录日志吗？`)) return
    setClearing(true)
    setError("")
    try {
      await clearLogs(before)
      setRevision((value) => value + 1)
      setBefore("")
    } catch (e) { setError(e instanceof Error ? e.message : "日志清理失败") }
    finally { setClearing(false) }
  }
  return <div className="h-full space-y-4 overflow-auto">
    <div className="flex flex-wrap gap-2" role="tablist" aria-label="日志类型">
      {canReadOperations && <Button variant={tab === "operations" ? "secondary" : "outline"} role="tab" aria-selected={tab === "operations"} onClick={() => { setBusy(true); setPage(1); setTab("operations") }}>操作日志</Button>}
      {canReadLogins && <Button variant={tab === "logins" ? "secondary" : "outline"} role="tab" aria-selected={tab === "logins"} onClick={() => { setBusy(true); setTab("logins") }}>登录日志</Button>}
    </div>
    {tab === "operations" && canReadOperations && <form className="flex flex-wrap gap-2" onSubmit={(e) => { e.preventDefault(); setBusy(true); setPage(1); setFilter(username.trim()); setRevision((v) => v + 1) }}>
      <Input className="max-w-xs" aria-label="按账号筛选日志" placeholder="账号名称（精确匹配）" value={username} onChange={(e) => setUsername(e.target.value)} />
      <Button disabled={busy} type="submit">查询 / 刷新</Button>
    </form>}
    <p className="text-sm text-muted-foreground">时间为 UTC。操作日志记录通过权限检查后的写操作；登录日志记录成功、失败和限流结果。发布任务结果请到网站发布查看。</p>
    {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
    {busy ? <p role="status">正在读取日志…</p> : tab === "operations" ? <div className="overflow-x-auto rounded-md border"><table className="w-full min-w-[760px] text-left text-sm"><thead><tr>{["时间（UTC）", "账号", "操作", "对象", "结果", "IP"].map((label) => <th key={label} className="whitespace-nowrap p-3">{label}</th>)}</tr></thead><tbody>
      {items.map((item) => <tr key={item.id} className="border-t"><td className="whitespace-nowrap p-3">{item.created_at}</td><td className="p-3">{item.username}</td><td className="p-3">{({ POST: "提交", PUT: "修改", PATCH: "修改", DELETE: "删除" } as Record<string, string>)[item.method] ?? item.method}</td><td className="break-all p-3">{item.path}</td><td className="whitespace-nowrap p-3">{item.status < 400 ? "成功" : "失败"}（{item.status}）</td><td className="p-3">{item.ip}</td></tr>)}
      {items.length === 0 && <tr><td colSpan={6} className="p-6 text-center text-muted-foreground">暂无操作日志</td></tr>}
    </tbody></table></div> : <div className="overflow-x-auto rounded-md border"><table className="w-full min-w-[620px] text-left text-sm"><thead><tr>{["时间（UTC）", "账号", "结果", "IP"].map((label) => <th key={label} className="whitespace-nowrap p-3">{label}</th>)}</tr></thead><tbody>{loginItems.map((item) => <tr key={item.id} className="border-t"><td className="whitespace-nowrap p-3">{item.created_at}</td><td className="p-3">{item.username}</td><td className="p-3">{item.success ? "登录成功" : "登录失败 / 受限"}</td><td className="p-3">{item.ip}</td></tr>)}{loginItems.length === 0 && <tr><td colSpan={4} className="p-6 text-center text-muted-foreground">暂无登录日志</td></tr>}</tbody></table></div>}
    {tab === "operations" && <div className="flex flex-wrap items-center gap-3"><Button variant="outline" disabled={busy || page <= 1} onClick={() => { setBusy(true); setPage(page - 1) }}>上一页</Button><span className="text-sm">第 {page} 页，共 {total} 条</span><Button variant="outline" disabled={busy || page * 30 >= total} onClick={() => { setBusy(true); setPage(page + 1) }}>下一页</Button></div>}
    {user.is_super && <div className="flex flex-wrap items-end gap-2 rounded-md border p-3"><label className="space-y-1 text-sm"><span className="block">清理该日期之前的日志（UTC）</span><Input type="date" value={before} onChange={(e) => setBefore(e.target.value)} /></label><Button variant="destructive" disabled={clearing || !before} onClick={() => void handleClear()}>{clearing ? "正在清理…" : "清理日志"}</Button></div>}
  </div>
}
