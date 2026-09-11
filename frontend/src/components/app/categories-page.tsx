import { useEffect, useMemo, useState, type ReactNode } from "react"
import {
  ChevronDown,
  ChevronRight,
  FolderTree,
  LoaderCircle,
  Plus,
  Trash2,
} from "lucide-react"

import {
  createCategory,
  deleteCategory,
  getCategories,
  getSystemModels,
  getThemeFiles,
  updateCategory,
  type CategoryInput,
  type CategoryItem,
  type SaveResponse,
  type SystemModel,
  type ThemeFile,
  type ThemeTemplateGroup,
} from "@/lib/api"
import { buildCategoryTree, flattenCategoryTree, type CategoryNode } from "@/lib/category-tree"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { ConfirmDialog, IconButton, InlineAlert } from "@/components/app/app-ui"
import { cn } from "@/lib/utils"
import { ScrollArea } from "@/components/ui/scroll-area"

const emptyCategory: CategoryInput = {
  name: "",
  parent_id: 0,
  order_id: 0,
  list_page_size: 14,
  page_type: "list",
  list_path: "category",
  list_file_pattern: "{id}.html",
  list_template: "category_list.html",
  cover_template: "category_cover.html",
  detail_path: "content",
  detail_file_pattern: "{id}.html",
  detail_template: "content_detail.html",
  model_id: 1,
}

function getCoverTemplates(templateGroups: ThemeTemplateGroup[], templateFiles: ThemeFile[]) {
  const group = templateGroups.find((g) => g.key === "cover")
  if (group?.files && group.files.length > 0) {
    return group.files
  }
  const matching = templateFiles.filter((f) => f.path.toLowerCase().includes("cover"))
  if (matching.length > 0) {
    return matching
  }
  return [{ path: "category_cover.html", size: 0, modified_at: "" }]
}

function getListTemplates(templateGroups: ThemeTemplateGroup[], templateFiles: ThemeFile[]) {
  const group = templateGroups.find((g) => g.key === "list")
  if (group?.files && group.files.length > 0) {
    return group.files
  }
  const matching = templateFiles.filter((f) => {
    const p = f.path.toLowerCase()
    return p.includes("list") || p.includes("sort")
  })
  if (matching.length > 0) {
    return matching
  }
  return [{ path: "category_list.html", size: 0, modified_at: "" }]
}

function defaultListPath(_parentID: number, parent?: CategoryItem) {
  if (parent?.list_path) return parent.list_path
  return "category"
}

function defaultListTemplate(parent?: CategoryItem, templateGroups: ThemeTemplateGroup[] = [], templateFiles: ThemeFile[] = []) {
  if (parent?.list_template) return parent.list_template
  const lists = getListTemplates(templateGroups, templateFiles)
  return lists[0]?.path || "category_list.html"
}

function defaultCoverTemplate(parent?: CategoryItem, templateGroups: ThemeTemplateGroup[] = [], templateFiles: ThemeFile[] = []) {
  if (parent?.cover_template) return parent.cover_template
  const covers = getCoverTemplates(templateGroups, templateFiles)
  return covers[0]?.path || "category_cover.html"
}

function defaultDetailPath(_listPath: string, parent?: CategoryItem) {
  return parent?.detail_path || "content"
}

function defaultDetailTemplate(_listPath: string, parent?: CategoryItem) {
  return parent?.detail_template || "content_detail.html"
}

function publicationMessage(result: SaveResponse, action: string) {
  if (!result.publication) return `${action}已保存，发布后会更新公开网站。`
  return result.publish_started
    ? `${action}已保存，正在生成网站；请在网站发布中查看结果。`
    : `${action}已保存并加入发布队列，将在当前任务完成后自动生成。`
}

