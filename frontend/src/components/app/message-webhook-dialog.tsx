import { useEffect, useState } from "react"
import { BellRing, Eye, EyeOff, LoaderCircle, Send } from "lucide-react"
import { toast } from "sonner"

import { getMessageWebhook, saveMessageWebhook, testMessageWebhook } from "@/lib/api"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { InlineAlert } from "@/components/app/app-ui"

// Per-site Feishu (Lark) custom-bot notification settings for new messages.
export function MessageWebhookDialog() {
  const [open, setOpen] = useState(false)
  const [loading, setLoading] = useState(false)
  const [enabled, setEnabled] = useState(false)
  const [webhookUrl, setWebhookUrl] = useState("")
  const [revealed, setRevealed] = useState(false)
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)
  const [error, setError] = useState("")

  const busy = saving || testing

  useEffect(() => {
    if (!open) return
    let active = true
    setLoading(true)
    setError("")
    setRevealed(false)
    getMessageWebhook()
      .then((settings) => {
        if (!active) return
        setEnabled(settings.enabled)
        setWebhookUrl(settings.webhook_url)
      })
      .catch((reason: Error) => {
        if (active) setError(reason.message || "加载通知设置失败")
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [open])

  async function handleSave() {
    setSaving(true)
    setError("")
    try {
      const settings = await saveMessageWebhook({ enabled, webhook_url: webhookUrl.trim() })
      setEnabled(settings.enabled)
      setWebhookUrl(settings.webhook_url)
      toast.success("留言通知设置已保存")
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "保存通知设置失败")
    } finally {
      setSaving(false)
    }
  }

  async function handleTest() {
    setTesting(true)
    setError("")
    try {
      await testMessageWebhook(webhookUrl.trim())
      toast.success("测试消息已发送，请查看飞书群")
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "测试消息发送失败")
    } finally {
      setTesting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger render={<Button variant="outline" size="sm" className="gap-1.5" />}>
        <BellRing className="size-4" />
        通知设置
      </DialogTrigger>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>留言通知设置</DialogTitle>
          <DialogDescription>
            新留言提交后自动推送到飞书群机器人，未启用时仅记录在后台
          </DialogDescription>
        </DialogHeader>

        {error ? <InlineAlert>{error}</InlineAlert> : null}

        <div className="space-y-4">
          <div className="flex items-center justify-between gap-4 rounded-lg border px-3 py-2.5">
            <div className="min-w-0">
              <Label htmlFor="message-webhook-enabled">飞书通知</Label>
              <p className="mt-0.5 text-xs text-muted-foreground">
                开启后每条新留言都会推送一次
              </p>
            </div>
            <div className="flex shrink-0 items-center gap-2">
              <span className="text-xs text-muted-foreground">{enabled ? "已启用" : "已停用"}</span>
              <Switch
                id="message-webhook-enabled"
                checked={enabled}
                disabled={loading || busy}
                onCheckedChange={(checked) => setEnabled(checked)}
              />
            </div>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="message-webhook-url">Webhook 地址</Label>
            <div className="flex gap-2">
              <Input
                id="message-webhook-url"
                type={revealed ? "text" : "password"}
                value={webhookUrl}
                disabled={loading || busy}
                autoComplete="off"
                spellCheck={false}
                placeholder="https://open.feishu.cn/open-apis/bot/v2/hook/..."
                onChange={(event) => setWebhookUrl(event.target.value)}
              />
              <Button
                type="button"
                variant="outline"
                size="icon"
                aria-label={revealed ? "隐藏 Webhook 地址" : "显示 Webhook 地址"}
                title={revealed ? "隐藏 Webhook 地址" : "显示 Webhook 地址"}
                onClick={() => setRevealed((value) => !value)}
              >
                {revealed ? <EyeOff /> : <Eye />}
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">
              在飞书群聊中选择「设置 → 群机器人 → 添加机器人 → 自定义机器人」，复制其 Webhook 地址；
              国际版 Lark 域名同样支持。
            </p>
          </div>
        </div>

        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            className="gap-1.5"
            disabled={loading || busy || !webhookUrl.trim()}
            onClick={handleTest}
          >
            {testing ? <LoaderCircle className="animate-spin" /> : <Send />}
            发送测试消息
          </Button>
          <Button type="button" className="gap-1.5" disabled={loading || busy} onClick={handleSave}>
            {saving ? <LoaderCircle className="animate-spin" /> : null}
            保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
