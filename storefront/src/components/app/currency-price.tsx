import { useTranslation } from "react-i18next"
import { money, type Variant } from "@/lib/api"

export function CurrencyPrice({ item, quantity = 1 }: { item: Variant; quantity?: number }) {
  const { t } = useTranslation()
  return <div className="space-y-1">
    <p className="text-base font-semibold tabular-nums"><bdi>{item.display_currency && item.display_amount ? t("currency.approximate", { amount: money(item.display_amount * quantity, item.display_currency) }) : money(item.amount * quantity, item.currency)}</bdi></p>
    {item.display_currency && <p className="text-xs font-normal leading-5 text-muted-foreground">{t("currency.actual", { amount: money(item.amount * quantity, item.currency) })}</p>}
    {item.pricing_unavailable && <p className="max-w-xs text-xs font-normal leading-5 text-amber-800">{t("currency.unavailable")}</p>}
  </div>
}
