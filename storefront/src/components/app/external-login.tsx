import { useState } from "react"
import { useTranslation } from "react-i18next"
import { Button } from "@/components/ui/button"
import { startProviderLogin } from "@/lib/api"
import { showError } from "./feedback"

export function ExternalLoginButton({ provider = "google", recover = false, next, bind = false, password = "", disabled = false }: { provider?: "google"|"facebook"; recover?: boolean; next: string; bind?: boolean; password?: string; disabled?: boolean }) {
  const { t } = useTranslation()
  const [busy, setBusy] = useState(false)
  async function start() {
    setBusy(true)
    try { const result = await startProviderLogin(provider,next, bind, password,recover); window.location.assign(result.url) }
    catch (error) { showError(error); setBusy(false) }
  }
  return <Button type="button" variant="outline" className="min-h-10 w-full gap-3 bg-white text-[#1f1f1f] hover:bg-gray-50 hover:text-[#1f1f1f]" disabled={disabled || busy} onClick={() => void start()}>
    {provider === "facebook" ? <svg viewBox="0 0 24 24" className="size-5 shrink-0 text-[#0866ff]" aria-hidden="true"><path fill="currentColor" d="M24 12A12 12 0 1 0 10 23.8v-8.3H7v-3.5h3V9.3c0-3 1.8-4.7 4.5-4.7 1.3 0 2.7.2 2.7.2v3h-1.5c-1.5 0-1.9.9-1.9 1.8v2.4H17l-.5 3.5H14v8.3A12 12 0 0 0 24 12Z" /></svg> : <svg viewBox="0 0 48 48" className="size-5 shrink-0" aria-hidden="true"><path fill="#EA4335" d="M24 9.5c3.54 0 6.71 1.22 9.21 3.6l6.85-6.85C35.9 2.38 30.47 0 24 0 14.62 0 6.51 5.38 2.56 13.22l7.98 6.19C12.43 13.72 17.74 9.5 24 9.5Z" /><path fill="#4285F4" d="M46.98 24.55c0-1.57-.15-3.09-.38-4.55H24v9.02h12.94c-.58 2.96-2.26 5.48-4.78 7.18l7.73 6c4.51-4.18 7.09-10.36 7.09-17.65Z" /><path fill="#FBBC05" d="M10.53 28.59A14.4 14.4 0 0 1 9.75 24c0-1.59.27-3.13.79-4.59l-7.98-6.19A23.88 23.88 0 0 0 0 24c0 3.87.93 7.53 2.56 10.78l7.97-6.19Z" /><path fill="#34A853" d="M24 48c6.48 0 11.93-2.13 15.89-5.8l-7.73-6c-2.15 1.45-4.92 2.3-8.16 2.3-6.26 0-11.57-4.22-13.47-9.91l-7.97 6.19C6.51 42.62 14.62 48 24 48Z" /></svg>}
    {recover && !busy ? t("password.verifyWith", { provider: provider === "google" ? "Google" : "Facebook" }) : t(busy ? "common.processing" : bind ? `${provider}.bind` : `${provider}.continue`)}
  </Button>
}
