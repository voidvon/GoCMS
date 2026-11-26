import { useId, useState, type FormEvent } from "react"
import { Check, Download, Info, LoaderCircle, RefreshCw, Settings } from "lucide-react"

import { version } from "../../../package.json"
import { checkForUpdate, installUpdate, type UpdateCheck } from "@/lib/api"
import { defaultAdminName, useAdminSettings } from "@/lib/admin-settings-context"
import { showError, showSuccess } from "@/components/app/admin-notifications"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ScrollArea } from "@/components/ui/scroll-area"

function AdminNameForm({ adminName }: { adminName: string }) {
  const { saveAdminName } = useAdminSettings()
  const inputId = useId()
  const [name, setName] = useState(adminName)
  const [saving, setSaving] = useState(false)
  const changed = name.trim() !== adminName

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (saving || !changed) return
    setSaving(true)
    try {
      setName(await saveAdminName(name))
      showSuccess("后台名称已保存")
    } catch (error) {
      showError(error, "保存后台名称失败")
    } finally {
      setSaving(false)
    }
  }

  return (
    <form onSubmit={handleSubmit} className="mt-5 space-y-4">
      <div className="space-y-2">
        <Label htmlFor={inputId}>后台名称</Label>
        <Input id={inputId} value={name} onChange={(event) => setName(event.target.value)} placeholder={defaultAdminName}
          aria-describedby={`${inputId}-help`} disabled={saving} />
        <p id={`${inputId}-help`} className="text-xs text-muted-foreground">
          所有站点共用此名称，显示在后台标签页和侧边栏。留空使用默认名称。
        </p>
      </div>
      <Button type="submit" size="sm" disabled={saving || !changed}>
        {saving && <LoaderCircle className="animate-spin" />}
        保存
      </Button>
    </form>
  )
}

export function SettingsDialog({ canEditAdminName }: { canEditAdminName: boolean }) {
  const { adminName, loadError, retry } = useAdminSettings()
  const [section, setSection] = useState<"general" | "about">(canEditAdminName ? "general" : "about")
  const sectionId = useId()
  const [update, setUpdate] = useState<UpdateCheck | null>(null)
  const [checking, setChecking] = useState(false)
  const [updating, setUpdating] = useState(false)

  async function handleCheckUpdate() {
    setChecking(true)
    try {
      setUpdate(await checkForUpdate(version))
    } catch (reason) {
      showError(reason, "检查更新失败")
    } finally {
      setChecking(false)
    }
  }

  async function handleInstallUpdate() {
    if (!update?.update_available || !update.can_update) {
      return
    }
    setUpdating(true)
    try {
      await installUpdate(version)
      void waitForRestart()
    } catch (reason) {
      setUpdating(false)
      showError(reason, "更新失败")
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
    showError("程序重启超时，请手动刷新页面")
  }

  const hasUpdate = update?.update_available && update.can_update
  const actionLabel = updating
    ? "更新中..."
    : checking
      ? "检查中..."
      : hasUpdate
        ? "立即更新"
        : "检查更新"
  const showGeneral = canEditAdminName && section === "general"

  return (
    <Dialog onOpenChange={(open) => { if (open) setSection(canEditAdminName ? "general" : "about") }}>
      <DialogTrigger
        render={<Button variant="ghost" size="icon" aria-label="设置" title="设置" />}
      >
        <Settings />
      </DialogTrigger>
      <DialogContent className="flex flex-col gap-0 overflow-hidden p-0 sm:max-w-2xl">
        <ScrollArea className="min-h-0 flex-1" contentClassName="pr-2">
          <div className="border-b px-5 py-4">
            <DialogTitle>设置</DialogTitle>
            <DialogDescription className="sr-only">查看 GoCMS 设置和版本信息</DialogDescription>
          </div>
          <div className="grid min-h-72 grid-cols-[112px_minmax(0,1fr)] sm:grid-cols-[180px_minmax(0,1fr)]">
            <nav aria-label="设置菜单" className="space-y-1 border-r bg-muted/30 p-2 sm:p-3">
              {canEditAdminName && (
                <Button variant={showGeneral ? "secondary" : "ghost"}
                  className="h-auto w-full justify-start whitespace-normal py-2 text-left"
                  aria-current={showGeneral ? "page" : undefined} aria-controls={sectionId} onClick={() => setSection("general")}
                >
                  <Settings />常规
                </Button>
              )}
              <Button variant={showGeneral ? "ghost" : "secondary"}
                className="h-auto w-full justify-start whitespace-normal py-2 text-left"
                aria-current={showGeneral ? undefined : "page"} aria-controls={sectionId} onClick={() => setSection("about")}
              >
                <Info />关于 GoCMS
              </Button>
            </nav>
            <section id={sectionId} aria-labelledby={`${sectionId}-title`} className="min-w-0 p-4 sm:p-6">
              <h2 id={`${sectionId}-title`} className="text-lg font-semibold">{showGeneral ? "常规" : "关于 GoCMS"}</h2>
              {showGeneral ? (
                adminName !== undefined ? <AdminNameForm adminName={adminName} /> : (
                  <div className="mt-5 space-y-2 text-sm text-muted-foreground">
                    <p>{loadError || "正在读取后台名称…"}</p>
                    {loadError && <Button variant="outline" size="sm" onClick={retry}>重试</Button>}
                  </div>
                )
              ) : (
                <>
                  <dl className="mt-6 space-y-6 text-sm">
                    <div className="flex items-start justify-between gap-4">
                      <div>
                        <dt className="text-muted-foreground">当前版本</dt>
                        <dd className="mt-1 font-medium">v{version}</dd>
                      </div>
                      <Button size="sm" variant={hasUpdate ? "default" : "outline"} className="shrink-0"
                        disabled={checking || updating} onClick={hasUpdate ? handleInstallUpdate : handleCheckUpdate}
                      >
                        {updating || checking ? <LoaderCircle className="animate-spin" /> : hasUpdate ? <Download /> : <RefreshCw />}
                        {actionLabel}
                      </Button>
                    </div>
                    <div>
                      <dt className="text-muted-foreground">GitHub 仓库</dt>
                      <dd className="mt-1">
                        <a href="https://github.com/voidvon/GoCMS" target="_blank" rel="noopener noreferrer"
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
                </>
              )}
            </section>
          </div>
        </ScrollArea>
      </DialogContent>
    </Dialog>
  )
}
