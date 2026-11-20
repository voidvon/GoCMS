import { useCallback, useEffect, useRef, useState, type FormEvent, type ReactNode } from "react"
import { Copy, Plus, RefreshCw, ShieldCheck, ShieldOff, Unplug } from "lucide-react"
import {
  addSecurityEntry, getSecurityEntries, getSecurityEvents, getSecurityNginx,
  getSecurityPlatform, getSecurityPolicy, revokeSecurityEntry, saveSecurityPlatform, saveSecurityPolicy, syncSecurityNginx,
  type AdminUser, type SecurityEntry, type SecurityEvent, type SecurityNginx, type SecurityPlatform, type SecurityPolicy,
} from "@/lib/api"
import { useSite } from "@/lib/site-context"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Textarea } from "@/components/ui/textarea"
import { ConfirmDialog, TablePagination } from "./app-ui"
import { showError, showSuccess } from "./admin-notifications"
import { TimeText } from "./time-text"
import { NginxConfigDialog } from "./nginx-config-dialog"

const syncLabels = { disabled: "未启用", pending: "待同步", applied: "已生效", failed: "同步失败" }
const actionLabels: Record<string, string> = { probe: "路径探测", ban: "自动封禁", deny: "手动封禁", allow: "加入白名单", revoke: "移除名单" }
const lines = (value: string) => value.split("\n").map(line => line.trim()).filter(Boolean)
const emptyEntry = () => ({ network: "", kind: "deny" as "deny" | "allow", reason: "", duration: "0" })

function Field({ label, id, hint, children }: { label: string; id: string; hint?: string; children: ReactNode }) {
  return <div className="space-y-2"><Label htmlFor={id}>{label}</Label>{children}{hint && <p className="text-xs leading-5 text-muted-foreground">{hint}</p>}</div>
}

