import { useCallback, useEffect, useState } from "react"
import { Bell, CheckCheck } from "lucide-react"
import { toast } from "sonner"
import { cn } from "cn"

import { getAdminNotifications, markAdminNotificationsRead, type AdminNotification } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { ScrollArea } from "@/components/ui/scroll-area"
import { TimeText } from "@/components/app/time-text"
import type { AdminView } from "@/lib/admin-router"

type NotificationBellProps = {
  siteId: number
  onNavigate: (view: AdminView) => void
}

// Where a notification of a given kind sends the reader.
const notificationViews: Record<string, AdminView> = {
  messages: "messages",
  content: "content",
  publish: "publish",
  traffic: "traffic",
}

// Background work that fails (a message that could not be pushed to Feishu, for
// example) files a notification here, so it is visible from every page instead
// of hiding inside the affected record.
export function NotificationBell({ siteId, onNavigate }: NotificationBellProps) {
  const [open, setOpen] = useState(false)
  const [items, setItems] = useState<AdminNotification[]>([])
  const [unread, setUnread] = useState(0)
  const [marking, setMarking] = useState(false)

  const load = useCallback(async () => {
    try {
      const page = await getAdminNotifications()
      setItems(page.items)
      setUnread(page.unread)
      return page
    } catch {
      // The bell must never break the shell.
      return null
    }
  }, [])

  useEffect(() => {
    let active = true
    void load().then((page) => {
      if (!active || !page || page.unread === 0) return
      const top = page.items.find((item) => !item.read_at)
      if (!top) return
      // Announce a backlog once per browser session: an unresolved failure
      // should not pop up again on every reload, while a genuinely new entry
      // still does.
      const key = `gocms-notify-announced:${siteId}`
      try {
        if (window.sessionStorage.getItem(key) === String(top.id)) return
        window.sessionStorage.setItem(key, String(top.id))
      } catch {
        // Storage unavailable: announce anyway.
      }
      const announce = page.items.some((item) => !item.read_at && item.level === "error")
        ? toast.error
        : toast.warning
      announce(`有 ${page.unread} 条未读后台通知`, {
        description: `${top.title}${top.occurrences > 1 ? `（${top.occurrences} 次）` : ""}`,
        action: { label: "查看", onClick: () => setOpen(true) },
      })
    })
    return () => {
      active = false
    }
  }, [load, siteId])

  // Keep the badge current while the tab is visible: failures happen in the
  // background and nobody should have to reload to notice them.
  useEffect(() => {
    const timer = window.setInterval(() => {
      if (document.visibilityState === "visible") void load()
    }, 60_000)
    return () => window.clearInterval(timer)
  }, [load])

  async function handleMarkAll() {
    setMarking(true)
    try {
      const result = await markAdminNotificationsRead({ all: true })
      setUnread(result.unread)
      setItems((prev) => prev.map((item) => (item.read_at ? item : { ...item, read_at: new Date().toISOString() })))
    } catch (reason) {
      toast.error(reason instanceof Error ? reason.message : "标记通知失败")
    } finally {
      setMarking(false)
    }
  }

  function handleOpenItem(item: AdminNotification) {
    setOpen(false)
    if (!item.read_at) {
      setUnread((prev) => Math.max(0, prev - 1))
      setItems((prev) => prev.map((entry) => (entry.id === item.id ? { ...entry, read_at: new Date().toISOString() } : entry)))
      void markAdminNotificationsRead({ ids: [item.id] }).catch(() => undefined)
    }
    onNavigate(notificationViews[item.link_view] ?? "overview")
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <Button
            variant="ghost"
            size="icon"
            className="relative"
            aria-label={unread > 0 ? `后台通知（${unread} 条未读）` : "后台通知"}
            title="后台通知"
          />
        }
      >
        <Bell />
        {unread > 0 ? (
          <span className="absolute -top-0.5 -right-0.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-destructive px-1 text-[10px] leading-none font-medium text-white">
            {unread > 99 ? "99+" : unread}
          </span>
        ) : null}
      </PopoverTrigger>
      <PopoverContent align="end" className="w-80 p-0">
        <div className="flex items-center justify-between gap-2 border-b px-3 py-2">
          <span className="text-sm font-medium">后台通知</span>
          {unread > 0 ? (
            <Button
              variant="ghost"
              size="xs"
              className="gap-1 text-muted-foreground"
              disabled={marking}
              onClick={handleMarkAll}
            >
              <CheckCheck className="size-3.5" />
              全部已读
            </Button>
          ) : (
            <span className="text-xs text-muted-foreground">已全部读完</span>
          )}
        </div>
        {items.length === 0 ? (
          <p className="px-3 py-6 text-center text-sm text-muted-foreground">暂无通知</p>
        ) : (
          <ScrollArea className="max-h-80">
            <ul className="divide-y">
              {items.map((item) => (
                <li key={item.id}>
                  <button
                    type="button"
                    onClick={() => handleOpenItem(item)}
                    className={cn(
                      "flex w-full flex-col gap-1 px-3 py-2.5 text-left transition-colors hover:bg-muted/60",
                      !item.read_at && "bg-muted/30",
                    )}
                  >
                    <span className="flex items-center gap-2 text-sm font-medium">
                      {!item.read_at ? (
                        <span
                          className={cn(
                            "size-1.5 shrink-0 rounded-full",
                            item.level === "error" ? "bg-destructive" : "bg-amber-500",
                          )}
                        />
                      ) : null}
                      <span className="min-w-0 flex-1">{item.title}</span>
                      {item.occurrences > 1 ? (
                        <span className="shrink-0 text-xs font-normal text-muted-foreground">
                          ×{item.occurrences}
                        </span>
                      ) : null}
                    </span>
                    {item.detail ? (
                      <span className="line-clamp-2 text-xs text-muted-foreground">{item.detail}</span>
                    ) : null}
                    <span className="text-xs text-muted-foreground">
                      <TimeText value={item.updated_at} />
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          </ScrollArea>
        )}
      </PopoverContent>
    </Popover>
  )
}
