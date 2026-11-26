import { createContext, useContext, useEffect, useState, type ReactNode } from "react"

import { getAdminSettings, updateAdminName } from "@/lib/api"
import { getErrorMessage } from "@/lib/errors"

export const defaultAdminName = "GoCMS 后台"

type AdminSettings = {
  adminName: string | undefined
  displayName: string
  loadError: string
  retry: () => void
  saveAdminName: (name: string) => Promise<string>
}

const AdminSettingsContext = createContext<AdminSettings | undefined>(undefined)

export function AdminSettingsProvider({ children }: { children: ReactNode }) {
  const [adminName, setAdminName] = useState<string>()
  const [loadError, setLoadError] = useState("")
  const [attempt, setAttempt] = useState(0)
  const displayName = adminName || defaultAdminName

  useEffect(() => {
    let active = true
    getAdminSettings().then(({ admin_name }) => {
      if (active) setAdminName(admin_name)
    }).catch((error: unknown) => {
      if (active) setLoadError(getErrorMessage(error, "读取后台名称失败"))
    })
    return () => { active = false }
  }, [attempt])

  useEffect(() => {
    const initialTitle = document.title
    document.title = `${displayName} · 内容管理`
    return () => { document.title = initialTitle }
  }, [displayName])

  function retry() {
    setLoadError("")
    setAttempt((current) => current + 1)
  }

  async function saveAdminName(name: string) {
    const { admin_name } = await updateAdminName(name)
    setAdminName(admin_name)
    return admin_name
  }

  return (
    <AdminSettingsContext.Provider value={{ adminName, displayName, loadError, retry, saveAdminName }}>
      {children}
    </AdminSettingsContext.Provider>
  )
}

export function useAdminSettings() {
  const settings = useContext(AdminSettingsContext)
  if (!settings) throw new Error("Admin settings must be used within AdminSettingsProvider")
  return settings
}
