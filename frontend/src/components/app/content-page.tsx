import { useEffect, useMemo, useRef, useState, type ChangeEvent, type FormEvent } from "react"
import { FileText, ImagePlus, Library, LoaderCircle, Pencil, Plus, Search, Trash2, Upload, X } from "lucide-react"

import {
  createContent,
  deleteContent,
  getCategories,
  getContent,
  getContentItem,
  uploadMedia,
  type CategoryItem,
  type Content,
  type ContentInput,
  type MediaAsset,
  type SaveResponse,
  updateContent,
} from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Textarea } from "@/components/ui/textarea"
import { ScrollArea } from "@/components/ui/scroll-area"
import { ConfirmDialog, IconButton, InlineAlert, SearchField, TablePagination } from "@/components/app/app-ui"
import { MediaPickerDialog } from "@/components/app/media-picker-dialog"
import { RichTextEditor } from "@/components/app/rich-text-editor"
import { flattenCategoryTree } from "@/lib/category-tree"

const pageSize = 20

const emptyContent: ContentInput = {
  title: "",
  code: "",
  category_id: 0,
  summary: "",
  content: "",
  cover_image: "",
  published_at: "",
  source: "",
  keywords: "",
  description: "",
  order_id: 0,
  featured: 0,
  visible: 1,
}

function categoryName(categories: CategoryItem[], categoryID: number) {
  return categories.find((category) => category.id === categoryID)?.name ?? "未分类"
}

function formatDate(value: string) {
  return value ? value.slice(0, 10).replaceAll("-", "/") : "暂无日期"
}

function dateTimeInput(value: string) {
  const normalized = value.trim().replace(" ", "T")
  return normalized.length >= 16 ? normalized.slice(0, 16) : ""
}

function dateTimeValue(value: string) {
  return value ? `${value.replace("T", " ")}:00` : ""
}

function publicationMessage(result: SaveResponse) {
  if (!result.publication) return "内容已保存，发布后会更新公开网站。"
  return result.publish_started
    ? "内容已保存，正在生成网站；请在网站发布中查看结果。"
    : "内容已保存并加入发布队列，将在当前任务完成后自动生成。"
}

