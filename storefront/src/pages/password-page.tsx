import { useState, type FormEvent } from "react"
import { useTranslation } from "react-i18next"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { PasswordInput } from "@/components/app/auth-panel"
import { ExternalLoginButton } from "@/components/app/external-login"
import { Failure, Loading, showError, showSuccess } from "@/components/app/feedback"
import { changePassword, forgotPassword, getLoginProviders, getPasswordOptions, resetPassword } from "@/lib/api"
import { Link, navigate } from "@/lib/router"
import { useResource, useShop } from "@/lib/shop-context"
import { localError } from "@/lib/errors"

export function ForgotPasswordPage() {
  const { t, i18n } = useTranslation()
  const options = useResource("password-options", getPasswordOptions)
  const providers = useResource("recovery-providers", getLoginProviders)
  const [busy, setBusy] = useState(false)
  const [sent, setSent] = useState(false)
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (busy) return
    const email = String(new FormData(event.currentTarget).get("email") || "").trim()
    setBusy(true)
    try { await forgotPassword(email, i18n.language); setSent(true) } catch (error) { showError(error) } finally { setBusy(false) }
  }
  return <div className="page max-w-xl"><section className="panel space-y-6">
    <h1>{t("password.forgot")}</h1><p className="muted">{t("password.description")}</p>
    {new URLSearchParams(window.location.search).has("recovery_error") && <p role="alert" className="text-sm text-destructive">{t("password.recoveryFailed")}</p>}
    {options.loading ? <Loading /> : options.error ? <Failure message={options.error} retry={options.reload} /> : options.data?.data.email_enabled ? sent ? <p role="status" className="rounded-xl bg-muted p-4 text-sm leading-7">{t("password.sent")}</p> : <form onSubmit={submit} className="space-y-5">
      <div className="field"><Label htmlFor="recovery-email">{t("checkout.email")}</Label><Input id="recovery-email" name="email" type="email" dir="ltr" autoComplete="email" required maxLength={254} disabled={busy} /></div>
      <Button type="submit" className="w-full" disabled={busy}>{t(busy ? "common.processing" : "password.send")}</Button>
    </form> : <p className="muted">{t("password.emailUnavailable")}</p>}
    {options.data?.data.support_email && <a className="text-link break-all" href={`mailto:${encodeURIComponent(options.data.data.support_email)}`}>{t("password.contact", { email: options.data.data.support_email })}</a>}
    {providers.error ? <Failure message={providers.error} retry={providers.reload} /> : (providers.data?.google || providers.data?.facebook) && <div className="space-y-4 border-t pt-5"><p className="muted">{t("password.verifyDescription")}</p>{(["google", "facebook"] as const).map(provider => providers.data?.[provider] && <ExternalLoginButton key={provider} provider={provider} recover next="/shop/account" disabled={busy} />)}</div>}
    <Link href="/shop/login" className="text-link">{t("password.backLogin")}</Link>
  </section></div>
}

export function ResetPasswordPage() {
  const { t } = useTranslation()
  const { refreshMember } = useShop()
  // Fragments are not sent in HTTP requests or Referer headers.
  const [token] = useState(() => new URLSearchParams(window.location.hash.slice(1)).get("token") || "")
  const [busy, setBusy] = useState(false)
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (busy) return
    const form = new FormData(event.currentTarget), password = String(form.get("password") || "")
    if (password !== String(form.get("confirm") || "")) { showError(localError("errors.passwordMismatch")); return }
    setBusy(true)
    try { await resetPassword(token, password); await refreshMember(); showSuccess(t("password.changed")); navigate("/shop/login?next=%2Fshop%2Faccount", true) } catch (error) { showError(error) } finally { setBusy(false) }
  }
  return <div className="page max-w-xl"><section className="panel space-y-6"><h1>{t("password.resetTitle")}</h1><p className="muted">{t("password.shared")}</p>
    {token.length === 43 ? <form onSubmit={submit} className="space-y-5"><NewPasswordFields busy={busy} /><Button type="submit" className="w-full" disabled={busy}>{t(busy ? "common.processing" : "password.resetSubmit")}</Button></form> : <p role="alert" className="text-sm text-destructive">{t("errors.invalid_reset_token")}</p>}
    <Link href="/shop/forgot-password" className="text-link">{t("password.forgot")}</Link>
  </section></div>
}
function NewPasswordFields({ busy }: { busy: boolean }) {
  const { t } = useTranslation()
  return <><div className="field"><Label htmlFor="new-password">{t("password.new")}</Label><PasswordInput id="new-password" name="password" autoComplete="new-password" required minLength={8} maxLength={1024} disabled={busy} aria-describedby="new-password-help" /><p id="new-password-help" className="muted">{t("auth.passwordHelp")}</p></div><div className="field"><Label htmlFor="confirm-password">{t("auth.confirmPassword")}</Label><PasswordInput id="confirm-password" name="confirm" autoComplete="new-password" required minLength={8} maxLength={1024} disabled={busy} /></div></>
}
export function ChangePasswordPanel() {
  const { t } = useTranslation()
  const { member, refreshMember } = useShop()
  const [busy, setBusy] = useState(false)
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (busy) return
    const form = new FormData(event.currentTarget), password = String(form.get("password") || "")
    if (password !== String(form.get("confirm") || "")) { showError(localError("errors.passwordMismatch")); return }
    setBusy(true)
    try { await changePassword(String(form.get("old") || ""), password); await refreshMember(); showSuccess(t("password.changed")); navigate("/shop/login?next=%2Fshop%2Faccount", true) } catch (error) { showError(error) } finally { setBusy(false) }
  }
  return <section className="panel mt-6 space-y-5"><h2>{t("password.changeTitle")}</h2><p className="muted">{t("password.shared")}</p>
    {member?.has_password ? <form onSubmit={submit} className="space-y-5"><div className="field"><Label htmlFor="current-password">{t("password.old")}</Label><PasswordInput id="current-password" name="old" autoComplete="current-password" required maxLength={1024} disabled={busy} /></div><NewPasswordFields busy={busy} /><Button type="submit" disabled={busy}>{t(busy ? "common.processing" : "password.changeSubmit")}</Button></form> : <><p className="muted">{t("password.noPassword")}</p><Link href="/shop/forgot-password" className="text-link">{t("password.setPassword")}</Link></>}
  </section>
}
