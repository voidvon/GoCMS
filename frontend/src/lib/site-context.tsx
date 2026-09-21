import { createContext, useContext, useEffect, useState, useCallback, type ReactNode } from "react"
import { getSites, setActiveSiteId, getActiveSiteId, type Site } from "@/lib/api"

type SiteContextType = {
  sites: Site[]
  activeSite: Site | undefined
  activeSiteId: number
  setActiveSite: (site: Site) => void
  loading: boolean
  refreshSites: () => Promise<void>
}

const SiteContext = createContext<SiteContextType>({
  sites: [],
  activeSite: undefined,
  activeSiteId: 1,
  setActiveSite: () => {},
  loading: false,
  refreshSites: async () => {},
})

export function SiteProvider({ children }: { children: ReactNode }) {
  const [sites, setSites] = useState<Site[]>([])
  const [activeSiteId, setActiveSiteIdState] = useState<number>(() => getActiveSiteId())
  const [loading, setLoading] = useState(false)

  const refreshSites = useCallback(async () => {
    setLoading(true)
    try {
      const list = await getSites()
      setSites(list)
      const currentSavedId = getActiveSiteId()
      const existing = list.find((s) => s.id === currentSavedId)
      if (existing) {
        setActiveSiteId(existing.id)
        setActiveSiteIdState(existing.id)
      } else {
        const def = list.find((s) => s.is_default) || list[0]
        if (def) {
          setActiveSiteId(def.id)
          setActiveSiteIdState(def.id)
        }
      }
    } catch (e) {
      console.error("Failed to load sites:", e)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refreshSites()
  }, [refreshSites])

  const setActiveSite = useCallback((site: Site) => {
    setActiveSiteId(site.id)
    setActiveSiteIdState(site.id)
  }, [])

  const activeSite = sites.find((s) => s.id === activeSiteId) || sites[0]

  return (
    <SiteContext.Provider
      value={{
        sites,
        activeSite,
        activeSiteId,
        setActiveSite,
        loading,
        refreshSites,
      }}
    >
      {children}
    </SiteContext.Provider>
  )
}

export function useSite() {
  return useContext(SiteContext)
}
