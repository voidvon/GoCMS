import { useCallback, useEffect, useRef, useState, type FormEvent } from "react"
import { Pencil, Plus, RefreshCw, Search, Trash2 } from "lucide-react"
import { deleteCommerceDimension, getCommerceDimensions, saveCommerceDimension } from "@/lib/api"
import type { AdminUser, CommerceDimension, CommerceDimensionRecord } from "@/lib/api"
import { useSite } from "@/lib/site-context"
import { useDialogState } from "@/lib/use-dialog-state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import { ConfirmDialog } from "./app-ui"
import { showError, showSuccess } from "./admin-notifications"
import { normalizedDimensions } from "./content-commerce-model"

const types = { select: "选项", text: "文本", number: "数字" }
const emptyDimension = (): CommerceDimensionRecord => ({ key: "", name: "", type: "select", unit: "", options: [], version: 0, product_count: 0 })

export function CommerceDimensionsPage({ user }: { user: AdminUser }) {
  const { activeSiteId } = useSite()
  const permissions = user.site_permissions?.[activeSiteId] ?? user.permissions
  const canManage = user.is_super || permissions.includes("*") || permissions.includes("payments.configure")
  const [items, setItems] = useState<CommerceDimensionRecord[]>([])
  const [query, setQuery] = useState("")
  const [loading, setLoading] = useState(true)
  const [failed, setFailed] = useState(false)
  const [saving, setSaving] = useState(false)
  const [editing, setEditing, editDialog] = useDialogState<CommerceDimensionRecord>()
  const [removing, setRemoving, removeDialog] = useDialogState<CommerceDimensionRecord>()
  const mounted = useRef(true)
  const loadSequence = useRef(0)
  const reload = useCallback(async () => {
    const sequence = ++loadSequence.current
    setLoading(true)
    try {
      const result = await getCommerceDimensions()
      if (mounted.current && sequence === loadSequence.current) { setItems(result.items); setFailed(false) }
    } catch (error) { if (mounted.current && sequence === loadSequence.current) { setFailed(true); showError(error) } }
    finally { if (mounted.current && sequence === loadSequence.current) setLoading(false) }
  }, [])
  useEffect(() => {
    mounted.current = true
    void reload()
    return () => { mounted.current = false }
  }, [reload])
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!editing || !canManage || saving) return
    setSaving(true)
    try {
      const normalized = normalizedDimensions([{ ...editing, key: editing.key || "new" }])[0]
      await saveCommerceDimension({ ...editing, ...normalized, key: editing.key })
      if (!mounted.current) return
      setEditing(null)
      showSuccess("规格维度已保存，引用内容已同步更新")
      await reload()
    } catch (error) { if (mounted.current) showError(error) }
    finally { if (mounted.current) setSaving(false) }
  }
  async function remove() {
    if (!removing || !canManage || saving) return
    setSaving(true)
    try {
      await deleteCommerceDimension(removing)
      if (!mounted.current) return
      setRemoving(null)
      showSuccess("规格维度已删除")
      await reload()
    } catch (error) { if (mounted.current) showError(error) }
    finally { if (mounted.current) setSaving(false) }
  }
  const matches = items.filter(d => `${d.name} ${d.unit} ${d.options.join(" ")}`.toLowerCase().includes(query.trim().toLowerCase()))
  function update(patch: Partial<CommerceDimensionRecord>) { if (editing) setEditing({ ...editing, ...patch }) }
  return <>
    <ScrollArea className="h-full" contentClassName="space-y-5 pb-8 pe-2">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="relative w-full sm:w-80"><Search className="pointer-events-none absolute start-3 top-2.5 size-4 text-muted-foreground" /><Input aria-label="搜索规格维度" className="ps-9" placeholder="搜索名称、单位或候选值" value={query} onChange={e => setQuery(e.target.value)} /></div>
        <div className="flex gap-2"><Button type="button" variant="outline" disabled={loading || saving} onClick={() => void reload()}><RefreshCw className="size-4" />刷新</Button><Button type="button" disabled={!canManage || saving} onClick={() => setEditing(emptyDimension())}><Plus className="size-4" />新增维度</Button></div>
      </div>
      <p className="text-sm leading-6 text-muted-foreground">在此统一定义规格维度，内容编辑中可直接选用并填写规格组合，无需开启在线销售；开启销售时再填写售价。名称及候选值的修改会同步到引用内容，SKU、价格及历史订单保持原有记录。</p>
      {!canManage && <p className="text-sm text-muted-foreground">当前为只读。新增、修改和删除需要“支付渠道配置”权限。</p>}
      {loading ? <p role="status" className="text-sm text-muted-foreground">正在读取规格维度…</p> : failed ? <p role="status" className="text-sm text-muted-foreground">规格维度加载失败，可点击刷新重试。</p> : !matches.length ? <p className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">{query.trim() ? "没有匹配的维度" : "还没有规格维度，创建后即可在内容中选用。"}</p> : <div className="space-y-3">{matches.map(d => <section key={d.key} className="flex flex-wrap items-start justify-between gap-4 rounded-xl border bg-card p-4">
        <div className="min-w-0 flex-1"><div className="flex flex-wrap items-center gap-2"><h2 className="font-medium">{d.name}</h2><Badge variant="secondary">{types[d.type]}</Badge>{d.unit && <Badge variant="outline">{d.unit}</Badge>}<span className="text-xs text-muted-foreground">{d.product_count} 条内容使用</span></div>{d.type === "select" && <p className="mt-2 line-clamp-2 break-words text-sm leading-6 text-muted-foreground">{d.options.join(" · ")}</p>}</div>
        <div className="flex gap-1"><Button type="button" variant="ghost" size="icon" disabled={saving} aria-label={`${canManage ? "编辑" : "查看"}维度 ${d.name}`} onClick={() => setEditing({ ...d, options: [...d.options] })}><Pencil className="size-4" /></Button><Button type="button" variant="ghost" size="icon" disabled={!canManage || saving || d.product_count > 0} title={d.product_count > 0 ? "先在内容中取消引用，再删除维度" : "删除维度"} aria-label={`删除维度 ${d.name}`} onClick={() => setRemoving(d)}><Trash2 className="size-4" /></Button></div>
      </section>)}</div>}
    </ScrollArea>
    <Dialog {...editDialog} onOpenChange={open => { if (!open && !saving) setEditing(null) }}>
      <DialogContent className="sm:max-w-xl"><DialogHeader><DialogTitle>{editing?.key ? "编辑规格维度" : "新增规格维度"}</DialogTitle><DialogDescription>按当前站点统一管理，产品可自由选用。在内容中配置实际需要的规格行即可，无需开启在线销售。</DialogDescription></DialogHeader>
        {editing && <ScrollArea className="max-h-[60dvh]" contentClassName="p-1"><form id="commerce-dimension-form" onSubmit={save}><fieldset disabled={saving || !canManage} className="space-y-4">
          <div className="space-y-1.5"><Label htmlFor="dimension-name">维度名称</Label><Input id="dimension-name" required autoFocus maxLength={40} value={editing.name} placeholder="例如口径、材质、配置" onChange={e => update({ name: e.target.value })} /></div>
          <div className="grid gap-4 sm:grid-cols-2"><div className="space-y-1.5"><Label>类型</Label><Select disabled={saving || !canManage || editing.product_count > 0} value={editing.type} onValueChange={type => { if (type) update({ type: type as CommerceDimension["type"] }) }}><SelectTrigger aria-label="规格维度类型" className="w-full"><SelectValue>{types[editing.type]}</SelectValue></SelectTrigger><SelectContent>{Object.entries(types).map(([key, label]) => <SelectItem key={key} value={key}>{label}</SelectItem>)}</SelectContent></Select></div><div className="space-y-1.5"><Label htmlFor="dimension-unit">单位（选填）</Label><Input id="dimension-unit" disabled={saving || !canManage || editing.product_count > 0} value={editing.unit} maxLength={20} placeholder="例如 mm" onChange={e => update({ unit: e.target.value })} /></div></div>
          {editing.type === "select" && <div className="space-y-1.5"><Label htmlFor="dimension-options">候选值（每行一个）</Label><Textarea id="dimension-options" className="resize-none" rows={6} required maxLength={650000} value={editing.options.join("\n")} placeholder={"DN15\nDN20\nDN25"} onChange={e => update({ options: e.target.value.split("\n") })} /></div>}
          {editing.product_count > 0 && <p className="text-xs leading-6 text-muted-foreground">已被 {editing.product_count} 条内容引用，类型与单位不可修改。可以更名、添加选项或移除未使用的选项；仍被规格行使用的候选值需先在内容中处理，历史订单不变。</p>}
        </fieldset></form></ScrollArea>}
        <DialogFooter><Button type="button" variant="outline" disabled={saving} onClick={() => setEditing(null)}>关闭</Button>{canManage && <Button type="submit" form="commerce-dimension-form" disabled={saving}>{saving ? "保存中…" : "保存"}</Button>}</DialogFooter>
      </DialogContent>
    </Dialog>
    <ConfirmDialog {...removeDialog} onOpenChange={open => { if (!open && !saving) setRemoving(null) }} title="删除规格维度" description={`确定删除“${removing?.name ?? ""}”吗？已被内容使用的维度需先取消引用。`} confirmLabel="确认删除" pending={saving} onConfirm={() => void remove()} />
  </>
}
