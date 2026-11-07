import { useEffect, useState } from "react"
import { getPricing, type PricingInfo } from "./api"
import { localError } from "./errors"
import { showError } from "@/components/app/feedback"

// Shared contract with theme purchase widgets. Explicit currency preferences
// and country hints are independent from UI language and never stored in URLs.
const currencyKey = (site: number) => `gocms-currency:${site}`
const countryKey = (site: number) => `gocms-market-country:${site}`
function read(key: string) { try { return localStorage.getItem(key) || "" } catch { return "" } }
export function rememberShoppingCountry(site: number, country: string) {
  if (!/^[A-Z]{2}$/.test(country)) return
  try { localStorage.setItem(countryKey(site), country) } catch {}
}
export function preferredCurrency(site: number) { return read(currencyKey(site)) }
export function useCurrency(site: number) {
  const [manual, setManual] = useState(() => preferredCurrency(site))
  const [country, setCountry] = useState(() => read(countryKey(site)))
  const [pricing, setPricing] = useState<PricingInfo | null>(null)
  const [loading, setLoading] = useState(true)
  const [version, setVersion] = useState(0)
  useEffect(() => {
    let active = true
    setLoading(true)
    getPricing(country).then(result => { if (active) setPricing(result) }).catch(error => { if (active) showError(error) }).finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [site, country, version])
  useEffect(() => {
    const refresh = () => { setManual(preferredCurrency(site)); setCountry(read(countryKey(site))); setVersion(v => v + 1) }
    const changed = (event: StorageEvent) => { if (event.key === null || event.key === currencyKey(site) || event.key === countryKey(site)) refresh() }
    window.addEventListener("storage", changed); window.addEventListener("pageshow", refresh)
    return () => { window.removeEventListener("storage", changed); window.removeEventListener("pageshow", refresh) }
  }, [site])
  const currency = pricing?.enabled ? pricing.currencies.some(item => item.code === manual) ? manual : pricing.recommended_currency : ""
  function chooseCurrency(code: string) {
    if (code !== "auto" && !pricing?.currencies.some(item => item.code === code)) return
    setManual(code === "auto" ? "" : code)
    try { if (code === "auto") localStorage.removeItem(currencyKey(site)); else localStorage.setItem(currencyKey(site), code) }
    catch { showError(localError("currency.storage")) }
  }

  return { currency, currencySelection: pricing?.currencies.some(item => item.code === manual) ? manual : "auto", pricing, pricingLoading: loading, chooseCurrency, refreshPricing: () => setVersion(v => v + 1) }
}
