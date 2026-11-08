import { ForgotPasswordPage, ResetPasswordPage } from "@/pages/password-page"
import { storefrontName } from "@/lib/store-name"
import { configureSiteLanguage, formatNumber } from "@/i18n"
import { CurrencySwitcher } from "@/components/app/currency-switcher"
import { LanguageSwitcher } from "@/components/app/language-switcher"
import { useTranslation } from "react-i18next"
import { useEffect, useState } from "react"
import { ArrowLeft, LogOut, Package, ReceiptText, ShoppingBag, UserRound } from "lucide-react"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Toaster } from "@/components/ui/sonner"
import { getStore, logout, setSiteScope } from "@/lib/api"
import { authURL, Link, navigate, useRoute } from "@/lib/router"
import { ShopProvider, useResource, useShop } from "@/lib/shop-context"
import { Failure, Loading, showError } from "@/components/app/feedback"
import { AddToCartPage } from "@/pages/add-to-cart-page"
import { AuthPage } from "@/pages/auth-page"
import { CartPage } from "@/pages/cart-page"
import { CheckoutPage } from "@/pages/checkout-page"
import { AccountPage } from "@/pages/account-page"
import { apiErrorKey, localError } from "@/lib/errors"
import { OrdersPage, PaymentLinkPage } from "@/pages/orders-page"

