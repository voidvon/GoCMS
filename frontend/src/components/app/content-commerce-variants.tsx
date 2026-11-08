import { useState } from "react"
import { Banknote, Trash2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { commerceCurrencies } from "./content-commerce-model"
import type { CommerceDraft, VariantDraft } from "./content-commerce-model"

export function CommerceVariantTable({ draft, disabled, onUpdate, onRemove }: {
  draft: CommerceDraft
  disabled: boolean
  onUpdate: (index: number, patch: Partial<VariantDraft>) => void
  onRemove: (index: number) => void
}) {
  const [editingPrices, setEditingPrices] = useState<string | null>(null)
  const priceIndex = draft.variants.findIndex(v => v.localID === editingPrices)
  const priceVariant = draft.variants[priceIndex]
  const dimensionOptions = draft.dimensions.map(d => {
    const values = new Set(d.options.map(s => s.trim()).filter(Boolean))
    return { values, items: [...values].map(option => <SelectItem key={option} value={option}>{option}</SelectItem>) }
  })
  return <>
    {!draft.variants.length ? <p className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">添加规格行，或配置选项维度后补全组合，再填写各行售价。</p> : <ScrollArea orientation="both" className="max-h-[32rem] w-full rounded-lg border" contentClassName="pb-2">
      <table className="w-full caption-bottom text-sm">
        <caption className="sr-only">各规格组合的售价、编号与启用状态</caption>
        <TableHeader><TableRow><TableHead scope="col">#</TableHead>{draft.dimensions.map(d => <TableHead scope="col" key={d.key}>{d.name || "未命名维度"}{d.unit && ` (${d.unit})`}</TableHead>)}<TableHead scope="col">规格编号</TableHead><TableHead scope="col">售价 ({draft.currency})</TableHead><TableHead scope="col">固定币种售价</TableHead><TableHead scope="col">启用</TableHead><TableHead scope="col"><span className="sr-only">操作</span></TableHead></TableRow></TableHeader>
        <TableBody>{draft.variants.map((variant, index) => <TableRow key={variant.localID}>
          <TableCell className="text-xs text-muted-foreground">{index + 1}</TableCell>
          {draft.dimensions.map((d, dimensionIndex) => {
            const value = variant.specifications[dimensionIndex]?.value ?? ""
            const options = dimensionOptions[dimensionIndex]
            const invalidOption = Boolean(value && !options.values.has(value))
            const label = `第 ${index + 1} 行 ${d.name || "未命名维度"}`
            const change = (value: string) => onUpdate(index, { specifications: draft.dimensions.map((definition, i) => ({ name: definition.name, value: i === dimensionIndex ? value : variant.specifications[i]?.value ?? "" })) })
            return <TableCell key={d.key}>{d.type === "select" ? <Select disabled={disabled} value={value || null} onValueChange={selected => { if (selected !== null) change(selected) }}><SelectTrigger aria-label={label} aria-invalid={invalidOption || undefined} className="w-full min-w-32"><SelectValue placeholder="选择规格">{value || undefined}</SelectValue></SelectTrigger><SelectContent>{invalidOption && <SelectItem value={value}>{value}（不在候选值中）</SelectItem>}{options.items}</SelectContent></Select> : <Input disabled={disabled} className="min-w-32" aria-label={label} value={value} inputMode={d.type === "number" ? "decimal" : "text"} maxLength={120} placeholder={d.unit || "填写规格"} onChange={e => change(e.target.value)} />}</TableCell>
          })}
          <TableCell><Input className="min-w-40" aria-label={`第 ${index + 1} 行规格编号`} disabled={disabled} maxLength={64} value={variant.sku} placeholder="留空自动生成" onChange={e => onUpdate(index, { sku: e.target.value })} /></TableCell>
          <TableCell><Input className="min-w-32" aria-label={`第 ${index + 1} 行售价 ${draft.currency}`} disabled={disabled} inputMode="decimal" value={variant.price} placeholder={draft.currency === "JPY" ? "整数金额" : "0.00"} onChange={e => onUpdate(index, { price: e.target.value })} /></TableCell>
          <TableCell><Button type="button" size="sm" variant="ghost" disabled={disabled} onClick={() => setEditingPrices(variant.localID)} aria-label={`设置第 ${index + 1} 行固定币种售价`}><Banknote className="size-4" />{Object.entries(variant.fixedPriceInputs).filter(([code, raw]) => code !== draft.currency && raw.trim()).length || "设置"}</Button></TableCell>
          <TableCell><Switch aria-label={`启用第 ${index + 1} 行规格`} disabled={disabled} checked={variant.enabled} onCheckedChange={enabled => onUpdate(index, { enabled })} /></TableCell>
          <TableCell><Button type="button" variant="ghost" size="icon" aria-label={`移除第 ${index + 1} 行规格`} disabled={disabled} onClick={() => onRemove(index)}><Trash2 className="size-4" /></Button></TableCell>
        </TableRow>)}</TableBody>
      </table>
    </ScrollArea>}
    <Dialog open={Boolean(priceVariant)} onOpenChange={open => { if (!open) setEditingPrices(null) }}>
      <DialogContent className="sm:max-w-xl"><DialogHeader><DialogTitle>第 {priceIndex + 1} 行 · 固定币种售价</DialogTitle><DialogDescription>留空时使用站点汇率；填写后优先使用该币种固定售价。与内容一同保存。开放币种及收款能力在“支付管理 → 币种与汇率”配置。</DialogDescription></DialogHeader>
        {priceVariant && <ScrollArea className="max-h-[60dvh]" contentClassName="grid gap-4 p-1 sm:grid-cols-2">{commerceCurrencies.filter(code => code !== draft.currency).map(code => <div className="space-y-1.5" key={code}><Label htmlFor={`${priceVariant.localID}-fixed-${code}`}>{code}</Label><Input id={`${priceVariant.localID}-fixed-${code}`} disabled={disabled} inputMode="decimal" placeholder="按汇率换算" value={priceVariant.fixedPriceInputs[code] ?? ""} onChange={e => onUpdate(priceIndex, { fixedPriceInputs: { ...priceVariant.fixedPriceInputs, [code]: e.target.value } })} /></div>)}</ScrollArea>}
        <Button type="button" variant="outline" onClick={() => setEditingPrices(null)}>完成</Button>
      </DialogContent>
    </Dialog>
  </>
}
