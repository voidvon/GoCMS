import { useEffect, useState, type ComponentType } from "react"
import {
  BarChart3,
  Boxes,
  ChevronsUpDown,
  FolderTree,
  LayoutDashboard,
  LogOut,
  PanelLeft,
  MessageSquareText,
  FileText,
  Globe,
  KeyRound,
  Languages,
  Palette,
  Paperclip,
} from "lucide-react"

import { logout, type AdminUser } from "@/lib/api"
import { LanguageProvider } from "@/lib/language-context"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuGroup,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet"
import { cn } from "@/lib/utils"
import { useAdminRoute, type AdminView } from "@/lib/admin-router"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { ScrollArea } from "@/components/ui/scroll-area"

import { DashboardPage } from "@/components/app/dashboard-page"
import { CategoriesPage } from "@/components/app/categories-page"
import { ModelsPage } from "@/components/app/models-page"
import { LanguagesPage } from "@/components/app/languages-page"
import { MessagesPage } from "@/components/app/messages-page"
import { ContentPage } from "@/components/app/content-page"
import { ThemePage } from "@/components/app/theme-page"
import { ApiKeysPage } from "@/components/app/api-keys-page"

import { MediaPage } from "@/components/app/media-page"
import { UsersPage } from "@/components/app/users-page"
import { LogsPage } from "@/components/app/logs-page"

import { PublishPage } from "@/components/app/publish-page"
import { SettingsDialog } from "@/components/app/settings-dialog"
import { ThemeToggle } from "@/components/app/theme-provider"

type AdminShellProps = {
  user: AdminUser
  onLogout: () => void
}

type NavigationProps = {
  user: AdminUser
  activeView: AdminView
  onNavigate: (view: AdminView) => void
  onClose?: () => void
  collapsed?: boolean
}

type NavigationItem = {
  id: AdminView
  label: string
  icon: ComponentType<{ className?: string }>
}

const navigationItems: NavigationItem[] = [
  { id: "overview", label: "仪表盘", icon: LayoutDashboard },
  { id: "content", label: "内容", icon: FileText },
  { id: "media", label: "附件", icon: Paperclip },
  { id: "categories", label: "分类", icon: FolderTree },
  { id: "languages", label: "多语言", icon: Languages },
  { id: "messages", label: "信息反馈", icon: MessageSquareText },
  { id: "publish", label: "网站发布", icon: Globe },
  { id: "theme", label: "模板管理", icon: Palette },
  { id: "models", label: "系统模型", icon: Boxes },
  { id: "api-keys", label: "API Key", icon: KeyRound },
  { id: "users", label: "用户与权限", icon: KeyRound },
  { id: "logs", label: "操作日志", icon: FileText },
]

function canView(user: AdminUser, view: AdminView) {
  if (view === "logs") return user.is_super || user.permissions.includes("logs") || user.permissions.includes("login_logs")
  return user.is_super || view === "overview" || user.permissions.includes(view)
}

function Navigation({ activeView, onNavigate, onClose, collapsed = false, user }: NavigationProps) {
  return (
    <nav aria-label="主导航" className="space-y-1">
      {navigationItems.filter((item) => canView(user, item.id)).map((item) => {
        const Icon = item.icon
        const active = activeView === item.id
        const button = (
          <Button
            variant={active ? "secondary" : "ghost"}
            className={cn("w-full justify-start gap-3", active && "font-medium", collapsed && "justify-center px-0")}
            aria-label={item.label}
            aria-current={active ? "page" : undefined}
            onClick={() => {
              onNavigate(item.id)
              onClose?.()
            }}
          >
            <Icon />
            {!collapsed && item.label}
          </Button>
        )
        return collapsed ? (
          <Tooltip key={item.id}>
            <TooltipTrigger render={button} />
            <TooltipContent side="right">{item.label}</TooltipContent>
          </Tooltip>
        ) : <div key={item.id}>{button}</div>
      })}
    </nav>
  )
}

type UserMenuProps = {
  user: AdminUser
  onLogout: () => void
  className?: string
  collapsed?: boolean
}

