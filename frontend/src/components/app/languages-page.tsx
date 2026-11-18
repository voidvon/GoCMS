import { useDialogState } from "@/lib/use-dialog-state"
import { useEffect, useRef, useState } from "react"
import { Combobox } from "@base-ui/react/combobox"
import {
  Check,
  ChevronDown,
  Contact,
  LoaderCircle,
  Monitor,
  Pencil,
  Plus,
  RefreshCw,
  Star,
  ShieldCheck,
  Trash2,
  X,
} from "lucide-react"

import {
  createLanguage,
  deleteLanguage,
  getContactCards,
  updateLanguage,
  type ContactCard,
  type Language,
  type LanguageInput,
} from "@/lib/api"
import { useLanguage } from "@/lib/language-context"
import { useSite } from "@/lib/site-context"
import { ConsentSettingsDialog } from "@/components/app/consent-settings-dialog"
import { ContactCardsDialog } from "@/components/app/contact-cards-dialog"
import { showError, showSuccess } from "@/components/app/admin-notifications"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ScrollArea } from "@/components/ui/scroll-area"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { ConfirmDialog, InlineAlert } from "@/components/app/app-ui"

function LanguageContactPicker({
  siteId, selected, inherited, fallbackName, onChange,
}: {
  siteId: number
  selected: string[]
  inherited: string[]
  fallbackName?: string
  onChange: (ids: string[]) => void
}) {
  const [cards, setCards] = useState<ContactCard[] | null>(null)
  const [failed, setFailed] = useState(false)
  const [attempt, setAttempt] = useState(0)
  const anchor = useRef<HTMLDivElement>(null)
  useEffect(() => {
    let active = true
    getContactCards(siteId).then(data => {
      if (active) setCards(data.cards)
    }).catch(error => {
      if (!active) return
      setFailed(true)
      showError(error, "加载联系方式失败")
    })
    return () => { active = false }
  }, [siteId, attempt])

  const items = cards?.map(card => ({ value: card.id, label: card.name })) ?? []
  const byID = new Map(items.map(item => [item.value, item]))
  const selectedItems = selected.map(id => byID.get(id) ?? { value: id, label: "卡片已移除" })
  const inheritedNames = inherited.flatMap(id => byID.get(id)?.label ?? [])
  const inheritedText = inheritedNames.length ? `继承${fallbackName}：${inheritedNames.join("、")}` : `留空继承${fallbackName}`
  const placeholder = !cards ? "加载联系方式…" : !cards.length && !selected.length ? "请先添加联系卡片" :
    selected.length ? "" : fallbackName ? inheritedText : "选择联系卡片"
  return (
    <div className="grid grid-cols-[auto_minmax(0,1fr)] items-center gap-3 border-t pt-3">
      <Label htmlFor="language-contact-cards" className="whitespace-nowrap">联系方式</Label>
      {failed ? (
        <Button id="language-contact-cards" type="button" variant="outline" className="justify-self-start" onClick={() => { setFailed(false); setAttempt(attempt + 1) }}>重新加载</Button>
      ) : (
        <Combobox.Root
          multiple
          items={items}
          value={selectedItems}
          onValueChange={selection => onChange(selection.map(item => item.value))}
          isItemEqualToValue={(item, value) => item.value === value.value}
          disabled={!cards || (!cards.length && !selected.length)}
        >
          <Combobox.Chips
            ref={anchor}
            data-slot="contact-chips"
            className="flex min-h-8 flex-wrap items-center gap-1 rounded-lg border border-input py-1 ps-2 pe-1 transition-colors focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50 dark:bg-input/30"
          >
            {selectedItems.map(item => (
              <Combobox.Chip
                key={item.value}
                data-slot="contact-chip"
                className="flex min-w-0 max-w-full items-center gap-1 rounded-md bg-secondary ps-1.5 pe-0.5 text-xs text-secondary-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                <span className="truncate">{item.label}</span>
                <Combobox.ChipRemove
                  aria-label={`移除${item.label}`}
                  className="grid size-5 shrink-0 place-items-center rounded-sm outline-none hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring"
                >
                  <X className="size-3" />
                </Combobox.ChipRemove>
              </Combobox.Chip>
            ))}
            <Combobox.Input
              id="language-contact-cards"
              placeholder={placeholder}
              title={!selected.length && fallbackName ? inheritedText : undefined}
              className="min-w-8 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground disabled:cursor-not-allowed"
            />
            <Combobox.Trigger aria-label="切换联系卡片选项" className="grid size-6 shrink-0 place-items-center rounded-sm text-muted-foreground outline-none hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50">
              <ChevronDown className="size-4" />
            </Combobox.Trigger>
          </Combobox.Chips>
          <Combobox.Portal>
            <Combobox.Positioner anchor={anchor} sideOffset={4} className="isolate z-50 w-(--anchor-width)">
              <Combobox.Popup className="rounded-lg bg-popover p-1 text-popover-foreground shadow-md ring-1 ring-foreground/10">
                <Combobox.Empty className="px-2 py-1.5 text-sm text-muted-foreground empty:hidden">没有匹配的卡片</Combobox.Empty>
                <ScrollArea className="max-h-[min(14rem,var(--available-height))]" contentClassName="pr-2">
                  <Combobox.List>
                    {(item: (typeof items)[number]) => (
                      <Combobox.Item
                        key={item.value}
                        value={item}
                        className="flex cursor-default items-center justify-between gap-2 rounded-md px-2 py-1.5 text-sm outline-none data-highlighted:bg-accent data-highlighted:text-accent-foreground"
                      >
                        <span className="truncate">{item.label}</span>
                        <Combobox.ItemIndicator><Check className="size-4" /></Combobox.ItemIndicator>
                      </Combobox.Item>
                    )}
                  </Combobox.List>
                </ScrollArea>
              </Combobox.Popup>
            </Combobox.Positioner>
          </Combobox.Portal>
        </Combobox.Root>
      )}
    </div>
  )
}

