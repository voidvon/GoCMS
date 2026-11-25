import { showError, showSuccess } from "@/components/app/admin-notifications"
import { getErrorMessage } from "@/lib/errors"
import { mediaImageAccept } from "@/lib/media"
import { useEffect, useRef, useState, type FormEvent } from "react"
import { Copy, FileText, ImageIcon, LoaderCircle, RefreshCw, Trash2, Upload, CloudUpload } from "lucide-react"

import { deleteMedia, getMedia, getMediaItem, uploadMedia, uploadPDF, type MediaAsset, type MediaDetail, type MediaKind, type Site } from "@/lib/api"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Button } from "@/components/ui/button"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { ConfirmDialog, InlineAlert, SearchField } from "@/components/app/app-ui"
import { PaginatedTable } from "@/components/app/paginated-table"
import { TimeText } from "@/components/app/time-text"
import { ScrollArea } from "@/components/ui/scroll-area"
import { useDialogState } from "@/lib/use-dialog-state"
import { useSite } from "@/lib/site-context"
import { MediaMigrationDialog } from "@/components/app/media-migration-dialog"

const pageSize = 20

function fileSize(bytes: number) {
  return bytes < 1024 * 1024 ? `${(bytes / 1024).toFixed(1)} KB` : `${(bytes / 1024 / 1024).toFixed(1)} MB`
}

