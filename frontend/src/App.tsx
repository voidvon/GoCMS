import { useEffect, useState } from "react"
import { LoaderCircle } from "lucide-react"

import { getSession, type AdminUser } from "@/lib/api"
import { AdminShell } from "@/components/app/admin-shell"
import { LoginPage } from "@/components/app/login-page"
import { ThemeProvider } from "@/components/app/theme-provider"
import { TooltipProvider } from "@/components/ui/tooltip"

function App() {
  const [user, setUser] = useState<AdminUser | null>(null)
  const [checkingSession, setCheckingSession] = useState(true)

  useEffect(() => {
    getSession()
      .then((response) => setUser(response.user))
      .catch(() => setUser(null))
      .finally(() => setCheckingSession(false))
  }, [])

  return (
    <ThemeProvider>
      <TooltipProvider delay={200}>
        {checkingSession ? (
          <main className="flex min-h-svh items-center justify-center bg-muted/30 text-muted-foreground">
            <LoaderCircle className="size-5 animate-spin" />
          </main>
        ) : user ? (
          <AdminShell user={user} onLogout={() => setUser(null)} />
        ) : (
          <LoginPage onSuccess={setUser} />
        )}
      </TooltipProvider>
    </ThemeProvider>
  )
}

export default App
