import { useEffect, useMemo, useState, type ReactNode } from "react"
import {
  ChevronDown,
  ChevronRight,
  FolderTree,
  LoaderCircle,
  Pencil,
  Plus,
  Trash2,
} from "lucide-react"

import {
  createCategory,
  deleteCategory,
  getCategories,
  updateCategory,
  type CategoryInput,
  type CategoryItem,
  type SaveResponse,
} from "@/lib/api"
import { buildCategoryTree, flattenCategoryTree, type CategoryNode } from "@/lib/category-tree"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { ConfirmDialog, IconButton, InlineAlert } from "@/components/app/app-ui"

const emptyCategory: CategoryInput = {
  name: "",
  parent_id: 0,
  order_id: 0,
  list_path: "valve",
  list_file_pattern: "{id}.html",
  detail_path: "Product",
  detail_file_pattern: "{id}.html",
}

function defaultListPath(parentID: number) {
  return parentID === 0 ? "valve" : "Products"
}

function publicationMessage(result: SaveResponse, action: string) {
  if (!result.publication) return `${action}已保存，发布后会更新公开网站。`
  return result.publish_started
    ? `${action}已保存，正在生成网站；请在网站发布中查看结果。`
    : `${action}已保存并加入发布队列，将在当前任务完成后自动生成。`
}

type CategoryRowProps = {
  node: CategoryNode
  depth: number
  expanded: boolean
  onToggle: () => void
  onAddChild: () => void
  onEdit: () => void
  onDelete: () => void
}

