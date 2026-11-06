import { useEffect, useState } from "react"
import { api } from "@/lib/api"
export type Place = { id: string; name: string; ascii?: string; country?: string; region?: string; region_name?: string }
type Directory = { items: Place[]; source: string; license: string }

// Abort old lookups so a slower response cannot replace the next country's cities.
export function useGeography(path: string | null, debounce = 0) {
  const [revision, setRevision] = useState(0)
  const [result, setResult] = useState<{ path: string | null; items: Place[]; loading: boolean; error: unknown }>({ path: null, items: [], loading: false, error: null })
  useEffect(() => {
    if (!path) return
    const controller = new AbortController()
    setResult({ path, items: [], loading: true, error: null })
    const timer = window.setTimeout(() => {
      void api<Directory>(path, { signal: controller.signal }).then(data => {
        if (!controller.signal.aborted) setResult({ path, items: data.items, loading: false, error: null })
      }).catch((error: unknown) => {
        if (!controller.signal.aborted) setResult({ path, items: [], loading: false, error })
      })
    }, debounce)
    return () => { controller.abort(); window.clearTimeout(timer) }
  }, [path, debounce, revision])
  const current = result.path === path && path ? result : { items: [], loading: Boolean(path), error: null }
  return { ...current, retry: () => setRevision(value => value + 1) }
}
export const textBytes = (value: string) => new TextEncoder().encode(value).length
