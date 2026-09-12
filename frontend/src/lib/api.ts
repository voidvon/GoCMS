export type AdminUser = {
  category_ids: number[] | null
  id: number
  username: string
  flags: string
  is_super: boolean
  group_id: number
  permissions: string[]
}

export type OperationLog = { id: number; username: string; method: string; path: string; status: number; ip: string; created_at: string }
export const getOperationLogs = (page: number, username: string) => request<{ items: OperationLog[]; total: number; page_size: number }>(`/api/admin/logs?page=${page}&username=${encodeURIComponent(username)}`)
export type LoginLog = { id: number; username: string; success: boolean; ip: string; created_at: string }
export const getLoginLogs = () => request<{ items: LoginLog[] }>("/api/admin/logins")
export const clearLogs = (before: string) => request<{ ok: boolean }>("/api/admin/logs/clear", { method: "POST", body: JSON.stringify({ before }) })

export type AdminAccount = {
  category_ids?: number[] | null
  id: number
  username: string
  group_id: number
  is_super: boolean
  disabled: boolean
  password?: string
}
export type AdminGroup = { id: number; name: string; permissions: string[] }
export const getAdminAccounts = () => request<AdminAccount[]>("/api/admin/users")
export const getAdminGroups = () => request<{ items: AdminGroup[]; permissions: { key: string; label: string }[] }>("/api/admin/groups")
export const saveAdminAccount = (input: AdminAccount) => request("/api/admin/users", { method: input.id ? "PUT" : "POST", body: JSON.stringify(input) })
export const deleteAdminAccount = (id: number) => request("/api/admin/users", { method: "DELETE", body: JSON.stringify({ id }) })
export const saveAdminGroup = (input: AdminGroup) => request("/api/admin/groups", { method: input.id ? "PUT" : "POST", body: JSON.stringify(input) })
export const deleteAdminGroup = (id: number) => request("/api/admin/groups", { method: "DELETE", body: JSON.stringify({ id }) })

export type AdminStats = {
  contents: number
  visible_contents: number
  messages: number
  pending_messages: number
}

export type ContentTranslationItem = {
  title?: string
  summary?: string
  content?: string
  keywords?: string
  description?: string
  extra_data?: Record<string, any>
  publish_status?: string
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
  model_id?: number
  extra_data?: Record<string, any>
  is_fallback?: boolean
  fallback_lang?: string
  translations?: Record<string, ContentTranslationItem>
}

export type ContentInput = Omit<Content, "id" | "route_key"> & {
  translations?: Record<string, ContentTranslationItem>
}

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
  class_id?: number
  class_name?: string
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
  ip?: string
  extra_data?: Record<string, any>
}

export type MessagePage = {
  page: number
  page_size: number
  total: number
  items: MessageItem[]
}

export type FeedbackItem = MessageItem
export type FeedbackPage = MessagePage

export type ModelTable = {
  id: number
  table_name: string
  name: string
  description: string
  is_default: number
  created_at: string
  field_count?: number
}

export type ModelField = {
  id: number
  table_id: number
  field_name: string
  field_label: string
  field_type: string
  field_options: string
  description: string
  sort_order: number
  is_system: number
  is_translatable?: number
}

export type Language = {
  id: number
  code: string
  name: string
  is_default: number
  is_fallback: number
  is_enabled: number
  sort_order: number
  path_prefix: string
}

export type LanguageInput = Omit<Language, "id">

export type SystemModel = {
  id: number
  name: string
  table_id: number
  table_name?: string
  description: string
  entry_fields: { field: string; label: string }[]
  must_fields: string[]
  is_default: number
  sort_order: number
  created_at: string
}

export type FeedbackClass = {
  id: number
  name: string
  description: string
  fields_config: { field: string; label: string }[]
  must_fields: string[]
  sort_order: number
  created_at: string
  item_count?: number
}

export type FeedbackField = {
  id: number
  field_name: string
  field_label: string
  field_type: string
  field_options: string
  description: string
  sort_order: number
  is_system: number
}

