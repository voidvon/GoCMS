import { useState } from "react"
import { Check, Download, Info, LoaderCircle, RefreshCw, Settings } from "lucide-react"

import { version } from "../../../package.json"
import { checkForUpdate, installUpdate, type UpdateCheck } from "@/lib/api"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"

export function SettingsDialog() {
  const [update, setUpdate] = useState<UpdateCheck | null>(null)
  const [checking, setChecking] = useState(false)
  const [updating, setUpdating] = useState(false)
  const [error, setError] = useState("")

  async function handleCheckUpdate() {
    setChecking(true)
    setError("")
    try {
      setUpdate(await checkForUpdate(version))
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "检查更新失败")
    } finally {
      setChecking(false)
    }
  }

  async function handleInstallUpdate() {
    if (!update?.update_available || !update.can_update) {
      return
    }
    setUpdating(true)
    setError("")
    try {
      await installUpdate(version)
      void waitForRestart()
    } catch (reason) {
      setUpdating(false)
      setError(reason instanceof Error ? reason.message : "更新失败")
    }
  }

  async function waitForRestart() {
    for (let attempt = 0; attempt < 30; attempt += 1) {
      await new Promise((resolve) => window.setTimeout(resolve, 1000))
      try {
        const response = await fetch("/api/health", { cache: "no-store" })
        if (response.ok) {
          window.location.reload()
          return
        }
      } catch {
        // The service is expected to be unavailable while the new process starts.
      }
    }
    setUpdating(false)
    setError("程序重启超时，请手动刷新页面")
  }

  const hasUpdate = update?.update_available && update.can_update
  const actionLabel = updating
    ? "更新中..."
    : checking
      ? "检查中..."
      : hasUpdate
        ? "立即更新"
        : "检查更新"

  return (
    <Dialog>
      <DialogTrigger
        render={<Button variant="ghost" size="icon" aria-label="设置" title="设置" />}
      >
        <Settings />
      </DialogTrigger>
      <DialogContent className="max-h-[85svh] gap-0 overflow-y-auto p-0 sm:max-w-2xl">
        <div className="border-b px-5 py-4">
          <DialogTitle>设置</DialogTitle>
          <DialogDescription className="sr-only">查看 GoCMS 设置和版本信息</DialogDescription>
        </div>
        <div className="grid min-h-72 grid-cols-[112px_minmax(0,1fr)] sm:grid-cols-[180px_minmax(0,1fr)]">
          <nav aria-label="设置菜单" className="border-r bg-muted/30 p-2 sm:p-3">
            <Button
              variant="secondary"
              className="h-auto w-full justify-start whitespace-normal py-2 text-left"
              aria-current="page"
              aria-controls="settings-about"
            >
              <Info />
              关于 GoCMS
            </Button>
          </nav>
          <section id="settings-about" aria-labelledby="settings-about-title" className="min-w-0 p-4 sm:p-6">
            <h2 id="settings-about-title" className="text-lg font-semibold">关于 GoCMS</h2>
            <dl className="mt-6 space-y-6 text-sm">
              <div className="flex items-start justify-between gap-4">
                <div>
                  <dt className="text-muted-foreground">当前版本</dt>
                  <dd className="mt-1 font-medium">v{version}</dd>
                </div>
                <Button
                  size="sm"
                  variant={hasUpdate ? "default" : "outline"}
                  className="shrink-0"
                  disabled={checking || updating}
                  onClick={hasUpdate ? handleInstallUpdate : handleCheckUpdate}
                >
                  {updating || checking ? <LoaderCircle className="animate-spin" /> : hasUpdate ? <Download /> : <RefreshCw />}
                  {actionLabel}
                </Button>
              </div>
              <div>
                <dt className="text-muted-foreground">GitHub 仓库</dt>
                <dd className="mt-1">
                  <a
                    href="https://github.com/voidvon/GoCMS"
                    target="_blank"
                    rel="noopener noreferrer"
                    className="break-all underline underline-offset-4 hover:text-muted-foreground"
                  >
                    https://github.com/voidvon/GoCMS
                  </a>
                </dd>
              </div>
            </dl>
            {update && (
              <div className="mt-6 flex items-start gap-2 text-sm text-muted-foreground">
                {!update.release_available ? (
                  <>
                    <Info className="mt-0.5 size-4 shrink-0" />
                    <p>暂无可用的 GitHub Release</p>
                  </>
                ) : update.update_available ? (
                  <>
                    <Download className="mt-0.5 size-4 shrink-0" />
                    <p>
                      发现新版本 v{update.latest_version}
                      {!update.can_update && "，当前平台暂无可用更新包"}
                    </p>
                  </>
                ) : (
                  <>
                    <Check className="mt-0.5 size-4 shrink-0" />
                    <p>当前已是最新版本</p>
                  </>
                )}
              </div>
            )}
            {error && <p className="mt-4 text-sm text-destructive">{error}</p>}
          </section>
        </div>
      </DialogContent>
    </Dialog>
  )
}
