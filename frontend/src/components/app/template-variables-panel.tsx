import { useEffect, useState, type FormEvent } from "react"
import { Check, Copy, FolderPlus, LoaderCircle, Pencil, Plus, Save, Search, Tag, Trash2 } from "lucide-react"

import {
  createTemplateLabel,
  createTemplateLabelCategory,
  deleteTemplateLabel,
  deleteTemplateLabelCategory,
  getTemplateLabels,
  updateTemplateLabel,
  updateTemplateLabelCategory,
  type TemplateLabel,
  type TemplateLabelCategory,
  type TemplateLabelContext,
  type TemplateLabelInput,
} from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Textarea } from "@/components/ui/textarea"
import { ConfirmDialog, IconButton, InlineAlert, SearchField, TablePagination } from "@/components/app/app-ui"
import { cn } from "@/lib/utils"

const emptyVariable: TemplateLabelInput = {
  key: "",
  name: "",
  category_id: 0,
  context: "any",
  description: "",
  content: "",
  sort_order: 0,
}

const contextOptions: { value: TemplateLabelContext; label: string }[] = [
  { value: "any", label: "通用" },
  { value: "home", label: "首页" },
  { value: "category", label: "栏目" },
  { value: "list", label: "列表项" },
  { value: "detail", label: "内容详情" },
]

function isTemplateLabelContext(value: string): value is TemplateLabelContext {
  return contextOptions.some((option) => option.value === value)
}

function variableInput(item: TemplateLabel): TemplateLabelInput {
  return {
    key: item.key,
    name: item.name,
    category_id: item.category_id,
    context: item.context,
    description: item.description,
    content: item.content,
    sort_order: item.sort_order,
  }
}

function sortCategories(items: TemplateLabelCategory[]) {
  return [...items].sort((left, right) => left.sort_order - right.sort_order || left.id - right.id)
}

function categoryLabel(categories: TemplateLabelCategory[], categoryID: number) {
  return categories.find((category) => category.id === categoryID)?.name ?? "未分类"
}