export type CategoryTranslationItem = {
  name?: string
  keywords?: string
  description?: string
  cover_content?: string
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
  keywords?: string
  description?: string
  cover_content?: string
  model_id?: number
  is_fallback?: boolean
  fallback_lang?: string
  translations?: Record<string, CategoryTranslationItem>
}

export type CategoryInput = Omit<CategoryItem, "id" | "route_id" | "content_count"> & {
  translations?: Record<string, CategoryTranslationItem>
}

export type ThemeFileKind = "css" | "js" | "image" | "template"
export type CustomFileKind = Exclude<ThemeFileKind, "template">

export type ThemeFile = {
  path: string
  size: number
  modified_at: string
}

export type ThemeFiles = {
  name: string
  active_theme: string
  themes: ThemeInfo[]
  template_groups_catalog: ThemeInfo[]
  css_files: ThemeFile[]
  js_files: ThemeFile[]
  image_files: ThemeFile[]
  custom_files: {
    css: ThemeFile[]
    js: ThemeFile[]
    images: ThemeFile[]
  }
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
  count: number
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

export type TemplateLabelContext = "any" | "home" | "category" | "list" | "detail"

export type TemplateLabelCategory = {
  id: number
  name: string
  sort_order: number
  label_count: number
}

export type TemplateLabel = {
  id: number
  key: string
  name: string
  category_id: number
  category_name: string
  context: TemplateLabelContext
  description: string
  content: string
  temptext?: string
  listvar?: string
  rownum?: number
  subnews?: number
  showdate?: string
  sort_order: number
  created_at: string
  updated_at: string
}

export type TemplateLabelInput = {
  key: string
  name: string
  category_id: number
  context: TemplateLabelContext
  description: string
  content: string
  temptext?: string
  listvar?: string
  rownum?: number
  subnews?: number
  showdate?: string
  sort_order: number
}

export type TemplateField = {
  name: string
  type: string
  description: string
}

export type TemplateTag = {
  name: string
  category: string
  signature: string
  description: string
  context: string
  example: string
  fields?: TemplateField[]
}

export type TemplateLabelsResponse = {
  page: number
  page_size: number
  total: number
  items: TemplateLabel[]
  categories: TemplateLabelCategory[]
  template_tags: TemplateTag[]
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

export function getContent(page: number, pageSize: number, search: string, categoryID = 0, lang?: string) {
  return request<ContentPage>(
    `/api/admin/content${query({ page, page_size: pageSize, q: search, category_id: categoryID || undefined, lang: lang || undefined })}`,
  )
}

export function getContentItem(id: number, lang?: string) {
  return request<Content>(`/api/admin/content/${id}${query({ lang: lang || undefined })}`)
}

export function getCategories(lang?: string) {
  return request<CategoryItem[]>(`/api/admin/categories${query({ lang: lang || undefined })}`)
}

export function getLanguages() {
  return request<Language[]>("/api/admin/languages")
}

export function createLanguage(payload: LanguageInput) {
  return request<{ ok: boolean; id: number }>("/api/admin/languages", {
    method: "POST",
    body: JSON.stringify(payload),
  })
}

export function updateLanguage(id: number, payload: LanguageInput) {
  return request<{ ok: boolean }>(`/api/admin/languages/${id}`, {
    method: "PUT",
    body: JSON.stringify(payload),
  })
}

export function deleteLanguage(id: number) {
  return request<{ ok: boolean }>(`/api/admin/languages/${id}`, {
    method: "DELETE",
  })
}

export function createCategory(payload: CategoryInput, publish = false, lang?: string) {
  return request<SaveResponse>(`/api/admin/categories${query({ publish: publish ? 1 : undefined, lang: lang || undefined })}`, {
    method: "POST",
    body: JSON.stringify(payload),
  })
}

export function updateCategory(id: number, payload: CategoryInput, publish = false, lang?: string) {
  return request<SaveResponse>(`/api/admin/categories/${id}${query({ publish: publish ? 1 : undefined, lang: lang || undefined })}`, {
    method: "PUT",
    body: JSON.stringify(payload),
  })
}

export function deleteCategory(id: number, publish = false) {
  return request<SaveResponse>(`/api/admin/categories/${id}${publish ? "?publish=1" : ""}`, {
    method: "DELETE",
  })
}

export function createContent(payload: ContentInput, publish = false, lang?: string) {
  return request<SaveResponse>(`/api/admin/content${query({ publish: publish ? 1 : undefined, lang: lang || undefined })}`, {
    method: "POST",
    body: JSON.stringify(payload),
  })
}

export function updateContent(id: number, payload: ContentInput, publish = false, lang?: string) {
  return request<SaveResponse>(`/api/admin/content/${id}${query({ publish: publish ? 1 : undefined, lang: lang || undefined })}`, {
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

// Model Tables, Fields, Models
export function getModelTables() {
  return request<ModelTable[]>("/api/admin/model-tables")
}

export function saveModelTable(data: Partial<ModelTable>) {
  if (data.id) {
    return request<{ ok: boolean }>(`/api/admin/model-tables/${data.id}`, {
      method: "PUT",
      body: JSON.stringify(data),
    })
  }
  return request<{ ok: boolean; id: number }>("/api/admin/model-tables", {
    method: "POST",
    body: JSON.stringify(data),
  })
}

export function deleteModelTable(id: number) {
  return request<{ ok: boolean }>(`/api/admin/model-tables/${id}`, { method: "DELETE" })
}

export function getModelFields(tableId?: number) {
  return request<ModelField[]>(`/api/admin/model-fields${query({ table_id: tableId || undefined })}`)
}

export function saveModelField(data: Partial<ModelField>) {
  if (data.id) {
    return request<{ ok: boolean }>(`/api/admin/model-fields/${data.id}`, {
      method: "PUT",
      body: JSON.stringify(data),
    })
  }
  return request<{ ok: boolean; id: number }>("/api/admin/model-fields", {
    method: "POST",
    body: JSON.stringify(data),
  })
}

export function deleteModelField(id: number) {
  return request<{ ok: boolean }>(`/api/admin/model-fields/${id}`, { method: "DELETE" })
}

export function getSystemModels() {
  return request<SystemModel[]>("/api/admin/models")
}

export function saveSystemModel(data: Partial<SystemModel>) {
  if (data.id) {
    return request<{ ok: boolean }>(`/api/admin/models/${data.id}`, {
      method: "PUT",
      body: JSON.stringify(data),
    })
  }
  return request<{ ok: boolean; id: number }>("/api/admin/models", {
    method: "POST",
    body: JSON.stringify(data),
  })
}

export function deleteSystemModel(id: number) {
  return request<{ ok: boolean }>(`/api/admin/models/${id}`, { method: "DELETE" })
}

// Custom Feedback
export function getFeedbacks(page: number, pageSize: number, classId?: number, state?: number | string, keyword?: string) {
  return request<FeedbackPage>(
    `/api/admin/feedback${query({
      page,
      page_size: pageSize,
      class_id: classId || undefined,
      state: state !== "" && state !== undefined ? state : undefined,
      keyword: keyword || undefined,
    })}`,
  )
}

export function updateFeedbackState(id: number, state: number) {
  return request<{ ok: boolean }>(`/api/admin/feedback/${id}`, {
    method: "PATCH",
    body: JSON.stringify({ state }),
  })
}

export function deleteFeedback(id: number) {
  return request<{ ok: boolean }>(`/api/admin/feedback/${id}`, {
    method: "DELETE",
  })
}

export function batchDeleteFeedbacks(ids: number[]) {
  return request<{ ok: boolean }>("/api/admin/feedback/batch-delete", {
    method: "POST",
    body: JSON.stringify({ ids }),
  })
}

export function getFeedbackClasses() {
  return request<FeedbackClass[]>("/api/admin/feedback-classes")
}

export function saveFeedbackClass(data: Partial<FeedbackClass>) {
  if (data.id) {
    return request<{ ok: boolean }>(`/api/admin/feedback-classes/${data.id}`, {
      method: "PUT",
      body: JSON.stringify(data),
    })
  }
  return request<{ ok: boolean; id: number }>("/api/admin/feedback-classes", {
    method: "POST",
    body: JSON.stringify(data),
  })
}

export function deleteFeedbackClass(id: number) {
  return request<{ ok: boolean }>(`/api/admin/feedback-classes/${id}`, { method: "DELETE" })
}

export function getFeedbackFields() {
  return request<FeedbackField[]>("/api/admin/feedback-fields")
}

export function saveFeedbackField(data: Partial<FeedbackField>) {
  if (data.id) {
    return request<{ ok: boolean }>(`/api/admin/feedback-fields/${data.id}`, {
      method: "PUT",
      body: JSON.stringify(data),
    })
  }
  return request<{ ok: boolean; id: number }>("/api/admin/feedback-fields", {
    method: "POST",
    body: JSON.stringify(data),
  })
}

export function deleteFeedbackField(id: number) {
  return request<{ ok: boolean }>(`/api/admin/feedback-fields/${id}`, { method: "DELETE" })
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
  return request<ThemeFiles>("/api/admin/templates")
}

export function activateTheme(id: string) {
  return request<{ ok: boolean; theme: ThemeInfo; publish_started: boolean; publication?: Publication }>("/api/admin/templates/activate", {
    method: "POST",
    body: JSON.stringify({ id }),
  })
}

export function importTheme(file: File) {
  const body = new FormData()
  body.append("theme", file)
  return request<{ ok: boolean; theme: ThemeInfo }>("/api/admin/templates/import", {
    method: "POST",
    body,
  })
}

export function themeExportURL(id?: string) {
  return endpoint(`/api/admin/templates/export${query({ id })}`)
}

export function getThemeFile(kind: ThemeFileKind, filePath: string) {
  const params = new URLSearchParams({ kind, path: filePath })
  return request<ThemeFileContent>(`/api/admin/templates?${params.toString()}`)
}

export function updateThemeFile(kind: ThemeFileKind, filePath: string, content: string) {
  return request<{ ok: boolean; file: ThemeFile }>("/api/admin/templates/files", {
    method: "PUT",
    body: JSON.stringify({ kind, path: filePath, content }),
  })
}

export function uploadThemeFile(kind: CustomFileKind, file: File, filePath?: string) {
  const body = new FormData()
  body.append("kind", kind)
  body.append("file", file)
  if (filePath) body.append("path", filePath)
  return request<{ ok: boolean; file: ThemeFile }>("/api/admin/templates/files", {
    method: "POST",
    body,
  })
}

export function deleteThemeFile(kind: CustomFileKind, filePath: string) {
  return request<{ ok: boolean }>(`/api/admin/templates/files${query({ kind, path: filePath })}`, {
    method: "DELETE",
  })
}

export function updateThemeAssignment(key: string, templatePath: string) {
  return request<{ ok: boolean; key: string; template_path: string }>(`/api/admin/templates/assignments/${encodeURIComponent(key)}`, {
    method: "PUT",
    body: JSON.stringify({ template_path: templatePath }),
  })
}

export function getTemplateLabels(page = 1, queryText = "", categoryID = 0) {
  return request<TemplateLabelsResponse>(`/api/admin/templates/label-templates${query({ page, page_size: 20, q: queryText, category_id: categoryID })}`)
}

export function createTemplateLabel(input: TemplateLabelInput, publish = false) {
  return request<{ ok: boolean; item: TemplateLabel; publication?: Publication; publish_started?: boolean }>(`/api/admin/templates/label-templates${publish ? "?publish=1" : ""}`, {
    method: "POST",
    body: JSON.stringify(input),
  })
}

export function updateTemplateLabel(id: number, input: TemplateLabelInput, publish = false) {
  return request<{ ok: boolean; item: TemplateLabel; publication?: Publication; publish_started?: boolean }>(`/api/admin/templates/label-templates/${id}${publish ? "?publish=1" : ""}`, {
    method: "PUT",
    body: JSON.stringify(input),
  })
}

export function deleteTemplateLabel(id: number, publish = false) {
  return request<{ ok: boolean; publication?: Publication; publish_started?: boolean }>(`/api/admin/templates/label-templates/${id}${publish ? "?publish=1" : ""}`, {
    method: "DELETE",
  })
}

export function createTemplateLabelCategory(name: string, sortOrder = 0) {
  return request<{ ok: boolean; category: TemplateLabelCategory }>("/api/admin/templates/label-categories", {
    method: "POST",
    body: JSON.stringify({ name, sort_order: sortOrder }),
  })
}

export function updateTemplateLabelCategory(id: number, name: string, sortOrder = 0) {
  return request<{ ok: boolean; category: TemplateLabelCategory }>(`/api/admin/templates/label-categories/${id}`, {
    method: "PUT",
    body: JSON.stringify({ name, sort_order: sortOrder }),
  })
}

export function deleteTemplateLabelCategory(id: number) {
  return request<{ ok: boolean }>(`/api/admin/templates/label-categories/${id}`, {
    method: "DELETE",
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

export type ApiKeyStatus = "active" | "revoked" | "expired"

export type ApiKey = {
  id: number
  admin_id: number
  admin_username: string
  name: string
  key_prefix: string
  expires_at?: string | null
  revoked_at?: string | null
  revoked_by_admin_id?: number | null
  created_by_admin_id?: number | null
  created_by_username?: string
  last_used_at?: string | null
  last_used_ip?: string
  created_at: string
  updated_at: string
  status: ApiKeyStatus
}

export type ApiKeyWithSecret = ApiKey & {
  key: string
}

export type ApiKeyEvent = {
  id: number
  api_key_id: number | null
  actor_admin_id: number | null
  actor_username?: string
  event_type: "created" | "revoked" | "rotated" | string
  client_ip: string
  metadata?: Record<string, any>
  created_at: string
}

export function getApiKeys() {
  return request<{ ok: boolean; success: boolean; data: ApiKey[] }>("/api/admin/api-keys")
}

export function createApiKey(data: { name: string; expires_at?: string | null }) {
  return request<{ ok: boolean; success: boolean; data: ApiKeyWithSecret; message?: string }>("/api/admin/api-keys", {
    method: "POST",
    body: JSON.stringify(data),
  })
}

export function rotateApiKey(id: number) {
  return request<{ ok: boolean; success: boolean; data: ApiKeyWithSecret; message?: string }>(`/api/admin/api-keys/${id}/rotate`, {
    method: "POST",
  })
}

export function revokeApiKey(id: number, reason?: string) {
  return request<{ ok: boolean; success: boolean; data: ApiKey; message?: string }>(`/api/admin/api-keys/${id}/revoke`, {
    method: "POST",
    body: JSON.stringify({ reason }),
  })
}

export function getApiKeyEvents(id: number) {
  return request<{ ok: boolean; success: boolean; data: ApiKeyEvent[] }>(`/api/admin/api-keys/${id}/events`)
}


export function generateLLMS() {
  return request<{ ok: boolean; path: string }>("/api/admin/publish/llms", { method: "POST" })
}

export type MediaReference = {
  content_id: number
  title: string
  field_name: string
}

export type MediaDetail = MediaAsset & { references: MediaReference[] }

export function getMediaItem(id: number) {
  return request<MediaDetail>(`/api/admin/media/${id}`)
}

export function deleteMedia(id: number) {
  return request<{ ok: boolean }>(`/api/admin/media/${id}`, { method: "DELETE" })
}
