import { useEffect, useMemo, useState, type FormEvent } from "react"
import {
  BookOpen,
  Check,
  ChevronDown,
  ChevronUp,
  Copy,
  FolderPlus,
  LoaderCircle,
  Pencil,
  Plus,
  Save,
  Search,
  Sparkles,
  Tag,
  Trash2,
} from "lucide-react"

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
  type TemplateTag,
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

const defaultTemptext = `<ul class="news-list">
[!--list.temp--]
  <!--list.var1-->
[!--list.temp--]
</ul>`

const defaultListvar = `<li><a href="[!--url--]">[!--title--]</a><span>[!--date--]</span></li>`

const emptyLabel: TemplateLabelInput = {
  key: "",
  name: "",
  category_id: 0,
  context: "any",
  description: "",
  content: "",
  temptext: defaultTemptext,
  listvar: defaultListvar,
  rownum: 1,
  subnews: 0,
  showdate: "Y-m-d H:i:s",
  sort_order: 0,
}

const contextOptions: { value: TemplateLabelContext; label: string }[] = [
  { value: "any", label: "通用" },
  { value: "home", label: "首页" },
  { value: "category", label: "栏目" },
  { value: "list", label: "列表项" },
  { value: "detail", label: "内容详情" },
]

const showdateOptions = [
  { value: "Y-m-d H:i:s", label: "2026-09-11 16:30:00 (Y-m-d H:i:s)" },
  { value: "Y-m-d", label: "2026-09-11 (Y-m-d)" },
  { value: "m-d", label: "09-11 (m-d)" },
  { value: "Y/m/d", label: "2026/09/11 (Y/m/d)" },
]

function isTemplateLabelContext(value: string): value is TemplateLabelContext {
  return contextOptions.some((option) => option.value === value)
}

function labelInput(item: TemplateLabel): TemplateLabelInput {
  return {
    key: item.key,
    name: item.name,
    category_id: item.category_id,
    context: item.context,
    description: item.description,
    content: item.content,
    temptext: item.temptext || (item.content ? `[!--list.temp--]\n<!--list.var1-->\n[!--list.temp--]` : defaultTemptext),
    listvar: item.listvar || item.content || defaultListvar,
    rownum: item.rownum || 1,
    subnews: item.subnews || 0,
    showdate: item.showdate || "Y-m-d H:i:s",
    sort_order: item.sort_order,
  }
}

function sortCategories(items: TemplateLabelCategory[]) {
  return [...items].sort((left, right) => left.sort_order - right.sort_order || left.id - right.id)
}

function categoryLabel(categories: TemplateLabelCategory[], categoryID: number) {
  return categories.find((category) => category.id === categoryID)?.name ?? "未分类"
}

