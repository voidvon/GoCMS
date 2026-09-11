import { useEffect, useState, type FormEvent } from "react"
import { ImageIcon, LoaderCircle } from "lucide-react"

import { getMedia, type MediaAsset } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { SearchField } from "@/components/app/app-ui"

type MediaPickerDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSelect: (asset: MediaAsset) => void
  title?: string
  description?: string
}

export function MediaPickerDialog({
  open,
  onOpenChange,
  onSelect,
  title = "选择素材",
  description = "从已上传的图片中选择封面。",
}: MediaPickerDialogProps) {
  const [assets, setAssets] = useState<MediaAsset[]>([])
  const [search, setSearch] = useState("")
  const [appliedSearch, setAppliedSearch] = useState("")
  const [loadedRequest, setLoadedRequest] = useState<string | null>(null)
  const [error, setError] = useState("")
  const requestKey = appliedSearch
  const loading = open && loadedRequest !== requestKey

  useEffect(() => {
    if (!open) return
    let active = true
    getMedia(1, 48, appliedSearch)
      .then((response) => {
        if (!active) return
        setAssets(response.items)
        setLoadedRequest(requestKey)
      })
      .catch((loadError) => {
        if (!active) return
        setError(loadError instanceof Error ? loadError.message : "素材加载失败")
        setLoadedRequest(requestKey)
      })
    return () => {
      active = false
    }
  }, [appliedSearch, open, requestKey])

  function submitSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError("")
    setAppliedSearch(search.trim())
  }

  function handleOpenChange(nextOpen: boolean) {
    if (!nextOpen) {
      setLoadedRequest(null)
      setError("")
    }
    onOpenChange(nextOpen)
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        <form className="flex gap-2" onSubmit={submitSearch}>
          <SearchField value={search} onChange={(event) => setSearch(event.target.value)} placeholder="搜索文件名" />
          <Button type="submit" variant="outline">搜索</Button>
        </form>
        {error ? <p role="alert" className="text-sm text-destructive">{error}</p> : null}
        {loading ? (
          <div className="flex h-48 items-center justify-center text-muted-foreground"><LoaderCircle className="size-5 animate-spin" /></div>
        ) : assets.length === 0 ? (
          <div className="flex h-48 flex-col items-center justify-center gap-2 text-sm text-muted-foreground"><ImageIcon className="size-6" />暂无图片素材</div>
        ) : (
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-4 md:grid-cols-6">
            {assets.map((asset) => (
              <button
                key={asset.id}
                type="button"
                className="group overflow-hidden rounded-lg border bg-muted text-left outline-none transition hover:border-foreground/40 focus-visible:ring-3 focus-visible:ring-ring/50"
                onClick={() => {
                  onSelect(asset)
                  handleOpenChange(false)
                }}
                title={asset.original_name || asset.url}
              >
                <span className="block aspect-square overflow-hidden">
                  <img src={asset.url} alt={asset.original_name} className="size-full object-cover transition group-hover:scale-105" />
                </span>
                <span className="block truncate px-2 py-1.5 text-xs text-muted-foreground">{asset.original_name || "未命名图片"}</span>
              </button>
            ))}
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
