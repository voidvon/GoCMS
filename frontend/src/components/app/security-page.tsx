import { useCallback, useEffect, useRef, useState, type FormEvent, type ReactNode } from "react"
import { Download, Plus, RefreshCw, ShieldCheck, ShieldOff, Unplug } from "lucide-react"
import {
  addSecurityEntry, downloadSecurityInstaller, getSecurityEntries, getSecurityEvents, getSecurityNginx,
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
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Textarea } from "@/components/ui/textarea"
import { ConfirmDialog, TablePagination } from "./app-ui"
import { showError, showSuccess } from "./admin-notifications"
import { TimeText } from "./time-text"

const syncLabels = { disabled: "未启用", pending: "待同步", applied: "已生效", failed: "同步失败" }
const actionLabels: Record<string, string> = { probe: "路径探测", ban: "自动封禁", deny: "手动封禁", allow: "加入白名单", revoke: "移除名单" }
const lines = (value: string) => value.split("\n").map(line => line.trim()).filter(Boolean)
const emptyEntry = () => ({ network: "", kind: "deny" as "deny" | "allow", reason: "", duration: "0" })

function Field({ label, id, hint, children }: { label: string; id: string; hint?: string; children: ReactNode }) {
  return <div className="space-y-2"><Label htmlFor={id}>{label}</Label>{children}{hint && <p className="text-xs leading-5 text-muted-foreground">{hint}</p>}</div>
}

