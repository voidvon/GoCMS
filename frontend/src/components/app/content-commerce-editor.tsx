import { Plus, Trash2 } from "lucide-react"
import type { CommerceProduct, CommerceVariant } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"

const currencies = ["USD", "EUR", "GBP", "HKD", "CNY", "JPY", "AUD", "CAD", "CHF", "SGD", "NZD"]
const kinds = { physical: "实物商品", digital: "数字商品", service: "服务" }
type VariantDraft = Omit<CommerceVariant, "amount"> & { price: string; localID: string }
export type CommerceDraft = Omit<CommerceProduct, "variants"> & { variants: VariantDraft[] }

let variantSequence = 0
function newVariant(): VariantDraft {
  return { id: 0, sku: "", specifications: [], enabled: true, price: "", localID: `variant-${++variantSequence}` }
}

export function commerceDraft(product?: CommerceProduct): CommerceDraft | undefined {
  if (!product) return undefined
  return {
    ...product,
    variants: product.variants.map(({ amount, ...variant }) => ({
      ...variant, specifications: variant.specifications ?? [],
      price: String(amount / (product.currency === "JPY" ? 1 : 100)), localID: `variant-${++variantSequence}`,
    })),
  }
}

export function commercePayload(draft?: CommerceDraft): CommerceProduct | undefined {
  if (!draft) return undefined
  const variants = draft.variants
    .filter((variant) => draft.enabled || variant.id || variant.price.trim() || variant.sku.trim() || variant.specifications.length)
    .map((variant) => {
      const price = variant.price
      const exponent = draft.currency === "JPY" ? 0 : 2
      const value = price.trim()
      if (!(exponent ? /^\d+(\.\d{1,2})?$/ : /^\d+$/).test(value)) throw new Error(`规格价格${exponent ? "最多保留两位小数" : "必须为整数"}`)
      const [whole, fraction = ""] = value.split(".")
      const amount = Number(exponent ? whole + fraction.padEnd(2, "0") : whole)
      if (!Number.isSafeInteger(amount) || amount <= 0 || amount > 99999999999) throw new Error("请填写有效的正数规格价格")
      return { id: variant.id, sku: variant.sku, specifications: variant.specifications, enabled: variant.enabled, amount }
    })
  if (draft.enabled && !variants.some((variant) => variant.enabled)) throw new Error("开启销售后至少需要一个已启用并配置价格的规格")
  return { enabled: draft.enabled, kind: draft.kind, currency: draft.currency, variants }
}

