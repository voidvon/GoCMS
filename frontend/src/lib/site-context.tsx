import { createContext, useContext, useEffect, useState, useCallback, type ReactNode } from "react"
import { getSiteOptions, getSites, setActiveSiteId, getActiveSiteId, type Site, type SiteOption } from "@/lib/api"

type SiteLists = { sites: SiteOption[]; managedSites: Site[] }

type SiteContextType = SiteLists & {
  activeSite: SiteOption | undefined
  activeSiteId: number
  setActiveSite: (site: SiteOption) => void
  loading: boolean
  refreshSites: () => Promise<void>
}

const SiteContext = createContext<SiteContextType>({
  sites: [],
  managedSites: [],
  activeSite: undefined,
  activeSiteId: 1,
  setActiveSite: () => {},
  loading: false,
  refreshSites: async () => {},
})

export function SiteProvider({ children, canManageSites }: { children: ReactNode; canManageSites: boolean }) {
  const [{ sites, managedSites }, setSiteLists] = useState<SiteLists>({ sites: [], managedSites: [] })
  const [activeSiteId, setActiveSiteIdState] = useState<number>(() => getActiveSiteId())
  const [loading, setLoading] = useState(false)

  const refreshSites = useCallback(async () => {
    setLoading(true)
    try {
      const managedSites = canManageSites ? await getSites() : []
      const list = canManageSites ? managedSites : await getSiteOptions()
      setSiteLists({ sites: list, managedSites })
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
  }, [canManageSites])

  useEffect(() => {
    void refreshSites()
  }, [refreshSites])

  const setActiveSite = useCallback((site: SiteOption) => {
    setActiveSiteId(site.id)
    setActiveSiteIdState(site.id)
  }, [])

  const activeSite = sites.find((s) => s.id === activeSiteId) || sites[0]

  return (
    <SiteContext.Provider
      value={{
        sites,
        managedSites,
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
