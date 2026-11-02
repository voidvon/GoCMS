import { useEffect, useState } from "react"
import { LoaderCircle, RefreshCw } from "lucide-react"
import { getSiteSettings, saveSiteSettings } from "@/lib/api"
import { useLanguage } from "@/lib/language-context"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { InlineAlert } from "@/components/app/app-ui"
import { showSuccess } from "@/components/app/admin-notifications"

type LanguagePolicy = {
  mode?: "inherit" | "on" | "off"
  text?: Record<string, string>
}

type ConsentConfig = {
  enabled: boolean
  version: string
  max_age_days: number
  purposes: string[]
  text?: Record<string, string>
  languages?: Record<string, LanguagePolicy>
}

const initialConfig: ConsentConfig = {
  enabled: false, version: "1", max_age_days: 180,
  purposes: ["preferences", "analytics", "marketing"], text: {}, languages: {},
}
const purposes = [["preferences", "偏好设置"], ["analytics", "访问分析"], ["marketing", "营销推广"]] as const
const textFields = [
  ["title", "弹窗标题"], ["description", "用途说明（包含实际服务、数据和用途）"],
  ["privacy_url", "隐私政策链接"], ["cookie_url", "Cookie 政策链接"],
] as const
const extraFields = [
  ["accept", "全部接受按钮"], ["reject", "拒绝非必要项按钮"], ["customize", "自定义设置按钮"],
  ["save", "保存选择按钮"], ["settings", "隐私设置入口"], ["close", "关闭按钮"],
  ["necessary", "严格必要用途说明"], ...purposes.map(([key, label]) => [key, `${label}用途说明`]),
  ["privacy_label", "隐私政策链接文字"], ["cookie_label", "Cookie 政策链接文字"],
] as const