function Shell() {
  const { t } = useTranslation()
  const route = useRoute(), url = new URL(route, window.location.origin)
  const { store, member, memberLoading, cart, refreshMember } = useShop()
  const [busy, setBusy] = useState(false)
  const path = url.pathname
  const title = path.startsWith("/shop/pay/") ? t("paymentLink.title") : path === "/shop/forgot-password" ? t("password.forgot") : path === "/shop/reset-password" ? t("password.resetTitle") : path === "/shop/account" ? t("google.account") : path === "/shop/login" ? t("auth.loginTitle") : path === "/shop/register" ? t("auth.registerTitle") : path === "/shop/checkout" ? t("checkout.title") : path === "/shop/cart/add" ? t("add.title") : path.startsWith("/shop/orders/") ? t("orders.detail") : path === "/shop/orders" ? t("common.orders") : t("common.cart")
  useEffect(() => {
    const oauthError = url.searchParams.get("oauth_error")
    if (oauthError) {
      const codes = ["microsoft_failed", "microsoft_cancelled", "microsoft_unavailable", "microsoft_conflict", "facebook_failed", "facebook_cancelled", "facebook_unavailable", "facebook_conflict", "google_failed", "google_cancelled", "google_unavailable", "google_conflict", "session_limit_reached"]
      showError(localError(apiErrorKey(codes.includes(oauthError) ? oauthError : "google_failed", 400)))
      url.searchParams.delete("oauth_error")
      window.history.replaceState(null, "", url.pathname + url.search)
    }
    document.title = t("common.pageTitle", { page: title, site: storefrontName(store.site_name) })
    const returned = url.searchParams.get("payment_order")
    if (returned) navigate(`/shop/orders/${encodeURIComponent(returned)}?returned=1`, true)
    else if (path === "/shop/" || path === "/shop") navigate("/shop/cart", true)
  }, [store.site_name, route, title, t])
  async function leave() {
    if (busy) return
    setBusy(true)
    try { await logout(); await refreshMember() } catch (error) { showError(error) }
    finally { setBusy(false) }
  }
  const count = cart.reduce((total, item) => total + item.quantity, 0)
  const next = path === "/shop/login" || path === "/shop/register" ? url.searchParams.get("next") || "/shop/cart" : route
  const handoffParams = new URLSearchParams(url.search)
  handoffParams.delete("lang")
  let page
  if (path === "/shop/forgot-password") page = <ForgotPasswordPage />
  else if (path === "/shop/reset-password") page = <ResetPasswordPage />
  else if (path === "/shop/account") page = <AccountPage />
  else if (path === "/shop/cart/add") page = <AddToCartPage key={handoffParams.toString()} search={url.search} />
  else if (path === "/shop/cart" || path === "/shop/" || path === "/shop") page = <CartPage />
  else if (path === "/shop/login" || path === "/shop/register") page = <AuthPage mode={path === "/shop/login" ? "login" : "register"} search={url.search} />
  else if (path === "/shop/checkout") page = <CheckoutPage search={url.search} />
  else if (/^\/shop\/pay\/ord_[a-f0-9]{32}$/.test(path)) page = <PaymentLinkPage key={path} id={path.split("/")[3]} search={url.search} />
  else if (path === "/shop/orders" || path.startsWith("/shop/orders/")) page = <OrdersPage id={path.split("/")[3]} search={url.search} />
  else page = <div className="page"><div className="panel py-16 text-center"><h1>{t("common.notFound")}</h1><Link href="/shop/cart" className="text-link mt-6">{t("common.backCart")}</Link></div></div>
  return <div className="storefront grid h-dvh grid-rows-[auto_minmax(0,1fr)]">
    <a href="#shop-content" className="sr-only z-50 bg-card p-3 focus:not-sr-only focus:absolute">{t("design.skipContent")}</a>
    <header className="z-10 border-b bg-card">
      <div className="mx-auto flex max-w-6xl flex-wrap items-center gap-x-6 gap-y-3 px-4 py-4 sm:px-8 sm:py-5">
        <a href="/" className="flex min-w-0 flex-1 items-center gap-3 sm:flex-none">
          <span className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-primary text-primary-foreground"><Package className="size-5" aria-hidden="true" /></span>
          <span className="min-w-0"><bdi className="block max-w-44 truncate text-base font-semibold sm:max-w-56">{storefrontName(store.site_name)}</bdi><span className="mt-0.5 block text-[11px] text-muted-foreground">{t("design.shoppingLabel")}</span></span>
        </a>
        <a href="/" className="text-link ms-auto hidden text-xs sm:inline-flex sm:text-sm"><ArrowLeft className="size-3.5 rtl:rotate-180" aria-hidden="true" />{t("common.backSite")}</a>
        <div className="hidden h-6 w-px bg-border md:block" aria-hidden="true" />
        <div className="ms-auto flex items-center gap-2 sm:ms-0 flex-wrap justify-end"><CurrencySwitcher /><LanguageSwitcher />{member && <Button variant="ghost" size="icon" className="size-10" disabled={busy} title={t("common.logout", { username: member.username })} aria-label={t("common.logout", { username: member.username })} onClick={() => void leave()}><LogOut className="size-4" /></Button>}</div>
      </div>
      <div className="border-t border-border/60"><nav className="mx-auto flex max-w-6xl flex-wrap items-center gap-1 px-4 py-2 sm:px-8" aria-label={t("common.navigation")}>
        <Link href="/shop/cart" className="nav-link" aria-current={path === "/shop/cart" || path === "/shop/checkout" || path === "/shop/cart/add" ? "page" : undefined}><ShoppingBag className="size-4" aria-hidden="true" />{t("common.cart")}{count > 0 && <bdi className="flex min-w-5 items-center justify-center rounded-full bg-primary px-1.5 text-[10px] leading-5 text-primary-foreground">{formatNumber(count)}</bdi>}</Link>
        <Link href="/shop/orders" className="nav-link" aria-current={path.startsWith("/shop/orders") ? "page" : undefined}><ReceiptText className="size-4" aria-hidden="true" />{t("common.orders")}</Link>
        <div className="ms-auto min-w-0">{member ? <Link href="/shop/account" aria-label={t("google.account")} className="flex items-center gap-2 px-2 text-sm text-muted-foreground"><UserRound className="size-4 shrink-0" aria-hidden="true" /><bdi className="max-w-28 truncate sm:max-w-52">{member.display_name || member.username}</bdi></Link> : !memberLoading && <Link href={authURL("login", next)} className="nav-link" aria-current={path === "/shop/login" || path === "/shop/register" ? "page" : undefined}><UserRound className="size-4" aria-hidden="true" />{t("common.login")}</Link>}</div>
      </nav></div>
    </header>
    <ScrollArea key={path} className="min-h-0" contentClassName="flex min-h-full flex-col">
      <main id="shop-content" tabIndex={-1} className="flex-1 outline-none">{page}</main>
      <footer className="border-t bg-card/60"><div className="mx-auto flex max-w-6xl flex-wrap items-center justify-between gap-4 px-4 py-6 text-xs text-muted-foreground sm:px-8"><span className="flex items-center gap-2"><Package className="size-3.5" aria-hidden="true" /><bdi>{storefrontName(store.site_name)}</bdi></span><a href="/" className="hover:text-primary">{t("common.continueSite")}</a></div></footer>
    </ScrollArea>
  </div>
}
export default function App() {
  const { t, i18n } = useTranslation()
  const store = useResource("storefront", async () => { const result = await getStore(); setSiteScope(result.site_id); configureSiteLanguage(result.site_id, result.default_language); return result })
  return <>{store.loading ? <Loading label={t("common.opening")} /> : store.error ? <div className="page"><Failure message={store.error} retry={store.reload} /></div> : store.data ? <ShopProvider key={store.data.site_id} store={store.data}><Shell /></ShopProvider> : null}<Toaster position="top-center" richColors closeButton dir={i18n.dir()} containerAriaLabel={t("common.notifications")} toastOptions={{ closeButtonAriaLabel: t("common.close") }} /></>
}