function MediaDetails({ asset, open, onOpenChangeComplete, onClose, onDeleted }: {
  asset: MediaAsset
  open: boolean
  onOpenChangeComplete: (open: boolean) => void
  onClose: () => void
  onDeleted: () => void
}) {
  const [detail, setDetail] = useState<MediaDetail | null>(null)
  const [error, setError] = useState("")
  const [retry, setRetry] = useState(0)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const { activeSite } = useSite()
  const publicURL = new URL(asset.url, activeSite?.domain ? `https://${activeSite.domain}` : window.location.origin).href

  useEffect(() => {
    let active = true
    getMediaItem(asset.id).then((result) => {
      if (active) setDetail(result)
    }).catch((err) => {
      if (active) setError(getErrorMessage(err, "附件详情加载失败"))
    })
    return () => { active = false }
  }, [asset.id, retry])

  async function copyURL() {
    try {
      await navigator.clipboard.writeText(publicURL)
      showSuccess("链接已复制")
    } catch {
      showError("复制失败，请手动复制下方地址")
    }
  }

  async function remove() {
    setDeleting(true)
    setError("")
    try {
      await deleteMedia(asset.id)
      setConfirmOpen(false)
      onDeleted()
    } catch (err) {
      showError(err, "删除附件失败")
      setConfirmOpen(false)
      setDetail(null)
      setRetry((value) => value + 1)
    } finally {
      setDeleting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={(open) => { if (!open && !deleting) onClose() }} onOpenChangeComplete={onOpenChangeComplete}>
      <DialogContent className="flex max-h-[calc(100dvh-2rem)] flex-col overflow-hidden sm:max-w-2xl">
        <ScrollArea className="min-h-0 flex-1" contentClassName="space-y-4 pr-2">
          <DialogHeader>
            <DialogTitle className="break-all pr-6">{asset.original_name || "未命名附件"}</DialogTitle>
            <DialogDescription>附件详情与内容引用</DialogDescription>
          </DialogHeader>
          {asset.kind === "pdf" ? (
            <Button variant="outline" render={<a href={asset.url} target="_blank" rel="noreferrer" />}><FileText />打开 PDF</Button>
          ) : (
            <div className="flex min-h-40 items-center justify-center rounded-lg border bg-muted/40 p-3">
              <img src={asset.url} alt={asset.original_name} className="max-h-72 max-w-full object-contain" />
            </div>
          )}
          {error ? <InlineAlert>{error}</InlineAlert> : null}
          <dl className="grid grid-cols-2 gap-3 text-sm">
            <div><dt className="text-muted-foreground">格式 / 大小</dt><dd>{asset.mime_type} · {fileSize(asset.size_bytes)}</dd></div>
            <div><dt className="text-muted-foreground">{asset.kind === "pdf" ? "文档语言" : "尺寸"}</dt><dd>{asset.kind === "pdf" ? asset.language : `${asset.width} × ${asset.height}`}</dd></div>
            <div><dt className="text-muted-foreground">存储位置</dt><dd>{asset.storage === "r2" ? "Cloudflare R2" : "本地磁盘"}</dd></div>
            <div><dt className="text-muted-foreground">上传人</dt><dd className="break-all">{asset.uploaded_by || "未知"}</dd></div>
            <div><dt className="text-muted-foreground">上传时间</dt><dd><TimeText value={asset.created_at} /></dd></div>
          </dl>
          <div className="space-y-2">
            <p className="text-sm font-medium">访问地址</p>
            <p className="select-all break-all rounded-md bg-muted p-2 text-xs">{publicURL}</p>
            <Button variant="outline" size="sm" onClick={() => void copyURL()}><Copy />复制链接</Button>
          </div>
          <div className="space-y-2 border-t pt-4">
            <p className="text-sm font-medium">资源引用{detail ? `（${detail.references.length} 处）` : ""}</p>
            <p className="text-xs text-muted-foreground">显示本站已记录的正文、封面、商品和订单引用。</p>
            {!detail ? (
              error ? <Button variant="outline" onClick={() => { setError(""); setRetry((value) => value + 1) }}>重试加载详情</Button>
                : <p role="status" className="text-sm text-muted-foreground">正在加载引用…</p>
            ) : detail.references.length === 0 ? (
              <p className="text-sm text-muted-foreground">暂无资源引用记录</p>
            ) : (
              <ScrollArea className="max-h-48" contentClassName="text-sm">
                <ul className="space-y-2">
                  {detail.references.map((reference) => (
                    <li key={`${reference.content_id}:${reference.field_name}`} className="flex items-start justify-between gap-3 rounded-md border p-2">
                      <a className="min-w-0 break-all underline underline-offset-4" href={reference.url || `/admin/content?edit=${reference.content_id}`}>
                        {reference.title || `内容 #${reference.content_id}`}
                      </a>
                      <span className="shrink-0 text-muted-foreground">{reference.field_name === "body" ? "正文" : reference.field_name === "cover_image" ? "封面" : reference.url ? "交易图片" : reference.field_name}</span>
                    </li>
                  ))}
                </ul>
              </ScrollArea>
            )}
          </div>
          <div className="flex justify-end gap-2 border-t pt-4">
            <Button variant="outline" disabled={deleting} onClick={onClose}>关闭</Button>
            <Button variant="destructive" disabled={!detail || detail.references.length > 0 || deleting} onClick={() => setConfirmOpen(true)}>
              <Trash2 />{detail && detail.references.length > 0 ? "资源引用中，不能删除" : "删除附件"}
            </Button>
          </div>
        </ScrollArea>
        <ConfirmDialog open={confirmOpen} onOpenChange={(open) => { if (!deleting) setConfirmOpen(open) }}
          title="删除附件？" description="将永久删除文件和附件记录。请确认外部链接已移除。"
          confirmLabel="确认删除" pending={deleting} onConfirm={() => void remove()} />
      </DialogContent>
    </Dialog>
  )
}

export function MediaPage({ canMigrate }: { canMigrate: boolean }) {
  const { activeSite } = useSite()
  const [storageOperation, setStorageOperation] = useState<{ site: Site; cleanup: boolean } | null>(null)
  const [kind, setKind] = useState<MediaKind>("image")
  const [language, setLanguage] = useState("")
  const [assets, setAssets] = useState<MediaAsset[]>([])
  const [search, setSearch] = useState("")
  const [appliedSearch, setAppliedSearch] = useState("")
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(0)
  const [revision, setRevision] = useState(0)
  const [loadedKey, setLoadedKey] = useState("")
  const [error, setError] = useState("")
  const [uploading, setUploading] = useState(false)
  const [selected, setSelected, selectedDialog] = useDialogState<MediaAsset>()
  const fileInput = useRef<HTMLInputElement>(null)
  const requestKey = JSON.stringify([kind, page, appliedSearch, revision])
  const loading = loadedKey !== requestKey

  useEffect(() => {
    let active = true
    getMedia(page, pageSize, appliedSearch, 0, kind, true).then((response) => {
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
      setError(getErrorMessage(err, "附件加载失败"))
      setLoadedKey(requestKey)
    })
    return () => { active = false }
  }, [kind, page, appliedSearch, requestKey])

  function submitSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setPage(1)
    setAppliedSearch(search.trim())
    setRevision((value) => value + 1)
  }

  async function upload(file: File) {
    setError("")
    const maxMiB = kind === "pdf" ? 64 : 10
    if (file.size > maxMiB * 1024 * 1024) {
      showError(`文件不能超过 ${maxMiB} MiB`)
      return
    }
    if (kind === "pdf" && !language.trim()) {
      showError("请输入文档语言代码")
      return
    }
    setUploading(true)
    try {
      const result = kind === "pdf" ? await uploadPDF(file, language.trim()) : await uploadMedia(file)
      setSearch("")
      setAppliedSearch("")
      setPage(1)
      setRevision((value) => value + 1)
      const saved = file.size - result.asset.size_bytes
      if (kind === "pdf") {
        showSuccess("reused" in result && result.reused ? `已复用 PDF：${result.asset.original_name}` : "PDF 已保存")
      } else {
        showSuccess(saved > 0 ? `图片上传成功，节省 ${fileSize(saved)}` : "图片上传成功")
      }
    } catch (err) {
      showError(err, "上传失败")
    } finally {
      setUploading(false)
    }
  }

  return (
    <div className="space-y-4 lg:flex lg:h-full lg:min-h-0 lg:flex-col lg:space-y-0 lg:gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <div className="flex gap-1" role="group" aria-label="附件类型">
          {(["image", "pdf"] as const).map((type) => (
            <Button key={type} size="sm" variant={kind === type ? "secondary" : "ghost"} aria-pressed={kind === type} disabled={uploading} onClick={() => {
              setKind(type)
              setSearch("")
              setAppliedSearch("")
              setPage(1)
            }}>
              {type === "image" ? "图片" : "PDF"}
            </Button>
          ))}
        </div>
        {kind === "pdf" ? (
          <div className="flex items-center gap-2">
            <Label htmlFor="pdf-language">文档语言</Label>
            <Input id="pdf-language" className="h-8 w-28" placeholder="de-DE" value={language} disabled={uploading}
              onChange={(event) => setLanguage(event.target.value)} />
          </div>
        ) : null}
        <Button size="sm" disabled={uploading || (kind === "pdf" && !language.trim())} onClick={() => fileInput.current?.click()}>{uploading ? <LoaderCircle className="animate-spin" /> : <Upload />}{uploading ? "上传中…" : kind === "pdf" ? "上传 PDF" : "上传图片"}</Button>
        {kind === "image" && canMigrate && activeSite && (
          <>
            <Button size="sm" variant="outline" disabled={uploading} onClick={() => setStorageOperation({ site: activeSite, cleanup: false })}>
              <CloudUpload />迁移到 R2
            </Button>
            <Button size="sm" variant="outline" disabled={uploading} onClick={() => setStorageOperation({ site: activeSite, cleanup: true })}>
              <Trash2 />清理本地副本
            </Button>
          </>
        )}
        <Button size="sm" variant="outline" disabled={loading || uploading} onClick={() => setRevision((value) => value + 1)}><RefreshCw className={loading ? "animate-spin" : ""} />刷新</Button>
        <input ref={fileInput} type="file" accept={kind === "pdf" ? ".pdf,application/pdf" : mediaImageAccept} className="hidden" aria-label="选择上传附件" onChange={(event) => {
          const file = event.target.files?.[0]
          event.target.value = ""
          if (file) void upload(file)
        }} />
        <form className="flex max-w-full items-center gap-2" onSubmit={submitSearch}>
          <div className="w-56 min-w-0">
            <SearchField size="sm" aria-label="搜索附件文件名" placeholder="搜索文件名" value={search} onChange={(event) => setSearch(event.target.value)} />
          </div>
          <Button size="sm" variant="outline" type="submit">搜索</Button>
        </form>
      </div>
      {storageOperation && (
        <MediaMigrationDialog
          site={storageOperation.site}
          cleanup={storageOperation.cleanup}
          onClose={() => setStorageOperation(null)}
          onChanged={() => setRevision((value) => value + 1)}
        />
      )}
      {error ? <InlineAlert>{error}<Button className="ml-2" variant="outline" size="sm" onClick={() => setRevision((value) => value + 1)}>重新加载</Button></InlineAlert> : null}
      <PaginatedTable
        className="lg:overflow-hidden"
        page={page}
        totalPages={Math.max(1, Math.ceil(total / pageSize))}
        total={total}
        pageSize={pageSize}
        loading={loading}
        onPageChange={setPage}
      >
        {loading ? (
          <div role="status" className="flex h-48 items-center justify-center gap-2 text-sm text-muted-foreground"><LoaderCircle className="size-5 animate-spin" />加载附件…</div>
        ) : assets.length === 0 ? (
          <div className="flex h-48 flex-col items-center justify-center gap-2 text-sm text-muted-foreground"><ImageIcon className="size-7" />{error ? "附件暂时无法显示" : appliedSearch ? "没有匹配的附件" : "暂无附件"}</div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-4">附件</TableHead>
                <TableHead>格式</TableHead>
                <TableHead>大小</TableHead>
                <TableHead>{kind === "pdf" ? "文档语言" : "尺寸"}</TableHead>
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
                        {asset.kind === "pdf" ? <FileText className="size-5 text-muted-foreground" /> : <img loading="lazy" src={asset.url} alt="" className="size-full object-contain" />}
                      </span>
                      <span className="max-w-48 truncate font-medium sm:max-w-72" title={asset.original_name}>{asset.original_name || "未命名附件"}{asset.status === "deleting" ? "（待完成删除）" : ""}</span>
                    </button>
                  </TableCell>
                  <TableCell className="text-muted-foreground">{asset.kind === "pdf" ? "PDF" : asset.mime_type.replace("image/", "").toUpperCase()}</TableCell>
                  <TableCell className="text-muted-foreground">{fileSize(asset.size_bytes)}</TableCell>
                  <TableCell className="text-muted-foreground">{asset.kind === "pdf" ? asset.language : `${asset.width} × ${asset.height}`}</TableCell>
                  <TableCell className="max-w-40 truncate text-muted-foreground" title={asset.uploaded_by}>{asset.uploaded_by || "未知"}</TableCell>
                  <TableCell className="text-muted-foreground"><TimeText value={asset.created_at} /></TableCell>
                  <TableCell className="pr-4 text-right">
                    <Button variant="ghost" size="sm" onClick={() => setSelected(asset)}>详情</Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </PaginatedTable>
      {selected ? <MediaDetails key={selected.id} asset={selected} {...selectedDialog} onClose={() => setSelected(null)} onDeleted={() => {
        setSelected(null)
        showSuccess("附件已删除")
        setRevision((value) => value + 1)
      }} /> : null}
    </div>
  )
}
