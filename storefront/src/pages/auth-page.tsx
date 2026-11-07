import { storefrontName } from "@/lib/store-name"
import { useTranslation } from "react-i18next"
import { useEffect } from "react"
import { ArrowLeft, ArrowRight, Package, ReceiptText, ShoppingBag } from "lucide-react"
import { AuthPanel } from "@/components/app/auth-panel"
import { Loading } from "@/components/app/feedback"
import { authReturnPath, navigate } from "@/lib/router"
import { useShop } from "@/lib/shop-context"

export function AuthPage({ mode, search }: { mode: "login" | "register"; search: string }) {
  const { t } = useTranslation()
  const { member, memberLoading, store, shoppingURL } = useShop()
  const next = authReturnPath(new URLSearchParams(search).get("next"))
  useEffect(() => {
    if (member && !memberLoading) navigate(next, true)
  }, [member, memberLoading, next])
  return <div className="page">
    <a href={shoppingURL} className="text-link mb-7"><ArrowLeft className="size-4 rtl:rotate-180" aria-hidden="true" />{t("common.continueSite")}</a>
    {memberLoading || member ? <Loading /> : <div className="mx-auto grid max-w-5xl overflow-hidden rounded-3xl border bg-card shadow-[0_12px_48px_-24px_#173e3630] md:grid-cols-[.9fr_1.1fr]">
      <aside className="auth-aside hidden flex-col md:flex justify-between gap-10 p-7 text-white sm:p-10 lg:p-12">
        <div><div className="mb-9 flex items-center gap-3"><Package className="size-5 shrink-0 text-white/80" aria-hidden="true" /><bdi className="min-w-0 break-words text-sm font-medium text-white/80">{storefrontName(store.site_name)}</bdi></div><p className="text-xs font-medium tracking-widest text-emerald-100/70">{t("design.accountLabel")}</p><h2 className="mt-4 text-3xl font-medium leading-tight tracking-tight sm:text-4xl">{t("design.accountTitle")}</h2><p className="mt-5 text-sm leading-7 text-white/75">{t("design.accountDescription")}</p></div>
        <div className="space-y-5 border-t border-white/15 pt-7">
          <div className="flex gap-3"><span className="flex size-9 shrink-0 items-center justify-center rounded-xl border border-white/15"><ShoppingBag className="size-4" aria-hidden="true" /></span><div><h3 className="text-sm font-medium">{t("common.cart")}</h3><p className="mt-1 text-xs leading-6 text-white/65">{t("auth.requiredMessage")}</p></div></div>
          <div className="flex gap-3"><span className="flex size-9 shrink-0 items-center justify-center rounded-xl border border-white/15"><ReceiptText className="size-4" aria-hidden="true" /></span><div><h3 className="text-sm font-medium">{t("common.orders")}</h3><p className="mt-1 text-xs leading-6 text-white/65">{t("orders.emptyMessage")}</p></div></div>
          <a href={shoppingURL} className="inline-flex items-center gap-2 pt-1 text-xs font-medium text-white/80 hover:text-white">{t("common.continueSite")}<ArrowRight className="size-3.5 rtl:rotate-180" aria-hidden="true" /></a>
        </div>
      </aside>
      <AuthPanel key={`${mode}:${next}`} mode={mode} next={next} />
    </div>}
  </div>
}
