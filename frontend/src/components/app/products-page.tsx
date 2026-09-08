import { useEffect, useMemo, useState, type FormEvent } from "react"
import {
  Archive,
  LoaderCircle,
  Package,
  Pencil,
  Plus,
  Search,
} from "lucide-react"

import {
  archiveProduct,
  createProduct,
  getCategories,
  getProducts,
  type CategoryItem,
  type Product,
  type ProductInput,
  updateProduct,
} from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Textarea } from "@/components/ui/textarea"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
  ConfirmDialog,
  IconButton,
  InlineAlert,
  SearchField,
  TablePagination,
} from "@/components/app/app-ui"

const pageSize = 12

const emptyProduct: ProductInput = {
  name: "",
  code: "",
  category_id: 0,
  remark: "",
  content: "",
  small_pic: "",
  big_pic: "",
  keywords: "",
  order_id: 0,
  featured: 0,
  visible: 1,
}

function categoryName(categories: CategoryItem[], categoryID: number) {
  return categories.find((category) => category.id === categoryID)?.name ?? "未分类"
}

function ProductEditor({
  product,
  categories,
  onSave,
  onCancel,
  saving,
}: {
  product: ProductInput
  categories: CategoryItem[]
  onSave: (product: ProductInput, publish?: boolean) => void
  onCancel: () => void
  saving: boolean
}) {
  const [form, setForm] = useState(product)

  function update<K extends keyof ProductInput>(key: K, value: ProductInput[K]) {
    setForm((current) => ({ ...current, [key]: value }))
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>{product.name ? "编辑产品" : "新增产品"}</DialogTitle>
        <DialogDescription>保存内容后，可同时发布到公开网站。</DialogDescription>
      </DialogHeader>
      <div className="grid gap-5 overflow-y-auto py-2 sm:grid-cols-2">
        <div className="space-y-2">
          <Label htmlFor="product-name">产品名称</Label>
          <Input id="product-name" value={form.name} onChange={(event) => update("name", event.target.value)} required />
        </div>
        <div className="space-y-2">
          <Label htmlFor="product-code">产品型号</Label>
          <Input id="product-code" value={form.code} onChange={(event) => update("code", event.target.value)} />
        </div>
        <div className="space-y-2">
          <Label>产品分类</Label>
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
          <Label htmlFor="product-order">排序值</Label>
          <Input id="product-order" type="number" value={form.order_id} onChange={(event) => update("order_id", Number(event.target.value))} />
        </div>
        <div className="space-y-2 sm:col-span-2">
          <Label htmlFor="product-remark">摘要 / SEO 描述</Label>
          <Textarea id="product-remark" value={form.remark} onChange={(event) => update("remark", event.target.value)} className="min-h-20" />
        </div>
        <div className="space-y-2 sm:col-span-2">
          <Label htmlFor="product-content">产品详情</Label>
          <Textarea id="product-content" value={form.content} onChange={(event) => update("content", event.target.value)} className="min-h-36 font-mono text-xs" />
        </div>
        <div className="space-y-2">
          <Label htmlFor="product-small-pic">缩略图地址</Label>
          <Input id="product-small-pic" value={form.small_pic} onChange={(event) => update("small_pic", event.target.value)} placeholder="/images/..." />
        </div>
        <div className="space-y-2">
          <Label htmlFor="product-big-pic">大图地址</Label>
          <Input id="product-big-pic" value={form.big_pic} onChange={(event) => update("big_pic", event.target.value)} placeholder="/images/..." />
        </div>
        <div className="space-y-2 sm:col-span-2">
          <Label htmlFor="product-keywords">关键词</Label>
          <Input id="product-keywords" value={form.keywords} onChange={(event) => update("keywords", event.target.value)} placeholder="用 | 分隔关键词" />
        </div>
        <div className="flex items-center justify-between rounded-lg border p-3 sm:col-span-2">
          <div>
            <p className="text-sm font-medium">公开展示</p>
            <p className="text-xs text-muted-foreground">关闭后产品只保留在后台列表中。</p>
          </div>
          <Switch checked={form.visible === 1} onCheckedChange={(checked) => update("visible", checked ? 1 : 0)} aria-label="公开展示" />
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
        <Button onClick={() => onSave(form)} disabled={saving || !form.name.trim()}>
          {saving ? <LoaderCircle className="animate-spin" /> : null}
          仅保存
        </Button>
        <Button onClick={() => onSave(form, true)} disabled={saving || !form.name.trim()}>保存并发布</Button>
      </DialogFooter>
    </>
  )
}

