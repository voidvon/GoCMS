import { loginProviders } from "@/lib/api"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import { Card } from "@/components/ui/card"
import { localError } from "@/lib/errors"
import { useTranslation } from "react-i18next"
import { useState, type ComponentProps, type FormEvent } from "react"
import { ArrowRight, Eye, EyeOff, LoaderCircle, UserRound } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { getLoginProviders, login, register } from "@/lib/api"
import { authURL, Link, navigate } from "@/lib/router"
import { useResource, useShop } from "@/lib/shop-context"
import { ExternalLoginButton } from "./external-login"
import { showError, showSuccess } from "./feedback"

export function AuthRequired({ next }: { next: string }) {
  const { t } = useTranslation()
  return <Card className="panel mx-auto flex max-w-lg flex-col items-center gap-5 py-10 text-center">
    <span className="flex size-14 items-center justify-center rounded-2xl bg-secondary text-primary"><UserRound className="size-6" aria-hidden="true" /></span>
    <h2>{t("auth.requiredTitle")}</h2>
    <p className="muted">{t("auth.requiredMessage")}</p>
    <div className="flex flex-wrap justify-center gap-3">
      <Button className="action-button" nativeButton={false} render={<Link href={authURL("login", next)} />}>{t("common.login")}</Button>
      <Button className="action-button" variant="outline" nativeButton={false} render={<Link href={authURL("register", next)} />}>{t("common.register")}</Button>
    </div>
  </Card>
}

export function AuthPanel({ mode, next }: { mode: "login" | "register"; next: string }) {
  const { t } = useTranslation()
  const { refreshMember } = useShop()
  const [busy, setBusy] = useState(false)
  const registering = mode === "register"
  const providers = useResource("login-providers", getLoginProviders)
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (busy) return
    const data = new FormData(event.currentTarget)
    const username = String(data.get("username") || "").trim()
    const password = String(data.get("password") || "")
    if (registering && /^\d+$/.test(username)) { showError(localError("errors.usernameDigits")); return }
    if (registering && password !== String(data.get("confirm_password") || "")) { showError(localError("errors.passwordMismatch")); return }
    setBusy(true)
    let registered = false
    try {
      if (registering) {
        await register(username, password, String(data.get("email") || "").trim())
        registered = true
        showSuccess(t("auth.created"))
      }
      await login(username, password)
      if (!await refreshMember()) navigate(authURL("login", next), true)
    } catch (error) {
      showError(error)
      if (registered) navigate(authURL("login", next), true)
    } finally { setBusy(false) }
  }
  return <section className="min-w-0 p-6 sm:p-10 lg:p-12">
    <Tabs value={mode} onValueChange={value => { if (value === "login" || value === "register") navigate(authURL(value, next)) }}>
      <TabsList className="mb-6 w-full" aria-label={t("design.accountLabel")}>
        <TabsTrigger value="login" disabled={busy}>{t("common.login")}</TabsTrigger>
        <TabsTrigger value="register" disabled={busy}>{t("common.register")}</TabsTrigger>
      </TabsList>
      <TabsContent value={mode}>
    <div className="mb-7"><h1 className="text-2xl sm:text-3xl">{registering ? t("auth.registerTitle") : t("auth.loginTitle")}</h1><p className="muted mt-3">{registering ? t("auth.registerDescription") : t("auth.loginDescription")}</p></div>
    {(loginProviders.some(provider => providers.data?.[provider] === true)) && <div className="mb-6 space-y-5">{loginProviders.map(provider => providers.data?.[provider] === true && <ExternalLoginButton key={provider} provider={provider} next={next} disabled={busy} />)}<p className="text-center text-xs text-muted-foreground">{t("google.or")}</p></div>}
    <form onSubmit={submit} className="space-y-5">
      <div className="field">
        <Label htmlFor="member-username">{registering ? t("auth.username") : t("auth.identifier")}</Label>
        <Input id="member-username" name="username" autoComplete="username" required minLength={registering ? 3 : undefined} maxLength={registering ? 64 : 254} pattern={registering ? "[A-Za-z0-9_\\-]{3,64}" : undefined} disabled={busy} aria-describedby={registering ? "username-help" : undefined} />
        {registering && <p id="username-help" className="muted">{t("auth.usernameHelp")}</p>}
      </div>
      {registering && <div className="field"><Label htmlFor="member-email">{t("auth.email")}</Label><Input id="member-email" name="email" dir="ltr" type="email" autoComplete="email" maxLength={128} disabled={busy} /></div>}
      <div className="field"><Label htmlFor="member-password">{t("auth.password")}</Label><PasswordInput id="member-password" name="password" autoComplete={registering ? "new-password" : "current-password"} required minLength={registering ? 8 : undefined} maxLength={1024} disabled={busy} aria-describedby={registering ? "password-help" : undefined} />{registering && <p id="password-help" className="muted">{t("auth.passwordHelp")}</p>}</div>
      {registering && <div className="field"><Label htmlFor="member-confirm-password">{t("auth.confirmPassword")}</Label><PasswordInput id="member-confirm-password" name="confirm_password" autoComplete="new-password" required minLength={8} maxLength={1024} disabled={busy} /></div>}
      <Button type="submit" className="action-button w-full" disabled={busy}>{busy && <LoaderCircle className="size-4 animate-spin" aria-hidden="true" />}{busy ? t("common.processing") : registering ? t("auth.registerSubmit") : t("common.login")}{!busy && <ArrowRight className="size-4 rtl:rotate-180" aria-hidden="true" />}</Button>
    </form>
    {!registering && <Link href="/shop/forgot-password" className="text-link mt-5">{t("password.forgot")}</Link>}
    <Button variant="link" nativeButton={false} disabled={busy} className="mt-5 h-auto min-h-10 max-w-full whitespace-normal px-0 text-start" render={<Link href={authURL(registering ? "login" : "register", next)} />}>{registering ? t("auth.toLogin") : t("auth.toRegister")}</Button>
      </TabsContent>
    </Tabs>
  </section>
}

export function PasswordInput(props: ComponentProps<typeof Input>) {
  const { t } = useTranslation()
  const [visible, setVisible] = useState(false)
  return <div className="relative">
    <Input {...props} type={visible ? "text" : "password"} className="pe-12" />
    <Button type="button" variant="ghost" size="icon" className="absolute end-1 top-1 size-9 text-muted-foreground" disabled={props.disabled} aria-label={t(visible ? "design.hidePassword" : "design.showPassword")} aria-controls={props.id} aria-pressed={visible} onClick={() => setVisible(value => !value)}>{visible ? <EyeOff className="size-4" aria-hidden="true" /> : <Eye className="size-4" aria-hidden="true" />}</Button>
  </div>
}
