import { ArrowLeft, ArrowRight, CreditCard, Info, LoaderCircle, Mail, RotateCcw } from "lucide-react"
import { CheckoutSteps, PageHeading, ProductImage, SectionHeading } from "@/components/app/commerce-ui"
import { ShippingAddress } from "@/components/app/shipping-address"
import { localError } from "@/lib/errors"
import { useTranslation } from "react-i18next"
import { useState, type FormEvent } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from "@/components/ui/select"
import { APIError, createOrder, getChannels, money, payOrder, specification, type Address, type OrderInput } from "@/lib/api"
import { useShop, useCartVariants, useResource } from "@/lib/shop-context"
import { Link, navigate } from "@/lib/router"
import { AuthRequired } from "@/components/app/auth-panel"
import { Empty, Failure, Loading, showError } from "@/components/app/feedback"
type Pending = { key: string; body: string; channel: number }
function readPending(key: string): Pending | null {
  try { const value = JSON.parse(sessionStorage.getItem(key) || "null"); return value && typeof value.key === "string" && typeof value.body === "string" && Number.isSafeInteger(value.channel) ? value : null } catch { return null }
}
function CheckoutForm({ currency }: { currency: string | null }) {
  const { t } = useTranslation()
  const { store, member, cart, consume, refreshMember, shoppingURL } = useShop()
  const variants = useCartVariants()
  const channelResource = useResource("checkout:channels", () => getChannels())
  const pendingKey = `gocms-checkout:${store.site_id}:${member!.id}`
  const [pending, setPending] = useState<Pending | null>(() => readPending(pendingKey))
  const [busy, setBusy] = useState(false)
  const [channel, setChannel] = useState("")
  const offers = variants.data?.items ?? []
  const selectedCurrency = currency || offers[0]?.currency || "USD"
  const lines = cart.flatMap(line => { const item = offers.find(offer => offer.id === line.variant_id); return item?.currency === selectedCurrency ? [{ ...item, quantity: line.quantity }] : [] })
  const total = lines.reduce((sum, line) => sum + line.amount * line.quantity, 0)
  const physical = lines.some(line => line.kind === "physical")
  const missing = cart.some(line => !offers.some(item => item.id === line.variant_id))
  const channels = channelResource.data?.channels.filter(item => item.currency === selectedCurrency) ?? []
  const channelID = channel || String(channels[0]?.id ?? "")
  async function place(saved: Pending) {
    if (busy) return
    setBusy(true)
    let ordered = false
    try {
      const order = await createOrder(saved.body, saved.key)
      ordered = true
      consume((JSON.parse(saved.body) as OrderInput).items)
      try { sessionStorage.removeItem(pendingKey) } catch {}
      setPending(null)
      navigate(`/shop/orders/${encodeURIComponent(order.id)}`)
      const attempt = await payOrder(order.id, saved.channel)
      if (!attempt.payment_url) throw localError("errors.paymentPending")
      window.location.assign(attempt.payment_url)
    } catch (error) {
      if (!ordered && error instanceof APIError && ([400, 404].includes(error.status) || error.code === "price_changed")) {
        try { sessionStorage.removeItem(pendingKey) } catch {}
        setPending(null); variants.reload()
      }
      showError(error)
      if (error instanceof APIError && error.status === 401) await refreshMember()
    } finally { setBusy(false) }
  }
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (busy || pending) return
    const form = new FormData(event.currentTarget)
    if (!channelID || !lines.length || missing) { showError(localError("errors.checkOptionsChannel")); return }
    const address: Address = { street: "", city: "", state: "", postcode: "", country: "" }
    if (physical) for (const key of Object.keys(address) as (keyof Address)[]) address[key] = String(form.get(key) || "").trim()
    address.country = address.country.toUpperCase()
    const input: OrderInput = { items: lines.map(line => ({ variant_id: line.id, quantity: line.quantity })), customer_name: String(form.get("customer_name")).trim(), email: String(form.get("email")).trim(), address, currency: selectedCurrency, expected_amount: total }
    const saved = { key: crypto.randomUUID(), body: JSON.stringify(input), channel: Number(channelID) }
    try { sessionStorage.setItem(pendingKey, JSON.stringify(saved)); setPending(saved) }
    catch { showError(localError("errors.checkoutStorage")); return }
    void place(saved)
  }
  if (pending) return <div className="panel mx-auto max-w-xl space-y-5"><span className="section-icon"><RotateCcw className="size-4" aria-hidden="true" /></span><h2>{t("checkout.resumeTitle")}</h2><p className="muted">{t("checkout.resumeMessage")}</p><Button className="action-button h-auto whitespace-normal py-3" disabled={busy} onClick={() => void place(pending)}>{busy && <LoaderCircle className="size-4 animate-spin" aria-hidden="true" />}{busy ? t("checkout.resuming") : t("checkout.resume")}</Button><Link className="block text-sm underline underline-offset-4" href="/shop/orders">{t("common.orders")}</Link></div>
  if (variants.loading || channelResource.loading) return <Loading />
  if (variants.error || channelResource.error) return <Failure message={variants.error || channelResource.error} retry={() => { variants.reload(); channelResource.reload() }} />
  if (!cart.length || !lines.length) return <Empty href={shoppingURL} title={t("checkout.emptyTitle")} message={t("checkout.emptyMessage")} />
  if (missing) return <div className="panel space-y-4"><h2>{t("checkout.unavailableTitle")}</h2><p className="muted">{t("checkout.unavailableMessage")}</p><Button nativeButton={false} render={<Link href="/shop/cart" />}>{t("common.backCart")}</Button></div>
  return <form onSubmit={submit} className="checkout-grid" aria-busy={busy}>
    <div className="min-w-0 space-y-6">
      <section className="panel form-panel">
        <SectionHeading icon={<Mail className="size-4" />}>{t("checkout.contact")}</SectionHeading>
        <div className="grid gap-5 sm:grid-cols-2">
          <div className="field"><Label htmlFor="customer-name">{t("checkout.name")}</Label><Input id="customer-name" name="customer_name" defaultValue={member?.display_name || member?.username} required maxLength={100} autoComplete="name" disabled={busy} /></div>
          <div className="field"><Label htmlFor="customer-email">{t("checkout.email")}</Label><Input id="customer-email" name="email" dir="ltr" type="email" defaultValue={member?.email} required maxLength={128} autoComplete="email" disabled={busy} /></div>
        </div>
      </section>
      {physical && <ShippingAddress disabled={busy} />}
      <section className="panel form-panel">
        <SectionHeading icon={<CreditCard className="size-4" />}>{t("checkout.paymentMethod")}</SectionHeading>
        {channels.length ? <div className="space-y-5">
          <div className="field"><Label htmlFor="checkout-channel">{t("checkout.channel")}</Label><Select disabled={busy} value={channelID} onValueChange={value => { if (value) setChannel(value) }}><SelectTrigger id="checkout-channel" className="w-full" aria-label={t("checkout.channel")}><SelectValue>{channels.find(item => String(item.id) === channelID)?.name}</SelectValue></SelectTrigger><SelectContent>{channels.map(item => <SelectItem key={item.id} value={String(item.id)}>{item.name}{item.sandbox ? ` (${t("common.sandbox")})` : ""}</SelectItem>)}</SelectContent></Select></div>
          {channels.find(item => String(item.id) === channelID)?.sandbox && <span className="status-badge status-waiting">{t("common.sandbox")}</span>}
          <p className="muted rounded-xl bg-muted p-4">{t("checkout.redirectNote")}</p>
        </div> : <p className="rounded-xl bg-amber-50 p-4 text-sm leading-7 text-amber-900" role="status">{t("checkout.noChannels")}</p>}
      </section>
    </div>
    <aside className="panel summary-panel space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3"><h2 className="text-lg">{t("checkout.review")}</h2><Link href="/shop/cart" className="text-link text-xs">{t("checkout.editCart")}</Link></div>
      <div className="divide-y">{lines.map(line => <div className="flex gap-3 py-4 first:pt-0" key={line.id}>
        <ProductImage src={line.image_url} className="size-14 rounded-lg p-1.5 sm:size-14 sm:p-1.5" />
        <div className="min-w-0 flex-1"><p className="text-sm font-medium leading-6">{t("checkout.line", { name: line.name, count: line.quantity })}</p><p className="mt-1 break-words text-xs leading-5 text-muted-foreground">{specification(line)}</p><p className="mt-2 text-sm font-semibold tabular-nums"><bdi>{money(line.amount * line.quantity, line.currency)}</bdi></p></div>
      </div>)}</div>
      <div className="space-y-2 border-t pt-5"><span className="muted">{t("checkout.subtotal")}</span><strong className="block text-3xl font-semibold tracking-tight tabular-nums"><bdi>{money(total, selectedCurrency)}</bdi></strong></div>
      <Button className="action-button h-auto w-full justify-between whitespace-normal py-3 text-start" type="submit" disabled={busy || !channels.length}>{busy ? <LoaderCircle className="size-4 animate-spin" aria-hidden="true" /> : <CreditCard className="size-4" aria-hidden="true" />}<span className="flex-1">{busy ? t("checkout.submitting") : t("checkout.submit")}</span><ArrowRight className="size-4 rtl:rotate-180" aria-hidden="true" /></Button>
      <div className="flex items-start gap-2 text-xs leading-6 text-muted-foreground"><Info className="mt-1 size-3.5 shrink-0" aria-hidden="true" /><p>{t("checkout.redirectNote")}</p></div>
    </aside>
  </form>
}
export function CheckoutPage({ search }: { search: string }) {
  const { t } = useTranslation()
  const { member, memberLoading } = useShop()
  const currency = new URLSearchParams(search).get("currency")
  return <div className="page">
    <PageHeading title={t("checkout.title")} description={t("checkout.description")} action={<Link href="/shop/cart" className="text-link"><ArrowLeft className="size-4 rtl:rotate-180" aria-hidden="true" />{t("common.backCart")}</Link>} />
    <CheckoutSteps current={2} />
    {memberLoading ? <Loading /> : !member ? <AuthRequired next={`/shop/checkout${search}`} /> : <CheckoutForm key={member.id} currency={currency} />}
  </div>
}
