import { useEffect, useState, type FormEvent } from "react"
import { Download, Plus, RefreshCw, Save, Trash2, Upload } from "lucide-react"
import { getCommercePricing, saveCommercePricing, type CurrencyInfo, type PricingSettings } from "@/lib/api"
import { showError, showSuccess } from "./admin-notifications"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { ScrollArea } from "@/components/ui/scroll-area"

export function CurrencySettings() {
  const [draft, setDraft] = useState<PricingSettings | null>(null)
  const [supported, setSupported] = useState<CurrencyInfo[]>([])
  const [busy, setBusy] = useState(false)
  const [failed, setFailed] = useState(false)
  const [version, setVersion] = useState(0)
  useEffect(() => {
    let active = true
    getCommercePricing().then(result => { if (active) { setDraft(result.settings); setSupported(result.supported); setFailed(false) } }).catch(error => { if (active) { showError(error); setFailed(true) } })
    return () => { active = false }
  }, [version])
  if (!draft) return <div className="py-10 text-sm text-muted-foreground">{failed ? <Button variant="outline" onClick={() => setVersion(v => v + 1)}>重新加载币种设置</Button> : "正在加载币种设置…"}</div>
  const value = draft
  const change = (patch: Partial<PricingSettings>) => setDraft({ ...value, ...patch })
  function setBase(code: string) {
    if (code === value.base_currency) return
    const pivot = value.currencies.find(item => item.currency === code)
    // Changing the base changes every rate's meaning. Clear dependent rates so
    // an old table cannot silently be saved against a different base.
    change({ base_currency: code, currencies: [{ currency: code, rate: "1", enabled: true, countries: pivot?.countries ?? [] }, ...value.currencies.filter(item => item.currency !== code).map(item => ({ ...item, rate: "" }))] })
  }
  async function save(event: FormEvent) {
    event.preventDefault()
    if (busy) return
    setBusy(true)
    try { setDraft(await saveCommercePricing({ ...value, currencies: value.currencies.map(rule => ({ ...rule, countries: rule.countries.filter(Boolean) })), trusted_proxies: value.trusted_proxies.filter(Boolean) })); showSuccess("币种与汇率设置已保存，历史订单保持原成交金额。") }
    catch (error) { showError(error) }
    finally { setBusy(false) }
  }
  function exportSettings() {
    const url = URL.createObjectURL(new Blob([JSON.stringify(value, null, 2)], { type: "application/json" }))
    const link = document.createElement("a"); link.href = url; link.download = "gocms-currencies.json"; document.body.appendChild(link); link.click(); link.remove(); setTimeout(() => URL.revokeObjectURL(url), 1000)
  }
  async function importSettings(file: File) {
    if (file.size > 16384) { showError(new Error("配置文件不能超过 16 KB")); return }
    try {
      const input = JSON.parse(await file.text()) as PricingSettings
      if (!input || typeof input !== "object" || typeof input.base_currency !== "string" || typeof input.enabled !== "boolean" || !Array.isArray(input.currencies) || input.currencies.length > supported.length || !Number.isInteger(input.max_age_hours) || typeof input.country_header !== "string" || !Array.isArray(input.trusted_proxies) || !input.trusted_proxies.every(item => typeof item === "string") || !input.currencies.every(item => item && typeof item.currency === "string" && typeof item.rate === "string" && typeof item.enabled === "boolean" && Array.isArray(item.countries) && item.countries.every(country => typeof country === "string"))) throw new Error("币种配置文件格式不正确")
      setDraft({ ...input, revision: value.revision, rates_updated_at: value.rates_updated_at, refresh_rates: true })
      showSuccess("已读取币种配置，请核对汇率后保存。")
    } catch (error) { showError(error) }
  }
  return <ScrollArea className="min-h-0 flex-1" contentClassName="pe-3 pb-6"><form onSubmit={save} className="max-w-5xl space-y-6">
    <div className="flex flex-wrap items-center justify-between gap-3"><div><h2 className="text-base font-semibold">币种与汇率</h2><p className="mt-1 text-sm text-muted-foreground">按当前站点配置，基准币种可自由选择。汇率表示：1 单位基准币种 = 多少单位目标币种。</p></div><div className="flex flex-wrap gap-2"><Button type="button" variant="outline" disabled={busy} onClick={exportSettings}><Download className="size-4" />导出配置</Button><Label className="inline-flex cursor-pointer items-center gap-2 rounded-lg border px-3 text-sm"><Upload className="size-4" />导入配置<Input type="file" accept="application/json,.json" className="sr-only" disabled={busy} aria-label="导入币种配置" onChange={event => { const file = event.target.files?.[0]; event.target.value = ""; if (file) void importSettings(file) }} /></Label><Button disabled={busy} type="submit"><Save className="size-4" />{busy ? "保存中…" : "保存设置"}</Button></div></div>
    <fieldset disabled={busy} className="space-y-6">
      <div className="flex flex-wrap items-center gap-3 rounded-lg border p-4"><Switch id="pricing-enabled" checked={value.enabled} onCheckedChange={enabled => change({ enabled })} /><Label htmlFor="pricing-enabled">启用多币种报价</Label><p className="text-xs text-muted-foreground">关闭时沿用各商品原币种和售价。</p></div>
      <div className="grid gap-5 rounded-lg border p-4 sm:grid-cols-2"><div className="space-y-2"><Label htmlFor="pricing-base">基准币种</Label><Select<string> value={value.base_currency || null} onValueChange={code => { if (code) setBase(code) }}><SelectTrigger id="pricing-base" className="w-full"><SelectValue placeholder="选择基准币种" /></SelectTrigger><SelectContent>{supported.map(item => <SelectItem key={item.code} value={item.code}>{item.code}</SelectItem>)}</SelectContent></Select><p className="text-xs leading-5 text-muted-foreground">新商品默认以此币种计价。切换基准会清空其他汇率，请按新基准重新填写；已有商品和订单不会自动改价。</p></div><div className="space-y-2"><Label htmlFor="pricing-age">汇率有效期（小时）</Label><Input id="pricing-age" type="number" min={1} max={8760} required value={value.max_age_hours} onChange={event => change({ max_age_hours: Number(event.target.value) })} /><p className="text-xs leading-5 text-muted-foreground">过期后暂停自动换算，回到商品原币种；固定币种售价仍有效。</p></div></div>
      <div className="space-y-3"><h3 className="text-sm font-semibold">开放币种与汇率</h3>{value.currencies.map((rule, index) => <div key={rule.currency} className="grid items-start gap-4 rounded-lg border p-4 sm:grid-cols-[90px_minmax(0,1fr)_minmax(0,1fr)_auto]">
        <div className="space-y-2"><p className="text-sm font-medium">{rule.currency}</p><div className="flex items-center gap-2"><Switch aria-label={`启用 ${rule.currency}`} disabled={rule.currency === value.base_currency} checked={rule.enabled} onCheckedChange={enabled => change({ currencies: value.currencies.map((item, i) => i === index ? { ...item, enabled } : item) })} /><span className="text-xs text-muted-foreground">{rule.currency === value.base_currency ? "基准" : "展示"}</span></div></div>
        <div className="space-y-2"><Label htmlFor={`rate-${rule.currency}`}>1 {value.base_currency || "基准币种"} = {rule.currency}</Label><Input id={`rate-${rule.currency}`} value={rule.rate} inputMode="decimal" required readOnly={rule.currency === value.base_currency} placeholder="填写汇率" onChange={event => change({ currencies: value.currencies.map((item, i) => i === index ? { ...item, rate: event.target.value } : item) })} /></div>
        <div className="space-y-2"><Label htmlFor={`countries-${rule.currency}`}>推荐国家／地区代码</Label><Input id={`countries-${rule.currency}`} value={rule.countries.join(", ")} placeholder="例如 CN, HK" onChange={event => change({ currencies: value.currencies.map((item, i) => i === index ? { ...item, countries: event.target.value.toUpperCase().split(/[,，]/).map(item => item.trim()) } : item) })} /></div>
        <Button type="button" variant="ghost" size="icon" className="sm:mt-6" aria-label={`移除 ${rule.currency}`} disabled={rule.currency === value.base_currency} onClick={() => change({ currencies: value.currencies.filter((_, i) => i !== index) })}><Trash2 className="size-4" /></Button>
      </div>)}
      <Select<string> value={null} onValueChange={code => { if (code) change({ currencies: [...value.currencies, { currency: code, rate: code === value.base_currency ? "1" : "", enabled: true, countries: [] }] }) }} disabled={!value.base_currency || value.currencies.length >= supported.length}><SelectTrigger className="w-full sm:w-60" aria-label="添加目标币种"><Plus className="size-4" /><SelectValue placeholder="添加目标币种" /></SelectTrigger><SelectContent>{supported.filter(item => !value.currencies.some(rule => rule.currency === item.code)).map(item => <SelectItem key={item.code} value={item.code}>{item.code}</SelectItem>)}</SelectContent></Select>
      <p className="text-xs leading-6 text-muted-foreground">优先使用规格中配置的固定币种售价，否则根据汇率换算并按目标币种最小单位四舍五入。只有启用了对应币种的支付渠道，才按目标币种下单；否则仅显示参考金额并注明实际支付币种。</p></div>
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border p-4"><div><p className="text-sm">汇率更新时间：{value.rates_updated_at ? new Date(value.rates_updated_at).toLocaleString() : "尚未保存"}</p><p className="mt-1 text-xs text-muted-foreground">本版手动维护汇率；修改汇率会自动更新时间。</p></div><Button type="button" variant="outline" onClick={() => { change({ refresh_rates: true }); showSuccess("已标记确认汇率，保存设置后重新计算有效期。") }}><RefreshCw className="size-4" />确认汇率仍有效</Button></div>
      <details className="rounded-lg border p-4"><summary className="cursor-pointer text-sm font-medium">自动识别地区（可选）</summary><div className="mt-4 grid gap-5 sm:grid-cols-2"><div className="space-y-2"><Label htmlFor="country-header">可信代理提供的国家请求头</Label><Input id="country-header" value={value.country_header} placeholder="留空关闭，例如 X-Country-Code" maxLength={64} onChange={event => change({ country_header: event.target.value.trim() })} /></div><div className="space-y-2"><Label htmlFor="trusted-proxies">可信代理 CIDR（逗号分隔）</Label><Input id="trusted-proxies" value={value.trusted_proxies.join(", ")} placeholder="例如 127.0.0.1/32" onChange={event => change({ trusted_proxies: event.target.value.split(",").map(item => item.trim()) })} /></div><p className="text-xs leading-6 text-muted-foreground sm:col-span-2">只接受配置网段代理提供的两位国家代码。请确保代理覆盖该请求头，不能透传访客自行填写的值。未配置或无法识别时使用基准币种；用户手动选择始终优先。</p></div></details>
    </fieldset>
  </form></ScrollArea>
}
