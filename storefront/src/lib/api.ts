import { apiErrorKey, LocalizedError } from "./errors"
import i18n, { currentLanguage } from "@/i18n"
export type Specification = { name: string; value: string; unit?: string }
export type Variant = { source_amount?: number; source_currency?: string; display_amount?: number; display_currency?: string; pricing_revision?: number; pricing_unavailable?: boolean; id: number; content_id: number; name: string; image_url: string; kind: "physical" | "digital" | "service"; currency: string; amount: number; sku: string; specifications: Specification[] }
export type OrderLine = Variant & { quantity: number; line_number: number; shipped_quantity: number }
export type Channel = { id: number; name: string; currency: string; provider: string; sandbox: boolean }
export type Store = { site_id: number; site_name: string; path: string; default_language?: string }
export type Member = { id: number; username: string; email: string; display_name: string; has_password: boolean }
export type CartLine = { variant_id: number; quantity: number }
export type Address = { phone: string; street: string; city: string; state: string; postcode: string; country: string }
export type OrderInput = { pricing_revision?: number; language: string; items: CartLine[]; email: string; customer_name: string; address: Address; expected_amount: number; currency: string }
export type Attempt = { id: string; account_id: number; status: string; payment_url?: string }
export type Shipment = { id: string; method: string; carrier: string; tracking_number: string; tracking_url: string; shipped_at: string; note: string; items: {line_number: number; quantity: number}[] }
export type Order = { shipments: Shipment[]; id: string; buyer_id: number; items: OrderLine[]; amount: number; quantity: number; currency: string; status: string; fulfillment_status: string; customer_name: string; email: string; address: Address; created_at: string; attempts?: Attempt[]; refunds?: { id: string; status: string }[] }
export class APIError extends LocalizedError {
  status: number
  code: string
  constructor(status: number, code = "") { super(apiErrorKey(code, status), { status }); this.status = status; this.code = code }
}
let siteScope: number | null = null
export function setSiteScope(site: number) { siteScope = site }
export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  if (siteScope) headers.set("X-Site-Id", String(siteScope))
  if (init.body) headers.set("Content-Type", "application/json")
  const response = await fetch(path, { ...init, headers, credentials: "same-origin", cache: "no-store" })
  const payload = await response.json().catch(() => null)
  if (!response.ok) throw new APIError(response.status, payload?.code ?? payload?.error?.code ?? "")
  return payload as T
}
export type PricingInfo = { enabled: boolean; base_currency: string; currencies: { code: string; exponent: number }[]; recommended_currency: string; revision: number }
export const getPricing = (country = "") => api<PricingInfo>(`/api/commerce/pricing${country ? `?country=${encodeURIComponent(country)}` : ""}`)
export const getStore = () => api<Store>("/api/commerce/storefront")
export const getChannels = () => api<{ channels: Channel[] }>("/api/commerce/channels")
export const getVariants = (lines: CartLine[], currency = "") => lines.length ? api<{ items: Variant[] }>(`/api/commerce/variants?ids=${lines.map(line => line.variant_id).join(",")}${currency ? `&currency=${encodeURIComponent(currency)}` : ""}`) : Promise.resolve({ items: [] as Variant[] })
export const getMember = () => api<{ data: Member }>("/api/v1/me")
export const login = (username: string, password: string) => api("/api/v1/auth/login", { method: "POST", body: JSON.stringify({ username, password }) })
export const register = (username: string, password: string, email = "") => api("/api/v1/auth/register", { method: "POST", body: JSON.stringify({ username, password, email }) })
export const logout = () => api("/api/v1/auth/logout", { method: "POST", body: "{}" })
export const createOrder = (body: string, key: string) => api<Order>("/api/commerce/orders", { method: "POST", headers: { "Idempotency-Key": key }, body })
export const getOrder = (id: string) => api<Order>(`/api/commerce/orders/${encodeURIComponent(id)}`)
export const getOrders = (page: number) => api<{ items: Order[] }>(`/api/commerce/orders?page=${page}`)
export const syncOrder = (id: string) => api<Order>(`/api/commerce/orders/${encodeURIComponent(id)}/sync`, { method: "POST", body: "{}" })
export const payOrder = (id: string, channel: number) => api<Attempt>(`/api/commerce/orders/${encodeURIComponent(id)}/pay`, { method: "POST", body: JSON.stringify({ account_id: channel }) })
export const money = (amount: number, currency: string) => new Intl.NumberFormat(currentLanguage(), { style: "currency", currency }).format(amount / (currency === "JPY" ? 1 : 100))
export const specification = (item: Variant) => item.specifications?.map(spec => `${spec.name}: ${spec.value}${spec.unit ? ` ${spec.unit}` : ""}`).join(" / ") || String(i18n.t("common.standardSpec"))

export const loginProviders = ["google", "facebook", "microsoft"] as const
export type LoginProvider = typeof loginProviders[number]
export const providerNames: Record<LoginProvider, string> = { google: "Google", facebook: "Facebook", microsoft: "Microsoft" }
export const getLoginProviders = () => api<Record<LoginProvider, boolean>>("/api/auth/providers")
export const startGoogleLogin = (next: string, bind = false, password = "") => api<{ url: string }>("/api/auth/google/start", { method: "POST", body: JSON.stringify({ next, bind, password }) })
export const getGoogleIdentity = () => api<{ data: { bound: boolean; has_password: boolean } }>("/api/v1/me/identities/google")
export const unbindGoogle = (password: string) => api("/api/v1/me/identities/google", { method: "DELETE", body: JSON.stringify({ password }) })

export const startProviderLogin = (provider: LoginProvider, next:string, bind=false,password="",recover=false) => api<{url:string}>(`/api/auth/${provider}/start`,{method:"POST",body:JSON.stringify({next,bind,password,recover})})
export const getProviderIdentity = (provider:string) => api<{ data: { bound:boolean; has_password:boolean } }>(`/api/v1/me/identities/${provider}`)
export const unbindProvider = (provider:string,password:string) => api(`/api/v1/me/identities/${provider}`,{method:"DELETE",body:JSON.stringify({password})})

export const getPasswordOptions = () => api<{data:{email_enabled:boolean;support_email:string}}>("/api/v1/auth/password-options")
export const forgotPassword = (email:string,language:string) => api("/api/v1/auth/forgot-password",{method:"POST",body:JSON.stringify({email,language})})
export const resetPassword = (token:string,password:string) => api("/api/v1/auth/reset-password",{method:"POST",body:JSON.stringify({token,password})})
export const changePassword = (old_password:string,new_password:string) => api("/api/v1/me/password",{method:"PUT",body:JSON.stringify({old_password,new_password})})