function CategoryRow({
  node,
  depth,
  expanded,
  onToggle,
  onAddChild,
  onEdit,
  onDelete,
}: CategoryRowProps) {
  const hasChildren = node.children.length > 0

  return (
    <div
      className="flex min-h-14 items-center gap-2 border-b px-3 py-2 last:border-b-0"
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
      <FolderTree className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-medium">{node.name}</p>
        <p className="truncate text-xs text-muted-foreground">
          {node.product_count} 个直接产品 · 排序 {node.order_id} · #{node.id}
        </p>
      </div>
      <div className="flex shrink-0 items-center gap-1">
        <IconButton variant="ghost" size="icon-sm" label={`在${node.name}下新增子分类`} onClick={onAddChild}>
          <Plus />
        </IconButton>
        <IconButton variant="ghost" size="icon-sm" label={`编辑${node.name}`} onClick={onEdit}>
          <Pencil />
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
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const [expanded, setExpanded] = useState<Set<number>>(new Set())
  const [editorOpen, setEditorOpen] = useState(false)
  const [editing, setEditing] = useState<CategoryItem | null>(null)
  const [form, setForm] = useState<CategoryInput>(emptyCategory)
  const [saving, setSaving] = useState(false)
  const [deleting, setDeleting] = useState<CategoryItem | null>(null)
  const [deleteSaving, setDeleteSaving] = useState(false)

  const tree = useMemo(() => buildCategoryTree(categories), [categories])
  const parentOptions = useMemo(
    () => flattenCategoryTree(categories, editing?.id),
    [categories, editing?.id],
  )

  async function refreshCategories() {
    const nextCategories = await getCategories()
    setCategories(nextCategories)
    setExpanded((current) => {
      if (current.size > 0) return current
      return new Set(buildCategoryTree(nextCategories).map((category) => category.id))
    })
  }

  useEffect(() => {
    let active = true
    getCategories()
      .then((nextCategories) => {
        if (!active) return
        setCategories(nextCategories)
        setExpanded(new Set(buildCategoryTree(nextCategories).map((category) => category.id)))
        setError("")
      })
      .catch((loadError) => {
        if (active) setError(loadError instanceof Error ? loadError.message : "分类加载失败")
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [])

  function update<K extends keyof CategoryInput>(key: K, value: CategoryInput[K]) {
    setForm((current) => ({ ...current, [key]: value }))
  }

  function openNew(parentID = 0) {
    setEditing(null)
    setForm({ ...emptyCategory, parent_id: parentID, list_path: defaultListPath(parentID) })
    setEditorOpen(true)
  }

  function openEdit(category: CategoryItem) {
    setEditing(category)
    setForm({
      name: category.name,
      parent_id: category.parent_id,
      order_id: category.order_id,
      list_path: category.list_path,
      list_file_pattern: category.list_file_pattern,
      detail_path: category.detail_path,
      detail_file_pattern: category.detail_file_pattern,
    })
    setEditorOpen(true)
  }

  function closeEditor(open: boolean) {
    setEditorOpen(open)
    if (!open) setEditing(null)
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
      setNotice(publicationMessage(result, "分类"))
      closeEditor(false)
      await refreshCategories()
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
        ? (result.publish_started ? "分类已删除，正在生成网站。" : "分类已删除并加入发布队列。")
        : "分类已删除，发布后会更新公开网站。")
      setDeleting(null)
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
        onToggle={() => toggle(node.id)}
        onAddChild={() => {
          setExpanded((current) => new Set(current).add(node.id))
          openNew(node.id)
        }}
        onEdit={() => openEdit(node)}
        onDelete={() => setDeleting(node)}
      />,
      ...(expanded.has(node.id) ? renderNodes(node.children, depth + 1) : []),
    ])
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-center">
        <div>
          <p className="text-sm text-muted-foreground">{categories.length.toLocaleString("zh-CN")} 个分类，支持多级产品目录。</p>
        </div>
        <Button onClick={() => openNew()}><Plus />新增顶级分类</Button>
      </div>

      {notice && <p role="status" className="text-sm text-muted-foreground">{notice}</p>}
      {error ? <InlineAlert>{error}</InlineAlert> : null}

      <Card>
        <CardHeader className="border-b">
          <CardTitle>产品分类树</CardTitle>
          <CardDescription>分类用于产品归类，并决定公开网站中的产品分类页面。</CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          {loading ? (
            <div className="flex h-36 items-center justify-center text-muted-foreground"><LoaderCircle className="size-5 animate-spin" /></div>
          ) : tree.length === 0 ? (
            <div className="flex h-36 flex-col items-center justify-center gap-3 text-sm text-muted-foreground">
              <FolderTree className="size-6" />
              <span>还没有产品分类</span>
            </div>
          ) : (
            <div className="max-h-[calc(100dvh-16rem)] overflow-auto">
              {renderNodes(tree)}
            </div>
          )}
        </CardContent>
      </Card>

      <Dialog open={editorOpen} onOpenChange={closeEditor}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{editing ? "编辑产品分类" : "新增产品分类"}</DialogTitle>
            <DialogDescription>分类保存后，可以选择立即重新生成公开网站。</DialogDescription>
          </DialogHeader>
          <div className="grid gap-5 py-2 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="category-name">分类名称</Label>
              <Input id="category-name" value={form.name} onChange={(event) => update("name", event.target.value)} autoFocus required />
            </div>
            <div className="space-y-2">
              <Label>父分类</Label>
              <Select value={String(form.parent_id)} onValueChange={(value) => update("parent_id", Number(value ?? 0))}>
                <SelectTrigger className="w-full"><SelectValue placeholder="选择父分类" /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="0">顶级分类</SelectItem>
                  {parentOptions.map((category) => (
                    <SelectItem key={category.id} value={String(category.id)}>
                      <span className="whitespace-pre">{"  ".repeat(category.depth)}{category.name}</span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2 sm:col-span-2">
              <Label htmlFor="category-order">排序值</Label>
              <Input id="category-order" type="number" min="0" value={form.order_id} onChange={(event) => update("order_id", Number(event.target.value))} />
              <p className="text-xs text-muted-foreground">同一父分类下数值越小越靠前。</p>
            </div>
            <div className="space-y-2">
              <Label htmlFor="category-list-path">分类列表目录</Label>
              <Input id="category-list-path" value={form.list_path} onChange={(event) => update("list_path", event.target.value)} placeholder="Products" required />
              <p className="text-xs text-muted-foreground">当前线上顶级分类为 valve，子分类为 Products。</p>
            </div>
            <div className="space-y-2">
              <Label htmlFor="category-list-file-pattern">分类列表文件名规则</Label>
              <Input id="category-list-file-pattern" value={form.list_file_pattern} onChange={(event) => update("list_file_pattern", event.target.value)} placeholder="{id}.html" required />
              <p className="text-xs text-muted-foreground">使用 {"{id}"}，分页会自动追加页码。</p>
            </div>
            <div className="space-y-2">
              <Label htmlFor="category-detail-path">产品详情目录</Label>
              <Input id="category-detail-path" value={form.detail_path} onChange={(event) => update("detail_path", event.target.value)} placeholder="Product" required />
              <p className="text-xs text-muted-foreground">当前线上详情目录为 Product。</p>
            </div>
            <div className="space-y-2">
              <Label htmlFor="category-detail-file-pattern">产品详情文件名规则</Label>
              <Input id="category-detail-file-pattern" value={form.detail_file_pattern} onChange={(event) => update("detail_file_pattern", event.target.value)} placeholder="{id}.html" required />
              <p className="text-xs text-muted-foreground">使用 {"{id}"} 代表产品 ID。</p>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => closeEditor(false)} disabled={saving}>取消</Button>
            <Button onClick={() => save()} disabled={saving || !form.name.trim()}>
              {saving ? <LoaderCircle className="animate-spin" /> : null}
              仅保存
            </Button>
            <Button onClick={() => save(true)} disabled={saving || !form.name.trim()}>保存并发布</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={Boolean(deleting)}
        onOpenChange={(open) => {
          if (!open) setDeleting(null)
        }}
        title="删除产品分类"
        description={
          deleting
            ? `确定删除“${deleting.name}”吗？分类必须没有子分类和产品后才能删除。`
            : "确认删除这个分类吗？"
        }
        confirmLabel="确认删除并发布"
        pending={deleteSaving}
        onConfirm={confirmDelete}
      />
    </div>
  )
}
