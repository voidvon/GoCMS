import { useTranslation } from "react-i18next"
import { ArrowLeft, ArrowRight, Info, Minus, Plus, ShoppingBag, Trash2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { money, specification } from "@/lib/api"
import { useShop, useCartVariants } from "@/lib/shop-context"
import { Link } from "@/lib/router"
import { Empty, Failure, Loading } from "@/components/app/feedback"
import { CheckoutSteps, PageHeading, ProductImage } from "@/components/app/commerce-ui"

export function CartPage() {
  const { t } = useTranslation()
  const { cart, update, remove, shoppingURL } = useShop()
  const resource = useCartVariants()
  const offers = resource.data?.items ?? []
  const missing = cart.some(line => !offers.some(offer => offer.id === line.variant_id))
  const count = cart.reduce((sum, line) => sum + line.quantity, 0)
  const totals = new Map<string, number>()
  for (const line of cart) {
    const item = offers.find(offer => offer.id === line.variant_id)
    if (item) totals.set(item.currency, (totals.get(item.currency) ?? 0) + item.amount * line.quantity)
  }
  return <div className="page">
    <PageHeading title={t("common.cart")} description={t("cart.description")} action={<a href={shoppingURL} className="text-link"><ArrowLeft className="size-4 rtl:rotate-180" aria-hidden="true" />{t("common.continueSite")}</a>} />
    {!cart.length ? <Empty href={shoppingURL} title={t("cart.emptyTitle")} message={t("cart.emptyMessage")} /> : resource.loading ? <Loading /> : resource.error ? <Failure message={resource.error} retry={resource.reload} /> : <>
      <CheckoutSteps current={1} />
      <div className="checkout-grid">
        <section aria-label={t("cart.linesLabel")} className="min-w-0 rounded-2xl border bg-card">
          <div className="flex flex-wrap items-center justify-between gap-2 border-b px-5 py-4 sm:px-7"><h2 className="flex items-center gap-2 text-base"><ShoppingBag className="size-4 text-primary" aria-hidden="true" />{t("cart.linesLabel")}</h2><span className="muted text-xs">{t("cart.items", { count })}</span></div>
          <div className="divide-y px-5 sm:px-7">{cart.map(line => {
            const item = offers.find(offer => offer.id === line.variant_id)
            return <article key={line.variant_id} className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-4 py-6 sm:gap-x-5">
              <ProductImage src={item?.image_url} className="row-span-2" />
              <div className="flex min-w-0 flex-wrap items-start justify-between gap-3">
                <div className="min-w-0 flex-1"><h2 className="text-base leading-6"><bdi>{item?.name ?? t("cart.unavailable")}</bdi></h2><p className="muted mt-1.5 break-words">{item ? specification(item) : t("cart.unavailableDetail", { id: line.variant_id })}</p>{item && <p className="mt-2 text-xs text-muted-foreground">{t("cart.unitPrice", { price: money(item.amount, item.currency) })}</p>}</div>
                <Button variant="ghost" size="icon" className="size-9 text-muted-foreground hover:bg-destructive/10 hover:text-destructive" aria-label={t("cart.remove", { name: item?.name ?? t("cart.unavailable") })} onClick={() => remove(line.variant_id)}><Trash2 className="size-4" /></Button>
              </div>
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div className="quantity-control"><Button variant="ghost" size="icon" className="size-9" aria-label={t("cart.decrease")} disabled={line.quantity <= 1} onClick={() => update(line.variant_id, line.quantity - 1)}><Minus className="size-3.5" /></Button><Input type="number" className="h-9 w-14 rounded-none border-0 bg-transparent text-center text-base shadow-none focus-visible:relative" aria-label={t("cart.quantity", { name: item?.name ?? t("cart.product") })} min={1} max={1000} value={line.quantity} onChange={event => update(line.variant_id, Number(event.target.value))} /><Button variant="ghost" size="icon" className="size-9" aria-label={t("cart.increase")} disabled={line.quantity >= 1000} onClick={() => update(line.variant_id, line.quantity + 1)}><Plus className="size-3.5" /></Button></div>
                {item && <p className="text-base font-semibold tabular-nums"><bdi>{money(item.amount * line.quantity, item.currency)}</bdi></p>}
              </div>
            </article>
          })}</div>
        </section>
        <aside className="panel summary-panel space-y-6">
          <div><p className="eyebrow mb-2">{t("checkout.review")}</p><h2>{t("cart.summary")}</h2></div>
          <p className="muted">{t("cart.items", { count })}</p>
          {missing && <p className="rounded-lg bg-destructive/5 p-3 text-sm leading-6 text-destructive" role="alert">{t("cart.removeUnavailable")}</p>}
          {[...totals].map(([currency, total]) => <div className="space-y-5 border-t pt-5" key={currency}>
            <div className="space-y-2"><p className="muted">{t("cart.subtotal", { currency })}</p><strong className="block text-3xl font-semibold tracking-tight tabular-nums"><bdi>{money(total, currency)}</bdi></strong></div>
            <Button className="action-button h-auto w-full justify-between whitespace-normal py-3 text-start" disabled={missing} nativeButton={false} render={<Link href={`/shop/checkout?currency=${currency}`} />}>{totals.size > 1 ? t("cart.checkoutCurrency", { currency }) : t("cart.checkout")}<ArrowRight className="size-4 rtl:rotate-180" aria-hidden="true" /></Button>
          </div>)}
          <div className="flex items-start gap-2.5 border-t pt-5 text-xs leading-6 text-muted-foreground"><Info className="mt-1 size-4 shrink-0" aria-hidden="true" /><p>{t("cart.priceNote")}</p></div>
        </aside>
      </div>
    </>}
  </div>
}
