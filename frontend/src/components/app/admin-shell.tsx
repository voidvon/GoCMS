import { useState, type ComponentType } from "react"
import {
  BarChart3,
  ChevronsUpDown,
  ClipboardList,
  FolderTree,
  LayoutDashboard,
  LogOut,
  Menu,
  FileText,
  Globe,
  Palette,
} from "lucide-react"

import { logout, type AdminUser } from "@/lib/api"
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

import { DashboardPage } from "@/components/app/dashboard-page"
import { CategoriesPage } from "@/components/app/categories-page"
import { MessagesPage } from "@/components/app/messages-page"
import { ContentPage } from "@/components/app/content-page"
import { ThemePage } from "@/components/app/theme-page"

import { PublishPage } from "@/components/app/publish-page"
import { SettingsDialog } from "@/components/app/settings-dialog"

type AdminShellProps = {
  user: AdminUser
  onLogout: () => void
}

type NavigationProps = {
  activeView: AdminView
  onNavigate: (view: AdminView) => void
  onClose?: () => void
}

type NavigationItem = {
  id: AdminView
  label: string
  icon: ComponentType<{ className?: string }>
}

const navigationItems: NavigationItem[] = [
  { id: "publish", label: "网站发布", icon: Globe },
  { id: "theme", label: "主题模板", icon: Palette },
  { id: "overview", label: "总览", icon: LayoutDashboard },
  { id: "content", label: "内容", icon: FileText },
  { id: "categories", label: "分类", icon: FolderTree },
  { id: "messages", label: "客户留言", icon: ClipboardList },
]

function Navigation({ activeView, onNavigate, onClose }: NavigationProps) {
  return (
    <nav className="space-y-1">
      {navigationItems.map((item) => {
        const Icon = item.icon
        const active = activeView === item.id
        return (
          <Button
            key={item.id}
            variant={active ? "secondary" : "ghost"}
            className={cn("w-full justify-start gap-3", active && "font-medium")}
            aria-current={active ? "page" : undefined}
            onClick={() => {
              onNavigate(item.id)
              onClose?.()
            }}
          >
            <Icon />
            {item.label}
          </Button>
        )
      })}
    </nav>
  )
}

type UserMenuProps = {
  user: AdminUser
  onLogout: () => void
  className?: string
}

function UserMenu({ user, onLogout, className }: UserMenuProps) {
  const initials = user.username.slice(0, 1).toUpperCase()

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="ghost"
            className={cn("min-w-0 justify-start gap-2 px-2", className)}
          >
            <Avatar size="sm">
              <AvatarFallback>{initials}</AvatarFallback>
            </Avatar>
            <span className="min-w-0 flex-1 truncate text-left text-sm">{user.username}</span>
            <ChevronsUpDown className="size-3.5 shrink-0 text-muted-foreground" />
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
  Pick<AdminShellProps, "user" | "onLogout">

function Sidebar({ activeView, onNavigate, user, onLogout }: SidebarProps) {
  return (
    <aside className="hidden w-60 shrink-0 border-r bg-muted/20 lg:block">
      <div className="sticky top-0 flex h-svh flex-col p-4">
        <div className="flex items-center gap-3 px-2 py-2">
          <span className="flex size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
            <BarChart3 className="size-4" />
          </span>
          <div>
            <p className="text-sm font-semibold tracking-tight">GoCMS 后台</p>
            <p className="text-xs text-muted-foreground">内容管理</p>
          </div>
        </div>
        <div className="mt-8 px-2 pb-2 text-[11px] font-medium tracking-wide text-muted-foreground">
          工作区
        </div>
        <Navigation activeView={activeView} onNavigate={onNavigate} />
        <div className="mt-auto border-t pt-4">
          <div className="flex items-center gap-1">
            <UserMenu user={user} onLogout={onLogout} className="flex-1" />
            <SettingsDialog />
          </div>
        </div>
      </div>
    </aside>
  )
}

function viewMeta(view: AdminView) {
  switch (view) {
 case "publish": return {title:"网站发布",description:"生成并发布公开站点"}
    case "theme":
      return { title: "主题模板", description: "管理当前主题的样式和页面模板" }
    case "content":
      return { title: "内容", description: "维护内容、分类和公开展示状态" }
    case "categories":
      return { title: "分类", description: "维护统一的栏目树和页面生成规则" }
    case "messages":
      return { title: "客户留言", description: "集中处理来自网站的客户咨询" }
    default:
      return { title: "总览", description: "站点内容和运营数据" }
  }
}

function ViewContent({ view }: { view: AdminView }) {
  switch (view) {
 case "publish": return <PublishPage />
    case "theme": return <ThemePage />
    case "content":
      return <ContentPage />
    case "categories":
      return <CategoriesPage />
    case "messages":
      return <MessagesPage />
    default:
      return <DashboardPage />
  }
}

export function AdminShell({ user, onLogout }: AdminShellProps) {
  const { view: activeView, navigate } = useAdminRoute()
  const [mobileOpen, setMobileOpen] = useState(false)
  const meta = viewMeta(activeView)

  async function handleLogout() {
    await logout().catch(() => undefined)
    onLogout()
  }

  const mobileNavigation = (
    <Navigation
      activeView={activeView}
      onNavigate={navigate}
      onClose={() => setMobileOpen(false)}
    />
  )

  return (
    <div className="flex min-h-svh bg-background">
      <Sidebar activeView={activeView} onNavigate={navigate} user={user} onLogout={handleLogout} />
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="sticky top-0 z-20 flex h-14 items-center justify-between border-b bg-background/95 px-4 backdrop-blur sm:px-6">
          <div className="flex items-center gap-3">
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
                <Menu />
              </SheetTrigger>
              <SheetContent side="left" className="flex w-72 flex-col p-4">
                <SheetHeader className="px-2">
                  <SheetTitle className="flex items-center gap-3">
                    <span className="flex size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
                      <BarChart3 className="size-4" />
                    </span>
                    GoCMS 后台
                  </SheetTitle>
                </SheetHeader>
                <div className="mt-6 flex-1 overflow-y-auto">{mobileNavigation}</div>
                <div className="mt-6 border-t pt-4">
                  <div className="flex items-center gap-1">
                    <UserMenu user={user} onLogout={handleLogout} className="flex-1" />
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
        <main className="flex-1 p-4 sm:p-6">
          <div className="mx-auto w-full max-w-screen-2xl">
            <ViewContent view={activeView} />
          </div>
        </main>
      </div>
    </div>
  )
}
