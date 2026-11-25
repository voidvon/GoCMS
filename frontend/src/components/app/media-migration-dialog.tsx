import { useEffect, useRef, useState } from "react"
import { LoaderCircle } from "lucide-react"

import { showError, showSuccess } from "@/components/app/admin-notifications"
import { ConfirmDialog } from "@/components/app/app-ui"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { ScrollArea } from "@/components/ui/scroll-area"
import { cleanMediaBatch, getMediaCleanup, getMediaMigration, migrateMediaBatch, type MediaMigrationFailure, type MediaMigrationPreview, type Site } from "@/lib/api"

export function MediaMigrationDialog({ site, cleanup, onClose, onChanged }: {
  site: Site
  cleanup: boolean
  onClose: () => void
  onChanged: () => void
}) {
  const [preview, setPreview] = useState<MediaMigrationPreview | null>(null)
  const [loading, setLoading] = useState(true)
  const [running, setRunning] = useState(false)
  const [stopping, setStopping] = useState(false)
  const [reload, setReload] = useState(0)
  const [completed, setCompleted] = useState(0)
  const [confirmCleanup, setConfirmCleanup] = useState<boolean | null>(null)
  const [failureCount, setFailureCount] = useState(0)
  const [failures, setFailures] = useState<MediaMigrationFailure[]>([])
  const stop = useRef(false)
  const mounted = useRef(false)
  const verb = cleanup ? "清理" : "迁移"

  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
      stop.current = true
    }
  }, [])

  useEffect(() => {
    let active = true
    const getPreview = cleanup ? getMediaCleanup : getMediaMigration
    getPreview(site.id).then((result) => {
      if (active) setPreview(result)
    }).catch((error) => {
      if (active) {
        setPreview(null)
        showError(error, `无法统计待${verb}图片`)
      }
    }).finally(() => {
      if (active) setLoading(false)
    })
    return () => { active = false }
  }, [site.id, reload, cleanup, verb])

  function reloadPreview() {
    setLoading(true)
    setReload((value) => value + 1)
  }

  async function runBatch(all: boolean) {
    if (!preview?.ready || preview.count === 0 || running) return
    stop.current = false
    setRunning(true)
    setStopping(false)
    setCompleted(0)
    setFailureCount(0)
    setFailures([])
    let afterId = 0
    let succeeded = 0
    let failed = 0
    try {
      let more: boolean
      do {
        const execute = cleanup ? cleanMediaBatch : migrateMediaBatch
        const result = await execute(site.id, afterId, preview.through_id)
        succeeded += "removed" in result ? result.removed : result.migrated
        failed += result.failures.length
        afterId = result.after_id
        more = all && result.more
        if (!mounted.current) return
        setCompleted(succeeded)
        setFailureCount(failed)
        setFailures((previous) => [...previous, ...result.failures].slice(0, 20))
      } while (more && !stop.current)
      if (failed > 0) {
        showError(`已${verb} ${succeeded} 张，${failed} 张失败。修复后可重试。`)
      } else {
        showSuccess(stop.current ? `${verb}已暂停，已${verb} ${succeeded} 张` : `已${verb} ${succeeded} 张图片`)
      }
    } catch (error) {
      if (mounted.current) showError(error, `${verb}中断，刷新统计后可继续`)
    } finally {
      if (mounted.current) {
        setRunning(false)
        setStopping(false)
        reloadPreview()
        onChanged()
      }
    }
  }

  function start(all: boolean) {
    if (cleanup) setConfirmCleanup(all)
    else void runBatch(all)
  }

  return (
    <Dialog open onOpenChange={(open) => { if (!open && !running) onClose() }}>
      <DialogContent className="flex max-h-[calc(100dvh-2rem)] flex-col overflow-hidden sm:max-w-lg" showCloseButton={!running}>
        <DialogHeader>
          <DialogTitle>{cleanup ? "清理本地图片副本" : "迁移图片到 R2"}</DialogTitle>
          <DialogDescription>
            {site.name} 的本地上传图片。{cleanup
              ? "逐张校验本地文件和 R2 对象，通过后移除本地副本。"
              : "每张图片通过校验后切换存储，保留原地址和本地副本。"}
          </DialogDescription>
        </DialogHeader>
        <ScrollArea className="min-h-0 flex-1" contentClassName="space-y-3 text-sm">
          {loading ? (
            <p role="status" className="flex items-center gap-2"><LoaderCircle className="size-4 animate-spin" />统计图片中…</p>
          ) : preview ? (
            <>
              <p>待{verb} {preview.count} 张，约 {(preview.bytes / 1024 / 1024).toFixed(1)} MiB。</p>
              {!preview.ready && <p className="text-muted-foreground">请先在「多站点 → 文件存储」配置 R2。</p>}
            </>
          ) : (
            <Button variant="outline" size="sm" onClick={reloadPreview}>重新统计</Button>
          )}
          {(running || completed > 0 || failureCount > 0) && (
            <p role="status">已{verb} {completed} 张，失败 {failureCount} 张。{stopping && "当前批次结束后暂停。"}</p>
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
              <Button variant="outline" disabled={loading || !preview?.ready || !preview.count} onClick={() => start(false)}>{cleanup ? "清理一批" : "试迁移一批"}</Button>
              <Button disabled={loading || !preview?.ready || !preview.count} onClick={() => start(true)}>{cleanup ? "清理剩余副本" : "迁移剩余图片"}</Button>
            </>
          )}
        </DialogFooter>
        <ConfirmDialog
          open={confirmCleanup !== null}
          onOpenChange={(open) => { if (!open) setConfirmCleanup(null) }}
          title="清理本地图片副本"
          description={`将移除 ${site.name} 的${confirmCleanup ? "剩余" : "一批"}本地副本。R2 对象和附件记录会保留。`}
          confirmLabel="确认清理"
          onConfirm={() => {
            const all = confirmCleanup === true
            setConfirmCleanup(null)
            void runBatch(all)
          }}
        />
      </DialogContent>
    </Dialog>
  )
}
