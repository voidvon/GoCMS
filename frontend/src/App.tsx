import { useEffect, useState } from "react"
import { LoaderCircle } from "lucide-react"

import { getSession, type AdminUser } from "@/lib/api"
import { APIError } from "@/lib/errors"
import { showError } from "@/components/app/admin-notifications"
import { AdminShell } from "@/components/app/admin-shell"
import { LoginPage } from "@/components/app/login-page"
import { ThemeProvider } from "@/components/app/theme-provider"
import { AdminToaster } from "@/components/app/admin-toaster"
import { TooltipProvider } from "@/components/ui/tooltip"
import { Button } from "@/components/ui/button"

function App() {
  const [user, setUser] = useState<AdminUser | null>(null)
  const [checkingSession, setCheckingSession] = useState(true)
  const [sessionCheckFailed, setSessionCheckFailed] = useState(false)
  const [sessionCheckAttempt, setSessionCheckAttempt] = useState(0)

  useEffect(() => {
    let active = true
    getSession()
      .then((response) => {
        if (active) setUser(response.user)
      })
      .catch((error: unknown) => {
        if (!active) return
        if (error instanceof APIError && error.status === 401) {
          setUser(null)
        } else {
          setSessionCheckFailed(true)
          showError(error, "登录状态检查失败，请重试")
        }
      })
      .finally(() => {
        if (active) setCheckingSession(false)
      })
    return () => { active = false }
  }, [sessionCheckAttempt])

  function retrySessionCheck() {
    setCheckingSession(true)
    setSessionCheckFailed(false)
    setSessionCheckAttempt((attempt) => attempt + 1)
  }

  return (
    <ThemeProvider>
      <TooltipProvider delay={200}>
        {checkingSession ? (
          <main className="flex min-h-svh items-center justify-center bg-muted/30 text-muted-foreground">
            <LoaderCircle className="size-5 animate-spin" />
          </main>
        ) : sessionCheckFailed ? (
          <main className="flex min-h-svh items-center justify-center bg-muted/30 px-4">
            <div className="space-y-4 text-center">
              <p className="font-medium">暂时无法确认登录状态</p>
              <p className="text-sm text-muted-foreground">请检查网络连接，然后重试。</p>
              <Button onClick={retrySessionCheck}>重新检查</Button>
            </div>
          </main>
        ) : user ? (
          <AdminShell user={user} onLogout={() => setUser(null)} />
        ) : (
          <LoginPage onSuccess={setUser} />
        )}
        <AdminToaster />
      </TooltipProvider>
    </ThemeProvider>
  )
}

export default App
