import { useEffect, useState, type FormEvent, type ReactNode } from "react"
import { Plus, Trash2 } from "lucide-react"
import { createManagedOrder, getOrderCreationOptions, uploadMedia, type CommerceOrder, type ManualOrderInput, type OrderCreationChannel, type OrderCatalogVariant } from "@/lib/api"
import { showError, showSuccess } from "@/components/app/admin-notifications"
import { MediaPickerDialog } from "@/components/app/media-picker-dialog"
import { OrderCatalogPicker } from "@/components/app/order-catalog-picker"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"

const kinds = { service: "服务", physical: "实物商品", digital: "数字商品" }
const languages = { en: "English", "zh-CN": "简体中文", es: "Español", fr: "Français", ru: "Русский", tr: "Türkçe", pt: "Português", id: "Bahasa Indonesia", ar: "العربية", th: "ไทย", vi: "Tiếng Việt" }
function Field({ label, children }: { label: string; children: ReactNode }) {
  return <Label className="grid gap-2 text-sm font-normal"><span className="font-medium">{label}</span>{children}</Label>
}
function newItem(): ManualOrderInput["items"][number] { return { name: "", kind: "service", price: "", quantity: 1, image_url: "" } }

export function CreateOrderDialog({ canMedia, onClose, onCreated }: { canMedia: boolean; onClose: () => void; onCreated: (order: CommerceOrder) => Promise<void> }) {
  const [input, setInput] = useState<ManualOrderInput>({ account_id: 0, customer_name: "", email: "", language: "en", address: {phone: "", street: "", city: "", state: "", country: "", postcode: ""}, items: [newItem()] })
  const [channels, setChannels] = useState<OrderCreationChannel[]>([])
  const [loading, setLoading] = useState(true), [busy, setBusy] = useState(false)
  const [mediaIndex, setMediaIndex] = useState<number | null>(null)
  const [request, setRequest] = useState<{key: string; body: string} | null>(null)
  const [catalogOpen, setCatalogOpen] = useState(false)
  const [sources, setSources] = useState<Record<number, OrderCatalogVariant>>({})
  useEffect(() => {
    let active = true
    void getOrderCreationOptions().then(result => {
      if (!active) return
      setChannels(result.items)
      setInput(value => ({...value, account_id: result.items[0]?.id || 0}))
    }).catch(reason => { if (active) showError(reason) }).finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [])
  const channel = channels.find(c => c.id === input.account_id)
  const physical = input.items.some(i => i.kind === "physical")
  const exponent = channel?.currency === "JPY" ? 0 : 2
  const currencyMismatch = input.items.some(item => item.price && item.currency && item.currency !== channel?.currency)
  const total = input.items.reduce((sum, i) => sum + Math.round(Number(i.price) * 10 ** exponent) * i.quantity, 0)
  const formattedTotal = channel && !currencyMismatch && Number.isSafeInteger(total) ? new Intl.NumberFormat("zh-CN", {style: "currency", currency: channel.currency}).format(total / 10 ** exponent) : "—"
  function updateItem(index: number, patch: Partial<ManualOrderInput["items"][number]>) {
    setInput(value => ({...value, items: value.items.map((item, i) => i === index ? {...item, ...patch, ...("price" in patch ? {currency: channel?.currency} : {})} : item)}))
  }
  function addVariant(offer: OrderCatalogVariant) {
    const existing = input.items.findIndex(item => item.variant_id === offer.id && item.currency === offer.currency)
    if (existing >= 0) {
      if (input.items[existing].quantity >= 1000) { showError("同一规格最多添加 1000 件"); return }
      updateItem(existing, {quantity: input.items[existing].quantity + 1})
    } else {
      const blank = input.items.findIndex(item => !item.variant_id && !item.name && !item.price && !item.image_url && item.quantity === 1)
      if (blank < 0 && input.items.length >= 50) { showError("一个订单最多添加 50 条明细"); return }
      const item = {variant_id: offer.id, currency: offer.currency, name: offer.name, kind: offer.kind, price: (offer.amount / (offer.currency === "JPY" ? 1 : 100)).toFixed(offer.currency === "JPY" ? 0 : 2), quantity: 1, image_url: offer.image_url}
      setInput(value => ({...value, items: blank >= 0 ? value.items.map((row, index) => index === blank ? item : row) : [...value.items, item]}))
    }
    setSources(value => ({...value, [offer.id]: offer}))
    showSuccess("商品规格已添加到订单")
  }
  async function submit(event: FormEvent) {
    event.preventDefault()
    if (busy || !channel || currencyMismatch) return
    const body = JSON.stringify(input)
    const key = request?.body === body ? request.key : crypto.randomUUID()
    setRequest({key, body}); setBusy(true)
    try {
      const order = await createManagedOrder(input, key)
      showSuccess("收款订单已创建")
      await onCreated(order)
    } catch (reason) { showError(reason) }
    finally { setBusy(false) }
  }
  return <>
    <Dialog open onOpenChange={open => { if (!open && !busy) onClose() }}>
      <DialogContent className="flex max-h-[90svh] flex-col overflow-hidden sm:max-w-3xl">
        <DialogHeader><DialogTitle>创建收款订单</DialogTitle><DialogDescription>填写约定的商品或服务明细，创建后获取客户专属付款链接。明细不会修改商品目录，客户无需登录即可付款。</DialogDescription></DialogHeader>
        <form className="flex min-h-0 flex-col gap-4" onSubmit={submit}>
          <ScrollArea className="min-h-0 max-h-[65svh]" contentClassName="space-y-5 pe-3 pb-2">
            <fieldset className="space-y-5" disabled={busy}>
              <Field label="收款渠道与币种"><Select value={String(input.account_id || "")} disabled={loading || !channels.length} onValueChange={value => { if (value) setInput({...input, account_id: Number(value)}) }}><SelectTrigger className="w-full" aria-label="收款渠道与币种"><SelectValue>{channel ? `${channel.name} · ${channel.currency}${channel.sandbox ? "（沙箱）" : ""}` : loading ? "正在加载…" : "暂无可用渠道"}</SelectValue></SelectTrigger><SelectContent>{channels.map(c => <SelectItem key={c.id} value={String(c.id)}>{c.name} · {c.currency}{c.sandbox ? "（沙箱）" : ""}</SelectItem>)}</SelectContent></Select></Field>
              <p className="break-all text-xs text-muted-foreground">{channel ? `付款链接使用已配置的公开地址：${channel.public_url}` : "请先在支付管理中配置密钥并启用收款渠道。"}</p>
              <div className="grid gap-3 sm:grid-cols-2">
                <Field label="客户姓名"><Input required maxLength={100} value={input.customer_name} onChange={e => setInput({...input, customer_name: e.target.value})}/></Field>
                <Field label="客户邮箱"><Input type="email" required maxLength={128} value={input.email} onChange={e => setInput({...input, email: e.target.value})}/></Field>
                <Field label="交付通知邮件语言"><Select value={input.language} onValueChange={value => { if (value) setInput({...input, language: value}) }}><SelectTrigger className="w-full"><SelectValue>{languages[input.language as keyof typeof languages]}</SelectValue></SelectTrigger><SelectContent>{Object.entries(languages).map(([code, name]) => <SelectItem key={code} value={code}>{name}</SelectItem>)}</SelectContent></Select></Field>
                <Field label={physical ? "联系电话（必填）" : "联系电话（可选）"}><Input type="tel" required={physical} maxLength={32} value={input.address.phone} onChange={e => setInput({...input, address: {...input.address, phone: e.target.value}})}/></Field>
              </div>
              <div className="space-y-3"><div className="flex flex-wrap items-center justify-between gap-2"><h3 className="font-medium">订单明细</h3><Button type="button" variant="outline" disabled={!channel} onClick={() => setCatalogOpen(value => !value)}>搜索现有商品 / 服务</Button></div>
                {catalogOpen && channel && <OrderCatalogPicker key={channel.id} channel={channel} disabled={busy} onAdd={addVariant} onClose={() => setCatalogOpen(false)}/>}
                {currencyMismatch && <p className="text-sm text-destructive" role="status">收款币种已切换，部分明细仍保留原币种价格。请移除后重新选择规格，或按当前币种重新填写单价。</p>}
                {input.items.map((item, index) => <Card key={index} className="gap-3 p-4">
                  <div className="flex items-center justify-between"><h4 className="text-sm font-medium">明细 {index + 1}</h4><Button type="button" size="icon" variant="ghost" aria-label={`删除明细 ${index + 1}`} onClick={() => setInput({...input, items: input.items.length === 1 ? [newItem()] : input.items.filter((_, i) => i !== index)})}><Trash2 className="size-4"/></Button></div>
                  {item.variant_id && <div className="space-y-2"><p className="break-words text-xs text-muted-foreground">来自现有商品 · 内容 #{sources[item.variant_id]?.content_id} · SKU {sources[item.variant_id]?.sku || "—"}</p><p className="break-words text-xs text-muted-foreground">{sources[item.variant_id]?.specifications?.map(s => `${s.name}: ${s.value}${s.unit ? ` ${s.unit}` : ""}`).join(" · ") || "标准规格"}</p><Button type="button" size="sm" variant="ghost" onClick={() => updateItem(index, {variant_id: undefined})}>改为手动明细</Button></div>}
                  <Field label="商品 / 服务名称"><Input required readOnly={!!item.variant_id} maxLength={128} value={item.name} onChange={e => updateItem(index, {name: e.target.value})}/></Field>
                  <div className="grid gap-3 sm:grid-cols-3">
                    <Field label="类型"><Select disabled={!!item.variant_id} value={item.kind} onValueChange={value => { if (value) updateItem(index, {kind: value as typeof item.kind}) }}><SelectTrigger className="w-full"><SelectValue>{kinds[item.kind]}</SelectValue></SelectTrigger><SelectContent>{Object.entries(kinds).map(([value, label]) => <SelectItem key={value} value={value}>{label}</SelectItem>)}</SelectContent></Select></Field>
                    <Field label={`单价（${channel?.currency || "—"}）`}><Input type="number" required min={exponent ? "0.01" : "1"} max={99999999999 / 10 ** exponent} step={exponent ? "0.01" : "1"} value={item.price} onChange={e => updateItem(index, {price: e.target.value})}/></Field>
                    <Field label="数量"><Input type="number" required min={1} max={1000} step={1} value={item.quantity} onChange={e => updateItem(index, {quantity: Number(e.target.value)})}/></Field>
                  </div>
                  {item.price && item.currency && item.currency !== channel?.currency && <p className="text-xs text-destructive">此单价原币种为 {item.currency}，请按 {channel?.currency} 重新填写。</p>}
                  <Field label="商品图片（收银台使用）"><Input required readOnly={!!item.variant_id} maxLength={255} placeholder="本站素材路径或公开 HTTPS 图片地址" value={item.image_url} onChange={e => updateItem(index, {image_url: e.target.value})}/></Field>
                  {item.image_url && <img className="size-16 rounded-md border object-cover" src={item.image_url} alt="商品图片预览"/>}
                  {canMedia && !item.variant_id && <div className="flex flex-wrap gap-2"><Button type="button" size="sm" variant="outline" onClick={() => setMediaIndex(index)}>从素材库选择</Button><Label className="inline-flex cursor-pointer items-center rounded-md border px-3 text-sm">上传图片<input className="sr-only" type="file" accept="image/*" disabled={busy} onChange={e => {
                    const file = e.target.files?.[0]; e.target.value = ""
                    if (!file) return
                    setBusy(true)
                    void uploadMedia(file).then(result => { updateItem(index, {image_url: result.asset.url}); showSuccess("图片已上传") }).catch(showError).finally(() => setBusy(false))
                  }}/></Label></div>}
                </Card>)}
                <Button type="button" variant="outline" disabled={input.items.length >= 50} onClick={() => setInput({...input, items: [...input.items, newItem()]})}><Plus className="size-4"/>添加明细</Button>
              </div>
              {physical && <div className="grid gap-3 rounded-lg border p-4 sm:grid-cols-2"><h3 className="font-medium sm:col-span-2">实物商品收货地址</h3>{([['country','国家代码（两位大写字母）',2],['state','省 / 州',128],['city','城市',50],['street','详细地址',256],['postcode','邮编',16]] as const).map(([key, label, max]) => <Field key={key} label={label}><Input required={key !== "state"} maxLength={max} pattern={key === "country" ? "[A-Z]{2}" : undefined} value={input.address[key]} onChange={e => setInput({...input, address: {...input.address, [key]: e.target.value}})}/></Field>)}</div>}
            </fieldset>
          </ScrollArea>
          <DialogFooter className="items-center"><strong className="me-auto text-lg">合计 {formattedTotal}</strong><Button type="button" variant="outline" disabled={busy} onClick={onClose}>取消</Button><Button type="submit" disabled={busy || loading || !channel || currencyMismatch}>{busy ? "正在创建…" : "创建并生成付款链接"}</Button></DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
    <MediaPickerDialog open={mediaIndex !== null} onOpenChange={open => { if (!open) setMediaIndex(null) }} onSelect={asset => { if (mediaIndex !== null) updateItem(mediaIndex, {image_url: asset.url}); setMediaIndex(null) }}/>
  </>
}
