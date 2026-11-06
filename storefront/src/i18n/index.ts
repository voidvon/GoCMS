import i18n from "i18next"
import { initReactI18next } from "react-i18next"
import en from "./locales/en.json"
import zhCN from "./locales/zh-CN.json"
import es from "./locales/es.json"
import fr from "./locales/fr.json"
import ru from "./locales/ru.json"
import tr from "./locales/tr.json"
import pt from "./locales/pt.json"
import id from "./locales/id.json"
import ar from "./locales/ar.json"
import th from "./locales/th.json"
import vi from "./locales/vi.json"

export const languages = [
  { code: "en", name: "English", direction: "ltr" },
  { code: "zh-CN", name: "简体中文", direction: "ltr" },
  { code: "es", name: "Español", direction: "ltr" },
  { code: "fr", name: "Français", direction: "ltr" },
  { code: "ru", name: "Русский", direction: "ltr" },
  { code: "tr", name: "Türkçe", direction: "ltr" },
  { code: "pt", name: "Português", direction: "ltr" },
  { code: "id", name: "Bahasa Indonesia", direction: "ltr" },
  { code: "ar", name: "العربية", direction: "rtl" },
  { code: "th", name: "ไทย", direction: "ltr" },
  { code: "vi", name: "Tiếng Việt", direction: "ltr" },
] as const
export type Language = typeof languages[number]["code"]
export type MessageKey = keyof typeof en
// Each locale must implement the English contract. Extra plural forms are allowed.
const catalogs = { en, "zh-CN": zhCN, es, fr, ru, tr, pt, id, ar, th, vi } satisfies Record<Language, typeof en & Record<string, string>>
let siteID: number | undefined
let siteDefault: Language | undefined
let manualLanguage: Language | undefined
const preferenceListeners = new Set<() => void>()
export type LanguageSelection = Language | "follow"
export const languageSelection = (): LanguageSelection => manualLanguage || "follow"
export function subscribeLanguageSelection(listener: () => void) {
  preferenceListeners.add(listener)
  return () => { preferenceListeners.delete(listener) }
}
const manualKey = (site: number) => `gocms-storefront-language:${site}`
const websiteKey = (site: number) => `gocms-site-language:${site}`

export function normalizeLanguage(value: string | null | undefined): Language | undefined {
  if (!value) return undefined
  const normalized = value.trim().replaceAll("_", "-").toLowerCase()
  const exact = languages.find(language => language.code.toLowerCase() === normalized)
  if (exact) return exact.code
  if (/^zh(?:-(?:cn|sg|hans)(?:-|$)|$)/.test(normalized)) return "zh-CN"
  return languages.find(language => language.code === normalized.split("-")[0])?.code
}
function readLanguage(key: string) {
  try { return normalizeLanguage(localStorage.getItem(key)) } catch { return undefined }
}
const browserLanguage = [...(navigator.languages || []), navigator.language].map(normalizeLanguage).find(Boolean)
function effectiveLanguage(): Language {
  return manualLanguage || (siteID ? readLanguage(websiteKey(siteID)) : undefined) || siteDefault || browserLanguage || "en"
}

void i18n.use(initReactI18next).init({
  resources: Object.fromEntries(Object.entries(catalogs).map(([language, translation]) => [language, { translation }])),
  lng: browserLanguage || "en",
  fallbackLng: "en",
  supportedLngs: languages.map(language => language.code),
  load: "currentOnly",
  keySeparator: false,
  initAsync: false,
  interpolation: { escapeValue: false },
  react: { useSuspense: false },
})
export function currentLanguage(): Language { return normalizeLanguage(i18n.resolvedLanguage || i18n.language) || "en" }
function applyDocumentLanguage() {
  const language = currentLanguage()
  document.documentElement.lang = language
  document.documentElement.dir = language === "ar" ? "rtl" : "ltr"
}
applyDocumentLanguage()
i18n.on("languageChanged", applyDocumentLanguage)
function refreshLanguage() {
  const language = effectiveLanguage()
  if (language !== currentLanguage()) void i18n.changeLanguage(language)
  for (const listener of preferenceListeners) listener()
}
// Resolve storage only after the API identifies the actual site, never from a
// URL, selected admin site, or a preference belonging to a different site.
export function configureSiteLanguage(site: number, defaultLanguage?: string) {
  siteID = site
  siteDefault = normalizeLanguage(defaultLanguage)
  manualLanguage = readLanguage(manualKey(site))
  refreshLanguage()
}
function readCurrentPreferences() {
  if (!siteID) return
  manualLanguage = readLanguage(manualKey(siteID))
  refreshLanguage()
}
function followStorage(event: StorageEvent) {
  if (!siteID) return
  try { if (event.storageArea !== localStorage) return } catch { return }
  if (event.key === null || event.key === manualKey(siteID)) readCurrentPreferences()
  else if (event.key === websiteKey(siteID)) refreshLanguage()
}
window.addEventListener("storage", followStorage)
window.addEventListener("pageshow", readCurrentPreferences)
if (import.meta.hot) import.meta.hot.dispose(() => {
  window.removeEventListener("storage", followStorage)
  window.removeEventListener("pageshow", readCurrentPreferences)
  i18n.off("languageChanged", applyDocumentLanguage)
})

// Only the language picker writes a manual preference. Website language and
// navigation never promote an automatically resolved language to a user choice.
export async function changeLanguage(value: string) {
  if (!siteID) return
  const language = value === "follow" ? undefined : normalizeLanguage(value)
  if (value !== "follow" && !language) return
  let saved = true
  try {
    if (language) localStorage.setItem(manualKey(siteID), language)
    else localStorage.removeItem(manualKey(siteID))
  } catch { saved = false }
  manualLanguage = language
  await i18n.changeLanguage(effectiveLanguage())
  for (const listener of preferenceListeners) listener()
  if (!saved) throw new Error(String(i18n.t("errors.languageStorage")))
}
export const formatNumber = (value: number, options?: Intl.NumberFormatOptions) => new Intl.NumberFormat(currentLanguage(), options).format(value)
export function formatDate(value: string) {
  const normalized = /^\d{4}-\d\d-\d\d \d\d:\d\d:\d\d$/.test(value) ? value.replace(" ", "T") + "Z" : value
  const date = new Date(normalized)
  return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat(currentLanguage(), { dateStyle: "medium", timeStyle: "short" }).format(date)
}
export default i18n