function UserMenu({ user, onLogout, className, collapsed = false }: UserMenuProps) {
  const initials = user.username.slice(0, 1).toUpperCase()

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="ghost"
            className={cn("min-w-0 justify-start gap-2 px-2", collapsed && "justify-center px-0", className)}
            aria-label={`账号：${user.username}`}
          >
            <Avatar size="sm">
              <AvatarFallback>{initials}</AvatarFallback>
            </Avatar>
            {!collapsed && <>
              <span className="min-w-0 flex-1 truncate text-left text-sm">{user.username}</span>
              <ChevronsUpDown className="size-3.5 shrink-0 text-muted-foreground" />
            </>}
          </Button>
        }
      />
      <DropdownMenuContent side="top" align="start" className="w-48">
        <DropdownMenuGroup>
          <DropdownMenuLabel>
            <p>{user.username}</p>
            <p className="font-normal text-muted-foreground">管理员账号</p>
          </DropdownMenuLabel>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={onLogout} variant="destructive">
          <LogOut />
          退出登录
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

type SidebarProps = Pick<NavigationProps, "activeView" | "onNavigate"> &
  Pick<AdminShellProps, "user" | "onLogout"> & { collapsed: boolean }

function Sidebar({ activeView, onNavigate, user, onLogout, collapsed }: SidebarProps) {
  return (
    <aside id="desktop-sidebar" aria-label="侧边栏" data-state={collapsed ? "collapsed" : "expanded"} className={cn("hidden shrink-0 border-r bg-muted/20 transition-[width] duration-200 motion-reduce:transition-none lg:block", collapsed ? "w-16" : "w-60")}>
      <div className={cn("sticky top-0 flex h-svh flex-col py-4", collapsed ? "px-2" : "px-4")}>
        <div className={cn("flex shrink-0 items-center gap-3 py-2", collapsed ? "justify-center" : "px-2")}>
          <span className="flex size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
            <BarChart3 className="size-4" />
          </span>
          {!collapsed && <div className="whitespace-nowrap">
            <p className="text-sm font-semibold tracking-tight">GoCMS 后台</p>
            <p className="text-xs text-muted-foreground">内容管理</p>
          </div>}
        </div>
        <ScrollArea className="mt-6 min-h-0 flex-1" contentClassName="pb-4">
          <Navigation user={user} activeView={activeView} onNavigate={onNavigate} collapsed={collapsed} />
        </ScrollArea>
        <div className="shrink-0 border-t pt-4">
          <div className={cn("flex items-center gap-1", collapsed && "flex-col")}>
            <UserMenu user={user} onLogout={onLogout} collapsed={collapsed} className={collapsed ? "w-full" : "flex-1"} />
            <ThemeToggle />
            <SettingsDialog />
          </div>
        </div>
      </div>
    </aside>
  )
}

function viewMeta(view: AdminView) {
  switch (view) {
    case "logs":
      return { title: "操作日志", description: "查看后台操作记录" }
    case "users":
      return { title: "用户与权限", description: "管理后台账号、用户组和模块权限" }
    case "media":
      return { title: "附件管理", description: "管理全站上传图片及内容引用" }
    case "publish":
      return { title: "网站发布", description: "生成并发布公开站点" }
    case "theme":
      return { title: "模板管理", description: "管理模板组、页面模板和自定义文件" }
    case "content":
      return { title: "内容", description: "维护内容、分类和公开展示状态" }
    case "categories":
      return { title: "分类", description: "维护统一的栏目树和页面生成规则" }
    case "models":
      return { title: "系统模型", description: "管理数据表、扩展字段与系统内容模型" }
    case "languages":
      return { title: "多语言配置", description: "配置网站多语言、主站与兜底语言回退机制" }
    case "messages":
      return { title: "信息反馈", description: "管理自定义反馈分类、字段及用户提交信息" }
    case "api-keys":
      return { title: "API Key", description: "管理用于外部系统调用接口的 API 凭据" }
    default:
      return { title: "仪表盘", description: "站点内容和运营数据" }
  }
}

function ViewContent({ view, user }: { view: AdminView; user: AdminUser }) {
  if (!canView(user, view)) return <p className="text-sm text-muted-foreground">当前用户组没有此模块的管理权限。</p>
  switch (view) {
    case "logs":
      return <LogsPage user={user} />
    case "users":
      return <UsersPage currentUserID={user.id} />
    case "media":
      return <MediaPage />
    case "publish":
      return <PublishPage />
    case "theme":
      return <ThemePage />
    case "content":
      return <ContentPage user={user} />
    case "categories":
      return <CategoriesPage />
    case "models":
      return <ModelsPage />
    case "languages":
      return <LanguagesPage />
    case "messages":
      return <MessagesPage />
    case "api-keys":
      return <ApiKeysPage />
    default:
      return <DashboardPage />
  }
}

