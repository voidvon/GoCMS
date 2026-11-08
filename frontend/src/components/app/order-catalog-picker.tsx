import { useEffect, useState } from "react"
import { searchOrderCatalog, type OrderCatalogVariant, type OrderCreationChannel } from "@/lib/api"
import { showError } from "@/components/app/admin-notifications"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"

export function OrderCatalogPicker({channel, disabled, onAdd, onClose}: {channel: OrderCreationChannel; disabled: boolean; onAdd: (item: OrderCatalogVariant) => void; onClose: () => void}) {
  const [query, setQuery] = useState("")
  const [search, setSearch] = useState("")
  const [page, setPage] = useState(1)
  const [version, setVersion] = useState(0)
  const [result, setResult] = useState<{key: string; items: OrderCatalogVariant[]; more: boolean} | null>(null)
  const key = `${channel.id}:${page}:${search}:${version}`
  const loading = result?.key !== key
  useEffect(() => {
    let active = true
    void searchOrderCatalog(channel.id, search, page).then(response => {
      if (active) setResult({key, items: response.items, more: response.has_more})
    }).catch(reason => {
      if (active) { setResult({key, items: [], more: false}); showError(reason) }
    })
    return () => { active = false }
  }, [channel.id, page, search, version, key])
  function find() { setSearch(query.trim()); setPage(1); setVersion(value => value + 1) }
  return <section className="space-y-3 rounded-lg border bg-muted/20 p-3" aria-label="搜索现有商品与服务">
    <div className="flex items-center justify-between gap-2"><h4 className="text-sm font-medium">从现有商品 / 服务添加</h4><Button type="button" size="sm" variant="ghost" onClick={onClose}>收起搜索</Button></div>
    <div className="flex gap-2"><Input aria-label="搜索商品或服务" placeholder="名称、内容编号、SKU 或规格" maxLength={128} value={query} disabled={disabled} onChange={e => setQuery(e.target.value)} onKeyDown={e => {if (e.key === "Enter") {e.preventDefault(); find()}}}/><Button type="button" variant="outline" disabled={disabled || loading} onClick={find}>搜索</Button></div>
    <p className="text-xs text-muted-foreground">仅列出本站已开启在线销售且可购买的内容规格。报价按 {channel.currency} 计算，添加后可调整本单单价和数量。</p>
    <ScrollArea className="max-h-72" contentClassName="divide-y">
      {loading ? <p className="py-8 text-center text-sm text-muted-foreground" role="status">正在搜索…</p> : !result?.items.length ? <p className="py-8 text-center text-sm text-muted-foreground">没有符合条件的商品或服务，请核对销售配置或更换关键词。</p> : result.items.map(item => {
        const available = !item.pricing_unavailable && item.currency === channel.currency
        const price = new Intl.NumberFormat("zh-CN", {style: "currency", currency: item.currency}).format(item.amount / (item.currency === "JPY" ? 1 : 100))
        return <article key={item.id} className="flex flex-wrap items-center gap-3 py-3">
          {item.image_url && <img src={item.image_url} alt="" className="size-12 rounded-md border object-cover"/>}
          <div className="min-w-0 flex-1 basis-40"><p className="break-words text-sm font-medium">{item.name}</p><p className="mt-1 break-words text-xs text-muted-foreground">内容 #{item.content_id} · SKU {item.sku || "—"}</p><p className="mt-1 break-words text-xs text-muted-foreground">{item.specifications?.map(s => `${s.name}: ${s.value}${s.unit ? ` ${s.unit}` : ""}`).join(" · ") || "标准规格"}</p></div>
          <div className="text-end"><p className="text-sm font-medium tabular-nums">{price}</p>{!available && <p className="mt-1 text-xs text-muted-foreground">暂无 {channel.currency} 报价</p>}</div>
          <Button type="button" size="sm" variant="outline" aria-label={`添加 ${item.name} ${item.sku || ""}`.trim()} disabled={disabled || !available} onClick={() => onAdd(item)}>添加</Button>
        </article>
      })}
    </ScrollArea>
    <div className="flex items-center justify-between gap-2 text-xs text-muted-foreground"><span>第 {page} 页 · 每页最多 50 个规格</span><div className="flex gap-2"><Button type="button" size="sm" variant="outline" disabled={disabled || loading || page === 1} onClick={() => setPage(value => value - 1)}>上一页</Button><Button type="button" size="sm" variant="outline" disabled={disabled || loading || !result?.more} onClick={() => setPage(value => value + 1)}>下一页</Button></div></div>
  </section>
}
