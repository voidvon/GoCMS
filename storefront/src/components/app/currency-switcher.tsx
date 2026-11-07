import { Coins } from "lucide-react"
import { useTranslation } from "react-i18next"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { useShop } from "@/lib/shop-context"

export function CurrencySwitcher() {
  const { t } = useTranslation()
  const { pricing, pricingLoading, currencySelection, chooseCurrency, currency } = useShop()
  if (!pricing?.enabled || !pricing.currencies.length) return null
  return <Select value={currencySelection} disabled={pricingLoading} onValueChange={value => { if (value) chooseCurrency(value) }}>
    <SelectTrigger className="h-10 min-w-28 gap-2 border-transparent bg-muted/60" aria-label={t("currency.label")}><Coins className="size-4" aria-hidden="true" /><SelectValue>{currencySelection === "auto" ? t("currency.auto", { currency }) : currency}</SelectValue></SelectTrigger>
    <SelectContent align="end"><SelectItem value="auto">{t("currency.auto", { currency: pricing.recommended_currency })}</SelectItem>{pricing.currencies.map(item => <SelectItem key={item.code} value={item.code}>{item.code}</SelectItem>)}</SelectContent>
  </Select>
}
