import { ArrowRight, CircleAlert, LoaderCircle, ShoppingBag } from "lucide-react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { getErrorMessage } from "@/lib/errors"

export const showError = (error: unknown) => toast.error(getErrorMessage(error))
export const showSuccess = (message: string) => toast.success(message)

export function Loading({ label }: { label?: string }) {
  const { t } = useTranslation()
  return <div className="flex min-h-64 flex-col items-center justify-center gap-4 text-sm text-muted-foreground" role="status"><span className="flex size-12 items-center justify-center rounded-2xl border bg-card"><LoaderCircle className="size-5 animate-spin text-primary" aria-hidden="true" /></span>{label ?? t("common.loading")}</div>
}
export function Failure({ message, retry }: { message: unknown; retry: () => void }) {
  const { t } = useTranslation()
  return <div className="panel flex flex-col items-center gap-5 py-12 text-center" role="alert"><span className="flex size-12 items-center justify-center rounded-full bg-destructive/10 text-destructive"><CircleAlert className="size-6" aria-hidden="true" /></span><p className="max-w-lg text-sm leading-7">{getErrorMessage(message)}</p><Button className="action-button" variant="outline" onClick={retry}>{t("common.retry")}</Button></div>
}
export function Empty({ title, message, href = "/" }: { title: string; message: string; href?: string }) {
  const { t } = useTranslation()
  return <div className="panel flex min-h-96 flex-col items-center justify-center py-14 text-center sm:py-20">
    <span className="mb-7 flex size-20 items-center justify-center rounded-3xl border border-primary/10 bg-secondary ring-8 ring-secondary/40"><ShoppingBag className="size-8 text-primary" strokeWidth={1.5} aria-hidden="true" /></span>
    <h2 className="text-2xl">{title}</h2><p className="muted mb-7 mt-3 max-w-md">{message}</p><Button className="action-button" nativeButton={false} render={<a href={href} />}>{t("common.continueSite")}<ArrowRight className="size-4 rtl:rotate-180" aria-hidden="true" /></Button>
  </div>
}