export function SecurityPage({ user }: { user: AdminUser }) {
  const { activeSite, activeSiteId, sites } = useSite()
  const permissions = user.site_permissions?.[activeSiteId] ?? user.permissions
  const canManage = user.is_super || permissions.includes("*") || permissions.includes("security.manage")
  const [scope, setScope] = useState(user.is_super ? "global" : "site")
  const [tab, setTab] = useState("entries")
  const [policy, setPolicy] = useState<SecurityPolicy | null>(null)
  const canManagePolicy = canManage && (user.is_super || policy?.ban_scope === "site")
  const [pathText, setPathText] = useState("")
  const [platform, setPlatform] = useState<SecurityPlatform | null>(null)
  const [savedPlatform, setSavedPlatform] = useState<SecurityPlatform | null>(null)
  const [proxyText, setProxyText] = useState("")
  const platformInput = platform ? { ...platform, trusted_proxies: lines(proxyText) } : null
  const platformDirty = platformInput !== null && JSON.stringify(platformInput) !== JSON.stringify(savedPlatform)
  const [nginx, setNginx] = useState<SecurityNginx | null>(null)
  const [entries, setEntries] = useState<SecurityEntry[]>([])
  const [events, setEvents] = useState<SecurityEvent[]>([])
  const [total, setTotal] = useState(0)
  const [eventTotal, setEventTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [eventPage, setEventPage] = useState(1)
  const [history, setHistory] = useState(false)
  const [loading, setLoading] = useState(true)
  const [snapshotTime, setSnapshotTime] = useState(Date.now)
  const [failed, setFailed] = useState(false)
  const [pending, setPending] = useState(false)
  const [dialog, setDialog] = useState(false)
  const [draft, setDraft] = useState(emptyEntry)
  const [removing, setRemoving] = useState<SecurityEntry | null>(null)
  const sequence = useRef(0)
  const mounted = useRef(true)

  const refreshLists = useCallback(() => {
    const current = ++sequence.current
    return Promise.all([getSecurityEntries(scope, history, page), getSecurityEvents(scope, eventPage)]).then(([entryData, eventData]) => {
      if (!mounted.current || sequence.current !== current) return
      const lastPage = Math.max(1, Math.ceil(entryData.total / 50))
      if (page > lastPage) {
        setLoading(true)
        setPage(lastPage)
        return
      }
      setEntries(entryData.items)
      setTotal(entryData.total)
      setEvents(eventData.items)
      setEventTotal(eventData.total)
      setSnapshotTime(Date.now())
      setFailed(false)
      setLoading(false)
    }).catch(error => {
      if (mounted.current && sequence.current === current) {
        setFailed(true)
        setLoading(false)
        showError(error)
      }
    })
  }, [scope, history, page, eventPage])

  useEffect(() => {
    mounted.current = true
    void getSecurityPolicy().then(value => {
      if (mounted.current) {
        setPolicy(!user.is_super && !value.enabled && value.ban_scope === "global" ? { ...value, ban_scope: "site" } : value)
        setPathText(value.paths.join("\n"))
      }
    }).catch(error => { if (mounted.current) showError(error) })
    if (user.is_super) {
      void getSecurityPlatform().then(value => { if (mounted.current) { setPlatform(value); setSavedPlatform(value); setProxyText(value.trusted_proxies.join("\n")) } }).catch(error => { if (mounted.current) showError(error) })
      const poll = () => { void getSecurityNginx().then(value => { if (mounted.current) setNginx(value) }).catch(error => { if (mounted.current) showError(error) }) }
      poll()
      const timer = window.setInterval(poll, 15000)
      return () => { mounted.current = false; window.clearInterval(timer) }
    }
    return () => { mounted.current = false }
  }, [user.is_super])
  useEffect(() => { void refreshLists() }, [refreshLists])

  async function act(operation: () => Promise<unknown>, message: string) {
    if (pending) return
    setPending(true)
    try {
      await operation()
      if (!mounted.current) return
      showSuccess(message)
      setLoading(true)
      await refreshLists()
      if (user.is_super) { const value = await getSecurityNginx(); if (mounted.current) setNginx(value) }
    } catch (error) {
      if (mounted.current) {
        showError(error)
        if (user.is_super) void getSecurityNginx().then(value => { if (mounted.current) setNginx(value) }).catch(() => {})
      }
    }
    finally { if (mounted.current) setPending(false) }
  }

  function savePolicy(event: FormEvent) {
    event.preventDefault()
    if (policy) void act(() => saveSecurityPolicy({ ...policy, paths: lines(pathText) }), "站点防护设置已保存")
  }
  function savePlatform(event: FormEvent) {
    event.preventDefault()
    if (platformInput) void act(() => persistPlatform(platformInput), "平台接入设置已保存")
  }
  async function persistPlatform(input: SecurityPlatform) {
    await saveSecurityPlatform(input)
    if (mounted.current) { setPlatform(input); setSavedPlatform(input) }
  }
  function syncNginx() {
    if (!platformInput) return
    void act(async () => {
      if (platformDirty) await persistPlatform(platformInput)
      await syncSecurityNginx()
    }, "Nginx 已确认当前配置")
  }
  function add(event: FormEvent) {
    event.preventDefault()
    void act(async () => {
      await addSecurityEntry(scope, { network: draft.network, kind: draft.kind, reason: draft.reason, expires_at: draft.duration === "0" ? "" : new Date(Date.now() + Number(draft.duration) * 1000).toISOString() })
      if (mounted.current) setDialog(false)
    }, "名单已保存，Go 防护立即生效；Nginx 按接入状态同步")
  }
  function chooseScope(value: string | null) {
    if (!value || value === scope) return
    setScope(value); setPage(1); setEventPage(1); setEntries([]); setEvents([]); setLoading(true)
  }
  function entryScopeLabel(siteId: number) {
    return siteId === 0 ? "全平台" : sites.find(site => site.id === siteId)?.name ?? `站点 #${siteId}`
  }

  function confirmRemoval() {
    if (!removing) return
    void act(async () => {
      await revokeSecurityEntry(removing.id, removing.site_id)
      if (mounted.current) setRemoving(null)
    }, "规则已移除")
  }

  const loadingState = loading ? "正在加载…" : failed ? "加载失败，请刷新重试。" : ""

  return <>
    <ScrollArea className="h-full" contentClassName="space-y-5 pb-8 pe-2">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-3">
          <ShieldCheck className="size-5 text-muted-foreground" />
          <Select value={scope} onValueChange={chooseScope} disabled={pending}>
            <SelectTrigger className="w-56" aria-label="安全规则范围"><SelectValue>{scope === "global" ? "全平台" : activeSite?.name ?? "当前站点"}</SelectValue></SelectTrigger>
            <SelectContent>{user.is_super && <SelectItem value="global">全平台</SelectItem>}<SelectItem value="site">{activeSite?.name ?? "当前站点"}</SelectItem></SelectContent>
          </Select>
          {nginx && <Badge variant={nginx.status.state === "failed" ? "destructive" : "outline"}>Nginx · {syncLabels[nginx.status.state]}</Badge>}
        </div>
        <Button variant="outline" disabled={loading || pending} onClick={() => { setLoading(true); void refreshLists() }}><RefreshCw className="size-4" />{tab === "events" ? "刷新日志" : "刷新记录"}</Button>
      </div>
      <Tabs value={tab} onValueChange={value => setTab(String(value))}>
        <ScrollArea orientation="horizontal" className="w-full"><TabsList><TabsTrigger value="entries">黑白名单</TabsTrigger><TabsTrigger value="policy">扫描防护</TabsTrigger><TabsTrigger value="events">安全日志</TabsTrigger>{user.is_super && <TabsTrigger value="nginx">Nginx 接入</TabsTrigger>}</TabsList></ScrollArea>
      </Tabs>

      {tab === "entries" && <section className="space-y-4" aria-label="IP 黑白名单">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-2"><Switch id="security-history" checked={history} onCheckedChange={value => { setHistory(value); setPage(1); setLoading(true) }} /><Label htmlFor="security-history">显示到期和已移除记录</Label></div>
          {canManage && <Button disabled={pending || loading || failed} onClick={() => { setDraft(emptyEntry()); setDialog(true) }}><Plus className="size-4" />{scope === "global" ? "添加全平台规则" : "添加 IP / 网段"}</Button>}
        </div>
        <ScrollArea orientation="horizontal" className="w-full rounded-xl border bg-card">
          <Table aria-label="IP 黑白名单" containerClassName="overflow-visible" className="min-w-[920px]">
            <TableHeader>
              <TableRow>
                <TableHead className="ps-4">IP / 网段</TableHead>
                <TableHead className="min-w-32">规则归属</TableHead>
                <TableHead>类型</TableHead>
                <TableHead>来源</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>原因</TableHead>
                <TableHead>添加时间</TableHead>
                <TableHead>到期时间</TableHead>
                {canManage && <TableHead className="pe-4 text-end">操作</TableHead>}
              </TableRow>
            </TableHeader>
            <TableBody>
              {loadingState || !entries.length ? (
                <TableRow>
                  <TableCell colSpan={canManage ? 9 : 8} className="h-24 text-center text-muted-foreground">
                    <span role="status">{loadingState || "当前范围暂无名单记录"}</span>
                  </TableCell>
                </TableRow>
              ) : entries.map(entry => {
                const expired = !!entry.expires_at && new Date(entry.expires_at).getTime() <= snapshotTime
                const inactive = !!entry.revoked_at || expired
                const allow = entry.kind === "allow"
                return (
                  <TableRow key={entry.id}>
                    <TableCell className="ps-4 font-mono text-xs">{entry.network}</TableCell>
                    <TableCell className="max-w-44 whitespace-normal wrap-anywhere">{entryScopeLabel(entry.site_id)}</TableCell>
                    <TableCell><Badge variant={allow ? "secondary" : "outline"}>{allow ? "白名单" : "封禁"}</Badge></TableCell>
                    <TableCell>{entry.source === "automatic" ? "自动" : "手动"}</TableCell>
                    <TableCell className="text-muted-foreground">{entry.revoked_at ? "已移除" : expired ? "已到期" : "有效"}</TableCell>
                    <TableCell className="w-full min-w-36 whitespace-normal wrap-anywhere">{entry.reason}</TableCell>
                    <TableCell className="text-xs text-muted-foreground"><TimeText value={entry.created_at} /></TableCell>
                    <TableCell className="text-xs text-muted-foreground">{entry.expires_at ? <TimeText value={entry.expires_at} /> : "长期有效"}</TableCell>
                    {canManage && <TableCell className="pe-4 text-end">
                      {!inactive && <Button variant="outline" size="sm" disabled={pending} onClick={() => setRemoving(entry)}>
                        <ShieldOff className="size-4" />{allow ? "移除白名单" : "解封"}
                      </Button>}
                    </TableCell>}
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </ScrollArea>
        {!loading && !failed && total > 0 && <TablePagination page={page} totalPages={Math.max(1, Math.ceil(total / 50))} total={total} pageSize={50} onPageChange={value => { setPage(value); setLoading(true) }} />}
      </section>}

      {tab === "policy" && <section className="max-w-3xl space-y-4" aria-label="扫描防护设置">
        {!policy ? <p role="status" className="text-sm text-muted-foreground">防护设置尚未加载，重新进入页面可重试。</p> : <form onSubmit={savePolicy} className="space-y-5"><fieldset disabled={!canManagePolicy || pending} className="space-y-5">
          <div className="flex items-center justify-between gap-4 rounded-xl border bg-card p-4"><div><Label htmlFor="security-enabled">启用{activeSite?.name ?? "当前站点"}扫描防护</Label><p className="mt-1 text-xs leading-5 text-muted-foreground">命中扫描路径立即拒绝，达到阈值后封禁 IP。普通 404 不计入阈值。</p></div><Switch id="security-enabled" checked={policy.enabled} onCheckedChange={enabled => setPolicy({ ...policy, enabled })} /></div>
          <Field label="自动封禁范围" id="security-ban-scope"><Select value={policy.ban_scope} onValueChange={ban_scope => { if (ban_scope === "global" || ban_scope === "site") setPolicy({ ...policy, ban_scope }) }}><SelectTrigger id="security-ban-scope" className="w-full"><SelectValue>{policy.ban_scope === "global" ? "全平台" : activeSite?.name ?? "当前站点"}</SelectValue></SelectTrigger><SelectContent>{(user.is_super || policy.ban_scope === "global") && <SelectItem value="global">全平台</SelectItem>}<SelectItem value="site">{activeSite?.name ?? "当前站点"}</SelectItem></SelectContent></Select></Field>
          <div className="grid gap-4 sm:grid-cols-3">
            <Field label="检测窗口（秒）" id="security-window"><Input id="security-window" type="number" required min={10} max={86400} value={policy.window_seconds} onChange={e => setPolicy({ ...policy, window_seconds: Number(e.target.value) })} /></Field>
            <Field label="触发次数" id="security-threshold"><Input id="security-threshold" type="number" required min={2} max={1000} value={policy.threshold} onChange={e => setPolicy({ ...policy, threshold: Number(e.target.value) })} /></Field>
            <Field label="封禁有效期" id="security-permanent"><div className="flex h-9 items-center gap-2"><Switch id="security-permanent" checked={policy.ban_seconds === 0} onCheckedChange={permanent => setPolicy({ ...policy, ban_seconds: permanent ? 0 : 3600 })} /><span className="text-sm">长期有效</span></div>{policy.ban_seconds !== 0 && <Input aria-label="封禁时长（秒）" type="number" required min={60} max={2592000} value={policy.ban_seconds} onChange={e => setPolicy({ ...policy, ban_seconds: Number(e.target.value) })} />}</Field>
          </div>
          <Field label="扫描路径" id="security-paths" hint="每行一条，忽略大小写；末尾 * 表示前缀匹配。仅添加确认不属于正常业务的路径；不能覆盖后台、API 或证书验证路径。"><Textarea id="security-paths" className="font-mono" rows={9} required value={pathText} onChange={e => setPathText(e.target.value)} /></Field>
        </fieldset>{!user.is_super && policy.ban_scope === "global" && <p className="text-xs text-muted-foreground">全平台自动封禁由超级管理员管理。</p>}{canManagePolicy && <Button type="submit" disabled={pending}>{pending ? "保存中…" : "保存站点防护"}</Button>}</form>}
      </section>}

      {tab === "events" && <section className="space-y-4" aria-label="安全日志">
        <p className="text-xs leading-5 text-muted-foreground">记录首次路径探测、自动封禁及名单变更，保留 30 天。同一窗口内的重复探测合并计数。</p>
        <ScrollArea orientation="horizontal" className="w-full rounded-xl border bg-card">
          <Table aria-label="安全日志" containerClassName="overflow-visible" className="min-w-[840px]">
            <TableHeader>
              <TableRow>
                <TableHead className="ps-4">时间</TableHead>
                <TableHead>事件</TableHead>
                <TableHead>IP / 网段</TableHead>
                <TableHead>域名</TableHead>
                <TableHead>请求路径</TableHead>
                <TableHead className="pe-4">原因</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loadingState || !events.length ? (
                <TableRow>
                  <TableCell colSpan={6} className="h-24 text-center text-muted-foreground">
                    <span role="status">{loadingState || "当前范围暂无安全日志"}</span>
                  </TableCell>
                </TableRow>
              ) : events.map(event => (
                <TableRow key={event.id}>
                  <TableCell className="ps-4 text-xs text-muted-foreground"><TimeText value={event.created_at} mode="datetime" /></TableCell>
                  <TableCell><Badge variant={event.action === "ban" ? "destructive" : "outline"}>{actionLabels[event.action] ?? event.action}</Badge></TableCell>
                  <TableCell className="font-mono text-xs">{event.ip || "—"}</TableCell>
                  <TableCell className="max-w-44 whitespace-normal wrap-anywhere text-xs">{event.host || "—"}</TableCell>
                  <TableCell className="max-w-80 whitespace-normal wrap-anywhere font-mono text-xs">{event.path || "—"}</TableCell>
                  <TableCell className="w-full min-w-36 whitespace-normal wrap-anywhere pe-4">{event.reason || "—"}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </ScrollArea>
        {!loading && !failed && eventTotal > 0 && <TablePagination page={eventPage} totalPages={Math.max(1, Math.ceil(eventTotal / 50))} total={eventTotal} pageSize={50} onPageChange={value => { setEventPage(value); setLoading(true) }} />}
      </section>}

      {tab === "nginx" && user.is_super && <section className="max-w-4xl space-y-5" aria-label="Nginx 接入设置">
        <div className="space-y-3 rounded-xl border bg-card p-4">
          <div className="flex flex-wrap items-center justify-between gap-3"><div className="flex items-center gap-2"><Unplug className="size-4" /><span className="font-medium">同步状态</span><Badge variant={nginx?.status.state === "failed" ? "destructive" : "outline"}>{nginx ? syncLabels[nginx.status.state] : "读取中"}</Badge></div><Button variant="outline" disabled={pending || !platform || (!platform.nginx_enabled && nginx?.status.state === "disabled")} onClick={syncNginx}><RefreshCw className="size-4" />{platformDirty ? "保存并同步" : "立即同步"}</Button></div>
          {platformDirty && <p className="text-xs leading-5 text-muted-foreground">接入设置已修改，将先保存当前配置再同步；更改配置路径或运行用户后需要重新执行初始化命令。</p>}
          {nginx?.status.applied_at && <p className="text-xs text-muted-foreground">上次确认：<TimeText value={nginx.status.applied_at} /></p>}
          {nginx?.status.error && <p className="break-words text-sm text-muted-foreground">{nginx.status.error}</p>}
          <p className="text-xs leading-5 text-muted-foreground">Go 防护独立生效。Nginx 初始化后约每 15 秒同步封禁和到期解封；检查或重载失败时恢复原配置并重试。</p>
        </div>
        <div className="space-y-3 rounded-xl border p-4">
          <div className="flex flex-wrap items-center gap-2"><h3 className="text-sm font-medium">一次性 root 初始化</h3><Badge variant={nginx?.setup.ready ? "secondary" : "outline"}>{nginx?.setup.ready ? "已授权" : "待初始化"}</Badge></div>
          <p className="text-sm leading-6 text-muted-foreground">先保存下方接入设置，再在宝塔终端以 root 执行以下命令。初始化会自动创建安全规则文件、接入对应站点并补齐 include，备份配置后授权同一 GoCMS 程序执行固定的 Nginx 管理操作。</p>
          {nginx?.init_command && <div className="flex items-start gap-2"><ScrollArea orientation="horizontal" className="min-w-0 flex-1 rounded-lg bg-muted" contentClassName="p-3"><code className="whitespace-nowrap text-xs">{nginx.init_command}</code></ScrollArea><Button type="button" variant="outline" size="icon" aria-label="复制 Nginx 初始化命令" onClick={() => void act(() => navigator.clipboard.writeText(nginx.init_command), "初始化命令已复制")}><Copy className="size-4" /></Button></div>}
          {nginx?.setup.message && <p className="break-words text-xs leading-5 text-muted-foreground">{nginx.setup.message}</p>}
          {nginx?.setup.initialized_at && <p className="text-xs text-muted-foreground">上次初始化：<TimeText value={nginx.setup.initialized_at} /></p>}
          <p className="text-xs leading-5 text-muted-foreground">日常运行用户：<code>{nginx?.runtime_user ?? "读取中"}</code>。完成后返回点击“立即同步”，无需下载接入包或运行其他常驻服务。</p>
        </div>
        {platform && <form onSubmit={savePlatform} className="space-y-5"><fieldset disabled={pending} className="space-y-5">
          <Field label="可信代理 IP / 网段" id="security-proxies" hint="每行一条，仅信任实际反向代理。GoCMS 与 Nginx 同机时通常填写 127.0.0.1/32 和 ::1/128；使用 CDN 时，还需在 Nginx 配置可信的真实 IP 来源。"><Textarea id="security-proxies" rows={3} placeholder={"127.0.0.1/32\n::1/128"} className="font-mono" value={proxyText} onChange={e => setProxyText(e.target.value)} /></Field>
          <div className="flex items-center justify-between gap-4 rounded-xl border p-4"><div><Label htmlFor="security-nginx-enabled">启用 Nginx 名单同步</Label><p className="mt-1 text-xs leading-5 text-muted-foreground">保存后自动同步；关闭后仍需完成一次同步以清除 Nginx 中的规则。</p></div><Switch id="security-nginx-enabled" checked={platform.nginx_enabled} onCheckedChange={nginx_enabled => setPlatform({ ...platform, nginx_enabled })} /></div>
          <div className="rounded-xl border p-4"><h3 className="mb-2 text-sm font-medium">Nginx 配置路径</h3><p className="mb-4 text-xs leading-5 text-muted-foreground">安全规则集中写入 gocms.conf，不存在时由初始化命令自动创建，无需手工建文件或添加 include。初始化按后台配置的域名识别宝塔已有站点，自动添加拦截入口并保留反向代理与证书配置。</p><div className="grid gap-4 sm:grid-cols-2">{([
            ["nginx_binary", "Nginx 可执行文件"], ["nginx_config", "Nginx 主配置文件"], ["server_config", "GoCMS 安全规则文件"], ["run_user", "GoCMS 系统运行用户"],
          ] as const).map(([key, label]) => <Field key={key} label={label} id={`security-${key}`}><Input id={`security-${key}`} required className="font-mono" value={platform[key]} onChange={e => setPlatform({ ...platform, [key]: e.target.value })} /></Field>)}</div></div>
        </fieldset><Button type="submit" disabled={pending}>保存平台设置</Button></form>}
        <div className="flex flex-wrap items-center gap-3"><NginxConfigDialog /><p className="text-xs text-muted-foreground">查看已保存接入设置对应的实际文件。</p></div>
        {nginx && <details className="rounded-xl border p-4"><summary className="cursor-pointer text-sm">查看待同步配置</summary><ScrollArea orientation="both" className="mt-3 max-h-80" contentClassName="p-1"><pre className="text-xs leading-5">{nginx.http_config}{"\n"}{nginx.server_config}</pre></ScrollArea></details>}
      </section>}
      {!canManage && <p className="text-xs text-muted-foreground">当前为只读。修改本站设置与名单需要“管理站点安全防护”权限。</p>}
    </ScrollArea>

    <Dialog open={dialog} onOpenChange={open => { if (!pending) setDialog(open) }}><DialogContent className="sm:max-w-lg"><DialogHeader><DialogTitle>添加{scope === "global" ? "全平台" : "本站"} IP 规则</DialogTitle><DialogDescription>支持 IPv4、IPv6 和 CIDR 网段。白名单可豁免扫描检测和相应范围的封禁。</DialogDescription></DialogHeader><ScrollArea className="max-h-[60dvh]" contentClassName="p-1"><form id="security-entry-form" onSubmit={add}><fieldset disabled={pending} className="space-y-4">
      <Field label="IP / 网段" id="security-network"><Input id="security-network" required autoFocus placeholder="203.0.113.25 或 203.0.113.0/24" value={draft.network} onChange={e => setDraft({ ...draft, network: e.target.value })} /></Field>
      <Field label="规则类型" id="security-kind"><Select value={draft.kind} onValueChange={kind => { if (kind === "allow" || kind === "deny") setDraft({ ...draft, kind }) }}><SelectTrigger id="security-kind" className="w-full"><SelectValue>{draft.kind === "deny" ? "封禁" : "白名单"}</SelectValue></SelectTrigger><SelectContent><SelectItem value="deny">封禁</SelectItem><SelectItem value="allow">白名单</SelectItem></SelectContent></Select></Field>
      <Field label="有效期" id="security-expiry"><Select value={draft.duration} onValueChange={duration => { if (duration) setDraft({ ...draft, duration }) }}><SelectTrigger id="security-expiry" className="w-full"><SelectValue>{({ "3600": "1 小时", "86400": "24 小时", "604800": "7 天", "0": "长期有效" } as Record<string, string>)[draft.duration]}</SelectValue></SelectTrigger><SelectContent><SelectItem value="0">长期有效</SelectItem><SelectItem value="3600">1 小时</SelectItem><SelectItem value="86400">24 小时</SelectItem><SelectItem value="604800">7 天</SelectItem></SelectContent></Select></Field>
      <Field label="原因" id="security-reason"><Textarea id="security-reason" required maxLength={150} rows={3} value={draft.reason} onChange={e => setDraft({ ...draft, reason: e.target.value })} /></Field>
    </fieldset></form></ScrollArea><DialogFooter><Button variant="outline" disabled={pending} onClick={() => setDialog(false)}>取消</Button><Button form="security-entry-form" type="submit" disabled={pending}>保存规则</Button></DialogFooter></DialogContent></Dialog>
    <ConfirmDialog open={!!removing} onOpenChange={open => { if (!open && !pending) setRemoving(null) }} title={removing?.kind === "deny" ? "解除封禁" : "移除白名单"} description={removing ? `确认移除「${entryScopeLabel(removing.site_id)}」中 ${removing.network} 的规则？Nginx 将在下次同步时应用。` : ""} confirmLabel="确认移除" pending={pending} onConfirm={confirmRemoval} />
  </>
}