export function ProductsPage() {
  const [products, setProducts] = useState<Product[]>([])
  const [categories, setCategories] = useState<CategoryItem[]>([])
  const [query, setQuery] = useState("")
  const [appliedQuery, setAppliedQuery] = useState("")
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")
  const [editorOpen, setEditorOpen] = useState(false)
  const [editing, setEditing] = useState<Product | null>(null)
  const [saving, setSaving] = useState(false)
  const [notice,setNotice] = useState("")
  const [archiving, setArchiving] = useState<Product | null>(null)
  const [archiveSaving, setArchiveSaving] = useState(false)

  useEffect(() => {
    getCategories().then(setCategories).catch(() => setCategories([]))
  }, [])

  useEffect(() => {
    let active = true
    getProducts(page, pageSize, appliedQuery)
      .then((response) => {
        if (!active) return
        setProducts(response.items)
        setTotal(response.total)
        setError("")
      })
      .catch((loadError) => {
        if (active) setError(loadError instanceof Error ? loadError.message : "产品加载失败")
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [appliedQuery, page])

  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  const formProduct = useMemo<ProductInput>(() => {
    if (!editing) return emptyProduct
    return {
      name: editing.name,
      code: editing.code,
      category_id: editing.category_id,
      remark: editing.remark,
      content: editing.content,
      small_pic: editing.small_pic,
      big_pic: editing.big_pic,
      keywords: editing.keywords,
      order_id: editing.order_id,
      featured: editing.featured,
      visible: editing.visible,
    }
  }, [editing])

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

  function openNew() {
    setEditing(null)
    setEditorOpen(true)
  }

  function openEdit(product: Product) {
    setEditing(product)
    setEditorOpen(true)
  }

  async function save(product: ProductInput, publish = false) {
    setSaving(true)
    try {
      if (editing) {
        const result = await updateProduct(editing.id, product, publish)
        setNotice(result.publication ? (result.publish_started ? "内容已保存，正在生成网站；请在网站发布中查看结果。" : "内容已保存并加入发布队列，将在当前任务完成后自动生成。") : "内容已保存，发布后会更新公开网站。")
      } else {
        const result = await createProduct(product, publish)
        setNotice(result.publication ? (result.publish_started ? "内容已保存，正在生成网站；请在网站发布中查看结果。" : "内容已保存并加入发布队列，将在当前任务完成后自动生成。") : "内容已保存，发布后会更新公开网站。")
      }
      setEditorOpen(false)
      const targetPage = editing ? page : 1
      if (!editing) setPage(1)
      const response = await getProducts(targetPage, pageSize, appliedQuery)
      setProducts(response.items)
      setTotal(response.total)
    } catch (saveError) {
      setError(saveError instanceof Error ? saveError.message : "保存失败")
    } finally {
      setSaving(false)
    }
  }

  async function confirmArchive() {
    if (!archiving) return
    setArchiveSaving(true)
    try {
      const result = await archiveProduct(archiving.id)
      setProducts((current) => current.map((item) => item.id === archiving.id ? {...item,visible:0} : item))
      setNotice(result.publish_started ? "产品已隐藏，正在发布；完成后会移除公开详情页。" : "产品已隐藏并加入发布队列，将在当前任务完成后自动生成。")
      setArchiving(null)
    } catch (archiveError) {
      setError(archiveError instanceof Error ? archiveError.message : "操作失败")
    } finally {
      setArchiveSaving(false)
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-center">
        <form className="flex w-full max-w-md gap-2" onSubmit={submitSearch}>
          <SearchField value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索名称、型号或关键词" />
          <Button type="submit" variant="outline"><Search />搜索</Button>
        </form>
        <Button onClick={openNew}><Plus />新增产品</Button>
      </div>

      {notice && <p role="status" className="text-sm text-muted-foreground">{notice}</p>}
      {error ? <InlineAlert>{error}</InlineAlert> : null}

      <Card>
        <CardHeader className="border-b">
          <div>
            <CardTitle>产品目录</CardTitle>
            <CardDescription>{total.toLocaleString("zh-CN")} 条记录</CardDescription>
          </div>
        </CardHeader>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-4">产品</TableHead>
                <TableHead>分类</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className="w-24 pr-4 text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading ? (
                <TableRow><TableCell colSpan={4} className="h-28 text-center text-muted-foreground"><LoaderCircle className="mx-auto size-5 animate-spin" /></TableCell></TableRow>
              ) : products.length === 0 ? (
                <TableRow><TableCell colSpan={4} className="h-28 text-center text-muted-foreground">没有匹配的产品</TableCell></TableRow>
              ) : products.map((product) => (
                <TableRow key={product.id}>
                  <TableCell className="max-w-[420px] pl-4">
                    <div className="flex min-w-0 items-center gap-3">
                      <div className="flex size-9 shrink-0 items-center justify-center overflow-hidden rounded-lg bg-muted text-xs text-muted-foreground">
                        {product.small_pic ? <img src={product.small_pic} alt="" className="size-full object-contain" /> : <Package className="size-4" />}
                      </div>
                      <div className="min-w-0">
                        <p className="truncate font-medium">{product.name}</p>
                        <p className="truncate text-xs text-muted-foreground">{product.code || "暂无型号"} · #{product.id}</p>
                      </div>
                    </div>
                  </TableCell>
                  <TableCell className="text-muted-foreground">{categoryName(categories, product.category_id)}</TableCell>
                  <TableCell>
                    <div className="flex gap-1.5">
                      <Badge variant={product.visible ? "default" : "outline"}>{product.visible ? "公开" : "隐藏"}</Badge>
                      {product.featured ? <Badge variant="secondary">推荐</Badge> : null}
                    </div>
                  </TableCell>
                  <TableCell className="pr-4 text-right">
                    <div className="flex justify-end gap-1">
                      <IconButton variant="ghost" size="icon-sm" onClick={() => openEdit(product)} label={`编辑${product.name}`}>
                        <Pencil />
                      </IconButton>
                      {product.visible ? (
                        <IconButton variant="ghost" size="icon-sm" onClick={() => setArchiving(product)} label={`隐藏${product.name}`}>
                          <Archive />
                        </IconButton>
                      ) : null}
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

      <Dialog open={editorOpen} onOpenChange={setEditorOpen}>
        <DialogContent className="max-h-[calc(100dvh-2rem)] sm:max-w-3xl overflow-y-auto">
          <ProductEditor product={formProduct} categories={categories} onSave={save} onCancel={() => setEditorOpen(false)} saving={saving} />
        </DialogContent>
      </Dialog>
      <ConfirmDialog
        open={Boolean(archiving)}
        onOpenChange={(open) => {
          if (!open) setArchiving(null)
        }}
        title="隐藏产品"
        description={
          archiving
            ? `确定隐藏“${archiving.name}”吗？隐藏后它将不再出现在公开目录中。`
            : "确认隐藏这个产品吗？"
        }
        confirmLabel="确认隐藏"
        pending={archiveSaving}
        onConfirm={confirmArchive}
      />
    </div>
  )
}