export function SecurityPage({ user }: { user: AdminUser }) {
  const { activeSite, activeSiteId } = useSite()
  const permissions = user.site_permissions?.[activeSiteId] ?? user.permissions
  const canManage = user.is_super || permissions.includes("*") || permissions.includes("security.manage")
  const [scope, setScope] = useState(user.is_super ? "global" : "site")
  const [tab, setTab] = useState("entries")
  const [policy, setPolicy] = useState<SecurityPolicy | null>(null)
  const canManagePolicy = canManage && (user.is_super || policy?.ban_scope === "site")
  const [pathText, setPathText] = useState("")
  const [platform, setPlatform] = useState<SecurityPlatform | null>(null)
  const [proxyText, setProxyText] = useState("")
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
      setEntries(entryData.items)
      setTotal(entryData.total)
      setEvents(eventData.items)
      setEventTotal(eventData.total)
      setSnapshotTime(Date.now())
      setFailed(false)
    }).catch(error => {
      if (mounted.current && sequence.current === current) {
        setFailed(true)
        showError(error)
      }
    }).finally(() => {
      if (mounted.current && sequence.current === current) setLoading(false)
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
      void getSecurityPlatform().then(value => { if (mounted.current) { setPlatform(value); setProxyText(value.trusted_proxies.join("\n")) } }).catch(error => { if (mounted.current) showError(error) })
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
    if (platform) void act(() => saveSecurityPlatform({ ...platform, trusted_proxies: lines(proxyText) }), "平台接入设置已保存")
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
  const loadingState = loading ? "正在读取安全记录…" : failed ? "加载失败，请刷新重试。" : ""

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
        <Button variant="outline" disabled={loading || pending} onClick={() => { setLoading(true); void refreshLists() }}><RefreshCw className="size-4" />刷新记录</Button>
      </div>
      <Tabs value={tab} onValueChange={value => setTab(String(value))}>
        <ScrollArea orientation="horizontal" className="w-full"><TabsList><TabsTrigger value="entries">黑白名单</TabsTrigger><TabsTrigger value="policy">扫描防护</TabsTrigger><TabsTrigger value="events">安全记录</TabsTrigger>{user.is_super && <TabsTrigger value="nginx">Nginx 接入</TabsTrigger>}</TabsList></ScrollArea>
      </Tabs>

      {tab === "entries" && <section className="space-y-4" aria-label="IP 黑白名单">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-2"><Switch id="security-history" checked={history} onCheckedChange={value => { setHistory(value); setPage(1); setLoading(true) }} /><Label htmlFor="security-history">显示到期和已移除记录</Label></div>
          {canManage && <Button disabled={pending || loading || failed} onClick={() => { setDraft(emptyEntry()); setDialog(true) }}><Plus className="size-4" />添加 IP / 网段</Button>}
        </div>
        {loadingState ? <p role="status" className="text-sm text-muted-foreground">{loadingState}</p> : !entries.length ? <div className="rounded-xl border border-dashed p-10 text-center"><ShieldCheck className="mx-auto mb-3 size-7 text-muted-foreground" /><p className="text-sm text-muted-foreground">当前范围暂无名单记录</p></div> : entries.map(entry => {
          const expired = !!entry.expires_at && new Date(entry.expires_at).getTime() <= snapshotTime
          const inactive = !!entry.revoked_at || expired
          return <article key={entry.id} className="space-y-3 rounded-xl border bg-card p-4">
            <div className="flex flex-wrap items-center justify-between gap-3"><div className="flex flex-wrap items-center gap-2"><span className="break-all font-mono text-sm">{entry.network}</span><Badge variant={entry.kind === "allow" ? "secondary" : "outline"}>{entry.kind === "allow" ? "白名单" : "封禁"}</Badge><span className="text-xs text-muted-foreground">{entry.revoked_at ? "已移除" : expired ? "已到期" : entry.source === "automatic" ? "自动封禁" : "手动添加"}</span></div>{canManage && !inactive && <Button variant="outline" size="sm" disabled={pending} onClick={() => setRemoving(entry)}><ShieldOff className="size-4" />{entry.kind === "deny" ? "解封" : "移除白名单"}</Button>}</div>
            <p className="break-words text-sm">{entry.reason}</p><div className="flex flex-wrap gap-x-5 gap-y-1 text-xs text-muted-foreground"><span>添加于 <TimeText value={entry.created_at} /></span><span>{entry.expires_at ? <>到期于 <TimeText value={entry.expires_at} /></> : "长期有效"}</span></div>
          </article>
        })}
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

      {tab === "events" && <section className="space-y-3" aria-label="安全记录">
        <p className="text-xs leading-5 text-muted-foreground">记录首次路径探测、自动封禁及名单变更，保留 30 天。同一窗口内的重复探测合并计数。</p>
        {loadingState ? <p role="status" className="text-sm text-muted-foreground">{loadingState}</p> : !events.length ? <p className="rounded-xl border border-dashed p-10 text-center text-sm text-muted-foreground">暂无安全记录</p> : events.map(event => <article key={event.id} className="space-y-2 rounded-xl border p-4"><div className="flex flex-wrap items-center justify-between gap-2"><div className="flex flex-wrap items-center gap-2"><Badge variant={event.action === "ban" ? "destructive" : "outline"}>{actionLabels[event.action] ?? event.action}</Badge>{event.ip && <span className="break-all font-mono text-sm">{event.ip}</span>}</div><span className="text-xs text-muted-foreground"><TimeText value={event.created_at} /></span></div>{event.path && <p className="break-all font-mono text-xs text-muted-foreground">{event.host}{event.path}</p>}<p className="text-sm">{event.reason}</p></article>)}
        {!loading && !failed && eventTotal > 0 && <TablePagination page={eventPage} totalPages={Math.max(1, Math.ceil(eventTotal / 50))} total={eventTotal} pageSize={50} onPageChange={value => { setEventPage(value); setLoading(true) }} />}
      </section>}

      {tab === "nginx" && user.is_super && <section className="max-w-4xl space-y-5" aria-label="Nginx 接入设置">
        {platform?.nginx_mode === "agent" && !nginx?.status.applied_at && <div className="space-y-2 rounded-xl border bg-muted/30 p-4"><p className="text-sm font-medium">当前使用独立代理模式</p><p className="text-sm leading-6 text-muted-foreground">此模式需要安装本机同步代理。使用“GoCMS 直接管理”可由当前程序读写配置并重载 Nginx。</p></div>}
        <div className="space-y-3 rounded-xl border bg-card p-4"><div className="flex flex-wrap items-center justify-between gap-3"><div className="flex items-center gap-2"><Unplug className="size-4" /><span className="font-medium">同步状态</span><Badge variant={nginx?.status.state === "failed" ? "destructive" : "outline"}>{nginx ? syncLabels[nginx.status.state] : "读取中"}</Badge></div><Button variant="outline" disabled={pending || !platform || (!platform.nginx_enabled && nginx?.status.state === "disabled")} onClick={() => void act(() => syncSecurityNginx(), "Nginx 已确认当前配置") }><RefreshCw className="size-4" />立即同步</Button></div>{nginx?.status.applied_at && <p className="text-xs text-muted-foreground">上次确认：<TimeText value={nginx.status.applied_at} /></p>}{nginx?.status.error && <p className="break-words text-sm text-muted-foreground">{nginx.status.error}</p>}<p className="text-xs leading-5 text-muted-foreground">Go 防护独立生效。接入后约每 15 秒同步一次；检查或重载失败时恢复原配置并重试。{platform?.nginx_mode === "agent" ? "独立代理可在 CMS 暂停时继续清理到期规则。" : "直接管理模式在 GoCMS 运行期间同步封禁和到期解封。"}</p></div>
        {platform && <form onSubmit={savePlatform} className="space-y-5"><fieldset disabled={pending} className="space-y-5">
          <Field label="可信代理 IP / 网段" id="security-proxies" hint="每行一条，仅信任实际反向代理。GoCMS 与 Nginx 同机时通常填写 127.0.0.1/32 和 ::1/128；未配置时使用连接来源 IP。使用 CDN 时，还需在 Nginx 配置可信的真实 IP 来源。"><Textarea id="security-proxies" rows={3} placeholder={"127.0.0.1/32\n::1/128"} className="font-mono" value={proxyText} onChange={e => setProxyText(e.target.value)} /></Field>
          <Field label="Nginx 管理方式" id="security-nginx-mode"><Select value={platform.nginx_mode} onValueChange={nginx_mode => { if (nginx_mode === "direct" || nginx_mode === "agent") setPlatform({ ...platform, nginx_mode }) }}><SelectTrigger id="security-nginx-mode" className="w-full"><SelectValue>{platform.nginx_mode === "direct" ? "GoCMS 直接管理" : "独立代理（兼容模式）"}</SelectValue></SelectTrigger><SelectContent><SelectItem value="direct">GoCMS 直接管理</SelectItem><SelectItem value="agent">独立代理（兼容模式）</SelectItem></SelectContent></Select></Field>
          <div className="flex items-center justify-between gap-4 rounded-xl border p-4"><div><Label htmlFor="security-nginx-enabled">启用 Nginx 名单同步</Label><p className="mt-1 text-xs leading-5 text-muted-foreground">保存配置后自动同步；关闭后仍需完成一次同步以清除 Nginx 中的规则。</p></div><Switch id="security-nginx-enabled" checked={platform.nginx_enabled} onCheckedChange={nginx_enabled => setPlatform({ ...platform, nginx_enabled })} /></div>
          {platform.nginx_mode === "agent" && <Field label="本机 Socket 路径" id="security-socket"><Input id="security-socket" required className="font-mono" value={platform.socket_path} onChange={e => setPlatform({ ...platform, socket_path: e.target.value })} /></Field>}
          <div className="rounded-xl border p-4"><h3 className="mb-2 text-sm font-medium">Nginx 配置路径</h3><p className="mb-4 text-xs leading-5 text-muted-foreground">{platform.nginx_mode === "agent" ? "以下路径变更后需重新安装代理接入包。" : "仅修改所选站点配置中的 GoCMS 安全标记区，主配置路径只用于检查和重载。站点配置须由主配置的 http 块引用。"}</p><div className="grid gap-4 sm:grid-cols-2">{([
            ["nginx_binary", "Nginx 可执行文件"], ["nginx_config", platform.nginx_mode === "agent" ? "Nginx 主配置文件" : "Nginx 主配置路径（只读）"], ["server_config", "宝塔站点配置文件"],
          ] as const).map(([key, label]) => <Field key={key} label={label} id={`security-${key}`}><Input id={`security-${key}`} required className="font-mono" value={platform[key]} onChange={e => setPlatform({ ...platform, [key]: e.target.value })} /></Field>)}</div></div>
          {platform.nginx_mode === "agent" && <Field label="GoCMS 系统运行用户" id="security-agent-user"><Input id="security-agent-user" required value={platform.agent_user} onChange={e => setPlatform({ ...platform, agent_user: e.target.value })} /></Field>}
        </fieldset><div className="flex flex-wrap gap-2"><Button type="submit" disabled={pending}>保存平台设置</Button>{platform.nginx_mode === "agent" && <Button type="button" variant="outline" disabled={pending} onClick={() => void act(async () => {
          const blob = await downloadSecurityInstaller(); if (!mounted.current) return
          const url = URL.createObjectURL(blob); const a = document.createElement("a"); a.href = url; a.download = "gocms-security-install.zip"; a.click(); window.setTimeout(() => URL.revokeObjectURL(url), 1000)
        }, "接入包已下载，使用已保存的安装参数") }><Download className="size-4" />下载接入包</Button>}</div></form>}
        {platform?.nginx_mode === "agent" ? <div className="space-y-2 text-sm leading-6 text-muted-foreground"><p>保存参数 → 下载并解压接入包 → 在服务器以 root 执行安装 → 返回后台开启同步。</p><p>安装命令：<code className="break-all text-xs">sudo sh install.sh /绝对路径/site</code></p></div> : <div className="space-y-2 text-sm leading-6 text-muted-foreground"><p>当前系统运行用户：<code>{nginx?.runtime_user ?? "读取中"}</code>。该用户需具备站点配置写入、配置检查与重载权限；无需安装其他服务。</p><p>确认路径 → 开启并保存同步 → 点击“立即同步”。只修改一个站点配置文件，原文件备份保存在 /etc/gocms-security/。</p></div>}
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
    <ConfirmDialog open={!!removing} onOpenChange={open => { if (!open && !pending) setRemoving(null) }} title={removing?.kind === "deny" ? "解除封禁" : "移除白名单"} description={`确认移除 ${removing?.network ?? ""} 的规则？Nginx 将在下次同步时应用。`} confirmLabel="确认移除" pending={pending} onConfirm={() => { if (removing) void act(async () => { await revokeSecurityEntry(scope, removing.id); if (mounted.current) setRemoving(null) }, "规则已移除") }} />
  </>
}