export function TemplateVariablesPanel() {
  const [items, setItems] = useState<TemplateLabel[]>([])
  const [categories, setCategories] = useState<TemplateLabelCategory[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [query, setQuery] = useState("")
  const [appliedQuery, setAppliedQuery] = useState("")
  const [categoryID, setCategoryID] = useState(0)
  const [selectedID, setSelectedID] = useState<number | null>(null)
  const [form, setForm] = useState<TemplateLabelInput>(emptyVariable)
  const [mode, setMode] = useState<"new" | "edit">("new")
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const [copied, setCopied] = useState<"" | "call" | "content">("")
  const [reload, setReload] = useState(0)

  const [categoryDialogOpen, setCategoryDialogOpen] = useState(false)
  const [editingCategory, setEditingCategory] = useState<TemplateLabelCategory | null>(null)
  const [categoryName, setCategoryName] = useState("")
  const [categorySortOrder, setCategorySortOrder] = useState(0)
  const [categorySaving, setCategorySaving] = useState(false)
  const [categoryDeleting, setCategoryDeleting] = useState(false)
  const [deletingCategory, setDeletingCategory] = useState<TemplateLabelCategory | null>(null)
  const [deletingItem, setDeletingItem] = useState<TemplateLabel | null>(null)

  const totalPages = Math.max(1, Math.ceil(total / 20))
  const selectedItem = items.find((item) => item.id === selectedID) ?? null
  const invocation = form.key ? `{{label "${form.key}" .}}` : '{{label "变量标识" .}}'

  useEffect(() => {
    let active = true
    getTemplateLabels(page, appliedQuery, categoryID)
      .then((response) => {
        if (!active) return
        setItems(response.items)
        setCategories(sortCategories(response.categories))
        setTotal(response.total)
        setError("")
      })
      .catch((loadError) => {
        if (active) setError(loadError instanceof Error ? loadError.message : "公共模板变量加载失败")
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [appliedQuery, categoryID, page, reload])

  function update<K extends keyof TemplateLabelInput>(field: K, value: TemplateLabelInput[K]) {
    setForm((current) => ({ ...current, [field]: value }))
  }

  function selectItem(item: TemplateLabel) {
    setMode("edit")
    setSelectedID(item.id)
    setForm(variableInput(item))
    setNotice("")
  }

  function openNew() {
    setMode("new")
    setSelectedID(null)
    setForm(emptyVariable)
    setNotice("")
  }

  function submitSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setLoading(true)
    setPage(1)
    setAppliedQuery(query.trim())
    setMode("new")
    setSelectedID(null)
    setForm(emptyVariable)
  }

  function selectCategory(value: string | null) {
    const nextCategoryID = Number(value ?? 0)
    setLoading(true)
    setCategoryID(Number.isNaN(nextCategoryID) ? 0 : nextCategoryID)
    setPage(1)
    setMode("new")
    setSelectedID(null)
    setForm(emptyVariable)
  }

  async function save(publish = false) {
    setSaving(true)
    setError("")
    setNotice("")
    try {
      const result = mode === "new"
        ? await createTemplateLabel(form, publish)
        : await updateTemplateLabel(selectedID ?? 0, form, publish)
      setMode("edit")
      setSelectedID(result.item.id)
      setForm(variableInput(result.item))
      setNotice(publish ? "公共模板变量已保存，网站正在重新生成。" : "公共模板变量已保存，发布后生效。")
      setLoading(true)
      setReload((value) => value + 1)
    } catch (saveError) {
      setError(saveError instanceof Error ? saveError.message : "公共模板变量保存失败")
    } finally {
      setSaving(false)
    }
  }

  async function remove() {
    if (!deletingItem) return
    setSaving(true)
    setError("")
    try {
      await deleteTemplateLabel(deletingItem.id)
      setNotice("公共模板变量已删除。")
      setMode("new")
      setSelectedID(null)
      setForm(emptyVariable)
      setDeletingItem(null)
      setLoading(true)
      setReload((value) => value + 1)
    } catch (deleteError) {
      setError(deleteError instanceof Error ? deleteError.message : "公共模板变量删除失败")
    } finally {
      setSaving(false)
    }
  }

  async function copy(value: string, kind: "call" | "content") {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(kind)
      window.setTimeout(() => setCopied(""), 1600)
    } catch {
      setError("复制失败，请检查剪贴板权限")
    }
  }

  function openCategoryManager() {
    setEditingCategory(null)
    setCategoryName("")
    setCategorySortOrder(0)
    setCategoryDialogOpen(true)
  }

  function editCategory(category: TemplateLabelCategory) {
    setEditingCategory(category)
    setCategoryName(category.name)
    setCategorySortOrder(category.sort_order)
  }

  function resetCategoryForm() {
    setEditingCategory(null)
    setCategoryName("")
    setCategorySortOrder(0)
  }

  async function saveCategory(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setCategorySaving(true)
    setError("")
    try {
      const result = editingCategory
        ? await updateTemplateLabelCategory(editingCategory.id, categoryName, categorySortOrder)
        : await createTemplateLabelCategory(categoryName, categorySortOrder)
      setCategories((current) => sortCategories(current.some((item) => item.id === result.category.id)
        ? current.map((item) => item.id === result.category.id ? result.category : item)
        : [...current, result.category]))
      if (!editingCategory) update("category_id", result.category.id)
      setNotice(editingCategory ? "变量分类已更新。" : "变量分类已创建。")
      resetCategoryForm()
      setLoading(true)
      setReload((value) => value + 1)
    } catch (saveError) {
      setError(saveError instanceof Error ? saveError.message : "变量分类保存失败")
    } finally {
      setCategorySaving(false)
    }
  }

  async function removeCategory() {
    if (!deletingCategory) return
    setCategoryDeleting(true)
    setError("")
    try {
      await deleteTemplateLabelCategory(deletingCategory.id)
      setCategories((current) => current.filter((item) => item.id !== deletingCategory.id))
      if (form.category_id === deletingCategory.id) update("category_id", 0)
      setNotice("分类已删除，原分类下的变量已转为未分类。")
      setDeletingCategory(null)
      setLoading(true)
      setReload((value) => value + 1)
    } catch (deleteError) {
      setError(deleteError instanceof Error ? deleteError.message : "变量分类删除失败")
    } finally {
      setCategoryDeleting(false)
    }
  }

  function changePage(nextPage: number) {
    setLoading(true)
    setPage(nextPage)
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="text-sm font-medium">公共模板变量</p>
          <p className="text-xs text-muted-foreground">管理跨模板调用的公共片段变量（如页面头部、尾部、导航、介绍等代码块）。</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" size="sm" onClick={openCategoryManager}>
            <FolderPlus />
            管理变量分类
          </Button>
          <Button size="sm" onClick={openNew}>
            <Plus />
            新增模板变量
          </Button>
        </div>
      </div>

      {notice && <p role="status" className="text-sm text-emerald-600 dark:text-emerald-400">{notice}</p>}
      {error && <InlineAlert>{error}</InlineAlert>}

      <div className="grid min-h-[38rem] overflow-hidden rounded-lg border lg:h-[calc(100dvh-14rem)] lg:grid-cols-[minmax(15rem,22rem)_minmax(0,1fr)]">
        <section className="flex min-h-0 flex-col border-b bg-muted/20 lg:border-r lg:border-b-0">
          <div className="space-y-2 border-b p-3">
            <form className="flex gap-2" onSubmit={submitSearch}>
              <SearchField value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索模板变量" />
              <Button type="submit" variant="outline" size="icon" aria-label="搜索模板变量" title="搜索模板变量"><Search /></Button>
            </form>
            <Select value={String(categoryID)} onValueChange={selectCategory}>
              <SelectTrigger className="w-full" aria-label="按变量分类筛选">
                <SelectValue>
                  {(value) => value === "0" ? "全部分类" : categoryLabel(categories, Number(value))}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="0">全部分类</SelectItem>
                {categories.map((category) => <SelectItem key={category.id} value={String(category.id)}>{category.name}</SelectItem>)}
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">{total.toLocaleString("zh-CN")} 个公共模板变量</p>
          </div>
          <ScrollArea className="min-h-0 flex-1" contentClassName="space-y-1 p-2">
            {loading ? (
              <div className="flex h-32 items-center justify-center text-muted-foreground"><LoaderCircle className="size-5 animate-spin" /></div>
            ) : items.length === 0 ? (
              <div className="flex min-h-40 flex-col items-center justify-center gap-2 text-center text-sm text-muted-foreground">
                <Tag className="size-6" />
                <p>还没有公共模板变量</p>
              </div>
            ) : items.map((item) => (
              <button
                key={item.id}
                type="button"
                className={cn(
                  "flex w-full items-start gap-2 rounded-md px-2.5 py-2 text-left transition-colors hover:bg-muted",
                  selectedID === item.id && mode === "edit" && "bg-background shadow-sm ring-1 ring-foreground/10"
                )}
                onClick={() => selectItem(item)}
              >
                <Tag className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-sm font-medium">{item.name}</span>
                  <span className="mt-0.5 block truncate font-mono text-[11px] text-muted-foreground">{item.key}</span>
                </span>
                <span className="shrink-0 text-[11px] text-muted-foreground">{item.category_name || "未分类"}</span>
              </button>
            ))}
          </ScrollArea>
          <TablePagination page={page} totalPages={totalPages} total={total} pageSize={20} loading={loading} onPageChange={changePage} />
        </section>

        <section className="flex min-h-0 min-w-0 flex-col bg-background">
          <div className="flex min-h-14 flex-wrap items-center justify-between gap-3 border-b px-4 py-3">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">{mode === "new" ? "新增公共模板变量" : form.name || "编辑公共模板变量"}</p>
              <p className="mt-1 truncate font-mono text-[11px] text-muted-foreground">{invocation}</p>
            </div>
            {mode === "edit" && selectedItem && <IconButton variant="ghost" size="icon-sm" label={`删除${selectedItem.name}`} className="text-destructive hover:text-destructive" onClick={() => setDeletingItem(selectedItem)}><Trash2 /></IconButton>}
          </div>
          <ScrollArea className="min-h-0 flex-1" contentClassName="space-y-5 p-4">
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="template-var-name">变量名称</Label>
                <Input id="template-var-name" value={form.name} onChange={(event) => update("name", event.target.value)} placeholder="例如：页面头部" />
              </div>
              <div className="space-y-2">
                <Label htmlFor="template-var-key">变量标识（调用名）</Label>
                <Input id="template-var-key" value={form.key} onChange={(event) => update("key", event.target.value)} placeholder="例如：header" className="font-mono" />
                <p className="text-[11px] text-muted-foreground">以字母开头，可用字母、数字、下划线和短横线。</p>
              </div>
              <div className="space-y-2">
                <Label>所属分类</Label>
                <Select value={String(form.category_id)} onValueChange={(value) => update("category_id", Number(value ?? 0))}>
                  <SelectTrigger className="w-full"><SelectValue placeholder="选择分类" /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="0">未分类</SelectItem>
                    {categories.map((category) => <SelectItem key={category.id} value={String(category.id)}>{category.name}</SelectItem>)}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-2">
                <Label>适用场景</Label>
                <Select value={form.context} onValueChange={(value) => { if (value && isTemplateLabelContext(value)) update("context", value) }}>
                  <SelectTrigger className="w-full"><SelectValue placeholder="选择场景" /></SelectTrigger>
                  <SelectContent>
                    {contextOptions.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-2 sm:col-span-2">
                <Label htmlFor="template-var-description">变量说明</Label>
                <Input id="template-var-description" value={form.description} onChange={(event) => update("description", event.target.value)} placeholder="说明这个模板变量用于什么位置" />
              </div>
            </div>

            <div className="space-y-2">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <Label htmlFor="template-var-content">变量内容 (HTML / Go Template)</Label>
                <Button variant="ghost" size="sm" onClick={() => void copy(form.content, "content")} disabled={!form.content}>
                  {copied === "content" ? <Check /> : <Copy />}
                  {copied === "content" ? "已复制" : "复制内容"}
                </Button>
              </div>
              <Textarea id="template-var-content" value={form.content} onChange={(event) => update("content", event.target.value)} className="min-h-80 resize-y font-mono text-xs leading-6" placeholder={'<header>\n  <nav>...</nav>\n</header>'} spellCheck={false} />
              <p className="text-[11px] text-muted-foreground">变量内容使用 Go template 语法；可包含 HTML、调用标签及站点配置。</p>
            </div>

            <div className="space-y-2 rounded-lg border bg-muted/20 p-3">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div>
                  <p className="text-sm font-medium">主题模板中的调用代码</p>
                  <p className="text-xs text-muted-foreground">在主题 HTML 任意位置插入此变量代码。</p>
                </div>
                <Button variant="outline" size="sm" onClick={() => void copy(invocation, "call")} disabled={!form.key}>
                  {copied === "call" ? <Check /> : <Copy />}
                  {copied === "call" ? "已复制" : "复制调用代码"}
                </Button>
              </div>
              <code className="block overflow-x-auto rounded-md bg-background px-3 py-2 font-mono text-xs leading-6">{invocation}</code>
            </div>
          </ScrollArea>
          <div className="flex flex-wrap justify-end gap-2 border-t p-4">
            {mode === "edit" && selectedItem && (
              <Button variant="ghost" className="mr-auto text-destructive hover:text-destructive" onClick={() => setDeletingItem(selectedItem)} disabled={saving}>
                <Trash2 />
                删除
              </Button>
            )}
            <Button variant="outline" onClick={openNew} disabled={saving}>重置</Button>
            <Button onClick={() => void save()} disabled={saving || !form.key.trim() || !form.name.trim() || !form.content.trim()}>
              {saving ? <LoaderCircle className="animate-spin" /> : <Save />}
              仅保存
            </Button>
            <Button onClick={() => void save(true)} disabled={saving || !form.key.trim() || !form.name.trim() || !form.content.trim()}>保存并发布</Button>
          </div>
        </section>
      </div>

      <Dialog open={categoryDialogOpen} onOpenChange={setCategoryDialogOpen}>
        <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-hidden sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>公共模板变量分类</DialogTitle>
            <DialogDescription>按用途组织整理可复用的公共模板变量片段。</DialogDescription>
          </DialogHeader>
          <ScrollArea className="max-h-[calc(100dvh-12rem)]" contentClassName="space-y-4 pr-2">
            <form className="space-y-3 rounded-lg border p-3" onSubmit={(event) => void saveCategory(event)}>
              <p className="text-sm font-medium">{editingCategory ? "编辑分类" : "新增分类"}</p>
              <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_6rem]">
                <div className="space-y-2">
                  <Label htmlFor="template-var-category-name">分类名称</Label>
                  <Input id="template-var-category-name" value={categoryName} onChange={(event) => setCategoryName(event.target.value)} placeholder="例如：页头页尾" />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="template-var-category-order">排序</Label>
                  <Input id="template-var-category-order" type="number" min="0" value={categorySortOrder} onChange={(event) => setCategorySortOrder(Number(event.target.value))} />
                </div>
              </div>
              <div className="flex justify-end gap-2">
                {editingCategory && <Button type="button" variant="ghost" onClick={resetCategoryForm}>取消编辑</Button>}
                <Button type="submit" disabled={categorySaving || !categoryName.trim()}>
                  {categorySaving ? <LoaderCircle className="animate-spin" /> : <Save />}
                  {editingCategory ? "保存分类" : "新增分类"}
                </Button>
              </div>
            </form>
            <div className="space-y-1">
              {categories.length === 0 ? <p className="py-6 text-center text-sm text-muted-foreground">暂无分类</p> : categories.map((category) => (
                <div key={category.id} className="flex items-center gap-2 rounded-md border px-3 py-2">
                  <span className="min-w-0 flex-1 truncate text-sm">{category.name}</span>
                  <span className="text-xs text-muted-foreground">{category.label_count} 个变量</span>
                  <IconButton variant="ghost" size="icon-sm" label={`编辑${category.name}`} onClick={() => editCategory(category)}><Pencil /></IconButton>
                  <IconButton variant="ghost" size="icon-sm" label={`删除${category.name}`} className="text-destructive hover:text-destructive" onClick={() => setDeletingCategory(category)}><Trash2 /></IconButton>
                </div>
              ))}
            </div>
          </ScrollArea>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCategoryDialogOpen(false)}>关闭</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={Boolean(deletingItem)}
        onOpenChange={(open) => { if (!open) setDeletingItem(null) }}
        title="删除公共模板变量"
        description={deletingItem ? `确定删除“${deletingItem.name}”吗？` : "确认删除这个模板变量吗？"}
        confirmLabel="确认删除"
        pending={saving}
        onConfirm={() => void remove()}
      />

      <ConfirmDialog
        open={Boolean(deletingCategory)}
        onOpenChange={(open) => { if (!open) setDeletingCategory(null) }}
        title="删除变量分类"
        description={deletingCategory ? `确定删除分类“${deletingCategory.name}”吗？分类下的变量会转为未分类。` : "确认删除这个分类吗？"}
        confirmLabel="删除分类"
        pending={categoryDeleting}
        onConfirm={() => void removeCategory()}
      />
    </div>
  )
}
