import { useState } from "react"
import {
  LoaderCircle,
  Pencil,
  Plus,
  RefreshCw,
  Star,
  ShieldCheck,
  Trash2,
} from "lucide-react"

import {
  createLanguage,
  deleteLanguage,
  updateLanguage,
  type Language,
  type LanguageInput,
} from "@/lib/api"
import { useLanguage } from "@/lib/language-context"
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

export function LanguagesPage() {
  const { languages, refreshLanguages, loading } = useLanguage()
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
    site_mode: "subdir",
    domain: "",
    output_dir: "",
  })
  const [submitting, setSubmitting] = useState(false)

  // Delete dialog state
  const [deletingId, setDeletingId] = useState<number | null>(null)

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
        setSuccessMsg("语言更新成功")
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
        is_fallback: 1,
      })
      setSuccessMsg(`已将 ${lang.name} 设为兜底语言`)
      await refreshLanguages()
    } catch (err: any) {
      setError(err?.message || "设置兜底语言失败")
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

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2">
        <Button size="sm" onClick={openCreate}>
          <Plus className="size-4 mr-2" />
          新增语言
        </Button>
        <Button variant="outline" size="sm" onClick={() => refreshLanguages()} disabled={loading}>
          <RefreshCw className={`size-4 mr-2 ${loading ? "animate-spin" : ""}`} />
          刷新
        </Button>
      </div>

      {error && <InlineAlert>{error}</InlineAlert>}
      {successMsg && <p role="status" className="text-sm text-green-600 dark:text-green-400">{successMsg}</p>}

      <Table containerClassName="rounded-md border">
            <TableHeader>
              <TableRow>
                <TableHead className="w-[140px]">语言代码</TableHead>
                <TableHead>语言名称</TableHead>
                <TableHead>路径前缀</TableHead>
                <TableHead>主站 / 兜底角色</TableHead>
                <TableHead className="w-[100px]">状态</TableHead>
                <TableHead className="w-[80px]">排序</TableHead>
                <TableHead className="w-[240px] text-right">操作</TableHead>
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
                        {lang.is_fallback === 1 ? (
                          <Badge variant="outline" className="gap-1 text-amber-600 border-amber-500/30 bg-amber-500/10">
                            <ShieldCheck className="size-3" />
                            兜底语言
                          </Badge>
                        ) : null}
                        {lang.is_default !== 1 && lang.is_fallback !== 1 && (
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
                        {lang.is_default !== 1 && (
                          <Button
                            variant="ghost"
                            size="sm"
                            className="h-8 text-xs text-blue-600 hover:text-blue-700"
                            onClick={() => handleSetDefault(lang)}
                            title="设为主站语言"
                          >
                            设为主站
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
                          disabled={lang.is_default === 1}
                          onClick={() => setDeletingId(lang.id)}
                          title={lang.is_default === 1 ? "主站语言不可删除" : "删除语言"}
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
      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{editingLang ? "编辑语言" : "新增语言"}</DialogTitle>
            <DialogDescription>
              配置语言信息。主站语言在根目录展示，其他语言在配置的路径前缀下展示。
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
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

            <div className="flex items-center justify-between border-t pt-3">
              <div>
                <Label htmlFor="is-enabled" className="text-sm font-medium">启用此语言</Label>
                <p className="text-xs text-muted-foreground">未启用的语言不会在前台生成或公开访问</p>
              </div>
              <Switch
                id="is-enabled"
                checked={formData.is_enabled === 1}
                onCheckedChange={(checked) => setFormData({ ...formData, is_enabled: checked ? 1 : 0 })}
                disabled={formData.is_default === 1}
              />
            </div>

            <div className="flex items-center justify-between border-t pt-3">
              <div>
                <Label htmlFor="is-default" className="text-sm font-medium">设为主站语言</Label>
                <p className="text-xs text-muted-foreground">主站语言的内容直接展示在网站根路径(/)</p>
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
                <Label htmlFor="is-fallback" className="text-sm font-medium">设为兜底语言</Label>
                <p className="text-xs text-muted-foreground">当其他语言某个字段为空时，自动使用该语言的值补充</p>
              </div>
              <Switch
                id="is-fallback"
                checked={formData.is_fallback === 1}
                onCheckedChange={(checked) => setFormData({ ...formData, is_fallback: checked ? 1 : 0 })}
              />
            </div>
          </div>
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
        open={deletingId !== null}
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
