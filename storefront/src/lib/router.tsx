import { useSyncExternalStore, type ComponentProps, type MouseEvent } from "react"
export function localPath(value: string | null | undefined, fallback = "/"): string {
  if (!value?.startsWith("/") || value.startsWith("//") || /[\\\u0000-\u0020\u007f]/.test(value)) return fallback
  try {
    const url = new URL(value, window.location.origin)
    if (url.origin !== window.location.origin || /^\/(?:api|admin)(?:\/|$)/i.test(url.pathname)) return fallback
    return url.pathname + url.search + url.hash
  } catch { return fallback }
}
export function authReturnPath(value: string | null | undefined) {
  const path = localPath(value, "/shop/cart")
  return /^\/shop\/(?:login|register|cart\/add)(?:[/?#]|$)/i.test(path) ? "/shop/cart" : path
}
export function shoppingReturnPath(value: string | null | undefined) {
  const path = localPath(value)
  return /^\/shop(?:[/?#]|$)/i.test(path) ? "/" : path
}
export function authURL(mode: "login" | "register", next: string) {
  return `/shop/${mode}?next=${encodeURIComponent(authReturnPath(next))}`
}
export function navigate(path: string, replace = false) {
  const target = localPath(path, "/shop/cart")
  if (!target.startsWith("/shop/")) {
    if (replace) window.location.replace(target)
    else window.location.assign(target)
    return
  }
  window.history[replace ? "replaceState" : "pushState"](null, "", target)
  window.dispatchEvent(new PopStateEvent("popstate"))
}
function subscribe(callback: () => void) { window.addEventListener("popstate", callback); return () => window.removeEventListener("popstate", callback) }
export function useRoute() { return useSyncExternalStore(subscribe, () => window.location.pathname + window.location.search) }
export function Link({ href, onClick, ...props }: ComponentProps<"a"> & { href: string }) {
  function click(event: MouseEvent<HTMLAnchorElement>) {
    onClick?.(event)
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || props.target === "_blank") return
    event.preventDefault(); navigate(href)
  }
  return <a href={href} onClick={click} {...props} />
}