export function AdminShell({ user, onLogout }: AdminShellProps) {
  return (
    <LanguageProvider>
      <AdminShellInner user={user} onLogout={onLogout} />
    </LanguageProvider>
  )
}

function AdminShellInner({ user, onLogout }: AdminShellProps) {
  const { view: activeView, navigate } = useAdminRoute()
  const [mobileOpen, setMobileOpen] = useState(false)
  const [collapsed, setCollapsed] = useState(() => {
    try { return localStorage.getItem("gocms-sidebar-collapsed") === "true" }
    catch { return false }
  })

  useEffect(() => {
    try { localStorage.setItem("gocms-sidebar-collapsed", String(collapsed)) }
    catch { /* Keep the toggle usable when browser storage is unavailable. */ }
  }, [collapsed])

  useEffect(() => {
    const desktop = window.matchMedia("(min-width: 1024px)")
    const onResize = () => { if (desktop.matches) setMobileOpen(false) }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key.toLowerCase() !== "b" || !(event.metaKey || event.ctrlKey) || event.altKey || event.repeat) return
      const target = event.target
      if (target instanceof HTMLElement && target.closest("input, textarea, select, [contenteditable]:not([contenteditable='false'])")) return
      event.preventDefault()
      if (desktop.matches) setCollapsed((value) => !value)
      else setMobileOpen((value) => !value)
    }
    desktop.addEventListener("change", onResize)
    window.addEventListener("keydown", onKeyDown)
    return () => {
      desktop.removeEventListener("change", onResize)
      window.removeEventListener("keydown", onKeyDown)
    }
  }, [])
  const meta = viewMeta(activeView)

  async function handleLogout() {
    await logout().catch(() => undefined)
    onLogout()
  }

  const mobileNavigation = (
    <Navigation
      user={user}
      activeView={activeView}
      onNavigate={navigate}
      onClose={() => setMobileOpen(false)}
    />
  )

  return (
    <div className="flex min-h-svh bg-background lg:h-svh lg:overflow-hidden">
      <Sidebar activeView={activeView} onNavigate={navigate} user={user} onLogout={handleLogout} collapsed={collapsed} />
      <div className="flex min-w-0 flex-1 flex-col lg:min-h-0">
        <header className="sticky top-0 z-20 flex h-14 shrink-0 items-center justify-between border-b bg-background/95 px-4 backdrop-blur sm:px-6">
          <div className="flex items-center gap-3">
            <Button variant="ghost" size="icon" className="hidden lg:inline-flex"
              aria-label={collapsed ? "展开侧边栏" : "收起侧边栏"}
              title={collapsed ? "展开侧边栏" : "收起侧边栏"}
              aria-expanded={!collapsed} aria-controls="desktop-sidebar"
              onClick={() => setCollapsed((value) => !value)}>
              <PanelLeft />
            </Button>
            <Sheet open={mobileOpen} onOpenChange={setMobileOpen}>
              <SheetTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon"
                    className="lg:hidden"
                    aria-label="打开导航"
                    title="打开导航"
                  />
                }
              >
                <PanelLeft />
              </SheetTrigger>
              <SheetContent side="left" className="flex w-72 max-w-[calc(100vw-2rem)] flex-col p-4">
                <SheetHeader className="px-2">
                  <SheetTitle className="flex items-center gap-3">
                    <span className="flex size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
                      <BarChart3 className="size-4" />
                    </span>
                    GoCMS 后台
                  </SheetTitle>
                </SheetHeader>
                <ScrollArea className="mt-6 min-h-0 flex-1" contentClassName="pr-2">
                  {mobileNavigation}
                </ScrollArea>
                <div className="mt-6 border-t pt-4">
                  <div className="flex items-center gap-1">
                    <UserMenu user={user} onLogout={handleLogout} className="flex-1" />
                    <ThemeToggle />
                    <SettingsDialog />
                  </div>
                </div>
              </SheetContent>
            </Sheet>
            <div>
              <h1 className="text-sm font-semibold tracking-tight">{meta.title}</h1>
              <p className="hidden text-xs text-muted-foreground sm:block">{meta.description}</p>
            </div>
          </div>
        </header>
        <main className="min-h-0 flex-1 overflow-auto p-4 sm:p-6 lg:overflow-hidden">
          <div className="mx-auto h-full w-full max-w-screen-2xl min-h-0">
            <ViewContent view={activeView} user={user} />
          </div>
        </main>
      </div>
    </div>
  )
}
