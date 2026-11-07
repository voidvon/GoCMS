import { ChangePasswordPanel } from "./password-page"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { AuthRequired } from "@/components/app/auth-panel"
import { ExternalLoginButton } from "@/components/app/external-login"
import { Failure, Loading, showError, showSuccess } from "@/components/app/feedback"
import { getProviderIdentity, getLoginProviders, unbindProvider } from "@/lib/api"
import { useResource, useShop } from "@/lib/shop-context"

export function AccountPage() {
  const { member, memberLoading } = useShop()
  if (memberLoading) return <Loading />
  if (!member) return <div className="page"><AuthRequired next="/shop/account" /></div>
  return <AccountSecurity key={member.id} />
}
function AccountSecurity() {
 const {t}=useTranslation()
 const {member}=useShop()
 const providers=useResource("login-providers",getLoginProviders)
 return <div className="page max-w-2xl"><h1>{t("google.account")}</h1><p className="muted mt-3"><bdi>{member?.display_name || member?.username}</bdi></p><ChangePasswordPanel />{providers.loading ? <Loading/> : providers.error ? <Failure message={providers.error} retry={providers.reload}/> : (["google","facebook"] as const).map(provider=>providers.data?.[provider]===true && <ProviderSecurity key={provider} provider={provider}/>)}</div>
}
function ProviderSecurity({provider}:{provider:"google"|"facebook"}) {
  const { t } = useTranslation()
  const status = useResource(`${provider}-identity`, async () => (await getProviderIdentity(provider)).data)
  const [password, setPassword] = useState("")
  const [busy, setBusy] = useState(false)
  const [confirm, setConfirm] = useState(false)
  async function unbind() {
    setBusy(true)
    try { await unbindProvider(provider,password); setPassword(""); setConfirm(false); showSuccess(t(`${provider}.unbound`)); status.reload() }
    catch (error) { showError(error) } finally { setBusy(false) }
  }
  return <div>
    {status.loading ? <Loading /> : status.error ? <Failure message={status.error} retry={status.reload} /> : status.data && <section className="panel mt-6 space-y-5">
      <p className="muted">{t(`${provider}.sharedNotice`)}</p>
      <div><h2>{t(`${provider}.identity`)}</h2><p className="muted mt-2">{t(status.data.bound ? `${provider}.bound` : `${provider}.notBound`)}</p></div>
      {status.data.has_password && <div className="field"><Label htmlFor={`${provider}-password`}>{t(`${provider}.password`)}</Label><Input id={`${provider}-password`} type="password" autoComplete="current-password" maxLength={1024} value={password} disabled={busy} onChange={event => setPassword(event.target.value)} /></div>}
      {!status.data.bound && <ExternalLoginButton provider={provider} next="/shop/account" bind password={password} disabled={status.data.has_password && !password} />}
      {status.data.bound && (status.data.has_password ? confirm ? <div className="space-y-3"><p className="muted">{t(`${provider}.confirmUnbind`)}</p><div className="flex flex-wrap gap-3"><Button disabled={busy || !password} onClick={() => void unbind()}>{t(`${provider}.unbind`)}</Button><Button variant="outline" disabled={busy} onClick={() => setConfirm(false)}>{t(`${provider}.cancel`)}</Button></div></div> : <Button variant="outline" disabled={busy || !password} onClick={() => setConfirm(true)}>{t(`${provider}.unbind`)}</Button> : <p className="muted">{t(`${provider}.keepAccess`)}</p>)}
    </section>}
  </div>
}
