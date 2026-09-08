import { useEffect, useState, type FormEvent } from "react"
import { LoaderCircle, Pencil, Search, Trash2 } from "lucide-react"

import {
  getNews,
  getNewsCategories,
  getNewsItem,
  deleteNews,
  updateNews,
  type CategoryItem,
  type NewsDetail,
  type NewsInput,
  type NewsItem,
  type SaveResponse,
} from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Textarea } from "@/components/ui/textarea"
import { ConfirmDialog, IconButton, InlineAlert, SearchField, TablePagination } from "@/components/app/app-ui"
import { RichTextEditor } from "@/components/app/rich-text-editor"

const pageSize = 12

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

function categoryName(categories: CategoryItem[], categoryID: number) {
  return categories.find((category) => category.id === categoryID)?.name ?? `分类 #${categoryID}`
}

function publicationMessage(result: SaveResponse) {
  if (!result.publication) return "内容已保存，发布后会更新公开网站。"
  return result.publish_started
    ? "内容已保存，正在生成网站；请在网站发布中查看结果。"
    : "内容已保存并加入发布队列，将在当前任务完成后自动生成。"
}

function NewsEditor({
  news,
  categories,
  onSave,
  onCancel,
  saving,
}: {
  news: NewsDetail
  categories: CategoryItem[]
  onSave: (news: NewsInput, publish?: boolean) => void
  onCancel: () => void
  saving: boolean
}) {
  const [form, setForm] = useState<NewsInput>({
    title: news.title,
    category_id: news.category_id,
    published_at: news.published_at,
    picture: news.picture,
    featured: news.featured,
    content: news.content,
    source: news.source,
    keywords: news.keywords,
    description: news.description,
  })

  function update<K extends keyof NewsInput>(key: K, value: NewsInput[K]) {
    setForm((current) => ({ ...current, [key]: value }))
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>编辑新闻</DialogTitle>
        <DialogDescription>保存内容后，可同时发布到公开网站。</DialogDescription>
      </DialogHeader>
      <div className="grid gap-5 overflow-y-auto py-2 sm:grid-cols-2">
        <div className="space-y-2 sm:col-span-2">
          <Label htmlFor="news-title">新闻标题</Label>
          <Input id="news-title" value={form.title} onChange={(event) => update("title", event.target.value)} required />
        </div>
        <div className="space-y-2">
          <Label>新闻分类</Label>
          <Select value={String(form.category_id)} onValueChange={(value) => update("category_id", Number(value ?? 0))}>
            <SelectTrigger className="w-full"><SelectValue placeholder="选择分类" /></SelectTrigger>
            <SelectContent>
              <SelectItem value="0">未分类</SelectItem>
              {categories.map((category) => (
                <SelectItem key={category.id} value={String(category.id)}>
                  {category.parent_id ? `　${category.name}` : category.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-2">
          <Label htmlFor="news-published-at">发布日期</Label>
          <Input
            id="news-published-at"
            type="datetime-local"
            value={dateTimeInput(form.published_at)}
            onChange={(event) => update("published_at", dateTimeValue(event.target.value))}
          />
        </div>
        <div className="space-y-2">
          <Label htmlFor="news-source">文章来源</Label>
          <Input id="news-source" value={form.source} onChange={(event) => update("source", event.target.value)} />
        </div>
        <div className="space-y-2">
          <Label htmlFor="news-picture">图片地址</Label>
          <Input id="news-picture" value={form.picture} onChange={(event) => update("picture", event.target.value)} placeholder="/images/..." />
        </div>
        <div className="space-y-2 sm:col-span-2">
          <Label htmlFor="news-keywords">关键词</Label>
          <Input id="news-keywords" value={form.keywords} onChange={(event) => update("keywords", event.target.value)} placeholder="用 | 分隔关键词" />
        </div>
        <div className="space-y-2 sm:col-span-2">
          <Label htmlFor="news-description">摘要 / SEO 描述</Label>
          <Textarea id="news-description" value={form.description} onChange={(event) => update("description", event.target.value)} className="min-h-20" />
        </div>
        <div className="space-y-2 sm:col-span-2">
          <Label htmlFor="news-content">新闻正文</Label>
          <RichTextEditor id="news-content" value={form.content} onChange={(value) => update("content", value)} />
        </div>
        <div className="flex items-center justify-between rounded-lg border p-3 sm:col-span-2">
          <div>
            <p className="text-sm font-medium">首页推荐</p>
            <p className="text-xs text-muted-foreground">控制静态首页模板中的推荐标记。</p>
          </div>
          <Switch checked={form.featured === 1} onCheckedChange={(checked) => update("featured", checked ? 1 : 0)} aria-label="首页推荐" />
        </div>
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={onCancel} disabled={saving}>取消</Button>
        <Button onClick={() => onSave(form)} disabled={saving || !form.title.trim()}>
          {saving ? <LoaderCircle className="animate-spin" /> : null}
          仅保存
        </Button>
        <Button onClick={() => onSave(form, true)} disabled={saving || !form.title.trim()}>保存并发布</Button>
      </DialogFooter>
    </>
  )
}

export function NewsPage() {
  const [items, setItems] = useState<NewsItem[]>([])
  const [categories, setCategories] = useState<CategoryItem[]>([])
  const [query, setQuery] = useState("")
  const [appliedQuery, setAppliedQuery] = useState("")
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const [editorOpen, setEditorOpen] = useState(false)
  const [editing, setEditing] = useState<NewsItem | null>(null)
  const [editingDetail, setEditingDetail] = useState<NewsDetail | null>(null)
  const [editorLoading, setEditorLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [deleting, setDeleting] = useState<NewsItem | null>(null)
  const [deleteSaving, setDeleteSaving] = useState(false)

  useEffect(() => {
    getNewsCategories().then(setCategories).catch(() => setCategories([]))
  }, [])

  useEffect(() => {
    let active = true
    getNews(page, pageSize, appliedQuery)
      .then((response) => {
        if (!active) return
        setItems(response.items)
        setTotal(response.total)
        setError("")
      })
      .catch((loadError) => {
        if (active) setError(loadError instanceof Error ? loadError.message : "新闻加载失败")
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [appliedQuery, page])

  function submitSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const nextQuery = query.trim()
    if (page === 1 && appliedQuery === nextQuery) return
    setLoading(true)
    setPage(1)
    setAppliedQuery(nextQuery)
  }

  function changePage(nextPage: number) {
    setLoading(true)
    setPage(nextPage)
  }

  async function openEdit(item: NewsItem) {
    setEditing(item)
    setEditingDetail(null)
    setEditorOpen(true)
    setEditorLoading(true)
    try {
      setEditingDetail(await getNewsItem(item.id))
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : "新闻详情加载失败")
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

  async function save(news: NewsInput, publish = false) {
    if (!editing) return
    const newsID = editing.id
    setSaving(true)
    try {
      const result = await updateNews(newsID, news, publish)
      setItems((current) => current.map((item) => item.id === newsID
        ? { ...item, title: news.title, category_id: news.category_id, published_at: news.published_at, picture: news.picture, featured: news.featured }
        : item))
      setNotice(publicationMessage(result))
      closeEditor(false)
    } catch (saveError) {
      setError(saveError instanceof Error ? saveError.message : "保存失败")
    } finally {
      setSaving(false)
    }
  }

  async function confirmDelete() {
    if (!deleting) return
    setDeleteSaving(true)
    try {
      const result = await deleteNews(deleting.id)
      setItems((current) => current.filter((item) => item.id !== deleting.id))
      setTotal((current) => Math.max(0, current - 1))
      if (items.length === 1 && page > 1) setPage(page - 1)
      setNotice(result.publish_started ? "新闻已删除，正在发布；完成后会移除公开详情页。" : "新闻已删除并加入发布队列。")
      setDeleting(null)
    } catch (deleteError) {
      setError(deleteError instanceof Error ? deleteError.message : "删除失败")
    } finally {
      setDeleteSaving(false)
    }
  }

  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  return (
    <div className="space-y-4">
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-center">
        <form className="flex w-full max-w-md gap-2" onSubmit={submitSearch}>
          <SearchField value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索新闻标题" />
          <Button type="submit" variant="outline"><Search />搜索</Button>
        </form>
      </div>
      {notice && <p role="status" className="text-sm text-muted-foreground">{notice}</p>}
      {error ? <InlineAlert>{error}</InlineAlert> : null}
      <Card>
        <CardHeader className="border-b">
          <div>
            <CardTitle>新闻列表</CardTitle>
            <CardDescription>{total.toLocaleString("zh-CN")} 条记录</CardDescription>
          </div>
        </CardHeader>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-4">标题</TableHead>
                <TableHead>发布日期</TableHead>
                <TableHead>推荐</TableHead>
                <TableHead className="pr-4 text-right">编号</TableHead>
                <TableHead className="w-28 pr-4 text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading ? (
                <TableRow><TableCell colSpan={5} className="h-28 text-center text-muted-foreground"><LoaderCircle className="mx-auto size-5 animate-spin" /></TableCell></TableRow>
              ) : items.length === 0 ? (
                <TableRow><TableCell colSpan={5} className="h-28 text-center text-muted-foreground">没有匹配的新闻</TableCell></TableRow>
              ) : items.map((item) => (
                <TableRow key={item.id}>
                  <TableCell className="max-w-[620px] pl-4">
                    <p className="truncate font-medium">{item.title}</p>
                    <p className="mt-1 text-xs text-muted-foreground">{categoryName(categories, item.category_id)}</p>
                  </TableCell>
                  <TableCell className="text-muted-foreground">{formatDate(item.published_at)}</TableCell>
                  <TableCell>{item.featured ? "首页推荐" : "普通"}</TableCell>
                  <TableCell className="pr-4 text-right text-muted-foreground">#{item.id}</TableCell>
                  <TableCell className="pr-4 text-right">
                    <div className="flex justify-end gap-1">
                      <IconButton variant="ghost" size="icon-sm" onClick={() => openEdit(item)} label={`编辑${item.title}`}>
                        <Pencil />
                      </IconButton>
                      <IconButton
                        variant="ghost"
                        size="icon-sm"
                        className="text-destructive hover:text-destructive"
                        onClick={() => setDeleting(item)}
                        label={`删除${item.title}`}
                      >
                        <Trash2 />
                      </IconButton>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          <TablePagination
            page={page}
            totalPages={totalPages}
            total={total}
            pageSize={pageSize}
            loading={loading}
            onPageChange={changePage}
          />
        </CardContent>
      </Card>

      <Dialog open={editorOpen} onOpenChange={closeEditor}>
        <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-5xl">
          {editorLoading || !editingDetail ? (
            <>
              <DialogHeader>
                <DialogTitle>编辑新闻</DialogTitle>
                <DialogDescription>正在加载新闻内容。</DialogDescription>
              </DialogHeader>
              <div className="flex h-32 items-center justify-center text-muted-foreground"><LoaderCircle className="size-5 animate-spin" /></div>
            </>
          ) : (
            <NewsEditor
              key={editingDetail.id}
              news={editingDetail}
              categories={categories}
              onSave={save}
              onCancel={() => closeEditor(false)}
              saving={saving}
            />
          )}
        </DialogContent>
      </Dialog>
      <ConfirmDialog
        open={Boolean(deleting)}
        onOpenChange={(open) => {
          if (!open) setDeleting(null)
        }}
        title="删除新闻"
        description={
          deleting
            ? `确定删除“${deleting.title}”吗？删除后新闻内容和公开详情页都将移除。`
            : "确认删除这条新闻吗？"
        }
        confirmLabel="确认删除"
        pending={deleteSaving}
        onConfirm={confirmDelete}
      />
    </div>
  )
}