function categoryInput(category: CategoryItem, templateGroups: ThemeTemplateGroup[] = [], templateFiles: ThemeFile[] = []): CategoryInput {
  const coverTemplates = getCoverTemplates(templateGroups, templateFiles)
  const isCoverValid = coverTemplates.some((f) => f.path === category.cover_template)
  const coverTemplate = isCoverValid && category.cover_template
    ? category.cover_template
    : (coverTemplates[0]?.path || "category_cover.html")

  const listTemplates = getListTemplates(templateGroups, templateFiles)
  const isListValid = listTemplates.some((f) => f.path === category.list_template)
  const listTemplate = isListValid && category.list_template
    ? category.list_template
    : (listTemplates[0]?.path || "category_list.html")

  return {
    name: category.name,
    parent_id: category.parent_id,
    order_id: category.order_id,
    list_page_size: category.list_page_size,
    page_type: category.page_type || "list",
    list_path: category.list_path,
    list_file_pattern: category.list_file_pattern,
    list_template: listTemplate,
    cover_template: coverTemplate,
    detail_path: category.detail_path,
    detail_file_pattern: category.detail_file_pattern,
    detail_template: category.detail_template,
    model_id: category.model_id || 1,
  }
}

type CategoryRowProps = {
  node: CategoryNode
  depth: number
  expanded: boolean
  selected: boolean
  onToggle: () => void
  onAddChild: () => void
  onSelect: () => void
  onDelete: () => void
}

function CategoryRow({
  node,
  depth,
  expanded,
  selected,
  onToggle,
  onAddChild,
  onSelect,
  onDelete,
}: CategoryRowProps) {
  const hasChildren = node.children.length > 0

  return (
    <div
      className={cn(
        "flex min-h-14 items-center gap-2 border-b px-3 py-2 last:border-b-0",
        selected ? "bg-accent/60" : "hover:bg-muted/50",
      )}
      style={{ paddingLeft: `${12 + depth * 24}px` }}
    >
      <div className="flex size-7 shrink-0 items-center justify-center">
        {hasChildren ? (
          <IconButton variant="ghost" size="icon-sm" label={expanded ? "收起子分类" : "展开子分类"} onClick={onToggle}>
            {expanded ? <ChevronDown /> : <ChevronRight />}
          </IconButton>
        ) : (
          <span className="size-1.5 rounded-full bg-border" aria-hidden="true" />
        )}
      </div>
      <button
        type="button"
        className="flex min-w-0 flex-1 items-center gap-2 rounded-md text-left outline-none focus-visible:ring-2 focus-visible:ring-ring"
        aria-pressed={selected}
        onClick={onSelect}
      >
        <FolderTree className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
        <span className="min-w-0 flex-1">
          <span className="block truncate text-sm font-medium">{node.name}</span>
          <span className="block truncate text-xs text-muted-foreground">{node.content_count} 条直接内容 · {node.page_type === "cover" ? "封面式" : "列表式"} · 排序 {node.order_id} · #{node.route_id}</span>
        </span>
      </button>
      <div className="flex shrink-0 items-center gap-1">
        <IconButton variant="ghost" size="icon-sm" label={`在${node.name}下新增子分类`} onClick={onAddChild}>
          <Plus />
        </IconButton>
        <IconButton
          variant="ghost"
          size="icon-sm"
          className="text-destructive hover:text-destructive"
          label={`删除${node.name}`}
          onClick={onDelete}
        >
          <Trash2 />
        </IconButton>
      </div>
    </div>
  )
}

