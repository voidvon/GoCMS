import { useCurrency } from "./currency"
import i18n from "@/i18n"
import { localError } from "./errors"
import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from "react"
import { APIError, getMember, getVariants, type CartLine, type Member, type Store } from "./api"
import { shoppingReturnPath } from "./router"
import { showError, showSuccess } from "@/components/app/feedback"

function decodeCart(raw: string | null): CartLine[] {
  try {
    const data: unknown = JSON.parse(raw || "[]")
    if (!Array.isArray(data)) return []
    const seen = new Set<number>()
    return data.filter((line): line is CartLine => {
      if (!line || !Number.isSafeInteger(line.variant_id) || line.variant_id <= 0 || !Number.isInteger(line.quantity) || line.quantity < 1 || line.quantity > 1000 || seen.has(line.variant_id) || seen.size >= 50) return false
      seen.add(line.variant_id); return true
    }).map(({ variant_id, quantity }) => ({ variant_id, quantity }))
  } catch { return [] }
}
function savedCart(key: string) { try { return decodeCart(localStorage.getItem(key)) } catch { return [] } }
type ShopContextValue = ReturnType<typeof useCurrency> & {
  store: Store; member: Member | null; memberLoading: boolean; refreshMember: () => Promise<boolean>
  shoppingURL: string; rememberShoppingURL: (path: string) => void
  cart: CartLine[]; add: (variant: number, quantity: number) => boolean; update: (variant: number, quantity: number) => void; remove: (variant: number) => void; consume: (lines: CartLine[]) => void
}
const ShopContext = createContext<ShopContextValue | null>(null)
export function useShop() { const context = useContext(ShopContext); if (!context) throw new Error("ShopProvider is required"); return context }
export function ShopProvider({ store, children }: { store: Store; children: ReactNode }) {
  const currency = useCurrency(store.site_id)
  const key = `gocms-cart:${store.site_id}`
  const [cart, setCart] = useState<CartLine[]>(() => savedCart(key))
  const cartRef = useRef(cart)
  const returnKey = `gocms-shopping-return:${store.site_id}`
  const [shoppingURL, setShoppingURL] = useState(() => {
    try { return shoppingReturnPath(sessionStorage.getItem(returnKey)) } catch { return "/" }
  })
  const [member, setMember] = useState<Member | null>(null)
  const [memberLoading, setMemberLoading] = useState(true)
  async function refreshMember() {
    setMemberLoading(true)
    try { const response = await getMember(); setMember(response.data); return true }
    catch (error) { setMember(null); if (!(error instanceof APIError && error.status === 401)) showError(error); return false }
    finally { setMemberLoading(false) }
  }
  useEffect(() => { void refreshMember() }, [])
  useEffect(() => {
    const changed = (event: StorageEvent) => {
      if (event.key === key || event.key === null) {
        const next = event.key === null ? savedCart(key) : decodeCart(event.newValue)
        cartRef.current = next; setCart(next)
      }
    }
    window.addEventListener("storage", changed); return () => window.removeEventListener("storage", changed)
  }, [key])
  function rememberShoppingURL(path: string) {
    const next = shoppingReturnPath(path)
    setShoppingURL(next)
    try { sessionStorage.setItem(returnKey, next) } catch {}
  }
  function saveCart(next: CartLine[]) {
    try { localStorage.setItem(key, JSON.stringify(next)) }
    catch { showError(localError("errors.cartStorage")); return false }
    cartRef.current = next; setCart(next)
    return true
  }
  function add(variant: number, quantity: number) {
    if (!Number.isSafeInteger(variant) || variant <= 0 || !Number.isInteger(quantity) || quantity < 1 || quantity > 1000) { showError(localError("errors.invalidCartInput")); return false }
    const current = cartRef.current
    const old = current.find(line => line.variant_id === variant)
    if (!old && current.length >= 50) { showError(localError("errors.cartLimit")); return false }
    if (old && old.quantity + quantity > 1000) { showError(localError("errors.quantityLimit")); return false }
    if (!saveCart(old ? current.map(line => line.variant_id === variant ? { ...line, quantity: line.quantity + quantity } : line) : [...current, { variant_id: variant, quantity }])) return false
    showSuccess(String(i18n.t("cart.added")))
    return true
  }
  function update(variant: number, quantity: number) { if (Number.isInteger(quantity) && quantity >= 1 && quantity <= 1000) saveCart(cartRef.current.map(line => line.variant_id === variant ? { ...line, quantity } : line)) }
  function remove(variant: number) { saveCart(cartRef.current.filter(line => line.variant_id !== variant)) }
  function consume(lines: CartLine[]) { saveCart(cartRef.current.map(line => ({ ...line, quantity: line.quantity - (lines.find(row => row.variant_id === line.variant_id)?.quantity ?? 0) })).filter(line => line.quantity > 0)) }
  return <ShopContext.Provider value={{ ...currency, store, member, memberLoading, refreshMember, shoppingURL, rememberShoppingURL, cart, add, update, remove, consume }}>{children}</ShopContext.Provider>
}
export function useResource<T>(key: string, loader: () => Promise<T>, keepPreviousData = false) {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<Error | null>(null)
  const [loading, setLoading] = useState(true)
  const [version, setVersion] = useState(0)
  const [resolvedKey, setResolvedKey] = useState("")
  useEffect(() => {
    let active = true
    setLoading(true); setError(null); if (!keepPreviousData) setData(null)
    loader().then(result => { if (active) setData(result) }).catch(reason => { if (active) setError(reason instanceof Error ? reason : localError("errors.load")) }).finally(() => { if (active) { setLoading(false); setResolvedKey(key) } })
    return () => { active = false }
  }, [key, version])
  return { data, error, loading: loading || resolvedKey !== key, reload: () => setVersion(value => value + 1) }
}
export function useCartVariants() {
  const { cart, store, currency, pricingLoading, refreshPricing } = useShop()
  const resource = useResource(`${store.site_id}:${currency}:${pricingLoading}:cart:${cart.map(line => line.variant_id).sort((a, b) => a-b).join(",")}`, () => getVariants(cart, currency), true)
  return { ...resource, loading: resource.loading || pricingLoading, reload: () => { refreshPricing(); resource.reload() } }
}
