import { MapPin } from "lucide-react"
import { SectionHeading } from "./commerce-ui"
import { useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { AddressSelect, type AddressOption } from "./address-select"
import { textBytes, useGeography } from "@/lib/geography"
import { getErrorMessage } from "@/lib/errors"

export function ShippingAddress({ disabled }: { disabled: boolean }) {
  const { t, i18n } = useTranslation()
  const [country, setCountry] = useState("")
  const [region, setRegion] = useState<AddressOption | null>(null)
  const [city, setCity] = useState<AddressOption | null>(null)
  const [manualCityText, setManualCityText] = useState("")
  const [manualCountry, setManualCountry] = useState(false)
  const [cityQuery, setCityQuery] = useState("")
  const [manualCity, setManualCity] = useState(false)
  const [manualRegion, setManualRegion] = useState(false)
  const countries = useGeography("/api/geography/countries")
  const regions = useGeography(country.length === 2 ? `/api/geography/regions?country=${encodeURIComponent(country)}` : null)
  const cityParams = new URLSearchParams({ country, region: !manualRegion ? region?.id || "" : "", q: cityQuery })
  const cities = useGeography(country.length === 2 && !manualCity ? `/api/geography/cities?${cityParams}` : null, 250)
  const countryOptions = useMemo(() => {
    let names: Intl.DisplayNames | undefined
    try { names = new Intl.DisplayNames([i18n.resolvedLanguage || "en"], { type: "region" }) } catch {}
    return countries.items.map(item => ({ id: item.id, label: names?.of(item.id) || item.name, search: `${item.id} ${item.name}` })).sort((a, b) => a.label.localeCompare(b.label, i18n.resolvedLanguage))
  }, [countries.items, i18n.resolvedLanguage])
  const regionOptions: AddressOption[] = regions.items.map(item => ({ id: item.id, label: item.name, formValue: item.name, search: `${item.ascii || ""} ${item.id}`, disabled: textBytes(item.name) > 128 }))
  const cityOptions: AddressOption[] = cities.items.map(item => ({ id: item.id, label: item.name, formValue: item.name, description: item.region_name, disabled: textBytes(item.name) > 50 }))
  function resetCity() { setCity(null); setCityQuery(""); setManualCityText("") }
  function changeCountry(value: string) {
    setCountry(value.toUpperCase()); setRegion(null); resetCity()
  }
  function chooseCity(value: AddressOption | null) {
    setCity(value)
    if (value) {
      setCityQuery("")
      const place = cities.items.find(item => item.id === value.id)
      const matching = regionOptions.find(item => item.id === place?.region && !item.disabled)
      if (!manualRegion && matching) setRegion(matching)
    }
  }
  function lookupError(error: unknown, retry: () => void) {
    return error ? <div className="muted space-y-1" role="status"><p>{t("geography.loadFailed")} {getErrorMessage(error)}</p><Button type="button" size="sm" variant="ghost" disabled={disabled} onClick={retry}>{t("common.retry")}</Button></div> : null
  }
  function validateText(input: HTMLInputElement, max: number) { input.setCustomValidity(textBytes(input.value.trim()) > max ? t("geography.maxBytes", { count: max }) : "") }
  const useManualRegion = manualRegion || (!regions.loading && (!regionOptions.length || Boolean(regions.error)))
  return <section className="panel form-panel space-y-5">
    <SectionHeading icon={<MapPin className="size-4" />}>{t("checkout.shipping")}</SectionHeading>
    <div className="grid gap-5 sm:grid-cols-2">
      <div className="field"><Label htmlFor="shipping-country">{t("geography.country")}</Label>
        {manualCountry || countries.error ? <Input id="shipping-country" name="country" value={country} onChange={event => changeCountry(event.target.value)} required maxLength={2} pattern="[A-Za-z]{2}" dir="ltr" autoComplete="country" disabled={disabled} placeholder="US" /> : <AddressSelect id="shipping-country" name="country" items={countryOptions} value={countryOptions.find(item => item.id === country) || null} onChange={value => changeCountry(value?.id || "")} required disabled={disabled} loading={countries.loading} />}
        {(manualCountry || Boolean(countries.error)) && <p className="muted">{t("geography.countryCodeHint")}</p>}
        {!countries.error && <Button type="button" variant="link" size="sm" className="h-auto self-start p-0" disabled={disabled} onClick={() => { setManualCountry(value => !value); if (manualCountry && !countryOptions.some(item => item.id === country)) changeCountry("") }}>{t(manualCountry ? "geography.chooseCountry" : "geography.manualCountry")}</Button>}
        {lookupError(countries.error, countries.retry)}
      </div>
      <div className="field"><Label htmlFor="shipping-state">{t("checkout.state")}</Label>
        {useManualRegion ? <Input id="shipping-state" name="state" key={country} defaultValue={region?.label || ""} maxLength={128} autoComplete="address-level1" disabled={disabled || country.length !== 2} onChange={event => validateText(event.target, 128)} /> : <AddressSelect key={country} id="shipping-state" name="state" items={regionOptions} value={region} onChange={value => { setRegion(value); resetCity() }} disabled={disabled || country.length !== 2 || regions.loading} loading={regions.loading} />}
        {country && regionOptions.length > 0 && <Button type="button" variant="link" size="sm" className="h-auto self-start p-0" disabled={disabled} onClick={() => { setManualRegion(value => !value); setRegion(null); resetCity() }}>{t(manualRegion ? "geography.chooseRegion" : "geography.manualRegion")}</Button>}
        {lookupError(regions.error, regions.retry)}
      </div>
      <div className="field sm:col-span-2"><Label htmlFor="shipping-city">{t("checkout.city")}</Label>
        {manualCity ? <Input id="shipping-city" name="city" value={manualCityText} required maxLength={50} autoComplete="address-level2" disabled={disabled || country.length !== 2} onChange={event => { setManualCityText(event.target.value); validateText(event.target, 50) }} /> : <AddressSelect key={`${country}:${manualRegion ? "" : region?.id || ""}`} id="shipping-city" name="city" items={cityOptions} value={city} onChange={chooseCity} onSearch={setCityQuery} required disabled={disabled || country.length !== 2} loading={cities.loading} />}
        {!country && <p className="muted">{t("geography.countryFirst")}</p>}
        <Button type="button" variant="link" size="sm" className="h-auto self-start p-0" disabled={disabled} onClick={() => { setManualCity(value => !value); resetCity() }}>{t(manualCity ? "geography.chooseCity" : "geography.manualCity")}</Button>
        {lookupError(cities.error, cities.retry)}
      </div>
    </div>
    <div className="field"><Label htmlFor="shipping-street">{t("checkout.street")}</Label><Input id="shipping-street" name="street" required maxLength={256} autoComplete="street-address" disabled={disabled} onChange={event => validateText(event.target, 256)} /></div>
    <div className="field"><Label htmlFor="shipping-postcode">{t("checkout.postcode")}</Label><Input id="shipping-postcode" name="postcode" required maxLength={16} autoComplete="postal-code" disabled={disabled} onChange={event => validateText(event.target, 16)} /></div>
    <p className="muted text-xs"><a className="underline underline-offset-4" href="https://www.geonames.org/" target="_blank" rel="noreferrer">GeoNames</a> · <a className="underline underline-offset-4" href="https://creativecommons.org/licenses/by/4.0/" target="_blank" rel="noreferrer">CC BY 4.0</a></p>
  </section>
}
