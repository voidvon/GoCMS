import i18n, { type MessageKey } from "@/i18n"

export class LocalizedError extends Error {
  translationKey: MessageKey
  values: Record<string, string | number>
  constructor(key: MessageKey, values: Record<string, string | number> = {}) {
    super(String(i18n.t(key, values)))
    this.translationKey = key
    this.values = values
  }
}
export function localError(key: MessageKey, values?: Record<string, string | number>) { return new LocalizedError(key, values) }
// Keep typed errors until rendering so an existing error follows language changes.
export function getErrorMessage(reason: unknown, fallback = String(i18n.t("errors.generic"))): string {
  function read(value: unknown, depth: number): string | undefined {
    if (depth > 4) return undefined
    if (value instanceof LocalizedError) return String(i18n.t(value.translationKey, value.values))
    if (value instanceof TypeError) return String(i18n.t("errors.network"))
    if (typeof value === "string") {
      const message = value.trim()
      return message && message !== "[object Object]" ? message : undefined
    }
    if (value instanceof Error) return read(value.message, depth + 1)
    if (value && typeof value === "object") {
      const record = value as Record<string, unknown>
      return read(record.error, depth + 1) ?? read(record.message, depth + 1)
    }
    return undefined
  }
  return read(reason, 0) ?? fallback
}
const errorKeys: Record<string, MessageKey> = Object.fromEntries([
  "invalid_reset_token", "reset_unavailable", "microsoft_failed", "microsoft_cancelled", "microsoft_unavailable", "microsoft_conflict", "facebook_failed", "facebook_cancelled", "facebook_unavailable", "facebook_conflict", "google_failed", "google_cancelled", "google_unavailable", "google_conflict", "invalid_input", "not_found", "conflict", "price_changed", "unavailable", "authentication_required", "invalid_credentials", "account_conflict", "session_limit_reached", "rate_limited", "origin_not_allowed", "forbidden", "internal_error", "unsupported_media_type", "method_not_allowed",
].map(code => [code, `errors.${code}`])) as Record<string, MessageKey>
export function apiErrorKey(code: string, status: number): MessageKey {
  return errorKeys[code] || errorKeys[({ 400: "invalid_input", 401: "authentication_required", 403: "forbidden", 404: "not_found", 409: "conflict", 429: "rate_limited" } as Record<number, string>)[status]] || (status >= 500 ? "errors.unavailable" : "errors.request")
}
