import { createContext, useContext, useEffect, useState, useCallback, type ReactNode } from "react"
import { getLanguages, type Language } from "@/lib/api"

const ACTIVE_LANG_STORAGE_KEY = "gocms_admin_lang"

type LanguageContextType = {
  languages: Language[]
  /** Language currently shown in the admin backend. */
  activeLang: string
  setActiveLang: (code: string) => void
  /** Follow the site-wide admin display language again, dropping the local choice. */
  resetActiveLang: () => void
  /** Primary site language: generated at the site root and used as the base language. */
  defaultLang: string
  /** Site-wide default language the admin backend displays in lists. */
  adminLang: string
  fallbackLang: string
  currentLanguage: Language | undefined
  loading: boolean
  refreshLanguages: () => Promise<void>
}

const LanguageContext = createContext<LanguageContextType>({
  languages: [],
  activeLang: "zh-CN",
  setActiveLang: () => {},
  resetActiveLang: () => {},
  defaultLang: "zh-CN",
  adminLang: "zh-CN",
  fallbackLang: "zh-CN",
  currentLanguage: undefined,
  loading: false,
  refreshLanguages: async () => {},
})

/**
 * Pick the language the admin backend falls back to: the configured admin
 * display language first, then the primary site language, then any enabled one.
 */
function resolveAdminLang(list: Language[]): string {
  const candidates = [
    list.find((l) => l.is_admin_default === 1 && l.is_enabled === 1),
    list.find((l) => l.is_default === 1 && l.is_enabled === 1),
    list.find((l) => l.is_enabled === 1),
    list.find((l) => l.is_admin_default === 1),
    list.find((l) => l.is_default === 1),
    list[0],
  ]
  return candidates.find((item) => item)?.code || "zh-CN"
}

export function LanguageProvider({ children }: { children: ReactNode }) {
  const [languages, setLanguages] = useState<Language[]>([])
  const [activeLang, setActiveLangState] = useState<string>(() => {
    return localStorage.getItem(ACTIVE_LANG_STORAGE_KEY) || ""
  })
  const [loading, setLoading] = useState(false)

  const refreshLanguages = useCallback(async () => {
    setLoading(true)
    try {
      const list = await getLanguages()
      setLanguages(list)
      const fallbackCode = resolveAdminLang(list)
      setActiveLangState((prev) => {
        if (prev && list.some((l) => l.code === prev && l.is_enabled === 1)) {
          return prev
        }
        return fallbackCode
      })
    } catch (e) {
      console.error("Failed to load languages:", e)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refreshLanguages()
  }, [refreshLanguages])

  const setActiveLang = useCallback((code: string) => {
    setActiveLangState(code)
    localStorage.setItem(ACTIVE_LANG_STORAGE_KEY, code)
  }, [])

  const resetActiveLang = useCallback(() => {
    localStorage.removeItem(ACTIVE_LANG_STORAGE_KEY)
    setActiveLangState(resolveAdminLang(languages))
  }, [languages])

  const defaultLang = languages.find((l) => l.is_default === 1)?.code || "zh-CN"
  const adminLang = languages.find((l) => l.is_admin_default === 1)?.code || defaultLang
  const fallbackLang = languages.find((l) => l.is_fallback === 1)?.code || defaultLang
  const currentLanguage = languages.find((l) => l.code === (activeLang || adminLang))

  return (
    <LanguageContext.Provider
      value={{
        languages,
        activeLang: activeLang || adminLang,
        setActiveLang,
        resetActiveLang,
        defaultLang,
        adminLang,
        fallbackLang,
        currentLanguage,
        loading,
        refreshLanguages,
      }}
    >
      {children}
    </LanguageContext.Provider>
  )
}

export function useLanguage() {
  return useContext(LanguageContext)
}
