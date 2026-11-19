import { useEffect, useState, type FormEvent } from "react"
import { LoaderCircle } from "lucide-react"

import { showError, showSuccess } from "@/components/app/admin-notifications"
import { InlineAlert } from "@/components/app/app-ui"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Switch } from "@/components/ui/switch"
import { getMediaStorage, saveMediaStorage, testMediaStorage, type MediaStorageSettings } from "@/lib/api"
import { getErrorMessage } from "@/lib/errors"

export function MediaStorageDialog({ onClose }: { onClose: () => void }) {
  const [settings, setSettings] = useState<MediaStorageSettings | null>(null)
  const [secret, setSecret] = useState("")
  const [pending, setPending] = useState<"test" | "save" | null>(null)
  const [error, setError] = useState("")
  const [retry, setRetry] = useState(0)

  useEffect(() => {
    let active = true
    getMediaStorage().then((result) => {
      if (active) setSettings(result)
    }).catch((err) => {
      if (active) setError(getErrorMessage(err, "无法加载文件存储设置"))
    })
    return () => { active = false }
  }, [retry])

  async function submit(action: "test" | "save") {
    if (!settings) return
    setPending(action)
    try {
      if (action === "test") {
        await testMediaStorage({ ...settings, secret })
        showSuccess("R2 读写测试通过")
      } else {
        const result = await saveMediaStorage({ ...settings, secret })
        setSettings(result)
        setSecret("")
        showSuccess("文件存储设置已保存")
      }
    } catch (err) {
      showError(err, action === "test" ? "连接测试失败" : "保存设置失败")
    } finally {
      setPending(null)
    }
  }

  function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    void submit("save")
  }

  return (
    <Dialog open onOpenChange={(open) => { if (!open && !pending) onClose() }}>
      <DialogContent className="flex max-h-[calc(100dvh-2rem)] flex-col overflow-hidden sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>文件存储</DialogTitle>
          <DialogDescription>所有站点共用一个 R2 存储桶，上传时自动按站点分目录。</DialogDescription>
        </DialogHeader>
        {!settings ? (
          error ? (
            <InlineAlert>{error}<Button variant="outline" size="sm" className="ml-2" onClick={() => { setError(""); setRetry((value) => value + 1) }}>重试</Button></InlineAlert>
          ) : (
            <div className="flex justify-center p-6" role="status" aria-label="加载中"><LoaderCircle className="animate-spin" /></div>
          )
        ) : (
          <form className="flex min-h-0 flex-col gap-4" onSubmit={save}>
            <ScrollArea className="min-h-0 flex-1" contentClassName="space-y-4 p-1">
              <div className="flex items-center justify-between gap-4">
                <Label htmlFor="media-r2-enabled">新 PDF 使用 Cloudflare R2</Label>
                <Switch id="media-r2-enabled" checked={settings.enabled} disabled={!!pending} onCheckedChange={(enabled) =>
                  setSettings({ ...settings, enabled, images: enabled && settings.images })
                } />
              </div>
              <div className="flex items-center justify-between gap-4">
                <Label htmlFor="media-r2-images">新图片也使用 R2</Label>
                <Switch id="media-r2-images" checked={settings.images} disabled={!!pending || !settings.enabled} onCheckedChange={(images) => setSettings({ ...settings, images })} />
              </div>
              <fieldset className="space-y-3" disabled={!!pending}>
                <div className="space-y-1.5">
                  <Label htmlFor="media-r2-endpoint">S3 Endpoint</Label>
                  <Input id="media-r2-endpoint" type="url" placeholder="https://<Account ID>.r2.cloudflarestorage.com" value={settings.endpoint} onChange={(e) => setSettings({ ...settings, endpoint: e.target.value })} />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="media-r2-bucket">存储桶名称</Label>
                  <Input id="media-r2-bucket" value={settings.bucket} onChange={(e) => setSettings({ ...settings, bucket: e.target.value })} />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="media-r2-access">Access Key ID</Label>
                  <Input id="media-r2-access" autoComplete="off" value={settings.access_key} onChange={(e) => setSettings({ ...settings, access_key: e.target.value })} />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="media-r2-secret">Secret Access Key</Label>
                  <Input id="media-r2-secret" type="password" autoComplete="new-password" value={secret} placeholder={settings.has_secret ? "已保存，留空继续使用" : undefined} onChange={(e) => setSecret(e.target.value)} />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="media-r2-url">下载域名（选填）</Label>
                  <Input id="media-r2-url" type="url" placeholder="https://downloads.example.com" value={settings.public_url} onChange={(e) => setSettings({ ...settings, public_url: e.target.value })} />
                  <p className="text-xs text-muted-foreground">填写已绑定到此 R2 桶的公开域名。留空时使用临时签名下载链接。</p>
                </div>
              </fieldset>
            </ScrollArea>
            <DialogFooter>
              <Button type="button" variant="outline" disabled={!!pending || !settings.endpoint.trim()} onClick={() => void submit("test")}>
                {pending === "test" && <LoaderCircle className="animate-spin" />}测试连接
              </Button>
              <Button type="submit" disabled={!!pending}>
                {pending === "save" && <LoaderCircle className="animate-spin" />}保存
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  )
}
