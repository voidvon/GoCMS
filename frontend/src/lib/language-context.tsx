import { createContext, useContext, useEffect, useState, useCallback, type ReactNode } from "react"
import { getLanguages, type Language } from "@/lib/api"

type LanguageContextType = {
  languages: Language[]
  activeLang: string
  setActiveLang: (code: string) => void
  defaultLang: string
  fallbackLang: string
  currentLanguage: Language | undefined
  loading: boolean
  refreshLanguages: () => Promise<void>
}

const LanguageContext = createContext<LanguageContextType>({
  languages: [],
  activeLang: "zh-CN",
  setActiveLang: () => {},
  defaultLang: "zh-CN",
  fallbackLang: "zh-CN",
  currentLanguage: undefined,
  loading: false,
  refreshLanguages: async () => {},
})

export function LanguageProvider({ children }: { children: ReactNode }) {
  const [languages, setLanguages] = useState<Language[]>([])
  const [activeLang, setActiveLangState] = useState<string>(() => {
    return localStorage.getItem("gocms_admin_lang") || ""
  })
  const [loading, setLoading] = useState(false)

  const refreshLanguages = useCallback(async () => {
    setLoading(true)
    try {
      const list = await getLanguages()
      setLanguages(list)
      const def = list.find((l) => l.is_default === 1) || list[0]
      if (def) {
        setActiveLangState((prev) => {
          if (prev && list.some((l) => l.code === prev && l.is_enabled === 1)) {
            return prev
          }
          return def.code
        })
      }
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
    localStorage.setItem("gocms_admin_lang", code)
  }, [])

  const defaultLang = languages.find((l) => l.is_default === 1)?.code || "zh-CN"
  const fallbackLang = languages.find((l) => l.is_fallback === 1)?.code || defaultLang
  const currentLanguage = languages.find((l) => l.code === (activeLang || defaultLang))

  return (
    <LanguageContext.Provider
      value={{
        languages,
        activeLang: activeLang || defaultLang,
        setActiveLang,
        defaultLang,
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
