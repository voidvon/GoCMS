import { toast } from "sonner"
import { getErrorMessage } from "@/lib/errors"

export function showSuccess(message: string) {
  toast.success(message)
}

export function showError(reason: unknown, fallback?: string) {
  toast.error(getErrorMessage(reason, fallback))
}