function ContentEditor({
  content,
  categories,
  onSave,
  onCancel,
  saving,
}: {
  content: ContentInput
  categories: CategoryItem[]
  onSave: (content: ContentInput, publish?: boolean) => void
  onCancel: () => void
  saving: boolean
}) {
  const [form, setForm] = useState(content)
  const categoryOptions = flattenCategoryTree(categories)
  const coverInputRef = useRef<HTMLInputElement>(null)
  const [coverUploading, setCoverUploading] = useState(false)
  const [bodyUploading, setBodyUploading] = useState(false)
  const [coverError, setCoverError] = useState("")
  const [mediaPickerOpen, setMediaPickerOpen] = useState(false)
  const uploading = coverUploading || bodyUploading

  function update<K extends keyof ContentInput>(key: K, value: ContentInput[K]) {
    setForm((current) => ({ ...current, [key]: value }))
  }

  function chooseCoverImage() {
    if (!coverUploading) coverInputRef.current?.click()
  }

  async function handleCoverUpload(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0]
    event.target.value = ""
    if (!file) return
    setCoverUploading(true)
    setCoverError("")
    try {
      const result = await uploadMedia(file)
      update("cover_image", result.asset.url)
    } catch (uploadError) {
      setCoverError(uploadError instanceof Error ? uploadError.message : "封面上传失败")
    } finally {
      setCoverUploading(false)
    }
  }

  function selectCover(asset: MediaAsset) {
    setCoverError("")
    update("cover_image", asset.url)
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>{content.title ? "编辑内容" : "新增内容"}</DialogTitle>
        <DialogDescription>内容使用所属分类的列表模板、详情模板和生成路径。</DialogDescription>
      </DialogHeader>
      <div className="grid gap-5 py-2 sm:grid-cols-2">
        <div className="space-y-2 sm:col-span-2">
          <Label htmlFor="content-title">标题</Label>
          <Input id="content-title" value={form.title} onChange={(event) => update("title", event.target.value)} required />
        </div>
        <div className="space-y-2">
          <Label htmlFor="content-code">编号 / 型号</Label>
          <Input id="content-code" value={form.code} onChange={(event) => update("code", event.target.value)} />
        </div>
        <div className="space-y-2">
          <Label>所属分类</Label>
          <Select value={String(form.category_id)} onValueChange={(value) => update("category_id", Number(value ?? 0))}>
            <SelectTrigger className="w-full">
              <SelectValue>
                {(value) => {
                  const selectedID = Number(value ?? 0)
                  return selectedID > 0
                    ? categoryOptions.find((category) => category.id === selectedID)?.name ?? "选择分类"
                    : "未分类"
                }}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="0">未分类</SelectItem>
              {categoryOptions.map((category) => (
                <SelectItem key={category.id} value={String(category.id)}>
                  <span className="whitespace-pre">{"  ".repeat(category.depth)}{category.name}</span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-2">
          <Label htmlFor="content-published-at">发布日期</Label>
          <Input id="content-published-at" type="datetime-local" value={dateTimeInput(form.published_at)} onChange={(event) => update("published_at", dateTimeValue(event.target.value))} />
        </div>
        <div className="space-y-2">
          <Label htmlFor="content-source">来源</Label>
          <Input id="content-source" value={form.source} onChange={(event) => update("source", event.target.value)} />
        </div>
        <div className="space-y-2 sm:col-span-2">
          <Label htmlFor="content-summary">摘要</Label>
          <Textarea id="content-summary" value={form.summary} onChange={(event) => update("summary", event.target.value)} className="min-h-20" />
        </div>
        <div className="space-y-2 sm:col-span-2">
          <Label htmlFor="content-body">正文</Label>
          <RichTextEditor id="content-body" value={form.content} onChange={(value) => update("content", value)} onUploadingChange={setBodyUploading} />
        </div>
        <div className="space-y-3 sm:col-span-2">
          <Label>封面图片</Label>
          <input
            ref={coverInputRef}
            type="file"
            accept="image/jpeg,image/png,image/gif"
            className="hidden"
            onChange={(event) => void handleCoverUpload(event)}
          />
          <div className="overflow-hidden rounded-lg border bg-muted">
            {form.cover_image ? (
              <img src={form.cover_image} alt="内容封面预览" className="aspect-[3/1] max-h-52 w-full object-contain" />
            ) : (
              <div className="flex aspect-[3/1] max-h-52 items-center justify-center gap-2 text-sm text-muted-foreground">
                <ImagePlus className="size-5" />
                暂未选择封面
              </div>
            )}
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Button type="button" variant="outline" onClick={chooseCoverImage} disabled={coverUploading}>
              {coverUploading ? <LoaderCircle className="animate-spin" /> : <Upload />}
              {coverUploading ? "上传中" : "上传封面"}
            </Button>
            <Button type="button" variant="outline" onClick={() => setMediaPickerOpen(true)} disabled={coverUploading}>
              <Library />素材库
            </Button>
            {form.cover_image ? (
              <IconButton label="清除封面" variant="ghost" size="icon-sm" onClick={() => update("cover_image", "")}>
                <X />
              </IconButton>
            ) : null}
          </div>
          {coverError ? <p role="alert" className="text-xs text-destructive">{coverError}</p> : null}
        </div>
        <div className="space-y-2">
          <Label htmlFor="content-order">排序值</Label>
          <Input id="content-order" type="number" min="0" value={form.order_id} onChange={(event) => update("order_id", Number(event.target.value))} />
        </div>
        <div className="space-y-2 sm:col-span-2">
          <Label htmlFor="content-keywords">关键词</Label>
          <Input id="content-keywords" value={form.keywords} onChange={(event) => update("keywords", event.target.value)} placeholder="用 | 分隔关键词" />
        </div>
        <div className="space-y-2 sm:col-span-2">
          <Label htmlFor="content-description">SEO 描述</Label>
          <Textarea id="content-description" value={form.description} onChange={(event) => update("description", event.target.value)} className="min-h-20" />
        </div>
        <div className="flex items-center justify-between rounded-lg border p-3 sm:col-span-2">
          <div>
            <p className="text-sm font-medium">公开展示</p>
            <p className="text-xs text-muted-foreground">关闭后不会生成公开详情和列表链接。</p>
          </div>
          <Switch checked={form.visible === 1} onCheckedChange={(checked) => update("visible", checked ? 1 : 0)} aria-label="公开展示" />
        </div>
        <div className="flex items-center justify-between rounded-lg border p-3 sm:col-span-2">
          <div>
            <p className="text-sm font-medium">首页推荐</p>
            <p className="text-xs text-muted-foreground">提供给主题首页的推荐内容标签使用。</p>
          </div>
          <Switch checked={form.featured === 1} onCheckedChange={(checked) => update("featured", checked ? 1 : 0)} aria-label="首页推荐" />
        </div>
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={onCancel} disabled={saving || uploading}>取消</Button>
        <Button onClick={() => onSave(form)} disabled={saving || uploading || !form.title.trim()}>
          {saving ? <LoaderCircle className="animate-spin" /> : null}
          仅保存
        </Button>
        <Button onClick={() => onSave(form, true)} disabled={saving || uploading || !form.title.trim()}>保存并发布</Button>
      </DialogFooter>
      <MediaPickerDialog open={mediaPickerOpen} onOpenChange={setMediaPickerOpen} onSelect={selectCover} />
    </>
  )
}

export function ContentPage() {
  const [items, setItems] = useState<Content[]>([])
  const [categories, setCategories] = useState<CategoryItem[]>([])
  const [query, setQuery] = useState("")
  const [appliedQuery, setAppliedQuery] = useState("")
  const [categoryID, setCategoryID] = useState(0)
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const [editorOpen, setEditorOpen] = useState(false)
  const [editing, setEditing] = useState<Content | null>(null)
  const [editingDetail, setEditingDetail] = useState<Content | null>(null)
  const [editorLoading, setEditorLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [deleting, setDeleting] = useState<Content | null>(null)
  const [deleteSaving, setDeleteSaving] = useState(false)

  const categoryOptions = useMemo(() => flattenCategoryTree(categories), [categories])

  useEffect(() => {
    getCategories().then(setCategories).catch(() => setCategories([]))
  }, [])

  useEffect(() => {
    let active = true
    getContent(page, pageSize, appliedQuery, categoryID)
      .then((response) => {
        if (!active) return
        setItems(response.items)
        setTotal(response.total)
        setError("")
      })
      .catch((loadError) => {
        if (active) setError(loadError instanceof Error ? loadError.message : "内容加载失败")
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [appliedQuery, categoryID, page])

  const formContent = useMemo<ContentInput>(() => {
    if (!editingDetail) return emptyContent
    return {
      title: editingDetail.title,
      code: editingDetail.code,
      category_id: editingDetail.category_id,
      summary: editingDetail.summary,
      content: editingDetail.content,
      cover_image: editingDetail.cover_image,
      published_at: editingDetail.published_at,
      source: editingDetail.source,
      keywords: editingDetail.keywords,
      description: editingDetail.description,
      order_id: editingDetail.order_id,
      featured: editingDetail.featured,
      visible: editingDetail.visible,
    }
  }, [editingDetail])

  function submitSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const nextQuery = query.trim()
    setLoading(true)
    setPage(1)
    setAppliedQuery(nextQuery)
  }

  function openNew() {
    setEditing(null)
    setEditingDetail(null)
    setEditorOpen(true)
  }

  async function openEdit(item: Content) {
    setEditing(item)
    setEditingDetail(null)
    setEditorOpen(true)
    setEditorLoading(true)
    try {
      setEditingDetail(await getContentItem(item.id))
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : "内容详情加载失败")
      setEditorOpen(false)
    } finally {
      setEditorLoading(false)
    }
  }

  function closeEditor(open: boolean) {
    setEditorOpen(open)
    if (!open) {
      setEditing(null)
      setEditingDetail(null)
    }
  }

  async function save(payload: ContentInput, publish = false) {
    setSaving(true)
    try {
      const result = editing
        ? await updateContent(editing.id, payload, publish)
        : await createContent(payload, publish)
      setNotice(publicationMessage(result))
      closeEditor(false)
      setLoading(true)
      const targetPage = editing ? page : 1
      if (!editing) setPage(1)
      const response = await getContent(targetPage, pageSize, appliedQuery, categoryID)
      setItems(response.items)
      setTotal(response.total)
    } catch (saveError) {
      setError(saveError instanceof Error ? saveError.message : "保存失败")
    } finally {
      setSaving(false)
      setLoading(false)
    }
  }

  async function confirmDelete() {
    if (!deleting) return
    setDeleteSaving(true)
    try {
      const result = await deleteContent(deleting.id, true)
      setItems((current) => current.filter((item) => item.id !== deleting.id))
      setTotal((current) => Math.max(0, current - 1))
      if (items.length === 1 && page > 1) setPage(page - 1)
      setNotice(result.publish_started ? "内容已删除，正在生成网站。" : "内容已删除并加入发布队列。")
      setDeleting(null)
    } catch (deleteError) {
      setError(deleteError instanceof Error ? deleteError.message : "删除失败")
    } finally {
      setDeleteSaving(false)
    }
  }

  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  return (
    <div className="space-y-4 lg:flex lg:h-full lg:min-h-0 lg:flex-col lg:space-y-0 lg:gap-4">
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-center">
        <div className="flex w-full flex-col gap-2 sm:flex-row">
          <form className="flex w-full max-w-md gap-2" onSubmit={submitSearch}>
            <SearchField value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索标题、编号或关键词" />
            <Button type="submit" variant="outline"><Search />搜索</Button>
          </form>
          <Select
            value={String(categoryID)}
            onValueChange={(value) => {
              setLoading(true)
              setPage(1)
              setCategoryID(Number(value ?? 0))
            }}
          >
            <SelectTrigger className="w-full sm:w-64" aria-label="按分类筛选">
              <SelectValue>
                {(value) => {
                  const selectedID = Number(value ?? 0)
                  return selectedID > 0
                    ? categoryOptions.find((category) => category.id === selectedID)?.name ?? "全部分类"
                    : "全部分类"
                }}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="0">全部分类</SelectItem>
              {categoryOptions.map((category) => (
                <SelectItem key={category.id} value={String(category.id)}>
                  <span className="whitespace-pre">{"  ".repeat(category.depth)}{category.name}</span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <Button onClick={openNew}><Plus />新增内容</Button>
      </div>
      {notice && <p role="status" className="text-sm text-muted-foreground">{notice}</p>}
      {error ? <InlineAlert>{error}</InlineAlert> : null}
      <div className="border-y lg:flex lg:min-h-0 lg:flex-1 lg:flex-col lg:overflow-hidden lg:border">
        <ScrollArea className="lg:h-0 lg:min-h-0 lg:flex-1">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-4">标题</TableHead>
                <TableHead>分类</TableHead>
                <TableHead>发布日期</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className="pr-4 text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading ? (
                <TableRow><TableCell colSpan={5} className="h-28 text-center text-muted-foreground"><LoaderCircle className="mx-auto size-5 animate-spin" /></TableCell></TableRow>
              ) : items.length === 0 ? (
                <TableRow><TableCell colSpan={5} className="h-28 text-center text-muted-foreground">没有匹配的内容</TableCell></TableRow>
              ) : items.map((item) => (
                <TableRow key={item.id}>
                  <TableCell className="max-w-[520px] pl-4">
                    <div className="flex min-w-0 items-center gap-3">
                      <div className="flex size-9 shrink-0 items-center justify-center overflow-hidden rounded-lg bg-muted text-muted-foreground">
                        {item.cover_image ? <img src={item.cover_image} alt="" className="size-full object-contain" /> : <FileText className="size-4" />}
                      </div>
                      <div className="min-w-0">
                        <p className="truncate font-medium">{item.title}</p>
                        <p className="truncate text-xs text-muted-foreground">{item.code || "无编号"} · #{item.id}</p>
                      </div>
                    </div>
                  </TableCell>
                  <TableCell className="text-muted-foreground">{categoryName(categories, item.category_id)}</TableCell>
                  <TableCell className="text-muted-foreground">{formatDate(item.published_at)}</TableCell>
                  <TableCell>
                    <div className="flex gap-1.5 text-xs">
                      <span>{item.visible ? "公开" : "隐藏"}</span>
                      {item.featured ? <span className="text-muted-foreground">推荐</span> : null}
                    </div>
                  </TableCell>
                  <TableCell className="pr-4 text-right">
                    <div className="flex justify-end gap-1">
                      <IconButton variant="ghost" size="icon-sm" onClick={() => openEdit(item)} label={`编辑${item.title}`}><Pencil /></IconButton>
                      <IconButton variant="ghost" size="icon-sm" className="text-destructive hover:text-destructive" onClick={() => setDeleting(item)} label={`删除${item.title}`}><Trash2 /></IconButton>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </ScrollArea>
        <TablePagination page={page} totalPages={totalPages} total={total} pageSize={pageSize} loading={loading} onPageChange={(nextPage) => { setLoading(true); setPage(nextPage) }} />
      </div>

      <Dialog open={editorOpen} onOpenChange={closeEditor}>
        <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-hidden sm:max-w-5xl">
          <ScrollArea className="max-h-[calc(100dvh-4rem)]" contentClassName="space-y-4 pr-2">
            {editorLoading ? (
              <><DialogHeader><DialogTitle>编辑内容</DialogTitle><DialogDescription>正在加载内容。</DialogDescription></DialogHeader><div className="flex h-32 items-center justify-center text-muted-foreground"><LoaderCircle className="size-5 animate-spin" /></div></>
            ) : <ContentEditor key={editing?.id ?? "new"} content={formContent} categories={categories} onSave={save} onCancel={() => closeEditor(false)} saving={saving} />}
          </ScrollArea>
        </DialogContent>
      </Dialog>
      <ConfirmDialog
        open={Boolean(deleting)}
        onOpenChange={(open) => { if (!open) setDeleting(null) }}
        title="删除内容"
        description={deleting ? `确定删除“${deleting.title}”吗？删除后公开详情页和列表链接都会移除。` : "确认删除这条内容吗？"}
        confirmLabel="确认删除"
        pending={deleteSaving}
        onConfirm={confirmDelete}
      />
    </div>
  )
}
