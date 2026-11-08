import { useEffect, useState } from "react"
import { ArrowDown, ArrowUp, ExternalLink, Plus, RefreshCw, Trash2, WandSparkles } from "lucide-react"
import { useSite } from "@/lib/site-context"
import { getCommerceCurrencyInfo, getCommerceDimensions } from "@/lib/api"
import type { CommerceDimension, CommerceDimensionRecord, CommerceProduct } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { pathForView } from "@/lib/admin-router"
import { ConfirmDialog } from "./app-ui"
import { showError, showSuccess } from "./admin-notifications"
import { CommerceVariantTable } from "./content-commerce-variants"
import { appendCombinations, combinationKey, commerceCurrencies, maxProductDimensions, maxProductVariants, newVariant } from "./content-commerce-model"
import type { CommerceDraft, VariantDraft } from "./content-commerce-model"

export { commerceDraft, commercePayload } from "./content-commerce-model"
export type { CommerceDraft } from "./content-commerce-model"

const kinds = { physical: "实物商品", digital: "数字商品", service: "服务" }
const dimensionTypes = { select: "选项", text: "文本", number: "数字" }

export function ContentCommerceEditor({ value, onChange, categoryEnabled, disabled = false }: {
  value?: CommerceDraft
  onChange: (value: CommerceDraft) => void
  categoryEnabled: boolean
  disabled?: boolean
}) {
  const { activeSiteId } = useSite()
  const [baseCurrency, setBaseCurrency] = useState("")
  const [library, setLibrary] = useState<CommerceDimensionRecord[]>([])
  const [dimensionsLoading, setDimensionsLoading] = useState(true)
  const [dimensionsFailed, setDimensionsFailed] = useState(false)
  const [dimensionsReload, setDimensionsReload] = useState(0)
  const [removing, setRemoving] = useState<{ kind: "dimension" | "variant"; index: number } | null>(null)
  useEffect(() => {
    let active = true
    setBaseCurrency("")
    getCommerceCurrencyInfo().then(result => { if (active) setBaseCurrency(result.base_currency) }).catch(error => { if (active) showError(error) })
    return () => { active = false }
  }, [activeSiteId])
  useEffect(() => {
    let active = true
    setDimensionsLoading(true)
    setLibrary([])
    getCommerceDimensions().then(result => {
      if (active) { setLibrary(result.items); setDimensionsFailed(false) }
    }).catch(error => { if (active) { setDimensionsFailed(true); showError(error) } })
      .finally(() => { if (active) setDimensionsLoading(false) })
    return () => { active = false }
  }, [activeSiteId, dimensionsReload])
  const source: CommerceDraft = value ?? { enabled: false, kind: "physical", currency: baseCurrency, dimensions: [], variants: [] }
  const draft: CommerceDraft = { ...source, dimensions: source.dimensions.map(d => {
    const current = library.find(item => item.key === d.key)
    return current && current.name === d.name && current.type === d.type && current.unit === d.unit ? { ...d, options: current.options } : d
  }) }
  const available = library.filter(d => !draft.dimensions.some(selected => selected.key === d.key || selected.name.trim().toLowerCase() === d.name.trim().toLowerCase()))
  function updateVariant(index: number, patch: Partial<VariantDraft>) {
    onChange({ ...draft, variants: draft.variants.map((variant, i) => i === index ? { ...variant, ...patch } : variant) })
  }
  function moveDimension(index: number, offset: number) {
    const order = draft.dimensions.map((_, i) => i)
    const target = order[index + offset]
    order[index + offset] = order[index]
    order[index] = target
    onChange({ ...draft, dimensions: order.map(i => draft.dimensions[i]), variants: draft.variants.map(v => ({ ...v, specifications: order.map(i => v.specifications[i]) })) })
  }
  function remove() {
    if (disabled || !removing) return
    if (removing.kind === "variant") {
      onChange({ ...draft, variants: draft.variants.filter((_, i) => i !== removing.index) })
    } else {
      const variants = draft.variants.map(v => ({ ...v, specifications: v.specifications.filter((_, i) => i !== removing.index) }))
      if (new Set(variants.map(v => combinationKey(v.specifications))).size !== variants.length) {
        showError("移除此维度后会出现重复组合，请先保留需要的规格行，再移除维度")
        setRemoving(null)
        return
      }
      onChange({ ...draft, dimensions: draft.dimensions.filter((_, i) => i !== removing.index), variants })
    }
    setRemoving(null)
  }
  function generate() {
    try {
      const variants = appendCombinations(draft)
      const added = variants.filter(v => !draft.variants.some(old => old.localID === v.localID)).length
      onChange({ ...draft, variants })
      showSuccess(added ? `已补充 ${added} 个组合，请填写各行售价后保存内容` : "所有组合已存在，原有价格已保留")
    } catch (error) { showError(error) }
  }
  return <section className="min-w-0 space-y-4 rounded-lg border bg-card p-3.5">
    <div className="flex flex-wrap items-center justify-between gap-3">
      <div><Label htmlFor="content-commerce-enabled" className="text-sm font-semibold">在线销售与规格价格</Label><p className="mt-1 text-xs text-muted-foreground">商品名称与图片使用本条内容的标题和封面，选择已有规格维度，并填写各组合价格。</p></div>
      <Switch id="content-commerce-enabled" disabled={disabled} checked={draft.enabled} onCheckedChange={enabled => onChange({ ...draft, enabled, variants: enabled && !draft.variants.length ? [newVariant(draft.dimensions)] : draft.variants })} />
    </div>
    {!categoryEnabled && <p className="text-xs text-muted-foreground">所属分类尚未开启在线销售。配置可以保存，分类开启且内容公开后才允许购买；子分类需要分别开启。</p>}
    {disabled && <p className="text-xs text-muted-foreground">销售配置需要“支付与交易管理”权限。</p>}
    {(draft.enabled || draft.variants.length > 0 || draft.dimensions.length > 0) && <>
      <div className="grid gap-4 sm:grid-cols-2">
        <div className="space-y-2"><Label>商品或服务类型</Label><Select disabled={disabled} value={draft.kind} onValueChange={kind => { if (kind) onChange({ ...draft, kind: kind as CommerceProduct["kind"] }) }}><SelectTrigger className="w-full" aria-label="商品或服务类型"><SelectValue>{kinds[draft.kind]}</SelectValue></SelectTrigger><SelectContent>{Object.entries(kinds).map(([key, label]) => <SelectItem key={key} value={key}>{label}</SelectItem>)}</SelectContent></Select></div>
        <div className="space-y-2"><Label>原始计价币种</Label><Select disabled={disabled} value={draft.currency} onValueChange={currency => { if (currency) onChange({ ...draft, currency }) }}><SelectTrigger className="w-full" aria-label="商品原始计价币种"><SelectValue placeholder="选择计价币种">{draft.currency || undefined}</SelectValue></SelectTrigger><SelectContent>{commerceCurrencies.map(currency => <SelectItem key={currency} value={currency}>{currency}</SelectItem>)}</SelectContent></Select></div>
      </div>
      <div className="space-y-3 border-t pt-4">
        <div className="flex flex-wrap items-center justify-between gap-3"><h3 className="text-sm font-semibold">选用规格维度</h3><a href={pathForView("commerce-dimensions")} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 text-xs text-muted-foreground underline underline-offset-4">维度管理<ExternalLink className="size-3" /><span className="sr-only">（新窗口打开）</span></a></div>
        <div className="flex flex-wrap items-center gap-2">
          {draft.dimensions.map((d, index) => <div key={d.key} className="flex items-center gap-1 rounded-lg border px-2 py-1"><span className="px-1 text-sm">{d.name}{d.unit && ` (${d.unit})`}</span><span className="text-xs text-muted-foreground">{dimensionTypes[d.type]}</span><Button type="button" variant="ghost" size="icon-sm" disabled={disabled || index === 0} aria-label={`上移维度 ${d.name}`} onClick={() => moveDimension(index, -1)}><ArrowUp className="size-3" /></Button><Button type="button" variant="ghost" size="icon-sm" disabled={disabled || index === draft.dimensions.length - 1} aria-label={`下移维度 ${d.name}`} onClick={() => moveDimension(index, 1)}><ArrowDown className="size-3" /></Button><Button type="button" variant="ghost" size="icon-sm" disabled={disabled} aria-label={`取消选用维度 ${d.name}`} onClick={() => setRemoving({ kind: "dimension", index })}><Trash2 className="size-3" /></Button></div>)}
          <Select<string> value={null} disabled={disabled || dimensionsLoading || dimensionsFailed || !available.length || draft.dimensions.length >= maxProductDimensions} onValueChange={key => {
            const selected = available.find(d => d.key === key)
            if (!selected) return
            const { name, type, unit, options } = selected
            const dimension: CommerceDimension = { key: selected.key, name, type, unit, options: [...options] }
            onChange({ ...draft, dimensions: [...draft.dimensions, dimension], variants: draft.variants.map(v => ({ ...v, specifications: [...v.specifications, { name, value: "" }] })) })
          }}><SelectTrigger className="w-full sm:w-60" aria-label="选择已有规格维度"><Plus className="size-4" /><SelectValue placeholder={dimensionsLoading ? "正在加载维度…" : "选择已有规格维度"} /></SelectTrigger><SelectContent>{available.map(d => <SelectItem key={d.key} value={d.key}>{d.name}{d.unit && ` (${d.unit})`} · {dimensionTypes[d.type]}</SelectItem>)}</SelectContent></Select>
          <Button type="button" size="icon-sm" variant="ghost" disabled={dimensionsLoading || disabled} aria-label="刷新可选规格维度" onClick={() => setDimensionsReload(v => v + 1)}><RefreshCw className="size-4" /></Button>
        </div>
        <p className="text-xs text-muted-foreground">名称、类型、单位及候选值在“规格维度”中统一维护。在新窗口新增后，点击刷新即可选用；无规格产品可不选择维度。</p>
      </div>
      <div className="space-y-3 border-t pt-4">
        <div className="flex flex-wrap items-start justify-between gap-3"><div><h3 className="text-sm font-semibold">规格与价格 <span className="font-normal text-muted-foreground">({draft.variants.length}/{maxProductVariants})</span></h3><p className="mt-1 text-xs text-muted-foreground">每行是一个可购买组合，独立设置售价与编号。无规格维度时保留一行标准规格。</p></div><div className="flex flex-wrap gap-2"><Button type="button" variant="outline" size="sm" disabled={disabled || !draft.dimensions.length} onClick={generate}><WandSparkles className="size-4" />补全选项组合</Button><Button type="button" variant="outline" size="sm" disabled={disabled || draft.variants.length >= maxProductVariants || (!draft.dimensions.length && draft.variants.length > 0)} onClick={() => onChange({ ...draft, variants: [...draft.variants, newVariant(draft.dimensions)] })}><Plus className="size-4" />添加规格行</Button></div></div>
        <CommerceVariantTable draft={draft} disabled={disabled} onUpdate={updateVariant} onRemove={index => setRemoving({ kind: "variant", index })} />
        <p className="text-xs leading-5 text-muted-foreground">最多 {maxProductDimensions} 个维度、{maxProductVariants} 个组合。补全组合仅新增缺失行，保留已有价格与编号，新行需单独填写售价；不售卖的组合可移除。修改币种后请重新核对价格。移除规格在保存后停止新购买，历史订单保留原规格与价格。</p>
      </div>
    </>}
    <ConfirmDialog open={Boolean(removing)} onOpenChange={open => { if (!open) setRemoving(null) }} title={removing?.kind === "dimension" ? "移除规格维度" : "移除规格行"} description={removing?.kind === "dimension" ? "移除此维度会同时移除各行对应的参数，其他参数与价格保留。如果产生重复组合，需要先整理规格行。保存后生效，历史订单保留原始快照。" : "此规格将在保存内容后停止新订单购买，历史订单保留原规格和价格。"} confirmLabel="确认移除" pending={disabled} onConfirm={remove} />
  </section>
}