export function ConsentSettingsDialog({ siteId, siteName, initialLanguage = "", onClose }: {
  siteId: number
  siteName: string
  initialLanguage?: string
  onClose: () => void
}) {
  const { languages } = useLanguage()
  const [config, setConfig] = useState<ConsentConfig>(initialConfig)
  const [scope, setScope] = useState(initialLanguage || "site")
  const [loading, setLoading] = useState(true)
  const [loaded, setLoaded] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState("")
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    let active = true
    getSiteSettings(siteId).then(data => {
      const parsed = data.consent_config ? JSON.parse(data.consent_config) : {}
      if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("隐私配置格式无效，请检查 consent_config")
      if (active) {
        setConfig({ ...initialConfig, ...parsed, purposes: parsed.purposes === null ? [] : parsed.purposes ?? initialConfig.purposes })
        setLoaded(true)
      }
    }).catch(reason => {
      if (active) setError(reason instanceof Error ? reason.message : "加载隐私配置失败")
    }).finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [siteId, attempt])

  const override = config.languages?.[scope] || {}
  const values = scope === "site" ? config.text || {} : override.text || {}
  const enabled = scope === "site" ? config.enabled : override.mode === "on" || (override.mode !== "off" && config.enabled)
  const scopeName = scope === "site" ? "站点默认" : languages.find(lang => lang.code === scope)?.name || scope

  function updateOverride(update: Partial<LanguagePolicy>) {
    setConfig({ ...config, languages: { ...config.languages, [scope]: { ...override, ...update } } })
  }
  function updateText(key: string, value: string) {
    const next = { ...values, [key]: value }
    if (scope === "site") setConfig({ ...config, text: next })
    else updateOverride({ text: next })
  }
  async function save(event: React.FormEvent) {
    event.preventDefault()
    setSaving(true)
    setError("")
    try {
      await saveSiteSettings({ consent_config: JSON.stringify(config) }, siteId)
      showSuccess(`“${siteName}”隐私配置已保存，请执行全站发布`)
      onClose()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "保存隐私配置失败")
    } finally { setSaving(false) }
  }
  function renderTextField(key: string, label: string) {
    const id = `consent-${key}`
    const placeholder = scope === "site"
      ? key.endsWith("_url") ? "/privacy.html 或 https://example.com/privacy" : "留空使用内置翻译（简体中文或英语）"
      : config.text?.[key] || "留空继承站点文案或使用内置翻译"
    return (
      <div key={key} className="space-y-1.5">
        <Label htmlFor={id}>{label}</Label>
        {key === "description" || purposes.some(([p]) => p === key) ? (
          <Textarea id={id} value={values[key] || ""} onChange={e => updateText(key, e.target.value)} placeholder={placeholder} rows={3} maxLength={8000} />
        ) : (
          <Input id={id} value={values[key] || ""} onChange={e => updateText(key, e.target.value)} placeholder={placeholder} maxLength={8000} />
        )}
      </div>
    )
  }

  return (
    <Dialog open onOpenChange={open => { if (!open && !saving) onClose() }}>
      <DialogContent className="flex max-h-[85dvh] flex-col overflow-hidden sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>隐私与 Cookie 同意 · {siteName}</DialogTitle>
          <DialogDescription>按站点设置默认策略，并为不同语言配置启用状态和文案。保存后需全站发布。</DialogDescription>
        </DialogHeader>
        {error && <InlineAlert>{error}</InlineAlert>}
        {loading ? <div className="flex h-40 items-center justify-center"><LoaderCircle className="size-6 animate-spin" /></div> : !loaded ? (
          <Button variant="outline" onClick={() => { setLoading(true); setError(""); setAttempt(attempt + 1) }}><RefreshCw />重新加载</Button>
        ) : (
          <form onSubmit={save} className="flex min-h-0 flex-1 flex-col gap-4">
            <ScrollArea className="min-h-0 flex-1" contentClassName="space-y-5 p-1 pr-3">
              <div className="space-y-2">
                <Label htmlFor="consent-scope">配置范围</Label>
                <Select value={scope} onValueChange={value => { if (value) setScope(value) }}>
                  <SelectTrigger id="consent-scope" className="w-full"><SelectValue>{scopeName}</SelectValue></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="site">站点默认</SelectItem>
                    {languages.map(lang => <SelectItem key={lang.code} value={lang.code}>{lang.name} ({lang.code}){lang.is_enabled ? "" : " · 已禁用"}</SelectItem>)}
                  </SelectContent>
                </Select>
              </div>
              <section className="space-y-4 rounded-lg border p-4">
                {scope === "site" ? (
                  <div className="flex items-center justify-between gap-4">
                    <Label htmlFor="consent-enabled">默认开启同意界面</Label>
                    <Switch id="consent-enabled" checked={config.enabled} onCheckedChange={value => setConfig({ ...config, enabled: value })} />
                  </div>
                ) : (
                  <div className="space-y-2">
                    <Label htmlFor="consent-mode">该语言的启用策略</Label>
                    <Select value={override.mode || "inherit"} onValueChange={value => { if (value) updateOverride({ mode: value as LanguagePolicy["mode"] }) }}>
                      <SelectTrigger id="consent-mode" className="w-full"><SelectValue>{{ inherit: "继承站点设置", on: "开启", off: "关闭" }[override.mode || "inherit"]}</SelectValue></SelectTrigger>
                      <SelectContent>
                        <SelectItem value="inherit">继承站点设置</SelectItem><SelectItem value="on">开启</SelectItem><SelectItem value="off">关闭</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                )}
                <p className="text-xs text-muted-foreground">{scopeName}当前{enabled ? "开启" : "关闭"}同意界面。语言不代表访客所在地；无法确定受众时，可对所有语言开启。</p>
                <p className="text-xs text-muted-foreground">关闭界面不会视为接受：已标记资源仍遵守已有选择，未选择时保持停用。</p>
                {scope === "site" && <>
                  <div className="grid grid-cols-2 gap-4">
                    <div className="space-y-1.5"><Label htmlFor="consent-version">策略版本</Label><Input id="consent-version" required maxLength={100} value={config.version} onChange={e => setConfig({ ...config, version: e.target.value })} /></div>
                    <div className="space-y-1.5"><Label htmlFor="consent-age">选择有效期（天）</Label><Input id="consent-age" type="number" min={1} max={365} required value={config.max_age_days} onChange={e => setConfig({ ...config, max_age_days: Number(e.target.value) })} /></div>
                  </div>
                  <p className="text-xs text-muted-foreground">实际用途或服务发生变化时更新策略版本。修改用途或有效期也会要求重新选择。</p>
                  <fieldset className="space-y-2">
                    <legend className="mb-2 text-sm font-medium">实际使用的可选用途</legend>
                    {purposes.map(([purpose, label]) => <div key={purpose} className="flex items-center justify-between"><Label htmlFor={`purpose-${purpose}`}>{label}</Label><Switch id={`purpose-${purpose}`} checked={config.purposes.includes(purpose)} onCheckedChange={checked => setConfig({ ...config, purposes: checked ? [...config.purposes, purpose] : config.purposes.filter(p => p !== purpose) })} /></div>)}
                  </fieldset>
                </>}
              </section>
              <section className="space-y-4">
                <h3 className="text-sm font-semibold">{scopeName}文案与政策链接</h3>
                <p className="text-xs text-muted-foreground">文案以纯文本显示。其他语言请配置对应翻译，并在政策中说明具体服务、保存期限和第三方信息。</p>
                {textFields.map(([key, label]) => renderTextField(key, label))}
                <details className="rounded-lg border p-4">
                  <summary className="cursor-pointer text-sm font-medium">按钮、用途与入口文案</summary>
                  <div className="mt-4 space-y-4">{extraFields.map(([key, label]) => renderTextField(key, label))}</div>
                </details>
              </section>
              <p className="rounded-lg bg-muted p-3 text-xs text-muted-foreground">主题需将统计、广告和第三方嵌入接入用途控制；仅显示弹窗无法阻止未接入的追踪。拒绝和接受入口会同时显示，访客可通过常驻“隐私设置”撤回选择。</p>
            </ScrollArea>
            <DialogFooter>
              <Button type="button" variant="outline" disabled={saving} onClick={onClose}>取消</Button>
              <Button type="submit" disabled={saving}>{saving && <LoaderCircle className="animate-spin" />}保存配置</Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  )
}