export function CategoriesPage() {
  const [categories, setCategories] = useState<CategoryItem[]>([])
  const [models, setModels] = useState<SystemModel[]>([])
  const [templateFiles, setTemplateFiles] = useState<ThemeFile[]>([])
  const [templateGroups, setTemplateGroups] = useState<ThemeTemplateGroup[]>([])
  const [loading, setLoading] = useState(true)
  const [templatesLoading, setTemplatesLoading] = useState(true)
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const [expanded, setExpanded] = useState<Set<number>>(new Set())
  const [editorActive, setEditorActive] = useState(false)
  const [editing, setEditing] = useState<CategoryItem | null>(null)
  const [form, setForm] = useState<CategoryInput>(emptyCategory)
  const [saving, setSaving] = useState(false)
  const [deleting, setDeleting] = useState<CategoryItem | null>(null)
  const [deleteSaving, setDeleteSaving] = useState(false)

  const tree = useMemo(() => buildCategoryTree(categories), [categories])
  const parentOptions = useMemo(() => flattenCategoryTree(categories, editing?.id), [categories, editing?.id])

  async function refreshCategories() {
    const nextCategories = await getCategories()
    setCategories(nextCategories)
    setExpanded((current) => {
      const availableIDs = new Set(nextCategories.map((category) => category.id))
      return new Set([...current].filter((categoryID) => availableIDs.has(categoryID)))
    })
    return nextCategories
  }

  useEffect(() => {
    let active = true
    Promise.all([getCategories(), getThemeFiles(), getSystemModels()])
      .then(([nextCategories, theme, nextModels]) => {
        if (!active) return
        setCategories(nextCategories)
        setModels(nextModels)
        setTemplateFiles(theme.template_files)
        setTemplateGroups(theme.template_groups)
        setExpanded(new Set())
        setError("")
      })
      .catch((loadError) => {
        if (active) setError(loadError instanceof Error ? loadError.message : "分类加载失败")
      })
      .finally(() => {
        if (active) {
          setLoading(false)
          setTemplatesLoading(false)
        }
      })
    return () => {
      active = false
    }
  }, [])

  function update<K extends keyof CategoryInput>(key: K, value: CategoryInput[K]) {
    setForm((current) => ({ ...current, [key]: value }))
  }

  function updatePageType(value: string) {
    const pageType = value === "cover" ? "cover" : "list"
    setForm((current) => {
      const coverTemplates = getCoverTemplates(templateGroups, templateFiles)
      const isCurrentCoverValid = coverTemplates.some((f) => f.path === current.cover_template)
      const nextCoverTemplate = isCurrentCoverValid && current.cover_template
        ? current.cover_template
        : (coverTemplates[0]?.path || "category_cover.html")

      const listTemplates = getListTemplates(templateGroups, templateFiles)
      const isCurrentListValid = listTemplates.some((f) => f.path === current.list_template)
      const nextListTemplate = isCurrentListValid && current.list_template
        ? current.list_template
        : (listTemplates[0]?.path || "category_list.html")

      return {
        ...current,
        page_type: pageType,
        cover_template: nextCoverTemplate,
        list_template: nextListTemplate,
        list_path: pageType === "cover" ? current.list_path : (!current.list_path ? "category" : current.list_path),
        list_file_pattern: pageType === "list" && !current.list_file_pattern.includes("{id}") ? "{id}.html" : current.list_file_pattern,
      }
    })
  }

  function templateOptions(current: string, dimension: string) {
    let relevant: ThemeFile[]
    if (dimension === "cover") {
      relevant = getCoverTemplates(templateGroups, templateFiles)
    } else if (dimension === "list") {
      relevant = getListTemplates(templateGroups, templateFiles)
    } else {
      const files = templateGroups.find((group) => group.key === dimension)?.files
      relevant = files && files.length > 0 ? files : templateFiles
    }
    if (relevant.some((file) => file.path === current)) return relevant
    return current
      ? [{ path: current, size: 0, modified_at: "" }, ...relevant]
      : relevant
  }

  function openNew(parentID = 0) {
    const parent = categories.find((category) => category.id === parentID)
    const listPath = defaultListPath(parentID, parent)
    setEditing(null)
    setForm({
      ...emptyCategory,
      parent_id: parentID,
      list_path: listPath,
      list_template: defaultListTemplate(parent, templateGroups, templateFiles),
      cover_template: defaultCoverTemplate(parent, templateGroups, templateFiles),
      detail_path: defaultDetailPath(listPath, parent),
      detail_template: defaultDetailTemplate(listPath, parent),
    })
    setEditorActive(true)
  }

  function openEdit(category: CategoryItem) {
    setEditing(category)
    setForm(categoryInput(category, templateGroups, templateFiles))
    setEditorActive(true)
  }

  function closeEditor() {
    setEditorActive(false)
    setEditing(null)
    setForm(emptyCategory)
  }

  async function save(publish = false) {
    const name = form.name.trim()
    if (!name) return
    setSaving(true)
    setError("")
    try {
      const payload = { ...form, name }
      const result = editing
        ? await updateCategory(editing.id, payload, publish)
        : await createCategory(payload, publish)
      setNotice(publicationMessage(result, "栏目"))
      const nextCategories = await refreshCategories()
      if (editing) {
        const updated = nextCategories.find((category) => category.id === editing.id)
        if (updated) {
          setEditing(updated)
          setForm(categoryInput(updated, templateGroups, templateFiles))
          setEditorActive(true)
        } else {
          closeEditor()
        }
      } else {
        closeEditor()
      }
    } catch (saveError) {
      setError(saveError instanceof Error ? saveError.message : "分类保存失败")
    } finally {
      setSaving(false)
    }
  }

  async function confirmDelete() {
    if (!deleting) return
    setDeleteSaving(true)
    setError("")
    try {
      const result = await deleteCategory(deleting.id, true)
      setNotice(result.publication
        ? (result.publish_started ? "栏目已删除，正在生成网站。" : "栏目已删除并加入发布队列。")
        : "栏目已删除，发布后会更新公开网站。")
      setDeleting(null)
      if (editing?.id === deleting.id) closeEditor()
      await refreshCategories()
    } catch (deleteError) {
      setError(deleteError instanceof Error ? deleteError.message : "分类删除失败")
    } finally {
      setDeleteSaving(false)
    }
  }

  function toggle(categoryID: number) {
    setExpanded((current) => {
      const next = new Set(current)
      if (next.has(categoryID)) next.delete(categoryID)
      else next.add(categoryID)
      return next
    })
  }

  function renderNodes(nodes: CategoryNode[], depth = 0): ReactNode[] {
    return nodes.flatMap((node) => [
      <CategoryRow
        key={node.id}
        node={node}
        depth={depth}
        expanded={expanded.has(node.id)}
        selected={editing?.id === node.id}
        onToggle={() => toggle(node.id)}
        onAddChild={() => {
          setExpanded((current) => new Set(current).add(node.id))
          openNew(node.id)
        }}
        onSelect={() => openEdit(node)}
        onDelete={() => setDeleting(node)}
      />,
      ...(expanded.has(node.id) ? renderNodes(node.children, depth + 1) : []),
    ])
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-center">
        <div>
          <p className="text-sm text-muted-foreground">{categories.length.toLocaleString("zh-CN")} 个栏目，所有内容共用一棵树。</p>
        </div>
        <Button onClick={() => openNew()}><Plus />新增顶级栏目</Button>
      </div>

      {notice && <p role="status" className="text-sm text-muted-foreground">{notice}</p>}
      {error ? <InlineAlert>{error}</InlineAlert> : null}

      <div className="grid gap-4 lg:grid-cols-[minmax(280px,0.85fr)_minmax(0,1.6fr)] lg:items-start">
        <Card className="min-w-0 lg:sticky lg:top-4">
          <CardHeader className="border-b">
            <CardTitle>栏目树</CardTitle>
            <CardDescription>选择栏目后在右侧编辑，子栏目默认收起。</CardDescription>
          </CardHeader>
          <CardContent className="p-0">
            {loading ? (
              <div className="flex h-36 items-center justify-center text-muted-foreground"><LoaderCircle className="size-5 animate-spin" /></div>
            ) : tree.length === 0 ? (
              <div className="flex h-36 flex-col items-center justify-center gap-3 text-sm text-muted-foreground">
                <FolderTree className="size-6" />
                <span>还没有栏目</span>
              </div>
            ) : (
              <ScrollArea className="max-h-[calc(100dvh-16rem)]" orientation="both">
                {renderNodes(tree)}
              </ScrollArea>
            )}
          </CardContent>
        </Card>

        <Card className="min-w-0">
          {editorActive ? (
            <>
              <CardHeader className="border-b">
                <CardTitle>{editing ? `编辑：${editing.name}` : "新增栏目"}</CardTitle>
                <CardDescription>栏目用于归类内容，并配置列表、详情模板与静态路径。</CardDescription>
              </CardHeader>
              <CardContent className="grid gap-5 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="category-name">分类名称</Label>
                  <Input id="category-name" value={form.name} onChange={(event) => update("name", event.target.value)} autoFocus required />
                </div>
                <div className="space-y-2">
                  <Label>父分类</Label>
                  <Select value={String(form.parent_id)} onValueChange={(value) => update("parent_id", Number(value ?? 0))}>
                    <SelectTrigger className="w-full">
                      <SelectValue>
                        {(value) => {
                          const selectedID = Number(value ?? 0)
                          return selectedID > 0
                            ? parentOptions.find((category) => category.id === selectedID)?.name ?? "选择父分类"
                            : "顶级栏目"
                        }}
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="0">顶级栏目</SelectItem>
                      {parentOptions.map((category) => (
                        <SelectItem key={category.id} value={String(category.id)}>
                          <span className="whitespace-pre">{"  ".repeat(category.depth)}{category.name}</span>
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-2">
                  <Label>栏目类型</Label>
                  <Select value={form.page_type} onValueChange={(value) => updatePageType(value ?? "list")}>
                    <SelectTrigger className="w-full">
                      <SelectValue placeholder="选择栏目类型">
                        {(value) => (value === "cover" ? "封面式" : "列表式")}
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="list">列表式</SelectItem>
                      <SelectItem value="cover">封面式</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-2">
                  <Label>所属系统模型</Label>
                  <Select
                    value={String(form.model_id ?? 1)}
                    onValueChange={(value) => update("model_id", Number(value ?? 1))}
                    disabled={Boolean(editing)}
                  >
                    <SelectTrigger className="w-full" disabled={Boolean(editing)}>
                      <SelectValue>
                        {(value) => {
                          const selectedID = Number(value ?? 0)
                          const m = models.find((model) => model.id === selectedID)
                          return m ? `${m.name} (${m.table_name || `表ID ${m.table_id}`})` : "选择系统模型"
                        }}
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      {models.map((model) => (
                        <SelectItem key={model.id} value={String(model.id)}>
                          {model.name} ({model.table_name || `表ID ${model.table_id}`})
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <p className="text-xs text-muted-foreground">
                    {editing
                      ? "已创建栏目的系统模型已锁定不可变更，以保障内容数据表一致性。"
                      : "新建栏目时选择对应数据模型，创建后不可更改。"}
                  </p>
                </div>
                <div className="space-y-2 sm:col-span-2">
                  <Label htmlFor="category-order">排序值</Label>
                  <Input id="category-order" type="number" min="0" value={form.order_id} onChange={(event) => update("order_id", Number(event.target.value))} />
                  <p className="text-xs text-muted-foreground">同一父分类下数值越小越靠前。</p>
                </div>
                {form.page_type === "list" ? <div className="space-y-2 sm:col-span-2">
                  <Label htmlFor="category-page-size">每页内容数</Label>
                  <Input id="category-page-size" type="number" min="1" max="200" value={form.list_page_size} onChange={(event) => update("list_page_size", Number(event.target.value))} />
                  <p className="text-xs text-muted-foreground">栏目列表页按这个数量生成分页。</p>
                </div> : null}
                <div className="space-y-2">
                  <Label htmlFor="category-list-path">
                    {form.page_type === "cover" ? "封面 URL 目录" : "栏目 URL 目录"}
                  </Label>
                  <Input
                    id="category-list-path"
                    value={form.list_path}
                    onChange={(event) => update("list_path", event.target.value)}
                    placeholder={form.page_type === "cover" ? "可留空，表示站点根目录" : "category"}
                    required={form.page_type === "list"}
                  />
                  <p className="text-xs text-muted-foreground">
                    {form.page_type === "cover" ? "封面式栏目允许留空，页面会直接生成在站点根目录。" : "列表页存放的目录路径，如 category。"}
                  </p>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="category-list-file-pattern">
                    {form.page_type === "cover" ? "封面文件名规则" : "列表文件名规则"}
                  </Label>
                  <Input
                    id="category-list-file-pattern"
                    value={form.list_file_pattern}
                    onChange={(event) => update("list_file_pattern", event.target.value)}
                    placeholder={form.page_type === "cover" ? "index.html 或 {id}.html" : "{id}.html"}
                    required
                  />
                  <p className="text-xs text-muted-foreground">
                    {form.page_type === "cover"
                      ? "封面式可填写固定文件名（如 index.html）或包含 {id}。"
                      : "列表式必须包含 {id}，分页会自动追加页码。"}
                  </p>
                </div>
                {form.page_type === "list" ? (
                  <div key="field-list-template" className="space-y-2">
                    <Label>列表模板</Label>
                    <Select
                      key="select-list-template"
                      value={form.list_template}
                      onValueChange={(value) => update("list_template", value ?? "")}
                      disabled={templatesLoading}
                    >
                      <SelectTrigger className="w-full">
                        <SelectValue placeholder="选择列表模板">
                          {(val) => val || "选择列表模板"}
                        </SelectValue>
                      </SelectTrigger>
                      <SelectContent>
                        {templateOptions(form.list_template, "list").map((file) => (
                          <SelectItem key={file.path} value={file.path}>
                            {file.path}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <p className="text-xs text-muted-foreground">此栏目生成列表页时使用的 HTML 模板。</p>
                  </div>
                ) : (
                  <div key="field-cover-template" className="space-y-2">
                    <Label>封面模板</Label>
                    <Select
                      key="select-cover-template"
                      value={form.cover_template}
                      onValueChange={(value) => update("cover_template", value ?? "")}
                      disabled={templatesLoading}
                    >
                      <SelectTrigger className="w-full">
                        <SelectValue placeholder="选择封面模板">
                          {(val) => val || "选择封面模板"}
                        </SelectValue>
                      </SelectTrigger>
                      <SelectContent>
                        {templateOptions(form.cover_template, "cover").map((file) => (
                          <SelectItem key={file.path} value={file.path}>
                            {file.path}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <p className="text-xs text-muted-foreground">此栏目生成封面页时使用的 HTML 模板。</p>
                  </div>
                )}
                <div className="space-y-2">
                  <Label htmlFor="category-detail-path">内容详情目录</Label>
                  <Input id="category-detail-path" value={form.detail_path} onChange={(event) => update("detail_path", event.target.value)} placeholder="content" required />
                  <p className="text-xs text-muted-foreground">详情页按内容所属栏目解析目录。</p>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="category-detail-file-pattern">详情文件名规则</Label>
                  <Input id="category-detail-file-pattern" value={form.detail_file_pattern} onChange={(event) => update("detail_file_pattern", event.target.value)} placeholder="{id}.html" required />
                  <p className="text-xs text-muted-foreground">使用 {"{id}"} 代表内容编号。</p>
                </div>
                <div className="space-y-2">
                  <Label>详情模板</Label>
                  <Select value={form.detail_template} onValueChange={(value) => update("detail_template", value ?? "")} disabled={templatesLoading}>
                    <SelectTrigger className="w-full">
                      <SelectValue placeholder="选择详情模板">
                        {(val) => val || "选择详情模板"}
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      {templateOptions(form.detail_template, "content").map((file) => <SelectItem key={file.path} value={file.path}>{file.path}</SelectItem>)}
                    </SelectContent>
                  </Select>
                  <p className="text-xs text-muted-foreground">此栏目下内容详情页使用的 HTML 模板。</p>
                </div>
              </CardContent>
              <CardFooter className="flex flex-wrap justify-end gap-2 border-t">
                <Button variant="outline" onClick={closeEditor} disabled={saving}>取消</Button>
                <Button onClick={() => save()} disabled={saving || !form.name.trim()}>
                  {saving ? <LoaderCircle className="animate-spin" /> : null}
                  仅保存
                </Button>
                <Button onClick={() => save(true)} disabled={saving || !form.name.trim()}>保存并发布</Button>
              </CardFooter>
            </>
          ) : (
            <CardContent className="flex min-h-[32rem] flex-col items-center justify-center gap-4 text-center">
              <FolderTree className="size-8 text-muted-foreground" />
              <div className="space-y-1">
                <p className="font-medium">选择一个栏目开始编辑</p>
                <p className="text-sm text-muted-foreground">也可以直接创建新的顶级栏目。</p>
              </div>
              <Button onClick={() => openNew()}><Plus />新增顶级栏目</Button>
            </CardContent>
          )}
        </Card>
      </div>

      <ConfirmDialog
        open={Boolean(deleting)}
        onOpenChange={(open) => {
          if (!open) setDeleting(null)
        }}
        title="删除栏目"
        description={
          deleting
            ? `确定删除“${deleting.name}”吗？栏目必须没有子栏目和内容后才能删除。`
            : "确认删除这个栏目吗？"
        }
        confirmLabel="确认删除并发布"
        pending={deleteSaving}
        onConfirm={confirmDelete}
      />
    </div>
  )
}
