export type AdminUser = {
  id: number
  username: string
  flags: string
}

export type AdminStats = {
  products: number
  visible_products: number
  news: number
  messages: number
  pending_messages: number
}

export type Product = {
  id: number
  name: string
  code: string
  category_id: number
  remark: string
  content: string
  small_pic: string
  big_pic: string
  keywords: string
  order_id: number
  featured: number
  visible: number
}

export type ProductInput = Omit<Product, "id">

export type ProductPage = {
  query: string
  page: number
  page_size: number
  total: number
  items: Product[]
}

export type NewsItem = {
  id: number
  title: string
  category_id: number
  published_at: string
  picture: string
  featured: number
}

export type NewsPage = {
  query: string
  page: number
  page_size: number
  total: number
  items: NewsItem[]
}

export type MessageItem = {
  id: number
  title: string
  name: string
  phone: string
  mobile: string
  email: string
  address: string
  content: string
  created_at: string
  state: number
  product_id: number
}

export type MessagePage = {
  page: number
  page_size: number
  total: number
  items: MessageItem[]
}

export type CategoryItem = {
  id: number
  name: string
  parent_id: number
  order_id: number
}

type SessionResponse = { user: AdminUser }

const apiBase = (import.meta.env.VITE_API_BASE ?? "").replace(/\/$/, "")

function endpoint(path: string) {
  return `${apiBase}${path}`
}

async function request<T>(path: string, init: RequestInit = {}) {
  const response = await fetch(endpoint(path), {
    ...init,
    credentials: "include",
    headers: {
      ...(init.body ? { "Content-Type": "application/json" } : {}),
      ...init.headers,
    },
  })
  const contentType = response.headers.get("content-type") ?? ""
  const payload = contentType.includes("application/json")
    ? await response.json()
    : await response.text()
  if (!response.ok) {
    const message =
      typeof payload === "object" && payload !== null && "error" in payload
        ? String(payload.error)
        : typeof payload === "string" && payload
          ? payload
          : `请求失败（${response.status}）`
    throw new Error(message)
  }
  return payload as T
}

function query(params: Record<string, string | number | undefined>) {
  const search = new URLSearchParams()
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== "") {
      search.set(key, String(value))
    }
  })
  const encoded = search.toString()
  return encoded ? `?${encoded}` : ""
}

export function getSession() {
  return request<SessionResponse>("/api/admin/session")
}

export function login(username: string, password: string) {
  return request<SessionResponse>("/api/admin/login", {
    method: "POST",
    body: JSON.stringify({ username, password }),
  })
}

export function logout() {
  return request<{ ok: boolean }>("/api/admin/logout", { method: "POST" })
}

export function getStats() {
  return request<AdminStats>("/api/admin/stats")
}

export function getProducts(page: number, pageSize: number, search: string) {
  return request<ProductPage>(
    `/api/admin/products${query({ page, page_size: pageSize, q: search })}`,
  )
}

export function getProduct(id: number) {
  return request<Product>(`/api/products/${id}`)
}

export function getCategories() {
  return request<CategoryItem[]>("/api/admin/categories")
}

export function createProduct(payload: ProductInput, publish = false) {
  return request<SaveResponse>(`/api/admin/products${publish ? "?publish=1" : ""}`, {
    method: "POST",
    body: JSON.stringify(payload),
  })
}

export function updateProduct(id: number, payload: ProductInput, publish = false) {
  return request<SaveResponse>(`/api/admin/products/${id}${publish ? "?publish=1" : ""}`, {
    method: "PUT",
    body: JSON.stringify(payload),
  })
}

export function archiveProduct(id: number) {
  return request<SaveResponse>(`/api/admin/products/${id}?publish=1`, {
    method: "DELETE",
  })
}

export function getNews(page: number, pageSize: number, search: string) {
  return request<NewsPage>(
    `/api/admin/news${query({ page, page_size: pageSize, q: search })}`,
  )
}

export function getMessages(page: number, pageSize: number) {
  return request<MessagePage>(
    `/api/admin/messages${query({ page, page_size: pageSize })}`,
  )
}

export function updateMessageState(id: number, state: number) {
  return request<{ ok: boolean }>(`/api/admin/messages/${id}`, {
    method: "PATCH",
    body: JSON.stringify({ state }),
  })
}

export type Publication = { state: "idle" | "running" | "success" | "failed"; started: string; finished: string; files: number; products: number; news: number; error?: string }
export type SaveResponse = { ok: boolean; publication?: Publication; publish_started?: boolean }
export function getPublication() { return request<Publication>("/api/admin/publish") }
export function publishSite() { return request<Publication>("/api/admin/publish", { method: "POST" }) }
