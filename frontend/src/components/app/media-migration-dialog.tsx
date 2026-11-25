import { useEffect, useRef, useState } from "react"
import { LoaderCircle } from "lucide-react"

import { showError, showSuccess } from "@/components/app/admin-notifications"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { ScrollArea } from "@/components/ui/scroll-area"
import { getMediaMigration, migrateMediaBatch, type MediaMigrationFailure, type MediaMigrationPreview, type Site } from "@/lib/api"

export function MediaMigrationDialog({ site, onClose, onChanged }: {
  site: Site
  onClose: () => void
  onChanged: () => void
}) {
  const [preview, setPreview] = useState<MediaMigrationPreview | null>(null)
  const [loading, setLoading] = useState(true)
  const [running, setRunning] = useState(false)
  const [stopping, setStopping] = useState(false)
  const [reload, setReload] = useState(0)
  const [migrated, setMigrated] = useState(0)
  const [failureCount, setFailureCount] = useState(0)
  const [failures, setFailures] = useState<MediaMigrationFailure[]>([])
  const stop = useRef(false)
  const mounted = useRef(false)

  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
      stop.current = true
    }
  }, [])

  useEffect(() => {
    let active = true
    getMediaMigration(site.id).then((result) => {
      if (active) setPreview(result)
    }).catch((error) => {
      if (active) {
        setPreview(null)
        showError(error, "无法统计待迁移图片")
      }
    }).finally(() => {
      if (active) setLoading(false)
    })
    return () => { active = false }
  }, [site.id, reload])

  function reloadPreview() {
    setLoading(true)
    setReload((value) => value + 1)
  }

  async function migrate(all: boolean) {
    if (!preview?.ready || preview.count === 0 || running) return
    stop.current = false
    setRunning(true)
    setStopping(false)
    setMigrated(0)
    setFailureCount(0)
    setFailures([])
    let afterId = 0
    let succeeded = 0
    let failed = 0
    try {
      let more: boolean
      do {
        const result = await migrateMediaBatch(site.id, afterId, preview.through_id)
        succeeded += result.migrated
        failed += result.failures.length
        afterId = result.after_id
        more = all && result.more
        if (!mounted.current) return
        setMigrated(succeeded)
        setFailureCount(failed)
        setFailures((previous) => [...previous, ...result.failures].slice(0, 20))
      } while (more && !stop.current)
      if (failed > 0) {
        showError(`已迁移 ${succeeded} 张，${failed} 张失败。修复后可重试。`)
      } else {
        showSuccess(stop.current ? `迁移已暂停，已迁移 ${succeeded} 张` : `已迁移 ${succeeded} 张图片`)
      }
    } catch (error) {
      if (mounted.current) showError(error, "迁移中断，刷新统计后可继续")
    } finally {
      if (mounted.current) {
        setRunning(false)
        setStopping(false)
        reloadPreview()
        onChanged()
      }
    }
  }

  return (
    <Dialog open onOpenChange={(open) => { if (!open && !running) onClose() }}>
      <DialogContent className="flex max-h-[calc(100dvh-2rem)] flex-col overflow-hidden sm:max-w-lg" showCloseButton={!running}>
        <DialogHeader>
          <DialogTitle>迁移图片到 R2</DialogTitle>
          <DialogDescription>
            {site.name} 的本地上传图片。每张图片通过校验后切换存储，保留原地址和本地副本。
          </DialogDescription>
        </DialogHeader>
        <ScrollArea className="min-h-0 flex-1" contentClassName="space-y-3 text-sm">
          {loading ? (
            <p role="status" className="flex items-center gap-2"><LoaderCircle className="size-4 animate-spin" />统计图片中…</p>
          ) : preview ? (
            <>
              <p>待迁移 {preview.count} 张，约 {(preview.bytes / 1024 / 1024).toFixed(1)} MiB。</p>
              {!preview.ready && <p className="text-muted-foreground">请先在「多站点 → 文件存储」配置 R2。</p>}
            </>
          ) : (
            <Button variant="outline" size="sm" onClick={reloadPreview}>重新统计</Button>
          )}
          {(running || migrated > 0 || failureCount > 0) && (
            <p role="status">已迁移 {migrated} 张，失败 {failureCount} 张。{stopping && "当前批次结束后暂停。"}</p>
          )}
          {failures.length > 0 && (
            <ul className="space-y-1 text-muted-foreground">
              {failures.map((item) => <li key={item.id} className="break-words">{item.name || `附件 #${item.id}`}：{item.error}</li>)}
              {failureCount > failures.length && <li>仅显示前 {failures.length} 条失败记录。</li>}
            </ul>
          )}
        </ScrollArea>
        <DialogFooter>
          {running ? (
            <Button variant="outline" disabled={stopping} onClick={() => {
              stop.current = true
              setStopping(true)
            }}>暂停</Button>
          ) : (
            <>
              <Button variant="outline" disabled={loading || !preview?.ready || !preview.count} onClick={() => void migrate(false)}>试迁移一批</Button>
              <Button disabled={loading || !preview?.ready || !preview.count} onClick={() => void migrate(true)}>迁移剩余图片</Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
