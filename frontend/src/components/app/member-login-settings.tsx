import { Card } from "@/components/ui/card"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import { useEffect, useState } from "react"
import { getMemberLogin, saveMemberLogin, initializeMemberLoginKey, exportMemberLoginKey, testMemberLogin, migrateMemberLogin, type MemberLoginStatus } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Badge } from "@/components/ui/badge"
import { ScrollArea } from "@/components/ui/scroll-area"
import { showError, showSuccess } from "@/components/app/admin-notifications"
import { SiteUsersPage } from "@/components/app/site-users-page"

export function MembersPage() {
  return <ScrollArea className="h-full" contentClassName="pb-6"><SiteUsersPage /></ScrollArea>
}

export function MemberLoginSettings() {
 const [provider,setProvider]=useState(new URLSearchParams(window.location.search).get("provider") === "facebook" ? "facebook" : "google")
 return <Tabs value={provider} onValueChange={value=>setProvider(String(value))} className="h-full min-h-0"><TabsList><TabsTrigger value="google">Google</TabsTrigger><TabsTrigger value="facebook">Facebook</TabsTrigger></TabsList>{(["google","facebook"] as const).map(id=><TabsContent key={id} value={id} keepMounted className="min-h-0 flex-1"><ProviderSettings provider={id} /></TabsContent>)}</Tabs>
}
function ProviderSettings({provider}:{provider:"google"|"facebook"}) {
 const name=provider==="google" ? "Google" : "Facebook"

  useEffect(() => {
    const url = new URL(window.location.href)
    const result = url.searchParams.get("oauth_test")
    if (!result || (url.searchParams.get("provider") || "google") !== provider) return
    if (result === "success") showSuccess(`${name} 授权测试成功，未创建会员或登录会话`)
    else showError(`${name} 授权测试失败，请检查回调地址、凭据和 ${name} 控制台配置`)
    url.searchParams.delete("oauth_test")
    window.history.replaceState(null, "", url.pathname + url.search + url.hash)
  }, [provider,name])
  const [data, setData] = useState<MemberLoginStatus | null>(null)
  const [secret, setSecret] = useState("")
  const [dirty, setDirty] = useState(false)
  const [busy, setBusy] = useState(false)
  const [loading, setLoading] = useState(true)
  const [importKey, setImportKey] = useState("")
  const [version, setVersion] = useState(0)
  useEffect(() => {
    let active = true
    getMemberLogin(provider).then(result => { if (active) { setData(result); setDirty(false) } }).catch(error => { if (active) showError(error) }).finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [version,provider])
  useEffect(() => {
    const refreshKey = () => { void getMemberLogin(provider).then(result => setData(current => current ? {...current,key_configured:result.key_configured,key_can_initialize:result.key_can_initialize,secret_readable:result.secret_readable} : current)).catch(showError) }
    window.addEventListener("member-login-key-updated",refreshKey)
    return () => window.removeEventListener("member-login-key-updated",refreshKey)
  },[provider])
  async function run(action: () => Promise<unknown>, message: string) {
    setBusy(true)
    try {
      await action()
      const result = await getMemberLogin(provider)
      setData(result)
      setDirty(false)
      showSuccess(message)
    } catch (error) { showError(error) } finally { setBusy(false) }
  }
  if (loading) return <p className="text-sm text-muted-foreground">正在读取登录配置…</p>
  if (!data) return <Button variant="outline" onClick={() => { setLoading(true); setVersion(value => value + 1) }}>重新加载</Button>
  const config = data.config
  const update = (change: Partial<typeof config>) => { setDirty(true); setData({ ...data, config: { ...config, ...change } }) }
  const canTest = data.callbacks.some(callback => callback.origin === window.location.origin)
  return <ScrollArea className="h-full" contentClassName="max-w-3xl space-y-6 pb-6">
    {data.legacy_configs.length > 0 && <Card className="space-y-3 rounded-xl border p-5 gap-0"><h2 className="font-semibold">选择沿用的旧配置</h2><p className="text-sm text-muted-foreground">检测到多套站点凭据，尚未自动选择。选择一套作为全平台统一凭据，或在下方填写新凭据。旧配置保留备查。</p>{data.legacy_configs.map(source => <Card key={source.site_id} className="flex flex-wrap items-center justify-between gap-3 rounded-lg border p-3 flex-row"><div className="min-w-0"><p className="text-sm">{source.site_name}</p><p className="break-all text-xs text-muted-foreground">{source.client_id}</p></div><Button type="button" variant="outline" disabled={busy || dirty} onClick={() => void run(() => migrateMemberLogin(source), "已沿用所选凭据，全部站点共享此配置")}>沿用这套配置</Button></Card>)}</Card>}
    <Card className="space-y-4 rounded-xl border p-5 gap-0">
      <div className="flex flex-wrap items-center justify-between gap-3"><div><h2 className="font-semibold">{name} 登录</h2><p className="mt-1 text-sm text-muted-foreground">所有站点共享一套凭据、开关和会员身份绑定，保存后立即生效。</p></div><Badge variant={config.enabled ? "default" : "secondary"}>{dirty ? "有未保存修改" : config.enabled ? "已开启" : "已关闭"}</Badge></div>
      <form className="space-y-5" onSubmit={event => {
        event.preventDefault()
        // Read actual input values so browser autofill is included even when it
        // did not dispatch React's change event.
        const fields = new FormData(event.currentTarget)
        const clientID = String(fields.get("client_id") ?? "").trim()
        const clientSecret = String(fields.get("secret") ?? "")
        update({ client_id: clientID })
        void run(async () => {
          await saveMemberLogin({ ...config, client_id: clientID }, clientSecret, provider)
          setSecret("")
        }, `${name} 登录配置已保存`)
      }}>
        <div className="flex items-center justify-between gap-4"><Label htmlFor={`${provider}-enabled`}>启用 {name} 登录</Label><Switch id={`${provider}-enabled`} checked={config.enabled} disabled={busy} onCheckedChange={enabled => update({ enabled })} /></div>
        <div className="space-y-2"><Label htmlFor={`${provider}-client`}>{provider === "facebook" ? "App ID" : "Client ID"}</Label><Input id={`${provider}-client`} name="client_id" dir="ltr" autoComplete="off" value={config.client_id} maxLength={512} disabled={busy} onChange={event => update({ client_id: event.target.value })} placeholder={provider === "google" ? "…apps.googleusercontent.com" : "Facebook App ID"} />{provider === "google" && <p className="text-xs text-muted-foreground">填写 Web 应用的完整 OAuth 客户端 ID，以 .apps.googleusercontent.com 结尾。</p>}</div>
        <div className="space-y-2"><Label htmlFor={`${provider}-secret`}>{provider === "facebook" ? "App Secret" : "Client Secret"}</Label><Input id={`${provider}-secret`} name="secret" type="password" dir="ltr" autoComplete="new-password" value={secret} maxLength={4096} disabled={busy || !data.key_configured} onChange={event => { setSecret(event.target.value); setDirty(true) }} placeholder={config.secret_set ? "已配置，留空保留现有密钥" : `填写 ${name} 客户端密钥`} /><p className="text-xs text-muted-foreground">密钥加密保存，不会回传到浏览器或主题。{config.secret_set && !data.secret_readable ? "现有密钥无法读取，请恢复加密密钥。" : ""}</p></div>
        <div className="flex items-center justify-between gap-4"><div><Label htmlFor={`${provider}-register`}>允许首次登录自动注册</Label><p className="mt-1 text-xs text-muted-foreground">关闭后仅允许已绑定的会员登录。相同 {name} 身份在不同站点使用同一会员账号，会员组仍按站点分配。</p></div><Switch id={`${provider}-register`} checked={config.auto_register} disabled={busy} onCheckedChange={auto_register => update({ auto_register })} /></div>
        {provider === "facebook" && <p className="text-xs text-muted-foreground">Facebook 新账号以应用内用户 ID 建立身份，不使用 Facebook 返回的邮箱匹配或合并会员。已有会员请登录后在账号安全中绑定。</p>}
        <p className="text-xs text-muted-foreground">关闭会在全部站点拒绝新的授权和正在进行的登录，已有会员会话继续有效。更新配置会使进行中的授权失效。</p>
        <div className="flex flex-wrap gap-2"><Button type="submit" disabled={busy}>{busy ? "处理中…" : "保存配置"}</Button><Button type="button" variant="outline" disabled={busy || dirty || !data.key_configured || !config.secret_set || !canTest} onClick={() => void run(async () => { const result = await testMemberLogin(provider); window.location.assign(result.url) }, `正在前往 ${name} 授权测试`)}>测试当前域名</Button></div>
      {!canTest && <p className="text-xs text-muted-foreground">请通过下方已列出的站点公开地址打开后台，再测试 {name} 授权。</p>}
      </form>
    </Card>
    <Card className="space-y-4 rounded-xl border p-5 gap-0"><div className="flex flex-wrap items-center justify-between gap-3"><h3 className="font-semibold">全部站点的授权回调地址</h3><Button type="button" variant="outline" disabled={!data.callbacks.length} onClick={() => void navigator.clipboard.writeText(data.callbacks.map(callback => callback.url).join("\n")).then(() => showSuccess("全部回调地址已复制")).catch(showError)}>复制全部</Button></div><p className="text-sm text-muted-foreground">将这些地址加入同一个 {name} Web 客户端的授权回调白名单。新增或修改站点域名后，也需更新 {name} 的授权列表，无需重复填写凭据。</p>
      {data.callbacks.length ? <ScrollArea className="max-h-80" contentClassName="space-y-3">{data.callbacks.map(callback => <Card key={callback.url} className="space-y-2 rounded-lg border p-3 gap-0"><p className="text-sm font-medium">{callback.site_name}</p><div className="flex gap-2"><Input aria-label={`${callback.site_name} 授权回调地址`} dir="ltr" readOnly value={callback.url} /><Button type="button" variant="outline" onClick={() => void navigator.clipboard.writeText(callback.url).then(() => showSuccess("回调地址已复制")).catch(showError)}>复制</Button></div></Card>)}</ScrollArea> : <p className="text-sm text-muted-foreground">暂无可用回调地址，请先在多站点管理中配置站点域名。</p>}
      <p className="text-xs text-muted-foreground">回调地址根据已启用站点的域名、别名、独立语言域名和站点公开地址生成。生产使用 HTTPS；本地开发可在站点公开地址中配置带端口的 HTTP localhost。通配域名需先补充具体域名。</p>
    </Card>
    <Card className="space-y-4 rounded-xl border p-5 gap-0"><div><h3 className="font-semibold">登录加密密钥</h3><p className="mt-1 text-sm text-muted-foreground">服务级密钥保护所有站点的登录凭据，与支付密钥独立。{data.key_configured ? "当前已配置。" : "当前未配置。"}</p></div>
      {data.key_configured ? <><p className="text-sm text-muted-foreground">备份请妥善保管；恢复数据库时也需恢复此密钥。</p><Button variant="outline" disabled={busy} onClick={() => void run(async () => { const blob = await exportMemberLoginKey(); const url = URL.createObjectURL(blob); const a = document.createElement("a"); a.href = url; a.download = "gocms-member-login.key"; a.click(); setTimeout(() => URL.revokeObjectURL(url), 1000) }, "加密密钥备份已下载")}>下载密钥备份</Button></> : <>
        <p className="text-sm text-muted-foreground">首次使用生成密钥；已有登录配置时必须导入原密钥，系统不会覆盖现有密钥文件。</p>
        <Button variant="outline" disabled={busy || !data.key_can_initialize} onClick={() => void run(async () => { await initializeMemberLoginKey("generate", ""); window.dispatchEvent(new Event("member-login-key-updated")) }, "登录加密密钥已生成，请下载备份")}>生成加密密钥</Button>
        <div className="space-y-2"><Label htmlFor={`${provider}-key-file`}>导入密钥备份文件</Label><Input id={`${provider}-key-file`} type="file" disabled={busy || !data.key_can_initialize} onChange={event => { const file = event.target.files?.[0]; if (!file) return; if (file.size > 256) { showError("密钥文件过大"); return }; void file.text().then(value => setImportKey(value.trim())).catch(showError) }} /></div>
        <Button variant="outline" disabled={busy || !data.key_can_initialize || !importKey} onClick={() => void run(async () => { await initializeMemberLoginKey("import", importKey); setImportKey(""); window.dispatchEvent(new Event("member-login-key-updated")) }, "登录加密密钥已恢复")}>导入并验证密钥</Button>
      </>}
    </Card>
    <section className="space-y-2 text-sm text-muted-foreground"><h3 className="font-medium text-foreground">{name} 控制台准备</h3><p>创建应用并配置网页登录、隐私政策及站点域名；Facebook 还需按控制台要求配置数据删除方式、访问权限及上线状态。仅请求登录所需的基本身份权限。测试成功不代表 {name} 已批准生产发布。</p><a className="underline" href={provider === "google" ? "https://console.cloud.google.com/auth/overview" : "https://developers.facebook.com/apps/"} target="_blank" rel="noopener noreferrer">打开 {name} 开发者控制台</a></section>
  </ScrollArea>
}
