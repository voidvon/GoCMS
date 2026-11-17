export class APIError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = "APIError"
    this.status = status
  }
}

// API errors may be plain text, { error: string }, or
// { error: { code, message } }. Never stringify an unknown object for the UI.
export function getErrorMessage(reason: unknown, fallback = "操作失败，请稍后重试"): string {
  function read(value: unknown, depth: number): string | undefined {
    if (depth > 4) return undefined
    if (typeof value === "string") {
      const message = value.trim()
      if (/<!doctype\s+html\b|<\s*(?:html|head|body)(?:\s|>)/i.test(message)) return undefined
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
