import { useCallback, useEffect, useState } from "react"

export type AdminView = "overview" | "content" | "categories" | "models" | "languages" | "messages" | "publish" | "theme" | "api-keys" | "media" | "users" | "logs"

const adminPaths: Record<AdminView, string> = {
  logs: "/admin/logs",
  users: "/admin/users",
  media: "/admin/media",
  overview: "/admin/",
  publish: "/admin/publish",
  theme: "/admin/theme",
  content: "/admin/content",
  categories: "/admin/categories",
  models: "/admin/models",
  languages: "/admin/languages",
  messages: "/admin/messages",
  "api-keys": "/admin/api-keys",
}

function normalizedPath(pathname: string) {
  if (pathname === "/admin") {
    return "/admin/"
  }
  return pathname.replace(/\/+$/, "") || "/"
}

export function viewForPath(pathname: string): AdminView {
  const path = normalizedPath(pathname)
  for (const [view, viewPath] of Object.entries(adminPaths)) {
    if (normalizedPath(viewPath) === path) {
      return view as AdminView
    }
  }
  return "overview"
}

export function pathForView(view: AdminView) {
  return adminPaths[view]
}

export function useAdminRoute() {
  const [view, setView] = useState<AdminView>(() => viewForPath(window.location.pathname))

  useEffect(() => {
    const handlePopState = () => setView(viewForPath(window.location.pathname))
    window.addEventListener("popstate", handlePopState)
    return () => window.removeEventListener("popstate", handlePopState)
  }, [])

  useEffect(() => {
    const canonicalPath = pathForView(view)
    if (window.location.pathname.startsWith("/admin") && window.location.pathname !== canonicalPath) {
      window.history.replaceState(null, "", canonicalPath)
    }
  }, [view])

  const navigate = useCallback((nextView: AdminView) => {
    const nextPath = pathForView(nextView)
    if (window.location.pathname !== nextPath) {
      window.history.pushState(null, "", nextPath)
    }
    setView(nextView)
  }, [])

  return { view, navigate }
}
