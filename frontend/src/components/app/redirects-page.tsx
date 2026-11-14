import { useCallback, useEffect, useRef, useState, type ChangeEvent, type FormEvent } from "react"
import { ArrowDown, Download, Pencil, Plus, RefreshCw, Search, Trash2, Upload } from "lucide-react"
import { deleteRedirect, getActiveSiteId, getRedirects, importRedirects, saveRedirect, type AdminUser, type RedirectRule } from "@/lib/api"
import { useSite } from "@/lib/site-context"
import { useDialogState } from "@/lib/use-dialog-state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { ConfirmDialog, TablePagination } from "./app-ui"
import { showError, showSuccess } from "./admin-notifications"

const emptyRule = (): RedirectRule => ({ id: 0, source_path: "", target_url: "", status_code: 301, enabled: true, preserve_query: true, note: "" })
const pageSize = 20

export function RedirectsPage({ user }: { user: AdminUser }) {
  const { activeSiteId } = useSite()
  const permissions = user.site_permissions?.[activeSiteId] ?? user.permissions
  const canManage = user.is_super || permissions.includes("*") || permissions.includes("redirects.manage")
  const [items, setItems] = useState<RedirectRule[]>([])
  const [query, setQuery] = useState("")
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(true)
  const [failed, setFailed] = useState(false)
  const [saving, setSaving] = useState(false)
  const [editing, setEditing, editDialog] = useDialogState<RedirectRule>()
  const [removing, setRemoving, removeDialog] = useDialogState<RedirectRule>()
  const fileInput = useRef<HTMLInputElement>(null)
  const mounted = useRef(true)
  const loadSequence = useRef(0)
  const reload = useCallback(() => {
    const sequence = ++loadSequence.current
    return getRedirects().then(result => {
      if (mounted.current && sequence === loadSequence.current) { setItems(result.items); setFailed(false) }
    }).catch(error => {
      if (mounted.current && sequence === loadSequence.current) { setFailed(true); showError(error) }
    }).finally(() => {
      if (mounted.current && sequence === loadSequence.current) setLoading(false)
    })
  }, [])
  useEffect(() => {
    mounted.current = true
    void reload()
    return () => { mounted.current = false }
  }, [reload])

  function refresh() {
    setLoading(true)
    return reload()
  }

  async function persist(rule: RedirectRule, closeDialog = false) {
    if (!canManage || saving) return
    setSaving(true)
    try {
      await saveRedirect(rule)
      if (!mounted.current) return
      if (closeDialog) setEditing(null)
      showSuccess(rule.enabled ? "重定向规则已保存并生效" : "重定向规则已停用")
      await refresh()
    } catch (error) { if (mounted.current) showError(error) }
    finally { if (mounted.current) setSaving(false) }
  }
  function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (editing) void persist(editing, true)
  }
  async function remove() {
    if (!removing || !canManage || saving) return
    setSaving(true)
    try {
      await deleteRedirect(removing.id)
      if (!mounted.current) return
      setRemoving(null)
      showSuccess("重定向规则已删除")
      await refresh()
    } catch (error) { if (mounted.current) showError(error) }
    finally { if (mounted.current) setSaving(false) }
  }
  async function importFile(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0]
    event.target.value = ""
    if (!file || !canManage || saving) return
    setSaving(true)
    try {
      if (file.size > 2 * 1024 * 1024) throw new Error("JSON 文件不能超过 2 MiB")
      const payload: unknown = JSON.parse(await file.text())
      if (!payload || typeof payload !== "object" || !("items" in payload) || !Array.isArray(payload.items) || !payload.items.length || payload.items.length > 1000) throw new Error("文件需包含 items 数组，每次导入 1–1000 条规则")
      if (!mounted.current || getActiveSiteId() !== activeSiteId) return
      const result = await importRedirects(payload.items)
      if (!mounted.current) return
      showSuccess(`已导入 ${result.imported} 条重定向规则`)
      await refresh()
    } catch (error) { if (mounted.current) showError(error) }
    finally { if (mounted.current) setSaving(false) }
  }
  function exportFile() {
    const data = { items: items.map(({ id: _id, ...rule }) => rule) }
    const url = URL.createObjectURL(new Blob([JSON.stringify(data, null, 2)], { type: "application/json" }))
    const link = document.createElement("a")
    link.href = url
    link.download = `redirects-site-${activeSiteId}.json`
    link.click()
    window.setTimeout(() => URL.revokeObjectURL(url), 1000)
  }
  function update(patch: Partial<RedirectRule>) { if (editing) setEditing({ ...editing, ...patch }) }
  const matches = items.filter(rule => `${rule.source_path} ${rule.target_url} ${rule.note}`.toLowerCase().includes(query.trim().toLowerCase()))
  const totalPages = Math.max(1, Math.ceil(matches.length / pageSize))
  const currentPage = Math.min(page, totalPages)
  const visible = matches.slice((currentPage - 1) * pageSize, currentPage * pageSize)

  return <>
    <ScrollArea className="h-full" contentClassName="space-y-5 pb-8 pe-2">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="relative w-full sm:w-80"><Search className="pointer-events-none absolute start-3 top-2.5 size-4 text-muted-foreground" /><Input aria-label="搜索重定向规则" className="ps-9" placeholder="搜索旧路径、目标地址或备注" value={query} onChange={e => { setQuery(e.target.value); setPage(1) }} /></div>
        <div className="flex flex-wrap gap-2">
          <Button type="button" variant="outline" disabled={loading || saving} onClick={() => void refresh()} aria-label="刷新重定向规则"><RefreshCw className="size-4" />刷新</Button>
          <Button type="button" variant="outline" disabled={loading || failed || saving || !items.length} onClick={exportFile}><Download className="size-4" />导出</Button>
          <Button type="button" variant="outline" disabled={!canManage || saving || loading} onClick={() => fileInput.current?.click()}><Upload className="size-4" />导入 JSON</Button>
          <Button type="button" disabled={!canManage || saving} onClick={() => setEditing(emptyRule())}><Plus className="size-4" />新增规则</Button>
          <input ref={fileInput} className="hidden" type="file" accept=".json,application/json" aria-label="导入重定向 JSON 文件" onChange={event => void importFile(event)} />
        </div>
      </div>
      <div className="space-y-1 text-sm leading-6 text-muted-foreground"><p>规则仅在当前站点生效，保存后立即应用，无需重新发布。来源按完整路径匹配，忽略末尾斜杠和查询参数；各语言子目录需分别配置。</p><p>批量导入可使用导出的 JSON 格式，每次最多 1000 条；来源重复或存在循环时整批取消，不覆盖已有规则。</p></div>
      {!canManage && <p className="text-sm text-muted-foreground">当前为只读，新增、修改、导入和删除需要“管理重定向规则”权限。</p>}
      {loading ? <p role="status" className="text-sm text-muted-foreground">正在读取重定向规则…</p> : failed ? <p role="status" className="text-sm text-muted-foreground">重定向规则加载失败，可点击刷新重试。</p> : !matches.length ? <p className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">{query.trim() ? "没有匹配的规则" : "还没有重定向规则，添加旧路径和对应的新地址即可。"}</p> : <div className="space-y-3">{visible.map(rule => <section key={rule.id} aria-label={`重定向 ${rule.source_path}`} className="rounded-xl border bg-card p-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-2"><Badge variant={rule.enabled ? "secondary" : "outline"}>{rule.status_code}</Badge><span className="text-xs text-muted-foreground">{rule.enabled ? "已启用" : "已停用"} · {rule.preserve_query ? "保留查询参数" : "不保留查询参数"}</span></div>
          <div className="flex items-center gap-2"><Switch aria-label={`启用规则 ${rule.source_path}`} checked={rule.enabled} disabled={!canManage || saving} onCheckedChange={enabled => void persist({ ...rule, enabled })} /><Button type="button" variant="ghost" size="icon" disabled={saving} aria-label={`${canManage ? "编辑" : "查看"}规则 ${rule.source_path}`} onClick={() => setEditing({ ...rule })}><Pencil className="size-4" /></Button><Button type="button" variant="ghost" size="icon" disabled={!canManage || saving} aria-label={`删除规则 ${rule.source_path}`} onClick={() => setRemoving(rule)}><Trash2 className="size-4" /></Button></div>
        </div>
        <div className="mt-2 space-y-1 font-mono text-sm"><p className="break-all">{rule.source_path}</p><ArrowDown className="size-3.5 text-muted-foreground" aria-label="重定向至" /><p className="break-all text-muted-foreground">{rule.target_url}</p></div>
        {rule.note && <p className="mt-3 whitespace-pre-wrap break-words text-sm text-muted-foreground">{rule.note}</p>}
      </section>)}</div>}
      {!loading && !failed && matches.length > 0 && <TablePagination page={currentPage} totalPages={totalPages} total={matches.length} pageSize={pageSize} onPageChange={setPage} />}
    </ScrollArea>
    <Dialog {...editDialog} onOpenChange={open => { if (!open && !saving) setEditing(null) }}>
      <DialogContent className="sm:max-w-xl"><DialogHeader><DialogTitle>{editing?.id ? "编辑重定向规则" : "新增重定向规则"}</DialogTitle><DialogDescription>301 用于永久迁移，302 用于临时跳转。仅重定向公开页面的 GET 和 HEAD 请求。</DialogDescription></DialogHeader>
        {editing && <ScrollArea className="max-h-[60dvh]" contentClassName="p-1"><form id="redirect-form" onSubmit={save}><fieldset disabled={saving || !canManage} className="space-y-4">
          <div className="space-y-1.5"><Label htmlFor="redirect-source">旧路径</Label><Input id="redirect-source" required autoFocus maxLength={2048} placeholder="/old-section/article" value={editing.source_path} onChange={e => update({ source_path: e.target.value })} /><p className="text-xs text-muted-foreground">以 / 开头，不含域名、查询参数或锚点。不会匹配子路径。</p></div>
          <div className="space-y-1.5"><Label htmlFor="redirect-target">目标地址</Label><Input id="redirect-target" required maxLength={4096} placeholder="/new-section/article 或 https://example.com/page" value={editing.target_url} onChange={e => update({ target_url: e.target.value })} /></div>
          <div className="space-y-1.5"><Label htmlFor="redirect-status">重定向类型</Label><Select disabled={saving || !canManage} value={String(editing.status_code)} onValueChange={value => { if (value) update({ status_code: Number(value) as 301 | 302 }) }}><SelectTrigger id="redirect-status" className="w-full"><SelectValue>{editing.status_code === 301 ? "301 · 永久重定向" : "302 · 临时重定向"}</SelectValue></SelectTrigger><SelectContent><SelectItem value="301">301 · 永久重定向</SelectItem><SelectItem value="302">302 · 临时重定向</SelectItem></SelectContent></Select></div>
          <div className="flex items-center justify-between gap-3 rounded-lg border p-3"><Label htmlFor="redirect-query" className="block"><span>保留查询参数</span><span className="mt-1 block text-xs font-normal leading-5 text-muted-foreground">保留访问来源等参数；目标地址的同名参数优先。</span></Label><Switch id="redirect-query" checked={editing.preserve_query} disabled={saving || !canManage} onCheckedChange={preserve_query => update({ preserve_query })} /></div>
          <div className="flex items-center justify-between rounded-lg border p-3"><Label htmlFor="redirect-enabled">启用此规则</Label><Switch id="redirect-enabled" checked={editing.enabled} disabled={saving || !canManage} onCheckedChange={enabled => update({ enabled })} /></div>
          <div className="space-y-1.5"><Label htmlFor="redirect-note">备注（选填）</Label><Textarea id="redirect-note" rows={2} maxLength={1000} value={editing.note} onChange={e => update({ note: e.target.value })} /></div>
        </fieldset></form></ScrollArea>}
        <DialogFooter><Button type="button" variant="outline" disabled={saving} onClick={() => setEditing(null)}>关闭</Button>{canManage && <Button type="submit" form="redirect-form" disabled={saving}>{saving ? "保存中…" : "保存规则"}</Button>}</DialogFooter>
      </DialogContent>
    </Dialog>
    <ConfirmDialog {...removeDialog} onOpenChange={open => { if (!open && !saving) setRemoving(null) }} title="删除重定向规则" description={`确定删除来源为“${removing?.source_path ?? ""}”的规则吗？删除后该地址恢复原来的访问行为。`} confirmLabel="确认删除" pending={saving} onConfirm={() => void remove()} />
  </>
}
