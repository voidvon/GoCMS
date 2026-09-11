import { useEffect, useRef, useState, type FormEvent } from "react"
import { Copy, ImageIcon, LoaderCircle, RefreshCw, Trash2, Upload } from "lucide-react"

import { deleteMedia, getMedia, getMediaItem, uploadMedia, type MediaAsset, type MediaDetail } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { ConfirmDialog, InlineAlert, SearchField, TablePagination } from "@/components/app/app-ui"

const pageSize = 20

function fileSize(bytes: number) {
  return bytes < 1024 * 1024 ? `${(bytes / 1024).toFixed(1)} KB` : `${(bytes / 1024 / 1024).toFixed(1)} MB`
}

function uploadDate(value: string) {
  const date = new Date(value.includes("T") ? value : `${value.replace(" ", "T")}Z`)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString("zh-CN")
}

function MediaDetails({ asset, onClose, onDeleted }: {
  asset: MediaAsset
  onClose: () => void
  onDeleted: () => void
}) {
  const [detail, setDetail] = useState<MediaDetail | null>(null)
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const [retry, setRetry] = useState(0)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [deleting, setDeleting] = useState(false)

  useEffect(() => {
    let active = true
    getMediaItem(asset.id).then((result) => {
      if (active) setDetail(result)
    }).catch((err) => {
      if (active) setError(err instanceof Error ? err.message : "附件详情加载失败")
    })
    return () => { active = false }
  }, [asset.id, retry])

  async function copyURL() {
    try {
      await navigator.clipboard.writeText(new URL(asset.url, window.location.origin).href)
      setNotice("链接已复制")
    } catch {
      setError("复制失败，请手动复制下方地址")
    }
  }

  async function remove() {
    setDeleting(true)
    setError("")
    try {
      await deleteMedia(asset.id)
      onDeleted()
    } catch (err) {
      setError(err instanceof Error ? err.message : "删除附件失败")
      setConfirmOpen(false)
      setDetail(null)
      setRetry((value) => value + 1)
    } finally {
      setDeleting(false)
    }
  }

  return (
    <Dialog open onOpenChange={(open) => { if (!open && !deleting) onClose() }}>
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="break-all pr-6">{asset.original_name || "未命名图片"}</DialogTitle>
          <DialogDescription>附件详情与内容引用</DialogDescription>
        </DialogHeader>
        <div className="flex min-h-40 items-center justify-center rounded-lg border bg-muted/40 p-3">
          <img src={asset.url} alt={asset.original_name} className="max-h-72 max-w-full object-contain" />
        </div>
        {error ? <InlineAlert>{error}</InlineAlert> : null}
        {notice ? <p role="status" className="text-sm text-muted-foreground">{notice}</p> : null}
        <dl className="grid grid-cols-2 gap-3 text-sm">
          <div><dt className="text-muted-foreground">格式 / 大小</dt><dd>{asset.mime_type} · {fileSize(asset.size_bytes)}</dd></div>
          <div><dt className="text-muted-foreground">尺寸</dt><dd>{asset.width} × {asset.height}</dd></div>
          <div><dt className="text-muted-foreground">上传人</dt><dd className="break-all">{asset.uploaded_by || "未知"}</dd></div>
          <div><dt className="text-muted-foreground">上传时间</dt><dd>{uploadDate(asset.created_at)}</dd></div>
        </dl>
        <div className="space-y-2">
          <p className="text-sm font-medium">访问地址</p>
          <p className="select-all break-all rounded-md bg-muted p-2 text-xs">{new URL(asset.url, window.location.origin).href}</p>
          <Button variant="outline" size="sm" onClick={() => void copyURL()}><Copy />复制链接</Button>
        </div>
        <div className="space-y-2 border-t pt-4">
          <p className="text-sm font-medium">内容引用{detail ? `（${detail.references.length} 处）` : ""}</p>
          <p className="text-xs text-muted-foreground">记录正文图片和封面的引用；无记录不代表全站未使用。</p>
          {!detail ? (
            error ? <Button variant="outline" onClick={() => { setError(""); setRetry((value) => value + 1) }}>重试加载详情</Button>
              : <p role="status" className="text-sm text-muted-foreground">正在加载引用…</p>
          ) : detail.references.length === 0 ? (
            <p className="text-sm text-muted-foreground">暂无内容引用记录</p>
          ) : (
            <ul className="max-h-48 space-y-2 overflow-y-auto text-sm">
              {detail.references.map((reference) => (
                <li key={`${reference.content_id}:${reference.field_name}`} className="flex items-start justify-between gap-3 rounded-md border p-2">
                  <a className="min-w-0 break-all underline underline-offset-4" href={`/admin/content?edit=${reference.content_id}`}>
                    {reference.title || `内容 #${reference.content_id}`}
                  </a>
                  <span className="shrink-0 text-muted-foreground">{reference.field_name === "body" ? "正文" : reference.field_name === "cover_image" ? "封面" : reference.field_name}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
        <div className="flex justify-end gap-2 border-t pt-4">
          <Button variant="outline" disabled={deleting} onClick={onClose}>关闭</Button>
          <Button variant="destructive" disabled={!detail || detail.references.length > 0 || deleting} onClick={() => setConfirmOpen(true)}>
            <Trash2 />{detail && detail.references.length > 0 ? "内容引用中，不能删除" : "删除附件"}
          </Button>
        </div>
        <ConfirmDialog open={confirmOpen} onOpenChange={(open) => { if (!deleting) setConfirmOpen(open) }}
          title="删除附件？" description="将永久删除该图片文件和附件记录。请确认它未被其他页面或外部链接使用。"
          confirmLabel="确认删除" pending={deleting} onConfirm={() => void remove()} />
      </DialogContent>
    </Dialog>
  )
}

export function MediaPage() {
  const [assets, setAssets] = useState<MediaAsset[]>([])
  const [search, setSearch] = useState("")
  const [appliedSearch, setAppliedSearch] = useState("")
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(0)
  const [revision, setRevision] = useState(0)
  const [loadedKey, setLoadedKey] = useState("")
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const [uploading, setUploading] = useState(false)
  const [selected, setSelected] = useState<MediaAsset | null>(null)
  const fileInput = useRef<HTMLInputElement>(null)
  const requestKey = JSON.stringify([page, appliedSearch, revision])
  const loading = loadedKey !== requestKey

  useEffect(() => {
    let active = true
    getMedia(page, pageSize, appliedSearch).then((response) => {
      if (!active) return
      const lastPage = Math.max(1, Math.ceil(response.total / pageSize))
      if (page > lastPage) { setPage(lastPage); return }
      setAssets(response.items)
      setTotal(response.total)
      setError("")
      setLoadedKey(requestKey)
    }).catch((err) => {
      if (!active) return
      setAssets([])
      setError(err instanceof Error ? err.message : "附件加载失败")
      setLoadedKey(requestKey)
    })
    return () => { active = false }
  }, [page, appliedSearch, requestKey])

  function submitSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setPage(1)
    setAppliedSearch(search.trim())
    setRevision((value) => value + 1)
  }

  async function upload(file: File) {
    setError("")
    setNotice("")
    if (file.size > 10 * 1024 * 1024) { setError("图片不能超过 10 MB"); return }
    if (!/\.(jpe?g|png|gif)$/i.test(file.name)) { setError("只支持 JPEG、PNG 和 GIF 图片"); return }
    setUploading(true)
    try {
      await uploadMedia(file)
      setSearch("")
      setAppliedSearch("")
      setPage(1)
      setRevision((value) => value + 1)
      setNotice("图片上传成功")
    } catch (err) {
      setError(err instanceof Error ? err.message : "上传失败")
    } finally {
      setUploading(false)
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Button disabled={uploading} onClick={() => fileInput.current?.click()}>{uploading ? <LoaderCircle className="animate-spin" /> : <Upload />}{uploading ? "上传中…" : "上传图片"}</Button>
        <Button variant="outline" disabled={loading || uploading} onClick={() => setRevision((value) => value + 1)}><RefreshCw className={loading ? "animate-spin" : ""} />刷新</Button>
        <input ref={fileInput} type="file" accept="image/jpeg,image/png,image/gif" className="hidden" aria-label="选择上传图片" onChange={(event) => {
          const file = event.target.files?.[0]
          event.target.value = ""
          if (file) void upload(file)
        }} />
        <form className="flex max-w-full items-center gap-2" onSubmit={submitSearch}>
          <div className="w-56 min-w-0">
            <SearchField aria-label="搜索附件文件名" placeholder="搜索文件名" value={search} onChange={(event) => setSearch(event.target.value)} />
          </div>
          <Button variant="outline" type="submit">搜索</Button>
        </form>
      </div>
      {error ? <InlineAlert>{error}<Button className="ml-2" variant="outline" size="sm" onClick={() => setRevision((value) => value + 1)}>重新加载</Button></InlineAlert> : null}
      {notice ? <p role="status" className="text-sm text-muted-foreground">{notice}</p> : null}
      {loading ? (
        <div role="status" className="flex h-48 items-center justify-center gap-2 text-sm text-muted-foreground"><LoaderCircle className="size-5 animate-spin" />加载附件…</div>
      ) : assets.length === 0 ? (
        <div className="flex h-48 flex-col items-center justify-center gap-2 text-sm text-muted-foreground"><ImageIcon className="size-7" />{error ? "附件暂时无法显示" : appliedSearch ? "没有匹配的附件" : "暂无附件，上传图片后即可在这里管理"}</div>
      ) : (
        <div className="overflow-hidden rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-4">附件</TableHead>
                <TableHead>格式</TableHead>
                <TableHead>大小</TableHead>
                <TableHead>尺寸</TableHead>
                <TableHead>上传人</TableHead>
                <TableHead>上传时间</TableHead>
                <TableHead className="pr-4 text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {assets.map((asset) => (
                <TableRow key={asset.id}>
                  <TableCell className="pl-4">
                    <button type="button" onClick={() => setSelected(asset)} className="flex items-center gap-3 rounded-md text-left outline-none focus-visible:ring-2 focus-visible:ring-ring" aria-label={`查看附件：${asset.original_name}`}>
                      <span className="flex size-12 shrink-0 items-center justify-center overflow-hidden rounded-md border bg-muted/40 p-1">
                        <img loading="lazy" src={asset.url} alt="" className="size-full object-contain" />
                      </span>
                      <span className="max-w-48 truncate font-medium sm:max-w-72" title={asset.original_name}>{asset.original_name || "未命名图片"}</span>
                    </button>
                  </TableCell>
                  <TableCell className="text-muted-foreground">{asset.mime_type.replace("image/", "").toUpperCase()}</TableCell>
                  <TableCell className="text-muted-foreground">{fileSize(asset.size_bytes)}</TableCell>
                  <TableCell className="text-muted-foreground">{asset.width} × {asset.height}</TableCell>
                  <TableCell className="max-w-40 truncate text-muted-foreground" title={asset.uploaded_by}>{asset.uploaded_by || "未知"}</TableCell>
                  <TableCell className="text-muted-foreground">{uploadDate(asset.created_at)}</TableCell>
                  <TableCell className="pr-4 text-right">
                    <Button variant="ghost" size="sm" onClick={() => setSelected(asset)}>详情</Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
      <TablePagination className="border-t-0" page={page} totalPages={Math.max(1, Math.ceil(total / pageSize))} total={total} pageSize={pageSize} loading={loading} onPageChange={setPage} />
      {selected ? <MediaDetails key={selected.id} asset={selected} onClose={() => setSelected(null)} onDeleted={() => {
        setSelected(null)
        setNotice("附件已删除")
        setRevision((value) => value + 1)
      }} /> : null}
    </div>
  )
}
