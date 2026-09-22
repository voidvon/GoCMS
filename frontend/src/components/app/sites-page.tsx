import { useState } from "react"
import {
  Globe,
  LoaderCircle,
  Pencil,
  Plus,
  RefreshCw,
  Settings,
  Star,
  Trash2,
  CheckCircle2,
} from "lucide-react"

import {
  deleteSite,
  getSiteSettings,
  saveSite,
  saveSiteSettings,
  type Site,
} from "@/lib/api"
import { useSite } from "@/lib/site-context"
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
import { Textarea } from "@/components/ui/textarea"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { ConfirmDialog, InlineAlert } from "@/components/app/app-ui"
import { showSuccess } from "@/components/app/admin-notifications"

export function SitesPage() {
  const { sites, refreshSites, loading, activeSiteId, setActiveSite } = useSite()
  const [error, setError] = useState("")

  // Add / Edit Dialog state
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingSite, setEditingSite] = useState<Site | null>(null)
  const [formData, setFormData] = useState<{
    name: string
    code: string
    domain: string
    aliasesText: string
    theme_id: string
    output_dir: string
    is_default: boolean
    status: "active" | "disabled"
  }>({
    name: "",
    code: "",
    domain: "",
    aliasesText: "",
    theme_id: "",
    output_dir: "",
    is_default: false,
    status: "active",
  })
  const [submitting, setSubmitting] = useState(false)

  // Delete Confirm Dialog state
  const [deleteTarget, setDeleteTarget] = useState<Site | null>(null)
  const [deleting, setDeleting] = useState(false)

  // Site Settings Dialog state
  const [settingsDialogOpen, setSettingsDialogOpen] = useState(false)
  const [settingsSite, setSettingsSite] = useState<Site | null>(null)
  const [settingsLoading, setSettingsLoading] = useState(false)
  const [settingsSaving, setSettingsSaving] = useState(false)
  const [standardSettings, setStandardSettings] = useState<Record<string, string>>({
    site_name: "",
    site_url: "",
    site_keywords: "",
    site_description: "",
    icp_beian: "",
    police_beian: "",
    copyright: "",
  })
  const [customSettingsList, setCustomSettingsList] = useState<Array<{ key: string; value: string }>>([])
  const [settingsError, setSettingsError] = useState("")

  const handleOpenSettings = async (site: Site) => {
    setSettingsSite(site)
    setSettingsDialogOpen(true)
    setSettingsLoading(true)
    setSettingsError("")
    try {
      const data = await getSiteSettings(site.id)
      const stdKeys = new Set([
        "site_name",
        "site_url",
        "site_keywords",
        "site_description",
        "icp_beian",
        "police_beian",
        "copyright",
      ])
      setStandardSettings({
        site_name: data.site_name || site.name || "",
        site_url: data.site_url || (site.domain ? `https://${site.domain}` : ""),
        site_keywords: data.site_keywords || data.keywords || "",
        site_description: data.site_description || data.description || "",
        icp_beian: data.icp_beian || data.icp || "",
        police_beian: data.police_beian || "",
        copyright: data.copyright || "",
      })
      const custom: Array<{ key: string; value: string }> = []
      for (const [k, v] of Object.entries(data)) {
        if (!stdKeys.has(k) && k !== "keywords" && k !== "description" && k !== "icp") {
          custom.push({ key: k, value: v })
        }
      }
      setCustomSettingsList(custom)
    } catch (err: unknown) {
      setSettingsError(err instanceof Error ? err.message : "加载站点配置失败")
    } finally {
      setSettingsLoading(false)
    }
  }

  const handleSaveSettings = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!settingsSite) return
    setSettingsSaving(true)
    setSettingsError("")
    try {
      const merged: Record<string, string> = { ...standardSettings }
      for (const item of customSettingsList) {
        const k = item.key.trim()
        if (k) {
          merged[k] = item.value
        }
      }
      await saveSiteSettings(merged, settingsSite.id)
      showSuccess(`站点“${settingsSite.name}”配置已保存`)
      setSettingsDialogOpen(false)
    } catch (err: unknown) {
      setSettingsError(err instanceof Error ? err.message : "保存配置失败")
    } finally {
      setSettingsSaving(false)
    }
  }

  const handleOpenAdd = () => {
    setEditingSite(null)
    setFormData({
      name: "",
      code: "",
      domain: "",
      aliasesText: "",
      theme_id: "",
      output_dir: "",
      is_default: false,
      status: "active",
    })
    setError("")
    setDialogOpen(true)
  }

  const handleOpenEdit = (site: Site) => {
    setEditingSite(site)
    setFormData({
      name: site.name,
      code: site.code,
      domain: site.domain,
      aliasesText: (site.aliases || []).join("\n"),
      theme_id: site.theme_id || "",
      output_dir: site.output_dir || "",
      is_default: site.is_default,
      status: site.status,
    })
    setError("")
    setDialogOpen(true)
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError("")

    if (!formData.name.trim()) {
      setError("请输入站点名称")
      return
    }
    if (!formData.code.trim()) {
      setError("请输入站点标识（用于系统识别，如 blog, shop）")
      return
    }

    const aliases = formData.aliasesText
      .split(/[\n,]+/)
      .map((s) => s.trim())
      .filter(Boolean)

    setSubmitting(true)
    try {
      await saveSite({
        id: editingSite?.id,
        name: formData.name.trim(),
        code: formData.code.trim().toLowerCase(),
        domain: formData.domain.trim().toLowerCase(),
        aliases,
        theme_id: formData.theme_id.trim(),
        output_dir: formData.output_dir.trim(),
        is_default: formData.is_default,
        status: formData.status,
      })

      showSuccess(editingSite ? "站点更新成功" : "新站点创建成功")
      setDialogOpen(false)
      await refreshSites()
    } catch (err: any) {
      setError(err.message || "保存站点失败")
    } finally {
      setSubmitting(false)
    }
  }

  const handleDelete = async () => {
    if (!deleteTarget) return
    setDeleting(true)
    setError("")
    try {
      await deleteSite(deleteTarget.id)
      showSuccess(`站点“${deleteTarget.name}”已删除`)
      setDeleteTarget(null)
      await refreshSites()
    } catch (err: any) {
      setError(err.message || "删除站点失败")
    } finally {
      setDeleting(false)
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-2xl font-bold tracking-tight">多站点管理</h2>
          <p className="text-muted-foreground text-sm">
            管理系统中的各个独立网站，配置独立域名、主题绑定及生成目录。
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => void refreshSites()} disabled={loading}>
            <RefreshCw className={`mr-2 h-4 w-4 ${loading ? "animate-spin" : ""}`} />
            刷新
          </Button>
          <Button size="sm" onClick={handleOpenAdd}>
            <Plus className="mr-2 h-4 w-4" />
            新建站点
          </Button>
        </div>
      </div>

      {error && <InlineAlert>{error}</InlineAlert>}

      <div className="rounded-md border bg-card">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-[80px]">ID</TableHead>
              <TableHead>站点名称</TableHead>
              <TableHead>站点标识</TableHead>
              <TableHead>主域名</TableHead>
              <TableHead>别名域名</TableHead>
              <TableHead>绑定主题</TableHead>
              <TableHead>生成目录</TableHead>
              <TableHead>状态</TableHead>
              <TableHead className="text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {sites.length === 0 ? (
              <TableRow>
                <TableCell colSpan={9} className="h-24 text-center text-muted-foreground">
                  {loading ? "加载中..." : "暂无站点"}
                </TableCell>
              </TableRow>
            ) : (
              sites.map((site) => {
                const isCurrent = site.id === activeSiteId
                return (
                  <TableRow key={site.id} className={isCurrent ? "bg-muted/40" : ""}>
                    <TableCell className="font-mono text-xs">{site.id}</TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <span className="font-medium">{site.name}</span>
                        {site.is_default && (
                          <Badge variant="secondary" className="gap-1 text-xs">
                            <Star className="h-3 w-3 fill-amber-400 text-amber-500" />
                            默认主站
                          </Badge>
                        )}
                        {isCurrent && (
                          <Badge variant="outline" className="border-green-500/40 text-green-600 gap-1 text-xs dark:text-green-400">
                            <CheckCircle2 className="h-3 w-3" />
                            当前管理
                          </Badge>
                        )}
                      </div>
                    </TableCell>
                    <TableCell>
                      <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs text-muted-foreground">
                        {site.code}
                      </code>
                    </TableCell>
                    <TableCell>
                      {site.domain ? (
                        <div className="flex items-center gap-1.5 text-sm">
                          <Globe className="h-3.5 w-3.5 text-muted-foreground" />
                          <span>{site.domain}</span>
                        </div>
                      ) : (
                        <span className="text-xs text-muted-foreground">未配置</span>
                      )}
                    </TableCell>
                    <TableCell>
                      {site.aliases && site.aliases.length > 0 ? (
                        <span className="text-xs text-muted-foreground">
                          {site.aliases.length} 个别名
                        </span>
                      ) : (
                        <span className="text-xs text-muted-foreground">-</span>
                      )}
                    </TableCell>
                    <TableCell>
                      <span className="text-xs">
                        {site.theme_id || <span className="text-muted-foreground">跟随默认</span>}
                      </span>
                    </TableCell>
                    <TableCell>
                      <code className="text-xs text-muted-foreground">
                        {site.output_dir ? `web/${site.output_dir}/` : `web/${site.id}/`}
                      </code>
                    </TableCell>
                    <TableCell>
                      {site.status === "active" ? (
                        <Badge variant="outline" className="text-xs text-emerald-600 dark:text-emerald-400 border-emerald-500/30">
                          已启用
                        </Badge>
                      ) : (
                        <Badge variant="destructive" className="text-xs">
                          已停用
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-1">
                        {!isCurrent && (
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => setActiveSite(site)}
                            className="h-8 text-xs text-primary"
                          >
                            切到此站
                          </Button>
                        )}
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8 text-muted-foreground hover:text-foreground"
                          onClick={() => void handleOpenSettings(site)}
                          title="站点参数配置"
                        >
                          <Settings className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8 text-muted-foreground hover:text-foreground"
                          onClick={() => handleOpenEdit(site)}
                          title="编辑站点"
                        >
                          <Pencil className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8 text-destructive hover:bg-destructive/10"
                          disabled={site.id === 1 || site.is_default}
                          onClick={() => setDeleteTarget(site)}
                          title={site.id === 1 || site.is_default ? "默认站点不可删除" : "删除站点"}
                        >
                          <Trash2 className="h-4 w-4" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                )
              })
            )}
          </TableBody>
        </Table>
      </div>

      {/* Add/Edit Site Dialog */}
      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>{editingSite ? "编辑站点" : "新建站点"}</DialogTitle>
            <DialogDescription>
              配置站点的基础信息、访问域名、绑定主题及静态生成输出目录。
            </DialogDescription>
          </DialogHeader>
          <form onSubmit={handleSubmit} className="space-y-4 py-2">
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="site-name">站点名称 *</Label>
                <Input
                  id="site-name"
                  placeholder="例如：科技资讯站"
                  value={formData.name}
                  onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                  required
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="site-code">站点标识 (Code) *</Label>
                <Input
                  id="site-code"
                  placeholder="例如：tech, news"
                  value={formData.code}
                  onChange={(e) => setFormData({ ...formData, code: e.target.value })}
                  disabled={editingSite?.id === 1}
                  required
                />
                <p className="text-[11px] text-muted-foreground">小写字母、数字与短横线</p>
              </div>
            </div>

            <div className="space-y-2">
              <Label htmlFor="site-domain">主域名 (Domain)</Label>
              <Input
                id="site-domain"
                placeholder="例如：tech.example.com"
                value={formData.domain}
                onChange={(e) => setFormData({ ...formData, domain: e.target.value })}
              />
              <p className="text-[11px] text-muted-foreground">
                用于根据 Host 请求头路由到本站，无需包含 http:// 协议头
              </p>
            </div>

            <div className="space-y-2">
              <Label htmlFor="site-aliases">别名域名列表 (每行一个)</Label>
              <Textarea
                id="site-aliases"
                placeholder="例如：&#10;www.tech.example.com&#10;m.tech.example.com"
                rows={3}
                value={formData.aliasesText}
                onChange={(e) => setFormData({ ...formData, aliasesText: e.target.value })}
              />
            </div>

            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="site-theme">绑定主题 ID</Label>
                <Input
                  id="site-theme"
                  placeholder="留空则跟随全局活动主题"
                  value={formData.theme_id}
                  onChange={(e) => setFormData({ ...formData, theme_id: e.target.value })}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="site-output">静态生成相对目录</Label>
                  <Input
                    id="site-output"
                    placeholder={editingSite ? `${editingSite.id}` : "留空则默认为站点 ID"}
                    value={formData.output_dir}
                    onChange={(e) => setFormData({ ...formData, output_dir: e.target.value })}
                  />
              </div>
            </div>

            <div className="flex items-center justify-between rounded-lg border p-3">
              <div className="space-y-0.5">
                <Label className="text-sm font-medium">设为默认站点</Label>
                <p className="text-xs text-muted-foreground">
                  {editingSite?.is_default
                    ? "当前为系统默认站点，不可直接取消；将其他站点设为默认时将自动接替"
                    : "当访问未绑定的域名或 IP 时，系统将回退到默认站点展示"}
                </p>
              </div>
              <Switch
                checked={formData.is_default}
                disabled={editingSite?.is_default}
                onCheckedChange={(checked) =>
                  setFormData({
                    ...formData,
                    is_default: checked,
                    status: checked ? "active" : formData.status,
                  })
                }
              />
            </div>

            <div className="flex items-center justify-between rounded-lg border p-3">
              <div className="space-y-0.5">
                <Label className="text-sm font-medium">站点启用状态</Label>
                <p className="text-xs text-muted-foreground">
                  {formData.is_default ? "默认站点必须保持启用" : "停用后前台将暂停服务"}
                </p>
              </div>
              <Switch
                checked={formData.status === "active"}
                disabled={formData.is_default}
                onCheckedChange={(checked) =>
                  setFormData({ ...formData, status: checked ? "active" : "disabled" })
                }
              />
            </div>

            <DialogFooter className="pt-2">
              <Button type="button" variant="outline" onClick={() => setDialogOpen(false)}>
                取消
              </Button>
              <Button type="submit" disabled={submitting}>
                {submitting && <LoaderCircle className="mr-2 h-4 w-4 animate-spin" />}
                {editingSite ? "保存更改" : "立即创建"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Site Parameters / Global Settings Dialog */}
      <Dialog open={settingsDialogOpen} onOpenChange={setSettingsDialogOpen}>
        <DialogContent className="max-w-2xl max-h-[85vh] flex flex-col">
          <DialogHeader>
            <DialogTitle>站点参数配置 - {settingsSite?.name}</DialogTitle>
            <DialogDescription>
              配置该站点的全局变量与模板标签参数（对应模板中的 {"{{setting \"key\"}}"} ），保存后静态发布即时生效。
            </DialogDescription>
          </DialogHeader>
          {settingsError && <InlineAlert>{settingsError}</InlineAlert>}
          {settingsLoading ? (
            <div className="flex h-48 items-center justify-center">
              <LoaderCircle className="h-6 w-6 animate-spin text-muted-foreground" />
            </div>
          ) : (
            <form onSubmit={handleSaveSettings} className="space-y-4 overflow-y-auto pr-1 flex-1 py-2">
              <div className="space-y-3 rounded-lg border p-3.5 bg-muted/20">
                <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">基础与链接</h4>
                <div className="grid grid-cols-2 gap-3">
                  <div className="space-y-1.5">
                    <Label htmlFor="std-site-name" className="text-xs">站点全称 (site_name)</Label>
                    <Input
                      id="std-site-name"
                      className="h-8 text-xs"
                      value={standardSettings.site_name}
                      onChange={(e) => setStandardSettings({ ...standardSettings, site_name: e.target.value })}
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label htmlFor="std-site-url" className="text-xs">站点主网址 (site_url)</Label>
                    <Input
                      id="std-site-url"
                      className="h-8 text-xs"
                      placeholder="https://example.com"
                      value={standardSettings.site_url}
                      onChange={(e) => setStandardSettings({ ...standardSettings, site_url: e.target.value })}
                    />
                  </div>
                </div>
              </div>

              <div className="space-y-3 rounded-lg border p-3.5 bg-muted/20">
                <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">SEO 与元信息</h4>
                <div className="space-y-2">
                  <div className="space-y-1.5">
                    <Label htmlFor="std-site-keywords" className="text-xs">全局关键词 (site_keywords)</Label>
                    <Input
                      id="std-site-keywords"
                      className="h-8 text-xs"
                      placeholder="关键词用逗号隔开"
                      value={standardSettings.site_keywords}
                      onChange={(e) => setStandardSettings({ ...standardSettings, site_keywords: e.target.value })}
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label htmlFor="std-site-description" className="text-xs">全局描述 (site_description)</Label>
                    <Textarea
                      id="std-site-description"
                      className="text-xs min-h-[60px]"
                      rows={2}
                      placeholder="网站的默认描述信息"
                      value={standardSettings.site_description}
                      onChange={(e) => setStandardSettings({ ...standardSettings, site_description: e.target.value })}
                    />
                  </div>
                </div>
              </div>

              <div className="space-y-3 rounded-lg border p-3.5 bg-muted/20">
                <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">备案与版权</h4>
                <div className="grid grid-cols-2 gap-3">
                  <div className="space-y-1.5">
                    <Label htmlFor="std-icp" className="text-xs">ICP 备案号 (icp_beian)</Label>
                    <Input
                      id="std-icp"
                      className="h-8 text-xs"
                      placeholder="例如：京ICP备12345678号"
                      value={standardSettings.icp_beian}
                      onChange={(e) => setStandardSettings({ ...standardSettings, icp_beian: e.target.value })}
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label htmlFor="std-police" className="text-xs">公安联网备案号 (police_beian)</Label>
                    <Input
                      id="std-police"
                      className="h-8 text-xs"
                      placeholder="例如：京公网安备11010802020110号"
                      value={standardSettings.police_beian}
                      onChange={(e) => setStandardSettings({ ...standardSettings, police_beian: e.target.value })}
                    />
                  </div>
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="std-copyright" className="text-xs">版权声明 (copyright)</Label>
                  <Input
                    id="std-copyright"
                    className="h-8 text-xs"
                    placeholder="例如：© 2026 某某公司 版权所有"
                    value={standardSettings.copyright}
                    onChange={(e) => setStandardSettings({ ...standardSettings, copyright: e.target.value })}
                  />
                </div>
              </div>

              <div className="space-y-3 rounded-lg border p-3.5 bg-muted/20">
                <div className="flex items-center justify-between">
                  <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">自定义扩展参数</h4>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="h-7 text-xs gap-1"
                    onClick={() => setCustomSettingsList([...customSettingsList, { key: "", value: "" }])}
                  >
                    <Plus className="h-3.5 w-3.5" />
                    添加参数
                  </Button>
                </div>
                {customSettingsList.length === 0 ? (
                  <p className="text-xs text-muted-foreground">暂无自定义参数，可按需添加自定义 Key-Value 并在模板中调用。</p>
                ) : (
                  <div className="space-y-2">
                    {customSettingsList.map((item, index) => (
                      <div key={index} className="flex items-center gap-2">
                        <Input
                          placeholder="键名 (key)"
                          className="h-8 text-xs w-1/3 font-mono"
                          value={item.key}
                          onChange={(e) => {
                            const updated = [...customSettingsList]
                            updated[index].key = e.target.value
                            setCustomSettingsList(updated)
                          }}
                        />
                        <Input
                          placeholder="配置值 (value)"
                          className="h-8 text-xs flex-1"
                          value={item.value}
                          onChange={(e) => {
                            const updated = [...customSettingsList]
                            updated[index].value = e.target.value
                            setCustomSettingsList(updated)
                          }}
                        />
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8 text-muted-foreground hover:text-destructive"
                          onClick={() => setCustomSettingsList(customSettingsList.filter((_, i) => i !== index))}
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    ))}
                  </div>
                )}
              </div>

              <DialogFooter className="pt-2 sticky bottom-0 bg-background pb-1">
                <Button type="button" variant="outline" onClick={() => setSettingsDialogOpen(false)}>
                  取消
                </Button>
                <Button type="submit" disabled={settingsSaving}>
                  {settingsSaving && <LoaderCircle className="mr-2 h-4 w-4 animate-spin" />}
                  保存配置
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>

      {/* Delete Confirmation */}
      <ConfirmDialog
        open={Boolean(deleteTarget)}
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null)
        }}
        title="确认删除站点？"
        description={`确定要删除站点“${deleteTarget?.name}”吗？此操作将级联删除该站点下的所有栏目、内容、留言及独立配置，操作不可撤销！`}
        confirmLabel="确认删除"
        confirmVariant="destructive"
        pending={deleting}
        onConfirm={handleDelete}
      />
    </div>
  )
}
