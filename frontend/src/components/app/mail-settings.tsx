import { Card } from "@/components/ui/card"
import { useEffect, useState, type FormEvent } from "react"
import { getMailSettings, saveMailSettings, testMailSettings, initializeMemberLoginKey, exportMemberLoginKey, type MailSettingsStatus } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { ScrollArea } from "@/components/ui/scroll-area"
import { showError, showSuccess } from "@/components/app/admin-notifications"

export function MailSettings() {
  const [data, setData] = useState<MailSettingsStatus | null>(null)
  const [busy, setBusy] = useState(false)
  const [password, setPassword] = useState("")
  const [recipient, setRecipient] = useState("")
  const [key, setKey] = useState("")
  const [dirty, setDirty] = useState(false)
  const [version, setVersion] = useState(0)
  useEffect(() => {
    let active = true
    getMailSettings().then(value => { if (active) { setData(value); setDirty(false) } }).catch(showError)
    return () => { active = false }
  }, [version])
  async function run(action: () => Promise<unknown>, message: string, reloadConfig = true) {
    setBusy(true)
    try { await action(); showSuccess(message); if (reloadConfig) setVersion(value => value + 1); else { const fresh = await getMailSettings(); setData(current => current ? { ...fresh, config: current.config } : fresh) } } catch (error) { showError(error) } finally { setBusy(false) }
  }
  if (!data) return <Button variant="outline" onClick={() => setVersion(value => value + 1)}>加载邮件配置</Button>
  const c = data.config
  const update = (fields: Partial<typeof c>) => { setDirty(true); setData({ ...data, config: { ...c, ...fields } }) }
  function save(event: FormEvent) { event.preventDefault(); void run(async () => { await saveMailSettings(c, password); setPassword("") }, "邮件配置已保存，全部站点立即生效") }
  return <ScrollArea className="h-full" contentClassName="max-w-3xl space-y-6 pb-6">
    <Card className="space-y-5 rounded-xl border p-5 gap-0"><h2 className="font-semibold">邮件服务与通知</h2><p className="text-sm text-muted-foreground">全部站点共用 SMTP 和联系邮箱。未开启邮件时，前台提供联系方式和已绑定第三方账号验证入口。</p>
      <form onSubmit={save} className="space-y-5">
        <div className="space-y-2"><Label htmlFor="support-email">公开联系邮箱</Label><Input id="support-email" type="email" dir="ltr" maxLength={254} value={c.support_email} disabled={busy} onChange={event => update({ support_email: event.target.value })} /><p className="text-xs text-muted-foreground">在找回密码页公开显示；关闭邮件时仍可显示。此地址用于联系管理员，不会自动批准密码重置。</p></div>
        <div className="flex items-center justify-between gap-4"><Label htmlFor="mail-enabled">启用邮件发送（找回密码与订单通知）</Label><Switch id="mail-enabled" checked={c.enabled} disabled={busy} onCheckedChange={enabled => update({ enabled })} /></div>
        <div className="grid gap-4 sm:grid-cols-2"><div className="space-y-2"><Label htmlFor="smtp-host">SMTP 主机</Label><Input id="smtp-host" dir="ltr" maxLength={253} value={c.host} disabled={busy} onChange={event => update({ host: event.target.value })} placeholder="smtp.example.com" /></div><div className="space-y-2"><Label htmlFor="smtp-port">端口</Label><Input id="smtp-port" type="number" min={1} max={65535} value={c.port} disabled={busy} onChange={event => update({ port: Number(event.target.value) })} /></div></div>
        <div className="space-y-2"><Label htmlFor="smtp-tls">连接安全</Label><Select value={c.tls} disabled={busy} onValueChange={value => { if (value === "tls" || value === "starttls") update({ tls: value }) }}><SelectTrigger id="smtp-tls"><SelectValue>{c.tls === "tls" ? "TLS（通常为 465）" : "STARTTLS（通常为 587）"}</SelectValue></SelectTrigger><SelectContent><SelectItem value="starttls">STARTTLS（通常为 587）</SelectItem><SelectItem value="tls">TLS（通常为 465）</SelectItem></SelectContent></Select></div>
        <div className="space-y-2"><Label htmlFor="smtp-user">SMTP 用户名</Label><Input id="smtp-user" dir="ltr" autoComplete="off" maxLength={254} value={c.username} disabled={busy} onChange={event => update({ username: event.target.value })} /><p className="text-xs text-muted-foreground">需要认证时填写；使用受信任的 TLS 中继可留空。</p></div>
        <div className="space-y-2"><Label htmlFor="smtp-password">SMTP 密码／授权码</Label><Input id="smtp-password" type="password" autoComplete="new-password" value={password} maxLength={4096} disabled={busy || !data.key_configured} onChange={event => { setPassword(event.target.value); setDirty(true) }} placeholder={c.password_set ? "已配置，留空保留" : "填写 SMTP 密码或应用授权码"} /><p className="text-xs text-muted-foreground">加密保存，不回传明文。{!data.secret_readable ? "现有密码无法读取，请恢复原凭据加密密钥。" : ""}</p></div>
        <div className="space-y-2"><Label htmlFor="smtp-from">发件邮箱</Label><Input id="smtp-from" type="email" dir="ltr" value={c.from_address} maxLength={254} disabled={busy} onChange={event => update({ from_address: event.target.value })} /></div>
        <div className="space-y-2"><Label htmlFor="smtp-name">发件人显示名称</Label><Input id="smtp-name" value={c.from_name} maxLength={200} disabled={busy} onChange={event => update({ from_name: event.target.value })} /></div>
        <p className="text-xs text-muted-foreground">只使用经过证书校验的 TLS 连接。关闭邮件或更新配置会使邮件重置链接失效，第三方验证仍由各自登录开关控制。</p>
        <Button type="submit" disabled={busy}>{busy ? "处理中…" : dirty ? "保存修改" : "保存配置"}</Button>
      </form>
    </Card>
    <Card className="space-y-4 rounded-xl border p-5 gap-0"><h3 className="font-semibold">发送测试邮件</h3><p className="text-sm text-muted-foreground">使用已保存配置发送；邮件总开关关闭时也可测试。</p><form className="space-y-3" onSubmit={event => { event.preventDefault(); void run(() => testMailSettings(recipient), "测试邮件已提交给邮件服务器，请检查收件箱及垃圾邮件") }}><Label htmlFor="smtp-test">测试收件邮箱</Label><Input id="smtp-test" type="email" required dir="ltr" maxLength={254} value={recipient} disabled={busy} onChange={event => setRecipient(event.target.value)} /><Button type="submit" variant="outline" disabled={busy || dirty || !c.host || !c.from_address || (c.username !== "" && (!c.password_set || !data.secret_readable))}>发送测试邮件</Button></form></Card>
    <Card className="space-y-4 rounded-xl border p-5 gap-0"><h3 className="font-semibold">凭据加密密钥</h3><p className="text-sm text-muted-foreground">与第三方登录共用服务级主密钥，SMTP 与登录凭据使用不同加密作用域。恢复数据库时需同时恢复原密钥。</p>
      {data.key_configured ? <Button type="button" variant="outline" disabled={busy} onClick={() => void run(async () => { const blob = await exportMemberLoginKey(); const url = URL.createObjectURL(blob); const a = document.createElement("a"); a.href = url; a.download = "gocms-member-login.key"; a.click(); setTimeout(() => URL.revokeObjectURL(url), 1000) }, "凭据加密密钥备份已下载", false)}>下载密钥备份</Button> : <><Button type="button" variant="outline" disabled={busy || !data.key_can_initialize} onClick={() => void run(() => initializeMemberLoginKey("generate", ""), "凭据加密密钥已生成，请下载备份", false)}>生成加密密钥</Button><div className="space-y-2"><Label htmlFor="mail-key-file">导入原密钥备份</Label><Input id="mail-key-file" type="file" disabled={busy || !data.key_can_initialize} onChange={event => { const file = event.target.files?.[0]; if (!file) return; if (file.size > 256) { showError("密钥文件过大"); return }; void file.text().then(value => setKey(value.trim())).catch(showError) }} /></div><Button type="button" variant="outline" disabled={busy || !key || !data.key_can_initialize} onClick={() => void run(async () => { await initializeMemberLoginKey("import", key); setKey("") }, "凭据加密密钥已恢复", false)}>导入并验证</Button></>}
    </Card>
  </ScrollArea>
}
