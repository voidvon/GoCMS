import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react"
import { getPublication, type Publication } from "@/lib/api"
import { getErrorMessage } from "@/lib/errors"
import { showError, showSuccess } from "@/components/app/admin-notifications"

const PublicationContext = createContext<{
  report: Publication | null
  syncError: string
  track: (report: Publication) => void
} | null>(null)

export function usePublication() {
  const context = useContext(PublicationContext)
  if (!context) throw new Error("缺少发布状态上下文")
  return context
}

export function PublicationProvider({ siteId, enabled, children }: { siteId: number; enabled: boolean; children: ReactNode }) {
  const [report, setReport] = useState<Publication | null>(null)
  const [syncError, setSyncError] = useState("")
  const latest = useRef<Publication | null>(null)
  const refresh = useRef<(() => void) | null>(null)

  const accept = useCallback((next: Publication) => {
    if (latest.current?.state === "running") {
      if (next.state === "success") showSuccess(`发布完成，已更新 ${next.files} 个文件。`)
      if (next.state === "failed") showError(next.error, "发布失败")
    }
    latest.current = next
    setReport(next)
    setSyncError("")
  }, [])

  useEffect(() => {
    if (!enabled) return
    let timer: number
    let request: AbortController | undefined
    async function poll() {
      window.clearTimeout(timer)
      request?.abort()
      if (document.hidden) return
      const controller = new AbortController()
      request = controller
      let delay = 30_000
      try {
        const next = await getPublication(siteId, true, controller.signal)
        if (controller.signal.aborted) return
        accept(next)
        if (next.state === "running") delay = 1000
      } catch (error) {
        if (controller.signal.aborted) return
        setSyncError(getErrorMessage(error, "无法读取发布状态"))
        delay = 5000
      }
      timer = window.setTimeout(poll, delay)
    }
    refresh.current = poll
    void poll()
    document.addEventListener("visibilitychange", poll)
    return () => {
      refresh.current = null
      request?.abort()
      window.clearTimeout(timer)
      document.removeEventListener("visibilitychange", poll)
    }
  }, [accept, enabled, siteId])

  function track(next: Publication) {
    if (!refresh.current) return
    accept(next)
    refresh.current()
  }

  return <PublicationContext.Provider value={{ report, syncError, track }}>{children}</PublicationContext.Provider>
}
