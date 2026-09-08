import { useState, type ComponentType } from "react"
import {
  BarChart3,
  BookOpenText,
  ChevronsUpDown,
  ClipboardList,
  FolderTree,
  LayoutDashboard,
  LogOut,
  Menu,
  Package,
  Globe,
  Settings2,
  UsersRound,
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
import { NewsPage } from "@/components/app/news-page"
import { ProductsPage } from "@/components/app/products-page"

import { PublishPage } from "@/components/app/publish-page"

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
  { id: "overview", label: "总览", icon: LayoutDashboard },
  { id: "products", label: "产品目录", icon: Package },
  { id: "categories", label: "产品分类", icon: FolderTree },
  { id: "news", label: "新闻内容", icon: BookOpenText },
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

function Sidebar({ activeView, onNavigate }: NavigationProps) {
  return (
    <aside className="hidden w-60 shrink-0 border-r bg-muted/20 lg:block">
      <div className="sticky top-0 flex h-svh flex-col p-4">
        <div className="flex items-center gap-3 px-2 py-2">
          <span className="flex size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
            <BarChart3 className="size-4" />
          </span>
          <div>
            <p className="text-sm font-semibold tracking-tight">彪维后台</p>
            <p className="text-xs text-muted-foreground">内容管理</p>
          </div>
        </div>
        <div className="mt-8 px-2 pb-2 text-[11px] font-medium tracking-wide text-muted-foreground">
          工作区
        </div>
        <Navigation activeView={activeView} onNavigate={onNavigate} />
        <div className="mt-auto space-y-1">
          <Button variant="ghost" className="w-full justify-start gap-3" disabled>
            <UsersRound />
            管理员
          </Button>
          <Button variant="ghost" className="w-full justify-start gap-3" disabled>
            <Settings2 />
            设置
          </Button>
        </div>
      </div>
    </aside>
  )
}

function viewMeta(view: AdminView) {
  switch (view) {
 case "publish": return {title:"网站发布",description:"生成并发布公开站点"}
    case "products":
      return { title: "产品目录", description: "维护公开产品、分类和展示状态" }
    case "categories":
      return { title: "产品分类", description: "维护产品分类树和目录层级" }
    case "news":
      return { title: "新闻内容", description: "查看新闻发布记录和首页推荐状态" }
    case "messages":
      return { title: "客户留言", description: "集中处理来自网站的客户咨询" }
    default:
      return { title: "总览", description: "站点内容和运营数据" }
  }
}

function ViewContent({ view }: { view: AdminView }) {
  switch (view) {
 case "publish": return <PublishPage />
    case "products":
      return <ProductsPage />
    case "categories":
      return <CategoriesPage />
    case "news":
      return <NewsPage />
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
  const initials = user.username.slice(0, 1).toUpperCase()

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
      <Sidebar activeView={activeView} onNavigate={navigate} />
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
              <SheetContent side="left" className="w-72 p-4">
                <SheetHeader className="px-2">
                  <SheetTitle className="flex items-center gap-3">
                    <span className="flex size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
                      <BarChart3 className="size-4" />
                    </span>
                    彪维后台
                  </SheetTitle>
                </SheetHeader>
                <div className="mt-6">{mobileNavigation}</div>
              </SheetContent>
            </Sheet>
            <div>
              <h1 className="text-sm font-semibold tracking-tight">{meta.title}</h1>
              <p className="hidden text-xs text-muted-foreground sm:block">{meta.description}</p>
            </div>
          </div>
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button variant="ghost" className="gap-2 px-1.5 sm:px-2">
                  <Avatar size="sm">
                    <AvatarFallback>{initials}</AvatarFallback>
                  </Avatar>
                  <span className="hidden max-w-28 truncate text-sm sm:inline">{user.username}</span>
                  <ChevronsUpDown className="hidden size-3.5 text-muted-foreground sm:block" />
                </Button>
              }
            />
            <DropdownMenuContent align="end" className="w-48">
              <DropdownMenuGroup><DropdownMenuLabel>
                <p>{user.username}</p>
                <p className="font-normal text-muted-foreground">管理员账号</p>
              </DropdownMenuLabel></DropdownMenuGroup>
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={handleLogout} variant="destructive">
                <LogOut />
                退出登录
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
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