export function LanguagesPage() {
  const { activeSiteId, activeSite } = useSite()
  const [privacyLanguage, setPrivacyLanguage] = useState<string | null>(null)
  const [contactsOpen, setContactsOpen] = useState(false)
  const {
    languages,
    refreshLanguages,
    loading,
    activeLang,
    setActiveLang,
    resetActiveLang,
    adminLang,
    currentLanguage,
  } = useLanguage()
  const [error, setError] = useState("")
  const [successMsg, setSuccessMsg] = useState("")

  // Add / Edit Dialog state
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingLang, setEditingLang] = useState<Language | null>(null)
  const [formData, setFormData] = useState<LanguageInput>({
    code: "",
    name: "",
    path_prefix: "",
    sort_order: 10,
    is_enabled: 1,
    is_default: 0,
    is_fallback: 0,
    is_admin_default: 0,
    site_mode: "subdir",
    domain: "",
    output_dir: "",
  })
  const [submitting, setSubmitting] = useState(false)

  // Delete dialog state
  const [deletingId, setDeletingId, deletingIdDialog] = useDialogState<number>()

  function openCreate() {
    setEditingLang(null)
    setFormData({
      code: "",
      name: "",
      path_prefix: "",
      sort_order: 10,
      is_enabled: 1,
      is_default: 0,
      is_fallback: 0,
      is_admin_default: 0,
      site_mode: "subdir",
      domain: "",
      output_dir: "",
    })
    setError("")
    setDialogOpen(true)
  }

  function openEdit(lang: Language) {
    setEditingLang(lang)
    setFormData({
      code: lang.code,
      name: lang.name,
      path_prefix: lang.path_prefix,
      sort_order: lang.sort_order,
      is_enabled: lang.is_enabled,
      is_default: lang.is_default,
      is_fallback: lang.is_fallback,
      is_admin_default: lang.is_admin_default ?? 0,
      site_mode: lang.site_mode || "subdir",
      domain: lang.domain || "",
      output_dir: lang.output_dir || "",
    })
    setError("")
    setDialogOpen(true)
  }

  async function handleSave() {
    if (!formData.code.trim() || !formData.name.trim()) {
      setError("语言代码和语言名称为必填项")
      return
    }

    setSubmitting(true)
    setError("")
    try {
      if (editingLang) {
        await updateLanguage(editingLang.id, formData)
        showSuccess(formData.contact_card_ids ? "语言及联系方式已保存，静态发布后生效" : "语言更新成功")
      } else {
        await createLanguage(formData)
        setSuccessMsg("语言添加成功")
      }
      setDialogOpen(false)
      await refreshLanguages()
    } catch (err: any) {
      setError(err?.message || "保存失败，请检查是否语言代码已存在")
    } finally {
      setSubmitting(false)
    }
  }

  async function handleSetDefault(lang: Language) {
    try {
      await updateLanguage(lang.id, {
        ...lang,
        contact_card_ids: undefined,
        is_default: 1,
        is_enabled: 1,
        path_prefix: "",
      })
      setSuccessMsg(`已将 ${lang.name} 设为主站语言`)
      await refreshLanguages()
    } catch (err: any) {
      setError(err?.message || "设置主站失败")
    }
  }

  async function handleSetFallback(lang: Language) {
    try {
      await updateLanguage(lang.id, {
        ...lang,
        contact_card_ids: undefined,
        is_fallback: 1,
      })
      setSuccessMsg(`已将 ${lang.name} 设为兜底语言`)
      await refreshLanguages()
    } catch (err: any) {
      setError(err?.message || "设置兜底语言失败")
    }
  }

  async function handleSetAdminDefault(lang: Language) {
    try {
      await updateLanguage(lang.id, {
        ...lang,
        contact_card_ids: undefined,
        is_admin_default: 1,
        is_enabled: 1,
      })
      setSuccessMsg(`已将 ${lang.name} 设为后台显示语言`)
      await refreshLanguages()
    } catch (err: any) {
      setError(err?.message || "设置后台显示语言失败")
    }
  }

  async function handleDelete() {
    if (!deletingId) return
    try {
      await deleteLanguage(deletingId)
      setSuccessMsg("语言已删除")
      setDeletingId(null)
      await refreshLanguages()
    } catch (err: any) {
      setError(err?.message || "删除失败")
    }
  }

  const enabledLanguages = languages.filter((lang) => lang.is_enabled === 1)
  const adminDefaultName = languages.find((lang) => lang.code === adminLang)?.name || adminLang
  const otherFallback = enabledLanguages.find(lang => lang.id !== editingLang?.id && lang.is_fallback === 1)
  const fallbackLanguage = formData.is_fallback && formData.is_enabled
    ? editingLang
    : otherFallback ?? (formData.is_default
      ? editingLang
      : enabledLanguages.find(lang => lang.id !== editingLang?.id && lang.is_default === 1))
  const inheritedContacts = fallbackLanguage?.id === editingLang?.id ? undefined : fallbackLanguage

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Button size="sm" onClick={openCreate}>
          <Plus className="size-4 mr-2" />
          新增语言
        </Button>
        <Button variant="outline" size="sm" onClick={() => setPrivacyLanguage("")}>
          <ShieldCheck className="size-4 mr-2" />隐私与 Cookie
        </Button>
        <Button variant="outline" size="sm" onClick={() => setContactsOpen(true)}>
          <Contact className="size-4 mr-2" />联系方式
        </Button>
        <Button variant="outline" size="sm" onClick={() => refreshLanguages()} disabled={loading}>
          <RefreshCw className={`size-4 mr-2 ${loading ? "animate-spin" : ""}`} />
          刷新
        </Button>
        {/* The backend display language is switched here and nowhere else. */}
        {enabledLanguages.length > 1 && (
          <div className="ml-auto flex items-center gap-2">
            <span className="text-xs text-muted-foreground">后台显示语言</span>
            <Select
              value={activeLang}
              onValueChange={(val) => {
                if (val) setActiveLang(val)
              }}
            >
              <SelectTrigger size="sm" className="w-44" aria-label="后台显示语言">
                <Monitor className="size-3.5 shrink-0 text-muted-foreground" />
                <SelectValue>{currentLanguage?.name || activeLang}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                {enabledLanguages.map((lang) => (
                  <SelectItem key={lang.code} value={lang.code}>
                    <div className="flex items-center gap-1.5">
                      <span>{lang.name}</span>
                      {lang.is_admin_default === 1 && (
                        <span className="text-[10px] text-muted-foreground">(后台默认)</span>
                      )}
                    </div>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {activeLang !== adminLang && (
              <Button
                variant="ghost"
                size="sm"
                className="h-7 text-xs text-muted-foreground"
                title={`恢复为后台默认语言：${adminDefaultName}`}
                onClick={() => resetActiveLang()}
              >
                跟随后台默认
              </Button>
            )}
          </div>
        )}
      </div>

      {error && <InlineAlert>{error}</InlineAlert>}
      {successMsg && <p role="status" className="text-sm text-green-600 dark:text-green-400">{successMsg}</p>}

      <Table containerClassName="rounded-md border">
            <TableHeader>
              <TableRow>
                <TableHead className="w-[140px]">语言代码</TableHead>
                <TableHead>语言名称</TableHead>
                <TableHead>路径前缀</TableHead>
                <TableHead>语言角色</TableHead>
                <TableHead className="w-[100px]">状态</TableHead>
                <TableHead className="w-[80px]">排序</TableHead>
                <TableHead className="w-[300px] text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {languages.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={7} className="h-32 text-center text-muted-foreground">
                    {loading ? "加载语言中..." : "暂无多语言配置"}
                  </TableCell>
                </TableRow>
              ) : (
                languages.map((lang) => (
                  <TableRow key={lang.id}>
                    <TableCell className="font-mono text-xs font-semibold">
                      {lang.code}
                    </TableCell>
                    <TableCell className="font-medium">
                      {lang.name}
                    </TableCell>
                    <TableCell className="text-xs">
                      {lang.is_default === 1 ? (
                        <span className="font-mono text-muted-foreground">/</span>
                      ) : lang.site_mode === "standalone" ? (
                        <div className="flex flex-col gap-0.5">
                          <div className="flex items-center gap-1">
                            <Badge variant="outline" className="text-[10px] h-4 px-1 text-purple-600 border-purple-500/30 bg-purple-500/10">
                              独立分站
                            </Badge>
                            <span className="font-mono font-medium text-purple-700 dark:text-purple-300">
                              {lang.domain || "(未绑定域名)"}
                            </span>
                          </div>
                          <span className="font-mono text-[11px] text-muted-foreground">
                            目录: {lang.output_dir || `<site_id>_${lang.code.toLowerCase()}`}
                          </span>
                        </div>
                      ) : (
                        <span className="font-mono text-muted-foreground">
                          {`/${lang.path_prefix || lang.code.toLowerCase()}/`}
                        </span>
                      )}
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1.5 flex-wrap">
                        {lang.is_default === 1 ? (
                          <Badge variant="default" className="gap-1 bg-blue-600 hover:bg-blue-600">
                            <Star className="size-3 fill-current" />
                            主站语言
                          </Badge>
                        ) : null}
                        {lang.is_admin_default === 1 ? (
                          <Badge variant="outline" className="gap-1 text-sky-600 border-sky-500/30 bg-sky-500/10">
                            <Monitor className="size-3" />
                            后台显示
                          </Badge>
                        ) : null}
                        {lang.is_fallback === 1 ? (
                          <Badge variant="outline" className="gap-1 text-amber-600 border-amber-500/30 bg-amber-500/10">
                            <ShieldCheck className="size-3" />
                            兜底语言
                          </Badge>
                        ) : null}
                        {lang.is_default !== 1 && lang.is_fallback !== 1 && lang.is_admin_default !== 1 && (
                          <span className="text-xs text-muted-foreground">副语言</span>
                        )}
                      </div>
                    </TableCell>
                    <TableCell>
                      {lang.is_enabled === 1 ? (
                        <Badge variant="outline" className="text-emerald-600 border-emerald-500/30 bg-emerald-500/10">
                          已启用
                        </Badge>
                      ) : (
                        <Badge variant="secondary">已禁用</Badge>
                      )}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {lang.sort_order}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-1">
                        <Button variant="ghost" size="icon" className="size-8" onClick={() => setPrivacyLanguage(lang.code)} title="该语言的隐私与 Cookie 同意" aria-label={`${lang.name}隐私与 Cookie 同意`}>
                          <ShieldCheck className="size-3.5" />
                        </Button>
                        {lang.is_default !== 1 && (
                          <Button
                            variant="ghost"
                            size="sm"
                            className="h-8 text-xs text-blue-600 hover:text-blue-700"
                            onClick={() => handleSetDefault(lang)}
                            title="设为站点主站语言（生成在网站根目录 /）"
                          >
                            设为主站
                          </Button>
                        )}
                        {lang.is_admin_default !== 1 && (
                          <Button
                            variant="ghost"
                            size="sm"
                            className="h-8 text-xs text-sky-600 hover:text-sky-700"
                            onClick={() => handleSetAdminDefault(lang)}
                            title="设为后台显示语言（分类与内容列表默认按该语言显示）"
                          >
                            设为后台
                          </Button>
                        )}
                        {lang.is_fallback !== 1 && (
                          <Button
                            variant="ghost"
                            size="sm"
                            className="h-8 text-xs text-amber-600 hover:text-amber-700"
                            onClick={() => handleSetFallback(lang)}
                            title="设为兜底语言"
                          >
                            设为兜底
                          </Button>
                        )}
                        <Button
                          variant="ghost"
                          size="icon"
                          className="size-8"
                          onClick={() => openEdit(lang)}
                          title="编辑语言"
                        >
                          <Pencil className="size-3.5" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="size-8 text-destructive hover:text-destructive"
                          disabled={lang.is_default === 1 || lang.is_admin_default === 1}
                          onClick={() => setDeletingId(lang.id)}
                          title={
                            lang.is_default === 1
                              ? "主站语言不可删除"
                              : lang.is_admin_default === 1
                                ? "后台显示语言不可删除"
                                : "删除语言"
                          }
                        >
                          <Trash2 className="size-3.5" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>

      {/* Add / Edit Dialog */}
      {privacyLanguage !== null && <ConsentSettingsDialog key={`${activeSiteId}:${privacyLanguage}`} siteId={activeSiteId} siteName={activeSite?.name || "当前站点"} initialLanguage={privacyLanguage} onClose={() => setPrivacyLanguage(null)} />}
      {contactsOpen && <ContactCardsDialog key={activeSiteId} siteId={activeSiteId} onClose={() => setContactsOpen(false)} />}
      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="flex max-h-[90dvh] flex-col sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{editingLang ? "编辑语言" : "新增语言"}</DialogTitle>
            <DialogDescription>
              配置语言信息。主站语言在根目录展示，其他语言在配置的路径前缀下展示；后台显示语言决定分类与内容列表默认展示哪种语言。
            </DialogDescription>
          </DialogHeader>
          <ScrollArea className="min-h-0" contentClassName="space-y-4 py-2 pr-3">
            <div className="space-y-2">
              <Label htmlFor="lang-code">语言代码 <span className="text-destructive">*</span></Label>
              <Input
                id="lang-code"
                placeholder="例如: en, ja, fr, zh-TW"
                value={formData.code}
                onChange={(e) => setFormData({ ...formData, code: e.target.value })}
                disabled={editingLang?.is_default === 1}
              />
              <p className="text-xs text-muted-foreground">符合 BCP 47 规范的语言代码，如 en, ja, zh-TW 等。</p>
            </div>

            <div className="space-y-2">
              <Label htmlFor="lang-name">语言名称 <span className="text-destructive">*</span></Label>
              <Input
                id="lang-name"
                placeholder="例如: English, 日本語, 繁體中文"
                value={formData.name}
                onChange={(e) => setFormData({ ...formData, name: e.target.value })}
              />
            </div>

            {formData.is_default !== 1 && (
              <div className="space-y-2 border-t pt-3">
                <Label>站点模式</Label>
                <div className="grid grid-cols-2 gap-2">
                  <Button
                    type="button"
                    variant={formData.site_mode === "standalone" ? "outline" : "default"}
                    size="sm"
                    className="h-9 justify-start text-xs"
                    onClick={() => setFormData({ ...formData, site_mode: "subdir" })}
                  >
                    子目录模式 (Subdir)
                  </Button>
                  <Button
                    type="button"
                    variant={formData.site_mode === "standalone" ? "default" : "outline"}
                    size="sm"
                    className="h-9 justify-start text-xs"
                    onClick={() => setFormData({ ...formData, site_mode: "standalone" })}
                  >
                    独立域名模式 (Standalone)
                  </Button>
                </div>
                <p className="text-xs text-muted-foreground">
                  {formData.site_mode === "standalone"
                    ? "独立域名模式：生成在与主站平级的独立物理目录，绑定独立域名，内页以根路径 / 渲染。"
                    : "子目录模式：生成在主站的子目录下（如 /en/）。"}
                </p>
              </div>
            )}

            {formData.is_default !== 1 && formData.site_mode === "standalone" ? (
              <>
                <div className="space-y-2">
                  <Label htmlFor="lang-domain">独立域名 (Domain)</Label>
                  <Input
                    id="lang-domain"
                    placeholder="例如: ru.spiraxsteam.com 或 bray.ru"
                    value={formData.domain || ""}
                    onChange={(e) => setFormData({ ...formData, domain: e.target.value })}
                  />
                  <p className="text-xs text-muted-foreground">
                    该独立语言分站绑定的专属域名（支持多级子域名或独立顶级域名）。
                  </p>
                </div>

                <div className="space-y-2">
                  <Label htmlFor="lang-output-dir">独立输出目录 (物理文件夹)</Label>
                  <Input
                    id="lang-output-dir"
                    placeholder={`留空默认: <site_id>_${formData.code.toLowerCase() || "lang"}`}
                    value={formData.output_dir || ""}
                    onChange={(e) => setFormData({ ...formData, output_dir: e.target.value })}
                  />
                  <p className="text-xs text-muted-foreground">
                    平级物理输出目录名，绝不嵌套在主站目录下。留空将自动根据站点与语言代码命名。
                  </p>
                </div>
              </>
            ) : (
              <div className="space-y-2">
                <Label htmlFor="path-prefix">路径前缀 (静态生成目录)</Label>
                <Input
                  id="path-prefix"
                  placeholder={formData.is_default === 1 ? "主站无需前缀，直接使用根路径 /" : "例如: en"}
                  value={formData.path_prefix}
                  onChange={(e) => setFormData({ ...formData, path_prefix: e.target.value })}
                  disabled={formData.is_default === 1}
                />
                <p className="text-xs text-muted-foreground">
                  {formData.is_default === 1
                    ? "主站语言默认生成在根目录(/)，无需前缀。"
                    : "副语言生成目录，例如输入 en，前台访问路径为 /en/。"}
                </p>
              </div>
            )}

            <div className="space-y-2">
              <Label htmlFor="sort-order">排序序号</Label>
              <Input
                id="sort-order"
                type="number"
                value={formData.sort_order}
                onChange={(e) => setFormData({ ...formData, sort_order: parseInt(e.target.value) || 0 })}
              />
            </div>

            {dialogOpen && editingLang && (
              <LanguageContactPicker
                key={`${activeSiteId}:${editingLang.id}`}
                siteId={activeSiteId}
                selected={formData.contact_card_ids ?? editingLang.contact_card_ids ?? []}
                inherited={inheritedContacts?.contact_card_ids ?? []}
                fallbackName={inheritedContacts?.name}
                onChange={ids => setFormData(current => ({ ...current, contact_card_ids: ids }))}
              />
            )}

            <div className="flex items-center justify-between border-t pt-3">
              <div>
                <Label htmlFor="is-enabled" className="text-sm font-medium">启用此语言</Label>
                <p className="text-xs text-muted-foreground">未启用的语言不会在前台生成或公开访问</p>
              </div>
              <Switch
                id="is-enabled"
                checked={formData.is_enabled === 1}
                onCheckedChange={(checked) => setFormData({ ...formData, is_enabled: checked ? 1 : 0 })}
                disabled={formData.is_default === 1 || formData.is_admin_default === 1}
              />
            </div>

            <div className="flex items-center justify-between border-t pt-3">
              <div>
                <Label htmlFor="is-default" className="text-sm font-medium">设为站点主站语言</Label>
                <p className="text-xs text-muted-foreground">
                  主站语言的内容直接展示在网站根路径(/)；它同时是内容基准语言，主站语言的标题/名称必填。
                </p>
              </div>
              <Switch
                id="is-default"
                checked={formData.is_default === 1}
                onCheckedChange={(checked) => {
                  setFormData({
                    ...formData,
                    is_default: checked ? 1 : 0,
                    is_enabled: checked ? 1 : formData.is_enabled,
                    path_prefix: checked ? "" : formData.path_prefix,
                  })
                }}
              />
            </div>

            <div className="flex items-center justify-between border-t pt-3">
              <div>
                <Label htmlFor="is-admin-default" className="text-sm font-medium">设为后台显示语言</Label>
                <p className="text-xs text-muted-foreground">
                  后台的分类列表与内容列表默认按该语言显示，方便维护人员使用自己的母语；管理员仍可在本页顶部临时切换自己浏览器使用的语言。
                </p>
              </div>
              <Switch
                id="is-admin-default"
                checked={formData.is_admin_default === 1}
                onCheckedChange={(checked) =>
                  setFormData({
                    ...formData,
                    is_admin_default: checked ? 1 : 0,
                    is_enabled: checked ? 1 : formData.is_enabled,
                  })
                }
              />
            </div>

            <div className="flex items-center justify-between border-t pt-3">
              <div>
                <Label htmlFor="is-fallback" className="text-sm font-medium">设为兜底语言</Label>
                <p className="text-xs text-muted-foreground">当其他语言某个字段为空时，自动使用该语言的值补充</p>
              </div>
              <Switch
                id="is-fallback"
                checked={formData.is_fallback === 1}
                onCheckedChange={(checked) => setFormData({ ...formData, is_fallback: checked ? 1 : 0 })}
              />
            </div>
          </ScrollArea>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDialogOpen(false)}>取消</Button>
            <Button onClick={handleSave} disabled={submitting}>
              {submitting ? <LoaderCircle className="size-4 animate-spin mr-2" /> : null}
              保存
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete Confirmation */}
      <ConfirmDialog
        {...deletingIdDialog}
        onOpenChange={(open) => {
          if (!open) setDeletingId(null)
        }}
        title="确认删除该语言？"
        description="删除语言不会删除已翻译的数据库记录，但该语言将不再被前台生成或在后台展示。"
        confirmLabel="确认删除"
        onConfirm={handleDelete}
      />
    </div>
  )
}