export function ContentCommerceEditor({ value, onChange, categoryEnabled, disabled = false }: {
  value?: CommerceDraft
  onChange: (value: CommerceDraft) => void
  categoryEnabled: boolean
  disabled?: boolean
}) {
  const draft: CommerceDraft = value ?? { enabled: false, kind: "physical", currency: "USD", variants: [] }
  function updateVariant(index: number, patch: Partial<VariantDraft>) {
    onChange({ ...draft, variants: draft.variants.map((variant, i) => i === index ? { ...variant, ...patch } : variant) })
  }
  return <section className="space-y-4 rounded-lg border bg-card p-3.5">
    <div className="flex flex-wrap items-center justify-between gap-3">
      <div><Label htmlFor="content-commerce-enabled" className="text-sm font-semibold">在线销售与规格价格</Label><p className="mt-1 text-xs text-muted-foreground">商品名称与图片直接使用本条内容的标题和封面，无需另外创建售卖项。</p></div>
      <Switch id="content-commerce-enabled" disabled={disabled} checked={draft.enabled} onCheckedChange={(enabled) => onChange({ ...draft, enabled, variants: enabled && !draft.variants.length ? [newVariant()] : draft.variants })} />
    </div>
    {!categoryEnabled && <p className="text-xs text-muted-foreground">所属分类尚未开启在线销售。配置可以保存，分类开启且内容公开后才允许购买；子分类需要分别开启。</p>}
    {disabled && <p className="text-xs text-muted-foreground">销售配置需要“支付与交易管理”权限。</p>}
    {(draft.enabled || draft.variants.length > 0) && <>
      <div className="grid gap-4 sm:grid-cols-2">
        <div className="space-y-2"><Label>商品或服务类型</Label><Select disabled={disabled} value={draft.kind} onValueChange={(kind) => { if (kind) onChange({ ...draft, kind: kind as CommerceProduct["kind"] }) }}><SelectTrigger className="w-full" aria-label="商品或服务类型"><SelectValue>{kinds[draft.kind]}</SelectValue></SelectTrigger><SelectContent>{Object.entries(kinds).map(([key, label]) => <SelectItem key={key} value={key}>{label}</SelectItem>)}</SelectContent></Select></div>
        <div className="space-y-2"><Label>收款币种</Label><Select disabled={disabled} value={draft.currency} onValueChange={(currency) => { if (currency) onChange({ ...draft, currency }) }}><SelectTrigger className="w-full" aria-label="商品收款币种"><SelectValue>{draft.currency}</SelectValue></SelectTrigger><SelectContent>{currencies.map((currency) => <SelectItem key={currency} value={currency}>{currency}</SelectItem>)}</SelectContent></Select></div>
      </div>
      <p className="text-xs text-muted-foreground">每行是一种可购买的规格组合，各自设置价格。不区分规格时，保留一行并直接填写价格即可。修改币种后请重新核对各规格价格。</p>
      <div className="space-y-3">{draft.variants.map((variant, index) => <div key={variant.localID} className="space-y-3 rounded-lg border p-3">
        <div className="flex flex-wrap items-center justify-between gap-2"><p className="text-sm font-medium">规格 {index + 1}</p><div className="flex items-center gap-3"><Label htmlFor={`variant-${variant.localID}-enabled`} className="text-xs">启用</Label><Switch id={`variant-${variant.localID}-enabled`} disabled={disabled} checked={variant.enabled} onCheckedChange={(enabled) => updateVariant(index, { enabled })} /><Button type="button" variant="ghost" size="icon" aria-label={`移除规格 ${index + 1}`} disabled={disabled} onClick={() => onChange({ ...draft, variants: draft.variants.filter((_, i) => i !== index) })}><Trash2 className="size-4" /></Button></div></div>
        <div className="grid gap-3 sm:grid-cols-2"><div className="space-y-2"><Label htmlFor={`variant-${variant.localID}-sku`}>规格编号（选填）</Label><Input id={`variant-${variant.localID}-sku`} disabled={disabled} value={variant.sku} placeholder="留空自动生成" onChange={(e) => updateVariant(index, { sku: e.target.value })} /></div><div className="space-y-2"><Label htmlFor={`variant-${variant.localID}-price`}>售价（{draft.currency}）</Label><Input id={`variant-${variant.localID}-price`} disabled={disabled} inputMode="decimal" value={variant.price} placeholder={draft.currency === "JPY" ? "例如 1000" : "例如 100.00"} onChange={(e) => updateVariant(index, { price: e.target.value })} /></div></div>
        {variant.specifications.map((specification, specIndex) => <div key={specIndex} className="grid grid-cols-[1fr_1fr_auto] items-center gap-2"><Input disabled={disabled} aria-label={`规格 ${index + 1} 参数 ${specIndex + 1} 名称`} placeholder="参数名，例如口径" value={specification.name} onChange={(e) => updateVariant(index, { specifications: variant.specifications.map((spec, i) => i === specIndex ? { ...spec, name: e.target.value } : spec) })} /><Input disabled={disabled} aria-label={`规格 ${index + 1} 参数 ${specIndex + 1} 值`} placeholder="参数值，例如 DN15" value={specification.value} onChange={(e) => updateVariant(index, { specifications: variant.specifications.map((spec, i) => i === specIndex ? { ...spec, value: e.target.value } : spec) })} /><Button type="button" variant="ghost" size="icon" aria-label="移除规格参数" disabled={disabled} onClick={() => updateVariant(index, { specifications: variant.specifications.filter((_, i) => i !== specIndex) })}><Trash2 className="size-4" /></Button></div>)}
        <Button type="button" variant="outline" size="sm" disabled={disabled || variant.specifications.length >= 12} onClick={() => updateVariant(index, { specifications: [...variant.specifications, { name: "", value: "" }] })}><Plus className="size-4" />添加规格参数</Button>
      </div>)}</div>
      <Button type="button" variant="outline" disabled={disabled || draft.variants.length >= 50} onClick={() => onChange({ ...draft, variants: [...draft.variants, newVariant()] })}><Plus className="size-4" />添加另一种规格</Button>
      <p className="text-xs text-muted-foreground">每条内容最多 50 个规格，每个规格最多 12 个参数。移除规格会停止新订单购买，历史订单保留原规格及价格。</p>
    </>}
  </section>
}