function BuiltinTemplateTagsDialog({
  open,
  onOpenChange,
  tags,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  tags: TemplateTag[]
}) {
  const [query, setQuery] = useState("")
  const [copied, setCopied] = useState("")
  const filteredTags = useMemo(() => {
    const normalized = query.trim().toLowerCase()
    if (!normalized) return tags
    return tags.filter((tag) => `${tag.name} ${tag.category} ${tag.signature} ${tag.description}`.toLowerCase().includes(normalized))
  }, [query, tags])

  async function copy(value: string) {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(value)
      window.setTimeout(() => setCopied(""), 1600)
    } catch {
      setCopied("")
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[calc(100dvh-2rem)] max-w-4xl overflow-hidden">
        <DialogHeader>
          <DialogTitle>系统内置标签语法参考</DialogTitle>
          <DialogDescription>发布器原生支持的函数与全局标签语法，可在模板中按需调用。</DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <SearchField value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索内置标签、函数名或说明" />
          <ScrollArea className="h-[calc(100dvh-14rem)] pr-2" contentClassName="space-y-3">
            <div className="grid gap-3 md:grid-cols-2">
              {filteredTags.map((tag) => (
                <section key={tag.name} className="space-y-2.5 rounded-lg border p-3.5 text-xs">
                  <div className="flex flex-wrap items-start justify-between gap-2">
                    <div>
                      <div className="flex flex-wrap items-center gap-1.5">
                        <span className="font-semibold text-foreground text-sm">{tag.name}</span>
                        <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">{tag.category}</span>
                      </div>
                      <p className="mt-0.5 text-[11px] text-muted-foreground">适用：{tag.context}</p>
                    </div>
                    <IconButton variant="ghost" size="icon-sm" label={`复制${tag.name}调用`} onClick={() => void copy(tag.signature)}>
                      {copied === tag.signature ? <Check className="text-emerald-500" /> : <Copy />}
                    </IconButton>
                  </div>
                  <code className="block overflow-x-auto rounded bg-muted/70 px-2.5 py-1.5 font-mono text-[11px] text-foreground leading-5">{tag.signature}</code>
                  <p className="text-muted-foreground leading-5">{tag.description}</p>
                  <div className="space-y-1.5">
                    <div className="flex items-center justify-between gap-2">
                      <span className="font-medium text-[11px]">调用示例</span>
                      <IconButton variant="ghost" size="icon-sm" label={`复制${tag.name}示例`} onClick={() => void copy(tag.example)}>
                        {copied === tag.example ? <Check className="text-emerald-500" /> : <Copy />}
                      </IconButton>
                    </div>
                    <pre className="max-h-28 overflow-auto rounded bg-muted/60 p-2 font-mono text-[11px] leading-5 whitespace-pre-wrap break-words"><code>{tag.example}</code></pre>
                  </div>
                  {tag.fields && tag.fields.length > 0 && (
                    <div className="divide-y rounded border text-[11px]">
                      {tag.fields.map((field) => (
                        <div key={`${tag.name}-${field.name}`} className="grid gap-1 px-2.5 py-1 sm:grid-cols-[minmax(6rem,0.8fr)_minmax(0,1.2fr)]">
                          <code className="font-mono text-foreground">{field.name} <span className="text-muted-foreground">({field.type})</span></code>
                          <span className="text-muted-foreground">{field.description}</span>
                        </div>
                      ))}
                    </div>
                  )}
                </section>
              ))}
            </div>
            {filteredTags.length === 0 && <p className="rounded-lg border py-12 text-center text-sm text-muted-foreground">没有匹配的模板标签</p>}
          </ScrollArea>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>关闭</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function TemplateLabelsPanel() {
  const [items, setItems] = useState<TemplateLabel[]>([])
  const [categories, setCategories] = useState<TemplateLabelCategory[]>([])
  const [builtinTags, setBuiltinTags] = useState<TemplateTag[]>([])
  const [query, setQuery] = useState("")
  const [appliedQuery, setAppliedQuery] = useState("")
  const [categoryID, setCategoryID] = useState(0)
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const [mode, setMode] = useState<"new" | "edit">("new")
  const [selectedID, setSelectedID] = useState<number | null>(null)
  const [form, setForm] = useState<TemplateLabelInput>(emptyLabel)
  const [saving, setSaving] = useState(false)
  const [copied, setCopied] = useState<"call" | "temptext" | "listvar" | "">("")
  const [showVariableHelp, setShowVariableHelp] = useState(false)
  const [builtinDialogOpen, setBuiltinDialogOpen] = useState(false)

  const [categoryDialogOpen, setCategoryDialogOpen] = useState(false)
  const [categoryName, setCategoryName] = useState("")
  const [categorySortOrder, setCategorySortOrder] = useState(0)
  const [editingCategory, setEditingCategory] = useState<TemplateLabelCategory | null>(null)
  const [categorySaving, setCategorySaving] = useState(false)
  const [deletingItem, setDeletingItem] = useState<TemplateLabel | null>(null)
  const [deletingCategory, setDeletingCategory] = useState<TemplateLabelCategory | null>(null)
  const [categoryDeleting, setCategoryDeleting] = useState(false)
  const [reload, setReload] = useState(0)

  useEffect(() => {
    let active = true
    getTemplateLabels(page, appliedQuery, categoryID)
      .then((response) => {
        if (!active) return
        setItems(response.items)
        setCategories(response.categories)
        setBuiltinTags(response.template_tags)
        setTotal(response.total)
        setError("")
      })
      .catch((loadError) => {
        if (active) setError(loadError instanceof Error ? loadError.message : "标签模板加载失败")
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [appliedQuery, categoryID, page, reload])

  const totalPages = Math.max(1, Math.ceil(total / 20))
  const invocation = `{{label "${form.key || "key"}" .}}`
  const selectedItem = useMemo(() => items.find((item) => item.id === selectedID) ?? null, [items, selectedID])

  function update<K extends keyof TemplateLabelInput>(key: K, value: TemplateLabelInput[K]) {
    setForm((current) => ({ ...current, [key]: value }))
  }

  function autoDetectRownum() {
    const matches = (form.temptext || "").match(/<!--list\.var\d*-->/g)
    const count = matches ? matches.length : 1
    update("rownum", count > 0 ? count : 1)
    setNotice(`已自动识别分列数：每次显示 ${count > 0 ? count : 1} 条记录`)
  }

  function insertText(
    elementId: string,
    insertContent: string,
    field: "temptext" | "listvar"
  ) {
    const textarea = document.getElementById(elementId) as HTMLTextAreaElement | null
    if (!textarea) {
      update(field, (form[field] || "") + insertContent)
      return
    }
    const start = textarea.selectionStart
    const end = textarea.selectionEnd
    const value = textarea.value
    const nextValue = value.substring(0, start) + insertContent + value.substring(end)
    update(field, nextValue)
    setTimeout(() => {
      textarea.focus()
      textarea.selectionStart = textarea.selectionEnd = start + insertContent.length
    }, 0)
  }

  function selectItem(item: TemplateLabel) {
    setMode("edit")
    setSelectedID(item.id)
    setForm(labelInput(item))
    setNotice("")
  }

  function openNew() {
    setMode("new")
    setSelectedID(null)
    setForm(emptyLabel)
    setNotice("")
  }

  function submitSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setLoading(true)
    setPage(1)
    setAppliedQuery(query.trim())
    setMode("new")
    setSelectedID(null)
    setForm(emptyLabel)
  }

  function selectCategory(value: string | null) {
    const nextCategoryID = Number(value ?? 0)
    setLoading(true)
    setCategoryID(Number.isNaN(nextCategoryID) ? 0 : nextCategoryID)
    setPage(1)
    setMode("new")
    setSelectedID(null)
    setForm(emptyLabel)
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
      setForm(labelInput(result.item))
      setNotice(publish ? "标签模板已保存，网站正在重新生成。" : "标签模板已保存，发布后会更新公开网站。")
      setLoading(true)
      setReload((value) => value + 1)
    } catch (saveError) {
      setError(saveError instanceof Error ? saveError.message : "标签模板保存失败")
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
      setNotice("标签模板已删除。")
      setMode("new")
      setSelectedID(null)
      setForm(emptyLabel)
      setDeletingItem(null)
      setLoading(true)
      setReload((value) => value + 1)
    } catch (deleteError) {
      setError(deleteError instanceof Error ? deleteError.message : "标签模板删除失败")
    } finally {
      setSaving(false)
    }
  }

  async function copy(value: string, kind: "call" | "temptext" | "listvar") {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(kind)
      window.setTimeout(() => setCopied(""), 1600)
    } catch {
      setError("复制失败，请检查浏览器剪贴板权限")
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
      setNotice(editingCategory ? "标签模板分类已更新。" : "标签模板分类已创建。")
      resetCategoryForm()
      setLoading(true)
      setReload((value) => value + 1)
    } catch (saveError) {
      setError(saveError instanceof Error ? saveError.message : "标签模板分类保存失败")
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
      setNotice("分类已删除，原分类下的模板已转为未分类。")
      setDeletingCategory(null)
      setLoading(true)
      setReload((value) => value + 1)
    } catch (deleteError) {
      setError(deleteError instanceof Error ? deleteError.message : "标签模板分类删除失败")
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
          <p className="text-sm font-medium">标签模板管理</p>
          <p className="text-xs text-muted-foreground">
            管理灵动标签和循环调用的模板。由页面模板内容与列表内容模板（list.var）两部分构成，通过特定标记循环渲染。
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" size="sm" onClick={() => setBuiltinDialogOpen(true)}>
            <BookOpen />
            查看内置标签语法
          </Button>
          <Button variant="outline" size="sm" onClick={openCategoryManager}>
            <FolderPlus />
            管理模板分类
          </Button>
          <Button size="sm" onClick={openNew}>
            <Plus />
            增加标签模板
          </Button>
        </div>
      </div>

      {notice && <p role="status" className="text-sm text-emerald-600 dark:text-emerald-400">{notice}</p>}
      {error && <InlineAlert>{error}</InlineAlert>}

      <div className="grid min-h-[42rem] overflow-hidden rounded-lg border lg:h-[calc(100dvh-14rem)] lg:grid-cols-[minmax(16rem,22rem)_minmax(0,1fr)]">
        {/* 左侧模板列表 */}
        <section className="flex min-h-0 flex-col border-b bg-muted/20 lg:border-r lg:border-b-0">
          <div className="space-y-2 border-b p-3">
            <form className="flex gap-2" onSubmit={submitSearch}>
              <SearchField value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索标签模板" />
              <Button type="submit" variant="outline" size="icon" aria-label="搜索标签模板" title="搜索标签模板">
                <Search />
              </Button>
            </form>
            <Select value={String(categoryID)} onValueChange={selectCategory}>
              <SelectTrigger className="w-full" aria-label="按标签模板分类筛选">
                <SelectValue>
                  {(value) => value === "0" ? "全部分类" : categoryLabel(categories, Number(value))}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="0">全部分类</SelectItem>
                {categories.map((category) => (
                  <SelectItem key={category.id} value={String(category.id)}>
                    {category.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">{total.toLocaleString("zh-CN")} 个标签模板</p>
          </div>
          <ScrollArea className="min-h-0 flex-1" contentClassName="space-y-1 p-2">
            {loading ? (
              <div className="flex h-32 items-center justify-center text-muted-foreground">
                <LoaderCircle className="size-5 animate-spin" />
              </div>
            ) : items.length === 0 ? (
              <div className="flex min-h-40 flex-col items-center justify-center gap-2 text-center text-sm text-muted-foreground">
                <Tag className="size-6" />
                <p>暂无标签模板，点击右上角增加</p>
              </div>
            ) : (
              items.map((item) => (
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
              ))
            )}
          </ScrollArea>
          <TablePagination page={page} totalPages={totalPages} total={total} pageSize={20} loading={loading} onPageChange={changePage} />
        </section>

        {/* 右侧表单编辑区：两块核心代码结构 */}
        <section className="flex min-h-0 min-w-0 flex-col bg-background">
          <div className="flex min-h-14 flex-wrap items-center justify-between gap-3 border-b px-4 py-3">
            <div className="min-w-0">
              <p className="truncate text-sm font-semibold">
                {mode === "new" ? "增加标签模板" : `修改标签模板：${form.name || selectedItem?.name}`}
              </p>
              <p className="mt-0.5 truncate font-mono text-[11px] text-muted-foreground">调用代码：{invocation}</p>
            </div>
            {mode === "edit" && selectedItem && (
              <IconButton
                variant="ghost"
                size="icon-sm"
                label={`删除${selectedItem.name}`}
                className="text-destructive hover:text-destructive"
                onClick={() => setDeletingItem(selectedItem)}
              >
                <Trash2 />
              </IconButton>
            )}
          </div>

          <ScrollArea className="min-h-0 flex-1" contentClassName="space-y-5 p-4">
            {/* 基础参数区 */}
            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              <div className="space-y-1.5">
                <Label htmlFor="template-name">模板名 (*)</Label>
                <Input
                  id="template-name"
                  value={form.name}
                  onChange={(event) => update("name", event.target.value)}
                  placeholder="例如：文字列表模板"
                />
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="template-key">调用标识 (*)</Label>
                <Input
                  id="template-key"
                  value={form.key}
                  onChange={(event) => update("key", event.target.value)}
                  placeholder="例如：news-list"
                  className="font-mono"
                />
              </div>

              <div className="space-y-1.5">
                <Label>所属分类</Label>
                <Select value={String(form.category_id)} onValueChange={(value) => update("category_id", Number(value ?? 0))}>
                  <SelectTrigger className="w-full">
                    <SelectValue placeholder="选择所属分类" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="0">未分类</SelectItem>
                    {categories.map((category) => (
                      <SelectItem key={category.id} value={String(category.id)}>
                        {category.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              <div className="space-y-1.5">
                <Label>适用场景</Label>
                <Select
                  value={form.context}
                  onValueChange={(value) => {
                    if (value && isTemplateLabelContext(value)) update("context", value)
                  }}
                >
                  <SelectTrigger className="w-full">
                    <SelectValue placeholder="选择适用场景" />
                  </SelectTrigger>
                  <SelectContent>
                    {contextOptions.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              <div className="space-y-1.5">
                <div className="flex items-center justify-between">
                  <Label htmlFor="template-rownum">每次显示记录数 (分列)</Label>
                  <button
                    type="button"
                    onClick={autoDetectRownum}
                    className="flex items-center gap-1 text-[11px] text-primary hover:underline"
                    title="从页面模板中根据 <!--list.var*--> 数量自动计算"
                  >
                    <Sparkles className="size-3" />
                    自动识别
                  </button>
                </div>
                <Input
                  id="template-rownum"
                  type="number"
                  min="1"
                  value={form.rownum}
                  onChange={(event) => update("rownum", Math.max(1, Number(event.target.value) || 1))}
                />
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="template-subnews">简介截取字数</Label>
                <div className="flex items-center gap-2">
                  <Input
                    id="template-subnews"
                    type="number"
                    min="0"
                    value={form.subnews}
                    onChange={(event) => update("subnews", Math.max(0, Number(event.target.value) || 0))}
                  />
                  <span className="shrink-0 text-xs text-muted-foreground">字 (0为不截取)</span>
                </div>
              </div>

              <div className="space-y-1.5 sm:col-span-2 lg:col-span-3">
                <Label htmlFor="template-showdate">时间显示格式</Label>
                <div className="grid gap-2 sm:grid-cols-[1fr_16rem]">
                  <Input
                    id="template-showdate"
                    value={form.showdate}
                    onChange={(event) => update("showdate", event.target.value)}
                    placeholder="Y-m-d H:i:s"
                    className="font-mono text-xs"
                  />
                  <Select value={form.showdate} onValueChange={(value) => { if (value) update("showdate", value) }}>
                    <SelectTrigger className="w-full text-xs">
                      <SelectValue placeholder="快速选择时间格式" />
                    </SelectTrigger>
                    <SelectContent>
                      {showdateOptions.map((opt) => (
                        <SelectItem key={opt.value} value={opt.value} className="text-xs">
                          {opt.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              </div>

              <div className="space-y-1.5 sm:col-span-2 lg:col-span-3">
                <Label htmlFor="template-description">模板说明</Label>
                <Input
                  id="template-description"
                  value={form.description}
                  onChange={(event) => update("description", event.target.value)}
                  placeholder="说明此模板的布局样式或使用场景"
                />
              </div>
            </div>

            {/* 代码区 1：页面模板内容 */}
            <div className="space-y-2 rounded-lg border p-3.5 bg-muted/10">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div>
                  <Label htmlFor="template-temptext" className="text-sm font-semibold">
                    页面模板内容 (*)
                  </Label>
                  <p className="text-[11px] text-muted-foreground">
                    结构格式：列表头 [!--list.temp--] 列表内容 [!--list.temp--] 列表尾
                  </p>
                </div>
                <div className="flex flex-wrap items-center gap-1.5">
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="h-7 text-xs"
                    onClick={() => insertText("template-temptext", "[!--list.temp--]\n<!--list.var1-->\n[!--list.temp--]", "temptext")}
                  >
                    插入循环标记
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="h-7 text-xs"
                    onClick={() => insertText("template-temptext", "<!--list.var1-->", "temptext")}
                  >
                    +&lt;!--list.var1--&gt;
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="h-7 text-xs"
                    onClick={() => insertText("template-temptext", "<!--list.var2-->", "temptext")}
                  >
                    +&lt;!--list.var2--&gt;
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    className="h-7 text-xs"
                    onClick={() => void copy(form.temptext || "", "temptext")}
                    disabled={!form.temptext}
                  >
                    {copied === "temptext" ? <Check className="text-emerald-500" /> : <Copy />}
                    {copied === "temptext" ? "已复制" : "复制"}
                  </Button>
                </div>
              </div>
              <Textarea
                id="template-temptext"
                value={form.temptext}
                onChange={(event) => update("temptext", event.target.value)}
                className="min-h-48 resize-y font-mono text-xs leading-6"
                placeholder={'<ul class="news-list">\n[!--list.temp--]\n  <!--list.var1-->\n[!--list.temp--]\n</ul>'}
                spellCheck={false}
              />
            </div>

            {/* 代码区 2：列表内容模板 (list.var) */}
            <div className="space-y-2 rounded-lg border p-3.5 bg-muted/10">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div>
                  <Label htmlFor="template-listvar" className="text-sm font-semibold">
                    列表内容模板 (list.var) (*)
                  </Label>
                  <p className="text-[11px] text-muted-foreground">
                    即“页面模板内容”中“&lt;!--list.var*--&gt;”对应每条信息循环输出的内容
                  </p>
                </div>
                <div className="flex flex-wrap items-center gap-1.5">
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    className="h-7 text-xs"
                    onClick={() => void copy(form.listvar || "", "listvar")}
                    disabled={!form.listvar}
                  >
                    {copied === "listvar" ? <Check className="text-emerald-500" /> : <Copy />}
                    {copied === "listvar" ? "已复制" : "复制"}
                  </Button>
                </div>
              </div>

              {/* 常用字段快捷插入工具条 */}
              <div className="flex flex-wrap items-center gap-1 rounded bg-muted/50 p-1.5 text-xs">
                <span className="mr-1 text-[11px] font-medium text-muted-foreground">常用字段：</span>
                <button
                  type="button"
                  className="rounded border bg-background px-1.5 py-0.5 text-[11px] font-mono hover:bg-muted"
                  onClick={() => insertText("template-listvar", "[!--title--]", "listvar")}
                  title="标题"
                >
                  +标题 [!--title--]
                </button>
                <button
                  type="button"
                  className="rounded border bg-background px-1.5 py-0.5 text-[11px] font-mono hover:bg-muted"
                  onClick={() => insertText("template-listvar", "[!--url--]", "listvar")}
                  title="内容链接"
                >
                  +链接 [!--url--]
                </button>
                <button
                  type="button"
                  className="rounded border bg-background px-1.5 py-0.5 text-[11px] font-mono hover:bg-muted"
                  onClick={() => insertText("template-listvar", "[!--image--]", "listvar")}
                  title="缩略图"
                >
                  +缩略图 [!--image--]
                </button>
                <button
                  type="button"
                  className="rounded border bg-background px-1.5 py-0.5 text-[11px] font-mono hover:bg-muted"
                  onClick={() => insertText("template-listvar", "[!--summary--]", "listvar")}
                  title="简介"
                >
                  +简介 [!--summary--]
                </button>
                <button
                  type="button"
                  className="rounded border bg-background px-1.5 py-0.5 text-[11px] font-mono hover:bg-muted"
                  onClick={() => insertText("template-listvar", "[!--date--]", "listvar")}
                  title="发布时间"
                >
                  +时间 [!--date--]
                </button>
                <button
                  type="button"
                  className="rounded border bg-background px-1.5 py-0.5 text-[11px] font-mono hover:bg-muted"
                  onClick={() => insertText("template-listvar", "[!--index--]", "listvar")}
                  title="序号"
                >
                  +序号 [!--index--]
                </button>
                <button
                  type="button"
                  className="rounded border bg-background px-1.5 py-0.5 text-[11px] font-mono hover:bg-muted"
                  onClick={() => insertText("template-listvar", "[!--category.name--]", "listvar")}
                  title="所属分类名"
                >
                  +分类 [!--category.name--]
                </button>
              </div>

              <Textarea
                id="template-listvar"
                value={form.listvar}
                onChange={(event) => update("listvar", event.target.value)}
                className="min-h-36 resize-y font-mono text-xs leading-6"
                placeholder={'<li><a href="[!--url--]">[!--title--]</a><span>[!--date--]</span></li>'}
                spellCheck={false}
              />
            </div>

            {/* 显示模板变量说明（折叠机制） */}
            <div className="rounded-lg border bg-muted/20">
              <button
                type="button"
                className="flex w-full items-center justify-between p-3 text-left font-medium text-xs hover:bg-muted/40"
                onClick={() => setShowVariableHelp(!showVariableHelp)}
              >
                <span>[ {showVariableHelp ? "隐藏模板变量说明" : "显示模板变量说明"} ]</span>
                {showVariableHelp ? <ChevronUp className="size-4" /> : <ChevronDown className="size-4" />}
              </button>
              {showVariableHelp && (
                <div className="border-t p-3.5 space-y-4 text-xs">
                  <div>
                    <p className="font-semibold text-foreground mb-1.5">(1)、页面模板内容支持的变量：</p>
                    <div className="grid gap-2 sm:grid-cols-2 md:grid-cols-3">
                      <div className="rounded border bg-background p-2">
                        <code className="font-mono text-primary">[!--category.name--]</code>
                        <span className="ml-1 text-muted-foreground">当前分类名称</span>
                      </div>
                      <div className="rounded border bg-background p-2">
                        <code className="font-mono text-primary">[!--category.id--]</code>
                        <span className="ml-1 text-muted-foreground">当前分类ID</span>
                      </div>
                      <div className="rounded border bg-background p-2">
                        <code className="font-mono text-primary">[!--category.url--]</code>
                        <span className="ml-1 text-muted-foreground">当前分类链接</span>
                      </div>
                      <div className="rounded border bg-background p-2">
                        <code className="font-mono text-primary">[!--site.name--]</code>
                        <span className="ml-1 text-muted-foreground">网站名称</span>
                      </div>
                      <div className="rounded border bg-background p-2">
                        <code className="font-mono text-primary">[!--site.url--]</code>
                        <span className="ml-1 text-muted-foreground">网站地址</span>
                      </div>
                      <div className="rounded border bg-background p-2">
                        <code className="font-mono text-primary">&lt;!--list.var编号--&gt;</code>
                        <span className="ml-1 text-muted-foreground">循环内容引用（如：&lt;!--list.var1--&gt;）</span>
                      </div>
                    </div>
                  </div>

                  <div>
                    <p className="font-semibold text-foreground mb-1.5">(2)、列表内容模板 (list.var) 支持的变量：</p>
                    <div className="grid gap-2 sm:grid-cols-2 md:grid-cols-3">
                      <div className="rounded border bg-background p-2">
                        <code className="font-mono text-primary">[!--title--]</code>
                        <span className="ml-1 text-muted-foreground">信息标题</span>
                      </div>
                      <div className="rounded border bg-background p-2">
                        <code className="font-mono text-primary">[!--url--]</code>
                        <span className="ml-1 text-muted-foreground">信息链接（别名：titleurl）</span>
                      </div>
                      <div className="rounded border bg-background p-2">
                        <code className="font-mono text-primary">[!--image--]</code>
                        <span className="ml-1 text-muted-foreground">封面/缩略图（别名：titlepic）</span>
                      </div>
                      <div className="rounded border bg-background p-2">
                        <code className="font-mono text-primary">[!--summary--]</code>
                        <span className="ml-1 text-muted-foreground">简介内容（别名：smalltext）</span>
                      </div>
                      <div className="rounded border bg-background p-2">
                        <code className="font-mono text-primary">[!--date--]</code>
                        <span className="ml-1 text-muted-foreground">发布时间（别名：newstime）</span>
                      </div>
                      <div className="rounded border bg-background p-2">
                        <code className="font-mono text-primary">[!--index--]</code>
                        <span className="ml-1 text-muted-foreground">序号（别名：no.num）</span>
                      </div>
                      <div className="rounded border bg-background p-2">
                        <code className="font-mono text-primary">[!--id--]</code>
                        <span className="ml-1 text-muted-foreground">信息ID</span>
                      </div>
                      <div className="rounded border bg-background p-2">
                        <code className="font-mono text-primary">[!--category.name--]</code>
                        <span className="ml-1 text-muted-foreground">所属分类名（别名：class.name）</span>
                      </div>
                      <div className="rounded border bg-background p-2">
                        <code className="font-mono text-primary">[!--category.url--]</code>
                        <span className="ml-1 text-muted-foreground">所属分类链接（别名：classurl）</span>
                      </div>
                    </div>
                    <p className="mt-2 text-[11px] text-muted-foreground">
                      * 提示：同时完全支持标准 Go 模板原生语法（如 <code className="font-mono">{'{{.Title}}'}</code>、<code className="font-mono">{'{{.URL}}'}</code>、<code className="font-mono">{'{{.Image}}'}</code>、<code className="font-mono">{'{{.Summary}}'}</code>、<code className="font-mono">{'{{.Date}}'}</code> 等）。
                    </p>
                  </div>
                </div>
              )}
            </div>

            {/* 主题模板调用代码提示 */}
            <div className="space-y-2 rounded-lg border bg-muted/20 p-3">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div>
                  <p className="text-sm font-medium">主题模板中调用此标签模板</p>
                  <p className="text-xs text-muted-foreground">在主题 HTML 任意位置调用此标签模板渲染输出：</p>
                </div>
                <Button variant="outline" size="sm" onClick={() => void copy(invocation, "call")} disabled={!form.key}>
                  {copied === "call" ? <Check className="text-emerald-500" /> : <Copy />}
                  {copied === "call" ? "已复制" : "复制调用代码"}
                </Button>
              </div>
              <code className="block overflow-x-auto rounded-md bg-background px-3 py-2 font-mono text-xs leading-6">
                {invocation}
              </code>
            </div>
          </ScrollArea>

          {/* 底部按钮栏 */}
          <div className="flex flex-wrap justify-end gap-2 border-t p-4">
            {mode === "edit" && selectedItem && (
              <Button
                variant="ghost"
                className="mr-auto text-destructive hover:text-destructive"
                onClick={() => setDeletingItem(selectedItem)}
                disabled={saving}
              >
                <Trash2 />
                删除模板
              </Button>
            )}
            <Button variant="outline" onClick={openNew} disabled={saving}>
              重置
            </Button>
            <Button
              onClick={() => void save()}
              disabled={saving || !form.key.trim() || !form.name.trim()}
            >
              {saving ? <LoaderCircle className="animate-spin" /> : <Save />}
              保存模板
            </Button>
            <Button
              onClick={() => void save(true)}
              disabled={saving || !form.key.trim() || !form.name.trim()}
            >
              保存并发布
            </Button>
          </div>
        </section>
      </div>

      {/* 分类管理弹窗 */}
      <Dialog open={categoryDialogOpen} onOpenChange={setCategoryDialogOpen}>
        <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-hidden sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>管理标签模板分类</DialogTitle>
            <DialogDescription>按用途或模型整理标签模板。</DialogDescription>
          </DialogHeader>
          <ScrollArea className="max-h-[calc(100dvh-12rem)]" contentClassName="space-y-4 pr-2">
            <form className="space-y-3 rounded-lg border p-3" onSubmit={(event) => void saveCategory(event)}>
              <p className="text-sm font-medium">{editingCategory ? "编辑分类" : "新增分类"}</p>
              <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_6rem]">
                <div className="space-y-2">
                  <Label htmlFor="template-label-category-name">分类名称</Label>
                  <Input
                    id="template-label-category-name"
                    value={categoryName}
                    onChange={(event) => setCategoryName(event.target.value)}
                    placeholder="例如：文章列表类"
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="template-label-category-order">排序</Label>
                  <Input
                    id="template-label-category-order"
                    type="number"
                    min="0"
                    value={categorySortOrder}
                    onChange={(event) => setCategorySortOrder(Number(event.target.value))}
                  />
                </div>
              </div>
              <div className="flex justify-end gap-2">
                {editingCategory && (
                  <Button type="button" variant="ghost" onClick={resetCategoryForm}>
                    取消编辑
                  </Button>
                )}
                <Button type="submit" disabled={categorySaving || !categoryName.trim()}>
                  {categorySaving ? <LoaderCircle className="animate-spin" /> : <Save />}
                  {editingCategory ? "保存分类" : "新增分类"}
                </Button>
              </div>
            </form>
            <div className="space-y-1">
              {categories.length === 0 ? (
                <p className="py-6 text-center text-sm text-muted-foreground">暂无分类</p>
              ) : (
                categories.map((category) => (
                  <div key={category.id} className="flex items-center gap-2 rounded-md border px-3 py-2">
                    <span className="min-w-0 flex-1 truncate text-sm">{category.name}</span>
                    <span className="text-xs text-muted-foreground">{category.label_count} 个模板</span>
                    <IconButton variant="ghost" size="icon-sm" label={`编辑${category.name}`} onClick={() => editCategory(category)}>
                      <Pencil />
                    </IconButton>
                    <IconButton
                      variant="ghost"
                      size="icon-sm"
                      label={`删除${category.name}`}
                      className="text-destructive hover:text-destructive"
                      onClick={() => setDeletingCategory(category)}
                    >
                      <Trash2 />
                    </IconButton>
                  </div>
                ))
              )}
            </div>
          </ScrollArea>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCategoryDialogOpen(false)}>
              关闭
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 内置标签语法弹窗 */}
      <BuiltinTemplateTagsDialog
        open={builtinDialogOpen}
        onOpenChange={setBuiltinDialogOpen}
        tags={builtinTags}
      />

      {/* 删除确认弹窗 */}
      <ConfirmDialog
        open={Boolean(deletingItem)}
        onOpenChange={(open) => {
          if (!open) setDeletingItem(null)
        }}
        title="删除标签模板"
        description={deletingItem ? `确定删除“${deletingItem.name}”吗？` : "确认删除这个标签模板吗？"}
        confirmLabel="确认删除"
        pending={saving}
        onConfirm={() => void remove()}
      />

      <ConfirmDialog
        open={Boolean(deletingCategory)}
        onOpenChange={(open) => {
          if (!open) setDeletingCategory(null)
        }}
        title="删除标签模板分类"
        description={deletingCategory ? `确定删除“${deletingCategory.name}”吗？分类下的模板会转为未分类。` : "确认删除这个分类吗？"}
        confirmLabel="删除分类"
        pending={categoryDeleting}
        onConfirm={() => void removeCategory()}
      />
    </div>
  )
}
