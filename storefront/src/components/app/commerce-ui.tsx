import { useState, type ReactNode } from "react"
import { Check, ChevronRight, Package, ShoppingBag, CreditCard, ReceiptText } from "lucide-react"
import { useTranslation } from "react-i18next"
import { formatNumber } from "@/i18n"
import { Link } from "@/lib/router"
import { cn } from "@/lib/utils"

export function PageHeading({ title, description, action }: { title: string; description?: string; action?: ReactNode }) {
  const { t } = useTranslation()
  return <div className="page-heading">
    <div className="min-w-0 space-y-3">
      <p className="eyebrow">{t("design.shoppingLabel")}</p>
      <h1>{title}</h1>
      {description && <p className="max-w-xl text-sm leading-7 text-muted-foreground sm:text-base">{description}</p>}
    </div>
    {action}
  </div>
}

export function CheckoutSteps({ current }: { current: 1 | 2 | 3 }) {
  const { t } = useTranslation()
  const steps = [
    { label: t("common.cart"), icon: ShoppingBag, href: "/shop/cart" },
    { label: t("checkout.title"), icon: CreditCard },
    { label: t("orders.detail"), icon: ReceiptText },
  ]
  return <ol className="checkout-steps" aria-label={t("design.steps")}>
    {steps.map((step, index) => {
      const number = index + 1, Icon = current > number ? Check : step.icon
      const content = <><span className={cn("step-icon", current === number && "step-current", current > number && "step-done")}><Icon className="size-4" aria-hidden="true" /></span><span className="min-w-0"><span className="step-number" aria-hidden="true">{formatNumber(number, { minimumIntegerDigits: 2 })}</span><span className="block break-words text-xs font-medium sm:text-sm">{step.label}</span></span></>
      return <li className={cn("flex min-w-0 flex-1 items-center gap-2 sm:gap-3", current < number && "text-muted-foreground")} key={step.label} aria-current={current === number ? "step" : undefined}>
        {step.href && current > number ? <Link className="flex min-w-0 items-center gap-2 rounded-lg sm:gap-3" href={step.href}>{content}</Link> : <div className="flex min-w-0 items-center gap-2 sm:gap-3">{content}</div>}
        {index < steps.length - 1 && <ChevronRight className="ms-auto hidden size-4 shrink-0 text-border rtl:rotate-180 sm:block" aria-hidden="true" />}
      </li>
    })}
  </ol>
}

export function ProductImage({ src, className }: { src?: string; className?: string }) {
  const [failed, setFailed] = useState<string>()
  return <div className={cn("product-image", className)}>
    {src && failed !== src ? <img src={src} alt="" loading="lazy" className="size-full object-contain" onError={() => setFailed(src)} /> : <Package className="size-7 text-muted-foreground/50" aria-hidden="true" />}
  </div>
}

export function SectionHeading({ icon, children }: { icon: ReactNode; children: ReactNode }) {
  return <div className="mb-6 flex items-center gap-3">
    <span className="section-icon" aria-hidden="true">{icon}</span>
    <h2 className="text-base sm:text-lg">{children}</h2>
  </div>
}
