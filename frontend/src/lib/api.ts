export type AdminUser = {
  id: number
  username: string
  flags: string
}

export type AdminStats = {
  contents: number
  visible_contents: number
  messages: number
  pending_messages: number
}

export type Content = {
  id: number
  route_key: string
  title: string
  code: string
  category_id: number
  summary: string
  content: string
  cover_image: string
  published_at: string
  source: string
  keywords: string
  description: string
  order_id: number
  featured: number
  visible: number
}

export type ContentInput = Omit<Content, "id" | "route_key">

export type MediaAsset = {
  id: number
  kind: "image"
  url: string
  original_name: string
  mime_type: string
  size_bytes: number
  width: number
  height: number
  sha256: string
  status: string
  uploaded_by: string
  created_at: string
}

export type MediaPage = {
  page: number
  page_size: number
  total: number
  items: MediaAsset[]
}

export type ContentPage = {
  query: string
  page: number
  page_size: number
  total: number
  items: Content[]
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
  content_id: number
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
  list_page_size: number
  page_type: "list" | "cover"
  route_id: number
  content_count: number
  list_path: string
  list_file_pattern: string
  list_template: string
  cover_template: string
  detail_path: string
  detail_file_pattern: string
  detail_template: string
}

export type CategoryInput = Omit<CategoryItem, "id" | "route_id" | "content_count">

export type ThemeFileKind = "css" | "template"

export type ThemeFile = {
  path: string
  size: number
  modified_at: string
}

export type ThemeFiles = {
  name: string
  active_theme: string
  themes: ThemeInfo[]
  css_files: ThemeFile[]
  template_files: ThemeFile[]
  template_groups: ThemeTemplateGroup[]
}

export type ThemeInfo = {
  id: string
  name: string
  version?: string
  description?: string
  author?: string
  active: boolean
}

export type ThemeTemplateGroup = {
  key: string
  label: string
  files: ThemeFile[]
  assignments: ThemeTemplateAssignment[]
}

export type ThemeTemplateAssignment = {
  key: string
  label: string
  dimension: string
  dimension_name: string
  template_path: string
  available: boolean
}

export type ThemeFileContent = ThemeFile & {
  kind: ThemeFileKind
  content: string
}

export type UpdateCheck = {
  current_version: string
  latest_version: string
  latest_tag: string
  update_available: boolean
  can_update: boolean
  release_available: boolean
  asset_name: string
  release_url: string
  release_notes: string
}

type SessionResponse = { user: AdminUser }

const apiBase = (import.meta.env.VITE_API_BASE ?? "").replace(/\/$/, "")

function endpoint(path: string) {
  return `${apiBase}${path}`
}

async function request<T>(path: string, init: RequestInit = {}) {
  const headers = new Headers(init.headers)
  if (init.body && !(init.body instanceof FormData) && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json")
  }
  const response = await fetch(endpoint(path), {
    ...init,
    credentials: "include",
    headers,
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

export function getContent(page: number, pageSize: number, search: string, categoryID = 0) {
  return request<ContentPage>(
    `/api/admin/content${query({ page, page_size: pageSize, q: search, category_id: categoryID || undefined })}`,
  )
}

export function getContentItem(id: number) {
  return request<Content>(`/api/admin/content/${id}`)
}

export function getCategories() {
  return request<CategoryItem[]>("/api/admin/categories")
}

export function createCategory(payload: CategoryInput, publish = false) {
  return request<SaveResponse>(`/api/admin/categories${publish ? "?publish=1" : ""}`, {
    method: "POST",
    body: JSON.stringify(payload),
  })
}

export function updateCategory(id: number, payload: CategoryInput, publish = false) {
  return request<SaveResponse>(`/api/admin/categories/${id}${publish ? "?publish=1" : ""}`, {
    method: "PUT",
    body: JSON.stringify(payload),
  })
}

export function deleteCategory(id: number, publish = false) {
  return request<SaveResponse>(`/api/admin/categories/${id}${publish ? "?publish=1" : ""}`, {
    method: "DELETE",
  })
}

export function createContent(payload: ContentInput, publish = false) {
  return request<SaveResponse>(`/api/admin/content${publish ? "?publish=1" : ""}`, {
    method: "POST",
    body: JSON.stringify(payload),
  })
}

export function updateContent(id: number, payload: ContentInput, publish = false) {
  return request<SaveResponse>(`/api/admin/content/${id}${publish ? "?publish=1" : ""}`, {
    method: "PUT",
    body: JSON.stringify(payload),
  })
}

export function deleteContent(id: number, publish = false) {
  return request<SaveResponse>(`/api/admin/content/${id}${publish ? "?publish=1" : ""}`, {
    method: "DELETE",
  })
}

export function uploadMedia(file: File) {
  const body = new FormData()
  body.append("file", file)
  return request<{ ok: boolean; asset: MediaAsset }>("/api/admin/media", {
    method: "POST",
    body,
  })
}

export function getMedia(page = 1, pageSize = 20, search = "", contentID = 0) {
  return request<MediaPage>(
    `/api/admin/media${query({ page, page_size: pageSize, q: search, content_id: contentID || undefined })}`,
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

export function deleteMessage(id: number) {
  return request<{ ok: boolean }>(`/api/admin/messages/${id}`, {
    method: "DELETE",
  })
}

export type Publication = { state: "idle" | "running" | "success" | "failed"; started: string; finished: string; files: number; contents: number; error?: string }
export type SaveResponse = { ok: boolean; publication?: Publication; publish_started?: boolean }
export function getPublication() { return request<Publication>("/api/admin/publish") }
export function publishSite() { return request<Publication>("/api/admin/publish", { method: "POST" }) }
export type SitemapFormat = "html" | "xml"
export type SitemapResponse = { ok: boolean; format: SitemapFormat; path: string }
export function generateSitemap(format: SitemapFormat) {
  return request<SitemapResponse>("/api/admin/publish/sitemap", {
    method: "POST",
    body: JSON.stringify({ format }),
  })
}

export function getThemeFiles() {
  return request<ThemeFiles>("/api/admin/theme")
}

export function activateTheme(id: string) {
  return request<{ ok: boolean; theme: ThemeInfo; publish_started: boolean; publication?: Publication }>("/api/admin/theme/activate", {
    method: "POST",
    body: JSON.stringify({ id }),
  })
}

export function importTheme(file: File) {
  const body = new FormData()
  body.append("theme", file)
  return request<{ ok: boolean; theme: ThemeInfo }>("/api/admin/theme/import", {
    method: "POST",
    body,
  })
}

export function themeExportURL(id?: string) {
  return endpoint(`/api/admin/theme/export${query({ id })}`)
}

export function getThemeFile(kind: ThemeFileKind, filePath: string) {
  const params = new URLSearchParams({ kind, path: filePath })
  return request<ThemeFileContent>(`/api/admin/theme?${params.toString()}`)
}

export function updateThemeAssignment(key: string, templatePath: string) {
  return request<{ ok: boolean; key: string; template_path: string }>(`/api/admin/theme/assignments/${encodeURIComponent(key)}`, {
    method: "PUT",
    body: JSON.stringify({ template_path: templatePath }),
  })
}

export function checkForUpdate(currentVersion: string) {
  return request<UpdateCheck>(`/api/admin/update/check${query({ current_version: currentVersion })}`)
}

export function installUpdate(currentVersion: string) {
  return request<{ ok: boolean; version: string; message: string }>("/api/admin/update", {
    method: "POST",
    body: JSON.stringify({ current_version: currentVersion }),
  })
}
