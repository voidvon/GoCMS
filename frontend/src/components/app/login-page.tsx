import { useState, type FormEvent } from "react"
import { ArrowRight, LoaderCircle, ShieldCheck } from "lucide-react"

import { login, type AdminUser } from "@/lib/api"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { InlineAlert } from "@/components/app/app-ui"

type LoginPageProps = {
  onSuccess: (user: AdminUser) => void
}

export function LoginPage({ onSuccess }: LoginPageProps) {
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const [error, setError] = useState("")
  const [submitting, setSubmitting] = useState(false)

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError("")
    setSubmitting(true)
    try {
      const response = await login(username, password)
      onSuccess(response.user)
    } catch (submitError) {
      setError(submitError instanceof Error ? submitError.message : "登录失败")
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <main className="flex min-h-svh items-center justify-center bg-muted/30 px-4 py-10">
      <div className="grid w-full max-w-4xl overflow-hidden rounded-xl bg-background shadow-xl ring-1 ring-foreground/10 lg:grid-cols-[1fr_420px]">
        <section className="hidden flex-col justify-between border-r bg-muted/40 p-10 lg:flex">
          <div>
            <div className="mb-8 flex items-center gap-2 text-sm font-medium">
              <span className="flex size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
                <ShieldCheck className="size-4" />
              </span>
              彪维流体设备
            </div>
            <p className="max-w-sm text-3xl font-semibold tracking-tight text-foreground">
              内容管理工作台
            </p>
            <p className="mt-3 max-w-sm text-sm leading-6 text-muted-foreground">
              集中维护内容、栏目和客户留言，数据由 Go 服务和 SQLite 提供。
            </p>
          </div>
          <p className="text-xs text-muted-foreground">UTF-8 · Go + SQLite</p>
        </section>

        <Card className="rounded-none border-0 shadow-none ring-0">
          <CardHeader className="px-8 pt-10 sm:px-10">
            <CardTitle className="text-xl">登录后台</CardTitle>
            <CardDescription>使用现有管理员账号继续</CardDescription>
          </CardHeader>
          <CardContent className="px-8 pb-10 sm:px-10">
            <form className="space-y-5" onSubmit={handleSubmit}>
              <div className="space-y-2">
                <Label htmlFor="username">用户名</Label>
                <Input
                  id="username"
                  autoComplete="username"
                  value={username}
                  onChange={(event) => setUsername(event.target.value)}
                  placeholder="输入用户名"
                  required
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="password">密码</Label>
                <Input
                  id="password"
                  type="password"
                  autoComplete="current-password"
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  placeholder="输入密码"
                  required
                />
              </div>
              {error ? (
                <InlineAlert>{error}</InlineAlert>
              ) : null}
              <Button className="w-full" type="submit" disabled={submitting}>
                {submitting ? (
                  <LoaderCircle className="animate-spin" />
                ) : (
                  <ArrowRight />
                )}
                进入后台
              </Button>
            </form>
          </CardContent>
        </Card>
      </div>
    </main>
  )
}
