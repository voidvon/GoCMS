import { formatDate, type MessageKey } from "@/i18n"
import { localError } from "@/lib/errors"
import { useTranslation } from "react-i18next"
import { useEffect, useState } from "react"
import { ArrowLeft, ArrowRight, Check, CircleAlert, CircleCheck, Clock3, CreditCard, LoaderCircle, PackageCheck, ReceiptText, RefreshCw, Undo2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from "@/components/ui/select"
import { getChannels, getOrder, getOrders, money, payOrder, specification, syncOrder, type Channel, type Order } from "@/lib/api"
import { useResource, useShop } from "@/lib/shop-context"
import { Link } from "@/lib/router"
import { PageHeading, ProductImage, SectionHeading } from "@/components/app/commerce-ui"
import { cn } from "@/lib/utils"
import { AuthRequired } from "@/components/app/auth-panel"
import { Empty, Failure, Loading, showError } from "@/components/app/feedback"
const states: Record<string, MessageKey> = { unpaid: "orders.unpaid", paid: "orders.paid", refunded: "orders.refunded" }
const fulfillment: Record<string, MessageKey> = { pending: "orders.fulfillmentPending", processing: "orders.fulfillmentProcessing", completed: "orders.fulfillmentCompleted", failed: "orders.fulfillmentFailed" }
function OrderList({ page }: { page: number }) {
  const { t } = useTranslation()
  const { shoppingURL } = useShop()
  const resource = useResource(`orders:${page}`, () => getOrders(page))
  if (resource.loading) return <Loading />
  if (resource.error) return <Failure message={resource.error} retry={resource.reload} />
  const items = resource.data?.items ?? []
  if (!items.length) return <Empty href={shoppingURL} title={t("orders.emptyTitle")} message={t("orders.emptyMessage")} />
  return <>
    <div className="space-y-5">{items.map(order => <article className="min-w-0 rounded-2xl border bg-card transition-shadow hover:shadow-sm" key={order.id}>
      <div className="flex flex-wrap items-center justify-between gap-3 border-b px-5 py-4 sm:px-6">
        <div className="min-w-0"><p className="mb-1 text-xs text-muted-foreground">{t("design.orderNumber")}</p><Link href={`/shop/orders/${order.id}`} className="break-all font-mono text-xs font-medium hover:text-primary"><bdi>{order.id}</bdi></Link></div>
        <PaymentBadge status={order.status} />
      </div>
      <div className="flex flex-wrap items-center gap-5 p-5 sm:p-6">
        <ProductImage src={order.items[0]?.image_url} />
        <div className="min-w-0 flex-1 basis-36"><h2 className="text-base leading-6"><bdi>{order.items[0]?.name}</bdi>{order.items.length > 1 ? t("orders.moreOptions", { count: order.items.length }) : ""}</h2><p className="muted mt-2">{t("cart.items", { count: order.quantity })}</p><p className="mt-2 text-xs leading-5 text-muted-foreground">{t("orders.created", { date: formatDate(order.created_at) })}</p></div>
        <div className="flex w-full items-center justify-between gap-4 border-t pt-4 sm:w-auto sm:flex-col sm:items-end sm:border-0 sm:pt-0"><strong className="text-lg font-semibold tabular-nums"><bdi>{money(order.amount, order.currency)}</bdi></strong><Button className="action-button" variant="outline" nativeButton={false} render={<Link href={`/shop/orders/${order.id}`} />}>{t("orders.view")}<ArrowRight className="size-4 rtl:rotate-180" aria-hidden="true" /></Button></div>
      </div>
      {order.status === "paid" && <div className="flex flex-wrap items-center gap-3 rounded-b-2xl border-t bg-muted/40 px-5 py-3 sm:px-6"><span className="text-xs text-muted-foreground">{t("design.fulfillmentStatus")}</span><FulfillmentBadge status={order.fulfillment_status} /></div>}
    </article>)}</div>
    <div className="mt-8 flex justify-end gap-3"><Button className="action-button" variant="outline" disabled={page === 1} nativeButton={false} render={<Link href={`/shop/orders?page=${page - 1}`} />}>{t("common.previous")}</Button><Button className="action-button" variant="outline" disabled={items.length < 50} nativeButton={false} render={<Link href={`/shop/orders?page=${page + 1}`} />}>{t("common.next")}</Button></div>
  </>
}
function OrderDetail({ id, returned }: { id: string; returned: boolean }) {
  const { t } = useTranslation()
  const [order, setOrder] = useState<Order | null>(null)
  const [channels, setChannels] = useState<Channel[]>([])
  const [error, setError] = useState<Error | null>(null)
  const [busy, setBusy] = useState(false)
  const [channel, setChannel] = useState("")
  const [version, setVersion] = useState(0)
  useEffect(() => {
    let active = true
    setOrder(null); setError(null)
    Promise.all([getOrder(id), getChannels()]).then(async ([result, available]) => {
      if (!active) return
      setOrder(result); setChannels(available.channels.filter(item => item.currency === result.currency))
      if (returned && result.status === "unpaid") { try { const synced = await syncOrder(id); if (active) setOrder(synced) } catch (reason) { if (active) showError(reason) } }
    }).catch(reason => { if (active) setError(reason instanceof Error ? reason : localError("errors.load")) })
    return () => { active = false }
  }, [id, version, returned])
  useEffect(() => {
    if (order?.status !== "unpaid") return
    let active = true
    const timer = window.setInterval(() => { if (!document.hidden) void getOrder(id).then(result => { if (active) setOrder(result) }).catch(() => {}) }, 5000)
    return () => { active = false; window.clearInterval(timer) }
  }, [id, order?.status])
  async function sync() { if (busy) return; setBusy(true); try { setOrder(await syncOrder(id)) } catch (reason) { showError(reason) } finally { setBusy(false) } }
  async function pay() {
    if (!order || busy) return
    const active = order.attempts?.find(attempt => ["creating", "pending", "failed"].includes(attempt.status))
    const selected = active?.account_id ?? Number(channel || channels[0]?.id)
    if (!selected) { showError(localError("errors.noChannels")); return }
    setBusy(true)
    try { const attempt = await payOrder(id, selected); if (!attempt.payment_url) throw localError("errors.paymentPending"); window.location.assign(attempt.payment_url) }
    catch (reason) { showError(reason); try { setOrder(await getOrder(id)) } catch {} }
    finally { setBusy(false) }
  }
  if (error) return <Failure message={error} retry={() => setVersion(value => value + 1)} />
  if (!order) return <Loading />
  const active = order.attempts?.find(attempt => ["creating", "pending", "failed"].includes(attempt.status))
  const paid = order.status === "paid"
  const StatusIcon = paid ? CircleCheck : order.status === "refunded" ? Undo2 : Clock3
  return <div className="checkout-grid">
    <div className="min-w-0 space-y-6">
      <section className={cn("panel border-s-4", paid ? "border-s-primary" : order.status === "unpaid" ? "border-s-amber-500" : "border-s-muted-foreground")}>
        <div className="flex items-start gap-4">
          <span className={cn("flex size-12 shrink-0 items-center justify-center rounded-2xl", paid ? "bg-secondary text-primary" : order.status === "unpaid" ? "bg-amber-50 text-amber-700" : "bg-muted text-muted-foreground")}><StatusIcon className="size-6" aria-hidden="true" /></span>
          <div className="min-w-0"><h2>{t(states[order.status] || "common.unknownStatus")}</h2><p className="muted mt-2">{order.status === "unpaid" ? t("orders.unpaidNote") : paid ? t(fulfillment[order.fulfillment_status] || "common.unknownStatus") : order.status === "refunded" ? t("orders.refundedNote") : t("common.unknownStatus")}</p></div>
        </div>
        {order.status !== "refunded" && <ol className="mt-7 grid gap-5 border-t pt-6 sm:grid-cols-3" aria-label={t("design.steps")}>
          <li className="flex items-start gap-2.5"><span className="mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-full bg-secondary text-primary"><Check className="size-3.5" aria-hidden="true" /></span><div><p className="text-xs font-medium">{t("design.orderPlaced")}</p><p className="mt-1 text-xs leading-5 text-muted-foreground">{formatDate(order.created_at)}</p></div></li>
          <li className="flex items-start gap-2.5"><span className={cn("mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-full", paid ? "bg-secondary text-primary" : "bg-amber-50 text-amber-700")}><CreditCard className="size-3.5" aria-hidden="true" /></span><div><p className="text-xs font-medium">{t("design.paymentStatus")}</p><p className="mt-1 text-xs leading-5 text-muted-foreground">{t(states[order.status] || "common.unknownStatus")}</p></div></li>
          <li className="flex items-start gap-2.5"><span className={cn("mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-full", paid && order.fulfillment_status === "completed" ? "bg-secondary text-primary" : "bg-muted text-muted-foreground")}><PackageCheck className="size-3.5" aria-hidden="true" /></span><div><p className="text-xs font-medium">{t("design.fulfillmentStatus")}</p><p className="mt-1 text-xs leading-5 text-muted-foreground">{paid ? t(fulfillment[order.fulfillment_status] || "common.unknownStatus") : t("orders.unpaid")}</p></div></li>
        </ol>}
      </section>
      <section className="panel">
        <SectionHeading icon={<ReceiptText className="size-4" />}>{t("design.orderItems")}</SectionHeading>
        <div className="divide-y">{order.items.map((item, index) => <article className="flex flex-wrap gap-4 py-5 first:pt-0 last:pb-0 sm:flex-nowrap" key={index}>
          <ProductImage src={item.image_url} />
          <div className="min-w-0 flex-1"><h2 className="text-base leading-6"><bdi>{item.name}</bdi></h2><p className="muted mt-2 break-words">{specification(item)}</p><p className="mt-2 text-xs text-muted-foreground">{t("cart.items", { count: item.quantity })}</p></div>
          <strong className="ms-auto text-base font-semibold tabular-nums"><bdi>{money(item.amount * item.quantity, order.currency)}</bdi></strong>
        </article>)}</div>
      </section>
    </div>
    <aside className="panel summary-panel space-y-6">
      <h2 className="text-lg">{t("orders.info")}</h2>
      <div><p className="mb-2 text-xs text-muted-foreground">{t("design.orderNumber")}</p><p className="break-all font-mono text-xs leading-6"><bdi>{order.id}</bdi></p></div>
      <p className="text-xs leading-6 text-muted-foreground">{t("orders.created", { date: formatDate(order.created_at) })}</p>
      <div className="space-y-2 border-y py-5"><p className="muted">{t("orders.amount")}</p><strong className="block text-3xl font-semibold tracking-tight tabular-nums"><bdi>{money(order.amount, order.currency)}</bdi></strong></div>
      <div className="space-y-2"><h3 className="text-xs font-medium text-muted-foreground">{t("checkout.contact")}</h3><p className="break-words text-sm font-medium"><bdi>{order.customer_name}</bdi></p><p className="muted break-all"><bdi>{order.email}</bdi></p></div>
      {order.address.street && <div className="space-y-2"><h3 className="text-xs font-medium text-muted-foreground">{t("checkout.shipping")}</h3><p className="muted break-words">{[order.address.country, order.address.state, order.address.city, order.address.street, order.address.postcode].filter(Boolean).join(" ")}</p></div>}
      {order.status === "unpaid" && <div className="space-y-4 border-t pt-5">
        <div className="field"><Label htmlFor="order-channel">{t("checkout.channel")}</Label>{active ? <p className="muted">{t("orders.existingChannel")}</p> : channels.length ? <Select disabled={busy} value={channel || String(channels[0].id)} onValueChange={value => { if (value) setChannel(value) }}><SelectTrigger id="order-channel" className="w-full" aria-label={t("orders.channel")}><SelectValue>{channels.find(item => String(item.id) === (channel || String(channels[0].id)))?.name}</SelectValue></SelectTrigger><SelectContent>{channels.map(item => <SelectItem key={item.id} value={String(item.id)}>{item.name}{item.sandbox ? ` (${t("common.sandbox")})` : ""}</SelectItem>)}</SelectContent></Select> : <p className="muted">{t("orders.noChannels")}</p>}</div>
        <Button className="action-button h-auto w-full whitespace-normal py-3" disabled={busy || (!active && !channels.length)} onClick={() => void pay()}>{busy ? <LoaderCircle className="size-4 animate-spin" aria-hidden="true" /> : <CreditCard className="size-4" aria-hidden="true" />}{busy ? t("common.processing") : t("orders.continuePayment")}</Button>
        <Button className="action-button h-auto w-full whitespace-normal py-3" variant="outline" disabled={busy} onClick={() => void sync()}><RefreshCw className={cn("size-4", busy && "animate-spin")} aria-hidden="true" />{t("orders.sync")}</Button>
      </div>}
      <Link href="/shop/orders" className="text-link flex justify-center border-t pt-5"><ArrowLeft className="size-4 rtl:rotate-180" aria-hidden="true" />{t("orders.all")}</Link>
    </aside>
  </div>
}
export function OrdersPage({ id, search }: { id?: string; search: string }) {
  const { t } = useTranslation()
  const { member, memberLoading } = useShop()
  const params = new URLSearchParams(search), page = Math.max(1, Number(params.get("page")) || 1)
  return <div className="page">
    <PageHeading title={id ? t("orders.detail") : t("common.orders")} description={t("design.orderDescription")} action={id ? <Link className="text-link" href="/shop/orders"><ArrowLeft className="size-4 rtl:rotate-180" aria-hidden="true" />{t("orders.all")}</Link> : undefined} />
    {memberLoading ? <Loading /> : !member ? <AuthRequired next={`/shop/orders${id ? `/${encodeURIComponent(id)}` : ""}${search}`} /> : id ? <OrderDetail key={`${member.id}:${id}`} id={id} returned={params.get("returned") === "1"} /> : <OrderList key={member.id} page={page} />}
  </div>
}

function PaymentBadge({ status }: { status: string }) {
  const { t } = useTranslation()
  const Icon = status === "paid" ? CircleCheck : status === "unpaid" ? Clock3 : Undo2
  return <span className={cn("status-badge", status === "paid" ? "status-positive" : status === "unpaid" ? "status-waiting" : "status-neutral")}><Icon className="size-3.5 shrink-0" aria-hidden="true" />{t(states[status] || "common.unknownStatus")}</span>
}
function FulfillmentBadge({ status }: { status: string }) {
  const { t } = useTranslation()
  const Icon = status === "completed" ? PackageCheck : status === "failed" ? CircleAlert : Clock3
  return <span className={cn("status-badge", status === "completed" ? "status-positive" : status === "failed" ? "status-danger" : "status-neutral")}><Icon className="size-3.5 shrink-0" aria-hidden="true" />{t(fulfillment[status] || "common.unknownStatus")}</span>
}
