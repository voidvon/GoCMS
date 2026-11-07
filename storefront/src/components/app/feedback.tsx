import { ArrowRight, CircleAlert, ShoppingBag } from "lucide-react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Empty as EmptyState, EmptyHeader, EmptyTitle, EmptyDescription, EmptyContent, EmptyMedia } from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import { getErrorMessage } from "@/lib/errors"

export const showError = (error: unknown) => toast.error(getErrorMessage(error))
export const showSuccess = (message: string) => toast.success(message)

export function Loading({ label }: { label?: string }) {
  const { t } = useTranslation()
  return <div className="flex min-h-64 flex-col items-center justify-center gap-4 text-sm text-muted-foreground" role="status">
    <div className="w-full max-w-sm space-y-3" aria-hidden="true"><Skeleton className="h-5 w-2/3" /><Skeleton className="h-24 w-full" /><Skeleton className="h-4 w-1/2" /></div>
    {label ?? t("common.loading")}
  </div>
}
export function Failure({ message, retry }: { message: unknown; retry: () => void }) {
  const { t } = useTranslation()
  return <Card><CardContent className="space-y-4 py-6"><Alert variant="destructive" className="text-start"><CircleAlert aria-hidden="true" /><AlertDescription>{getErrorMessage(message)}</AlertDescription></Alert><Button type="button" variant="outline" onClick={retry}>{t("common.retry")}</Button></CardContent></Card>
}
export function Empty({ title, message, href = "/" }: { title: string; message: string; href?: string }) {
  const { t } = useTranslation()
  return <Card><EmptyState className="min-h-96 py-14 sm:py-20">
    <EmptyHeader><EmptyMedia variant="icon"><ShoppingBag aria-hidden="true" /></EmptyMedia><EmptyTitle>{title}</EmptyTitle><EmptyDescription>{message}</EmptyDescription></EmptyHeader>
    <EmptyContent><Button nativeButton={false} render={<a href={href} />}>{t("common.continueSite")}<ArrowRight className="size-4 rtl:rotate-180" aria-hidden="true" /></Button></EmptyContent>
  </EmptyState></Card>
}
