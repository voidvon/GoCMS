import { Info, Settings } from "lucide-react"

import { version } from "../../../package.json"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"

export function SettingsDialog() {
  return (
    <Dialog>
      <DialogTrigger
        render={<Button variant="ghost" size="icon" aria-label="设置" title="设置" />}
      >
        <Settings />
      </DialogTrigger>
      <DialogContent className="max-h-[85svh] gap-0 overflow-y-auto p-0 sm:max-w-2xl">
        <div className="border-b px-5 py-4">
          <DialogTitle>设置</DialogTitle>
          <DialogDescription className="sr-only">查看 GoCMS 设置和版本信息</DialogDescription>
        </div>
        <div className="grid min-h-72 grid-cols-[112px_minmax(0,1fr)] sm:grid-cols-[180px_minmax(0,1fr)]">
          <nav aria-label="设置菜单" className="border-r bg-muted/30 p-2 sm:p-3">
            <Button
              variant="secondary"
              className="h-auto w-full justify-start whitespace-normal py-2 text-left"
              aria-current="page"
              aria-controls="settings-about"
            >
              <Info />
              关于 GoCMS
            </Button>
          </nav>
          <section id="settings-about" aria-labelledby="settings-about-title" className="min-w-0 p-4 sm:p-6">
            <h2 id="settings-about-title" className="text-lg font-semibold">关于 GoCMS</h2>
            <dl className="mt-6 space-y-6 text-sm">
              <div>
                <dt className="text-muted-foreground">当前版本</dt>
                <dd className="mt-1 font-medium">v{version}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">GitHub 仓库</dt>
                <dd className="mt-1">
                  <a
                    href="https://github.com/voidvon/GoCMS"
                    target="_blank"
                    rel="noopener noreferrer"
                    className="break-all underline underline-offset-4 hover:text-muted-foreground"
                  >
                    https://github.com/voidvon/GoCMS
                  </a>
                </dd>
              </div>
            </dl>
          </section>
        </div>
      </DialogContent>
    </Dialog>
  )
}
