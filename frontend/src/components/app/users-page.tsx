import { useEffect, useState } from "react"
import {
  getAdminAccounts,
  getAdminGroups,
  saveAdminAccount,
  deleteAdminAccount,
  saveAdminGroup,
  deleteAdminGroup,
  getCategories,
  type AdminAccount,
  type AdminGroup,
  type CategoryItem,
  type Site,
} from "@/lib/api"
import { useSite } from "@/lib/site-context"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { ConfirmDialog, InlineAlert } from "@/components/app/app-ui"
import { cn } from "@/lib/utils"

function renderGroupSites(grp: AdminGroup | undefined, allSites: Site[]) {
  if (!grp) {
    return <span className="text-xs text-muted-foreground">未分配</span>
  }
  if (grp.site_permissions && Object.keys(grp.site_permissions).length > 0) {
    const siteIds = Object.keys(grp.site_permissions).map(Number)
    const matched = siteIds.map((id) => allSites.find((s) => s.id === id)).filter(Boolean) as Site[]
    if (matched.length === 0) {
      return <span className="text-xs text-muted-foreground">{siteIds.length} 个站点</span>
    }
    return (
      <div className="flex flex-wrap gap-1 max-w-xs">
        {matched.map((s) => (
          <Badge key={s.id} variant="outline" className="text-[11px]">
            {s.name}
          </Badge>
        ))}
      </div>
    )
  }
  if (grp.site_ids == null) {
    return <Badge variant="secondary" className="text-[11px]">全部站点</Badge>
  }
  if (grp.site_ids.length === 0) {
    return <span className="text-xs text-destructive font-medium">未分配站点</span>
  }
  const matched = grp.site_ids
    .map((id) => allSites.find((s) => s.id === id))
    .filter(Boolean) as Site[]
  return (
    <div className="flex flex-wrap gap-1 max-w-xs">
      {matched.map((s) => (
        <Badge key={s.id} variant="outline" className="text-[11px]">
          {s.name}
        </Badge>
      ))}
    </div>
  )
}

function renderAccountSites(item: AdminAccount, groups: AdminGroup[], allSites: Site[]) {
  if (item.is_super) {
    return <span className="text-xs text-muted-foreground">全部站点（超管）</span>
  }
  const grp = groups.find((g) => g.id === item.group_id)
  return renderGroupSites(grp, allSites)
}

function renderActiveSitePerms(
  item: AdminAccount,
  groups: AdminGroup[],
  activeSite: Site | undefined,
  allPermissions: { key: string; label: string }[]
) {
  if (item.is_super) {
    return <Badge variant="default" className="text-[11px]">全部权限（超管）</Badge>
  }
  if (!activeSite) {
    return <span className="text-xs text-muted-foreground">-</span>
  }
  const grp = groups.find((g) => g.id === item.group_id)
  if (!grp) {
    return <span className="text-xs text-muted-foreground">未分组</span>
  }
  let perms: string[] = []
  if (grp.site_permissions && grp.site_permissions[activeSite.id]) {
    perms = grp.site_permissions[activeSite.id]
  } else if (grp.site_ids == null || grp.site_ids.includes(activeSite.id)) {
    perms = grp.permissions
  }
  if (perms.length === 0) {
    return <span className="text-xs text-destructive">本站无权限</span>
  }
  const labels = perms.map((key) => allPermissions.find((p) => p.key === key)?.label ?? key)
  if (labels.length <= 2) {
    return <span className="text-xs">{labels.join("、")}</span>
  }
  return (
    <span className="text-xs" title={labels.join("、")}>
      {labels.slice(0, 2).join("、")} 等 {labels.length} 项
    </span>
  )
}

export function UsersPage({ currentUserID }: { currentUserID: number }) {
  const { sites, activeSite } = useSite()
  const [accounts, setAccounts] = useState<AdminAccount[]>([])
  const [categories, setCategories] = useState<CategoryItem[]>([])
  const [groups, setGroups] = useState<AdminGroup[]>([])
  const [permissions, setPermissions] = useState<{ key: string; label: string }[]>([])
  const [account, setAccount] = useState<AdminAccount | null>(null)
  const [group, setGroup] = useState<AdminGroup | null>(null)
  const [deleteAccountTarget, setDeleteAccountTarget] = useState<AdminAccount | null>(null)
  const [deleteGroupTarget, setDeleteGroupTarget] = useState<AdminGroup | null>(null)
  const [groupsDialogOpen, setGroupsDialogOpen] = useState(false)
  const [filterCurrentSite, setFilterCurrentSite] = useState(true)
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const [loaded, setLoaded] = useState(false)

  async function refresh() {
    const [users, groupData, categoryList] = await Promise.all([
      getAdminAccounts(),
      getAdminGroups(),
      getCategories().catch(() => []),
    ])
    setCategories(categoryList)
    setAccounts(users)
    setGroups(groupData.items)
    setPermissions(groupData.permissions)
    setLoaded(true)
  }

  useEffect(() => {
    void refresh().catch((e: Error) => setError(e.message))
  }, [])

  async function run(action: () => Promise<unknown>, reloadSession = false) {
    setBusy(true)
    setError("")
    try {
      await action()
      if (reloadSession) {
        window.location.reload()
        return
      }
      setAccount(null)
      setGroup(null)
      await refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : "操作失败")
    } finally {
      setBusy(false)
    }
  }

  const selectedGroup = groups.find((g) => g.id === account?.group_id)

  const filteredAccounts = accounts.filter((item) => {
    if (!filterCurrentSite || !activeSite) return true
    if (item.is_super) return true
    const grp = groups.find((g) => g.id === item.group_id)
    if (!grp) return false
    if (grp.site_permissions && Object.keys(grp.site_permissions).length > 0) {
      return grp.site_permissions[activeSite.id] !== undefined
    }
    if (grp.site_ids == null) return true
    return grp.site_ids.includes(activeSite.id)
  })

  function toggleSiteInGroup(siteId: number, enabled: boolean) {
    if (!group) return
    const current = { ...(group.site_permissions || {}) }
    if (enabled) {
      current[siteId] = permissions.map((p) => p.key)
    } else {
      delete current[siteId]
    }
    setGroup({
      ...group,
      site_permissions: current,
    })
  }

  function togglePermInGroupSite(siteId: number, permKey: string, checked: boolean) {
    if (!group) return
    const current = { ...(group.site_permissions || {}) }
    const perms = current[siteId] || []
    const next = checked ? [...perms, permKey] : perms.filter((k) => k !== permKey)
    current[siteId] = next
    setGroup({
      ...group,
      site_permissions: current,
    })
  }

  function selectAllPermsForSite(siteId: number) {
    if (!group) return
    const current = { ...(group.site_permissions || {}) }
    current[siteId] = permissions.map((p) => p.key)
    setGroup({
      ...group,
      site_permissions: current,
    })
  }

  function clearAllPermsForSite(siteId: number) {
    if (!group) return
    const current = { ...(group.site_permissions || {}) }
    current[siteId] = []
    setGroup({
      ...group,
      site_permissions: current,
    })
  }

  function handleCreateGroup() {
    setError("")
    const defaultSitePerms: Record<number, string[]> = {}
    if (activeSite) {
      defaultSitePerms[activeSite.id] = permissions.map((p) => p.key)
    } else if (sites.length > 0) {
      defaultSitePerms[sites[0].id] = permissions.map((p) => p.key)
    }
    setGroup({
      id: 0,
      name: "",
      permissions: [],
      site_ids: null,
      site_permissions: defaultSitePerms,
    })
  }

  function handleEditGroup(item: AdminGroup) {
    setError("")
    const sitePerms: Record<number, string[]> = { ...(item.site_permissions || {}) }
    if (Object.keys(sitePerms).length === 0 && item.permissions.length > 0) {
      const targetSites = item.site_ids ? sites.filter((s) => item.site_ids?.includes(s.id)) : sites
      for (const s of targetSites) {
        sitePerms[s.id] = [...item.permissions]
      }
    }
    setGroup({
      ...item,
      site_permissions: sitePerms,
    })
  }

  return (
    <div className="h-full space-y-6 overflow-auto pb-6">
      {error && !account && !group && !groupsDialogOpen && (
        <InlineAlert>{error}</InlineAlert>
      )}

      {!loaded && <p className="text-sm text-muted-foreground">正在读取账号和用户组…</p>}

      {/* 后台账号管理 */}
      <section className="space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-3">
            <h2 className="font-medium text-base">后台账号</h2>
            <Button
              size="sm"
              disabled={busy || !loaded}
              onClick={() => {
                setError("")
                setAccount({
                  id: 0,
                  username: "",
                  password: "",
                  group_id: groups[0]?.id ?? 0,
                  is_super: false,
                  disabled: false,
                  category_ids: null,
                })
              }}
            >
              新增账号
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={busy || !loaded}
              onClick={() => {
                setError("")
                setGroupsDialogOpen(true)
              }}
            >
              用户组管理
            </Button>
          </div>
          {activeSite && (
            <div className="flex items-center gap-2">
              <label className="flex items-center gap-1.5 text-xs text-muted-foreground cursor-pointer select-none">
                <input
                  type="checkbox"
                  checked={filterCurrentSite}
                  onChange={(e) => setFilterCurrentSite(e.target.checked)}
                />
                <span>仅显示当前站点（{activeSite.name}）账号</span>
              </label>
              <span className="text-xs text-muted-foreground">
                ({filteredAccounts.length} / {accounts.length})
              </span>
            </div>
          )}
        </div>
        <p className="text-sm text-muted-foreground">
          普通账号由所属用户组决定其在不同站点下的具体模块权限；栏目范围可单独指定；超级管理员拥有所有站点的最高权限。
        </p>

        <div className="overflow-x-auto rounded-md border">
          <table className="w-full text-left text-sm">
            <thead className="bg-muted/40">
              <tr>
                <th className="p-3">账号</th>
                <th className="p-3">用户组</th>
                <th className="p-3">适用站点</th>
                <th className="p-3">本站模块权限</th>
                <th className="p-3">栏目范围</th>
                <th className="p-3">状态</th>
                <th className="p-3 text-right">操作</th>
              </tr>
            </thead>
            <tbody>
              {filteredAccounts.length === 0 && loaded ? (
                <tr>
                  <td colSpan={7} className="p-6 text-center text-sm text-muted-foreground">
                    {filterCurrentSite && activeSite
                      ? `当前站点（${activeSite.name}）暂无授权账号，可通过上方取消勾选查看全部账号`
                      : "暂无后台账号"}
                  </td>
                </tr>
              ) : (
                filteredAccounts.map((item) => (
                  <tr key={item.id} className="border-t">
                    <td className="p-3">
                      <div className="flex items-center gap-2">
                        <span className="font-medium">{item.username}</span>
                        {item.id === currentUserID && (
                          <Badge variant="secondary" className="text-[10px]">当前账号</Badge>
                        )}
                      </div>
                    </td>
                    <td className="p-3">
                      {item.is_super ? (
                        <Badge variant="default" className="text-xs">超级管理员</Badge>
                      ) : (
                        <span className="text-xs">
                          {groups.find((g) => g.id === item.group_id)?.name ?? "未分组"}
                        </span>
                      )}
                    </td>
                    <td className="p-3">
                      {renderAccountSites(item, groups, sites)}
                    </td>
                    <td className="p-3">
                      {renderActiveSitePerms(item, groups, activeSite, permissions)}
                    </td>
                    <td className="p-3">
                      {item.is_super ? (
                        <span className="text-xs text-muted-foreground">全部栏目</span>
                      ) : item.category_ids == null ? (
                        <span className="text-xs text-muted-foreground">全部栏目</span>
                      ) : item.category_ids.length === 0 ? (
                        <span className="text-xs text-destructive">未授权栏目</span>
                      ) : (
                        <Badge variant="outline" className="text-xs">已指定 {item.category_ids.length} 个栏目</Badge>
                      )}
                    </td>
                    <td className="p-3">
                      {item.disabled ? (
                        <Badge variant="destructive" className="text-xs">已停用</Badge>
                      ) : (
                        <Badge variant="outline" className="text-xs text-emerald-600 dark:text-emerald-400 border-emerald-200 dark:border-emerald-800">
                          启用
                        </Badge>
                      )}
                    </td>
                    <td className="whitespace-nowrap p-3 text-right space-x-2">
                      <Button
                        variant="outline"
                        size="sm"
                        disabled={busy}
                        onClick={() => {
                          setError("")
                          setAccount({
                            ...item,
                            password: "",
                            category_ids: item.category_ids ?? null,
                          })
                        }}
                      >
                        编辑
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        className="text-destructive hover:text-destructive hover:bg-destructive/10"
                        disabled={busy}
                        onClick={() => setDeleteAccountTarget(item)}
                      >
                        删除
                      </Button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </section>

      {/* 用户组管理列表 Dialog */}
      <Dialog
        open={groupsDialogOpen}
        onOpenChange={(open) => {
          if (!open && !busy) {
            setGroupsDialogOpen(false)
            setError("")
          }
        }}
      >
        <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-4xl">
          <DialogHeader>
            <div className="flex flex-wrap items-center justify-between gap-3 pr-6">
              <div>
                <DialogTitle>用户组管理</DialogTitle>
                <DialogDescription className="mt-1">
                  用户组支持针对不同站点分别配置独立的业务模块操作权限。未启用的站点，该组账号将无法访问且不在此站点中出现。
                </DialogDescription>
              </div>
              <Button
                size="sm"
                disabled={busy || !loaded}
                onClick={handleCreateGroup}
              >
                新增用户组
              </Button>
            </div>
          </DialogHeader>

          {error && !group && !deleteGroupTarget && (
            <InlineAlert>{error}</InlineAlert>
          )}

          <div className="overflow-x-auto rounded-md border">
            <table className="w-full text-left text-sm">
              <thead className="bg-muted/40">
                <tr>
                  <th className="p-3 font-medium">用户组名称</th>
                  <th className="p-3 font-medium">适用站点</th>
                  <th className="p-3 font-medium">模块权限</th>
                  <th className="p-3 font-medium text-right">操作</th>
                </tr>
              </thead>
              <tbody>
                {groups.length === 0 ? (
                  <tr>
                    <td colSpan={4} className="p-8 text-center text-sm text-muted-foreground">
                      暂无用户组，可点击右上角“新增用户组”创建
                    </td>
                  </tr>
                ) : (
                  groups.map((item) => {
                    const hasSitePerms = item.site_permissions && Object.keys(item.site_permissions).length > 0
                    const memberCount = accounts.filter((a) => !a.is_super && a.group_id === item.id).length
                    return (
                      <tr key={item.id} className="border-t hover:bg-muted/10 transition-colors">
                        <td className="p-3">
                          <div className="flex items-center gap-2">
                            <span className="font-medium">{item.name}</span>
                            <span className="text-xs text-muted-foreground">
                              ({memberCount} 个账号)
                            </span>
                          </div>
                        </td>
                        <td className="p-3">
                          {renderGroupSites(item, sites)}
                        </td>
                        <td className="p-3">
                          {hasSitePerms ? (
                            <div className="flex flex-wrap gap-1.5 max-w-md">
                              {Object.entries(item.site_permissions || {}).map(([sId, pList]) => {
                                const sObj = sites.find((s) => s.id === Number(sId))
                                return (
                                  <span
                                    key={sId}
                                    className="inline-flex items-center gap-1 rounded bg-muted/60 px-2 py-0.5 text-xs text-muted-foreground"
                                  >
                                    <span className="font-medium text-foreground">{sObj?.name || `站点#${sId}`}</span>:
                                    <span>{pList.length} 项权限</span>
                                  </span>
                                )
                              })}
                            </div>
                          ) : (
                            <span className="text-xs text-muted-foreground">
                              {item.permissions.length > 0
                                ? item.permissions.map((key) => permissions.find((p) => p.key === key)?.label ?? key).join("、")
                                : "未授权业务模块"}
                            </span>
                          )}
                        </td>
                        <td className="whitespace-nowrap p-3 text-right space-x-2">
                          <Button
                            disabled={busy}
                            variant="outline"
                            size="sm"
                            onClick={() => handleEditGroup(item)}
                          >
                            编辑
                          </Button>
                          <Button
                            disabled={busy}
                            variant="ghost"
                            size="sm"
                            className="text-destructive hover:text-destructive hover:bg-destructive/10"
                            onClick={() => setDeleteGroupTarget(item)}
                          >
                            删除
                          </Button>
                        </td>
                      </tr>
                    )
                  })
                )}
              </tbody>
            </table>
          </div>

          <DialogFooter>
            <Button
              variant="outline"
              disabled={busy}
              onClick={() => {
                setGroupsDialogOpen(false)
                setError("")
              }}
            >
              关闭
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 账号编辑 Dialog */}
      <Dialog
        open={account !== null}
        onOpenChange={(open) => {
          if (!open && !busy) {
            setAccount(null)
            setError("")
          }
        }}
      >
        <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-lg">
          {account && (
            <form
              className="space-y-4"
              onSubmit={(e) => {
                e.preventDefault()
                void run(() => saveAdminAccount(account), account.id === currentUserID)
              }}
            >
              <DialogHeader>
                <DialogTitle>{account.id ? "编辑账号" : "新增账号"}</DialogTitle>
                <DialogDescription>
                  {account.id
                    ? "修改账号密码、角色身份及可管理的栏目范围。"
                    : "创建新的管理账号，并为其分配用户组与栏目权限。"}
                </DialogDescription>
              </DialogHeader>

              {error && <InlineAlert>{error}</InlineAlert>}

              <div className="space-y-4 py-2">
                <label className="block space-y-1 text-sm">
                  <span className="font-medium">账号名称</span>
                  <Input
                    required
                    value={account.username}
                    autoComplete="off"
                    placeholder="请输入账号名称"
                    onChange={(e) => setAccount({ ...account, username: e.target.value })}
                  />
                </label>

                <label className="block space-y-1 text-sm">
                  <span className="font-medium">
                    {account.id ? "新密码（留空保留原密码）" : "密码（至少 8 字节）"}
                  </span>
                  <Input
                    type="password"
                    autoComplete="new-password"
                    required={!account.id}
                    placeholder={account.id ? "留空则保持原密码不变" : "至少 8 位密码"}
                    value={account.password ?? ""}
                    onChange={(e) => setAccount({ ...account, password: e.target.value })}
                  />
                </label>

                <div className="rounded-md border p-3 bg-muted/20">
                  <label className="flex items-center gap-2 text-sm font-medium cursor-pointer">
                    <input
                      type="checkbox"
                      checked={account.is_super}
                      onChange={(e) => setAccount({ ...account, is_super: e.target.checked })}
                    />
                    <span>超级管理员</span>
                  </label>
                  <p className="mt-1 text-xs text-muted-foreground pl-6">
                    超级管理员拥有所有站点、栏目与后台模块的最高管理权限，不受用户组及数据范围限制。
                  </p>
                </div>

                {!account.is_super && (
                  <>
                    <div className="space-y-2">
                      <label className="block space-y-1 text-sm">
                        <span className="font-medium">所属用户组</span>
                        <select
                          className="block h-9 w-full rounded-md border bg-background px-3 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
                          required
                          value={account.group_id || ""}
                          onChange={(e) => setAccount({ ...account, group_id: Number(e.target.value) })}
                        >
                          <option value="">请选择用户组</option>
                          {groups.map((g) => (
                            <option key={g.id} value={g.id}>
                              {g.name}
                            </option>
                          ))}
                        </select>
                        {groups.length === 0 && (
                          <div className="flex items-center justify-between text-xs text-destructive">
                            <span>暂无可分配用户组，请先新增用户组。</span>
                            <button
                              type="button"
                              className="text-primary hover:underline ml-2 cursor-pointer"
                              onClick={() => {
                                setAccount(null)
                                setGroupsDialogOpen(true)
                              }}
                            >
                              管理用户组
                            </button>
                          </div>
                        )}
                      </label>

                      {selectedGroup && (
                        <div className="rounded-md border bg-muted/40 p-2.5 text-xs text-muted-foreground space-y-1">
                          <div className="flex items-center gap-1.5 font-medium text-foreground">
                            <span>该组适用站点：</span>
                            {renderGroupSites(selectedGroup, sites)}
                          </div>
                          <p>账号在各站点的模块操作权限由所属用户组分别定义，仅在所选用户组适用的站点中出现并可登录。</p>
                        </div>
                      )}
                    </div>

                    {/* 栏目权限（指定栏目） */}
                    <fieldset className="space-y-2 rounded-md border p-3">
                      <div className="flex items-center justify-between">
                        <legend className="px-1 text-sm font-medium">栏目内容管理范围</legend>
                        <span className="text-xs text-muted-foreground">（可指定栏目）</span>
                      </div>
                      <label className="flex items-center gap-2 text-sm font-medium cursor-pointer">
                        <input
                          type="checkbox"
                          checked={account.category_ids == null}
                          onChange={(e) =>
                            setAccount({
                              ...account,
                              category_ids: e.target.checked ? null : [],
                            })
                          }
                        />
                        <span>全部栏目（包含以后新增的栏目）</span>
                      </label>

                      {account.category_ids != null && (
                        <div className="space-y-2 rounded-md border bg-muted/30 p-2.5">
                          <div className="flex items-center justify-between text-xs">
                            <span className="text-muted-foreground">勾选允许该账号管理内容的栏目：</span>
                            <div className="flex gap-2">
                              <button
                                type="button"
                                className="text-primary hover:underline"
                                onClick={() => setAccount({ ...account, category_ids: categories.map((c) => c.id) })}
                              >
                                全选
                              </button>
                              <button
                                type="button"
                                className="text-muted-foreground hover:underline"
                                onClick={() => setAccount({ ...account, category_ids: [] })}
                              >
                                清空
                              </button>
                            </div>
                          </div>
                          {categories.length === 0 ? (
                            <p className="text-xs text-muted-foreground">暂无可分配栏目。</p>
                          ) : (
                            <div className="grid grid-cols-2 gap-2 max-h-40 overflow-y-auto pt-1">
                              {categories.map((c) => {
                                const checked = account.category_ids?.includes(c.id) ?? false
                                return (
                                  <label
                                    key={c.id}
                                    className="flex items-center gap-2 text-xs cursor-pointer hover:text-foreground"
                                  >
                                    <input
                                      type="checkbox"
                                      checked={checked}
                                      onChange={(e) => {
                                        const next = e.target.checked
                                          ? [...(account.category_ids ?? []), c.id]
                                          : (account.category_ids ?? []).filter((id) => id !== c.id)
                                        setAccount({ ...account, category_ids: next })
                                      }}
                                    />
                                    <span className="truncate">{c.name}</span>
                                  </label>
                                )
                              })}
                            </div>
                          )}
                          {account.category_ids.length === 0 && (
                            <p className="text-xs text-destructive">未勾选任何栏目，该账号将不能管理任何内容。</p>
                          )}
                        </div>
                      )}
                      <p className="text-xs text-muted-foreground">
                        逐个授权，不自动包含下级栏目；未勾选任何栏目时不能管理内容。栏目配置管理由用户组权限独立控制。
                      </p>
                    </fieldset>
                  </>
                )}

                <div className="rounded-md border p-3 bg-muted/20">
                  <label className="flex items-center gap-2 text-sm font-medium cursor-pointer">
                    <input
                      type="checkbox"
                      checked={account.disabled}
                      onChange={(e) => setAccount({ ...account, disabled: e.target.checked })}
                    />
                    <span>停用账号</span>
                  </label>
                  <p className="mt-1 text-xs text-muted-foreground pl-6">
                    停用后该账号将无法登录管理后台或调用 API。
                  </p>
                </div>

                {account.id !== 0 && (
                  <p className="text-xs text-muted-foreground">
                    提示：保存修改后该账号需重新登录，已有的 API Key 将会自动撤销。
                  </p>
                )}
              </div>

              <DialogFooter>
                <Button
                  disabled={busy}
                  type="button"
                  variant="outline"
                  onClick={() => {
                    setAccount(null)
                    setError("")
                  }}
                >
                  取消
                </Button>
                <Button disabled={busy} type="submit">
                  {busy ? "正在保存…" : "保存账号"}
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>

      {/* 用户组编辑 Dialog（方案 A：按站点独立配置模块权限） */}
      <Dialog
        open={group !== null}
        onOpenChange={(open) => {
          if (!open && !busy) {
            setGroup(null)
            setError("")
          }
        }}
      >
        <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
          {group && (
            <form
              className="space-y-4"
              onSubmit={(e) => {
                e.preventDefault()
                void run(() => saveAdminGroup(group))
              }}
            >
              <DialogHeader>
                <DialogTitle>{group.id ? "编辑用户组" : "新增用户组"}</DialogTitle>
                <DialogDescription>
                  针对不同站点分别配置独立的业务模块操作权限。未启用的站点，该组账号将无法访问且不在此站点中出现。
                </DialogDescription>
              </DialogHeader>

              {error && <InlineAlert>{error}</InlineAlert>}

              <div className="space-y-4 py-2">
                <label className="block space-y-1 text-sm">
                  <span className="font-medium">用户组名称</span>
                  <Input
                    required
                    value={group.name}
                    placeholder="例如：主站运营组、分站审核组"
                    onChange={(e) => setGroup({ ...group, name: e.target.value })}
                  />
                </label>

                {/* 快捷批量操作 */}
                <div className="flex flex-wrap items-center justify-between gap-2 rounded-md bg-muted/40 p-2.5 text-xs">
                  <span className="font-medium">站点与模块权限配置（不同站点独立授权）：</span>
                  <div className="flex items-center gap-2">
                    <button
                      type="button"
                      className="text-primary hover:underline"
                      onClick={() => {
                        const all: Record<number, string[]> = {}
                        for (const s of sites) {
                          all[s.id] = permissions.map((p) => p.key)
                        }
                        setGroup({ ...group, site_permissions: all })
                      }}
                    >
                      全部站点全选模块
                    </button>
                    <span className="text-muted-foreground">·</span>
                    <button
                      type="button"
                      className="text-muted-foreground hover:underline"
                      onClick={() => {
                        setGroup({ ...group, site_permissions: {} })
                      }}
                    >
                      清空所有站点
                    </button>
                  </div>
                </div>

                {/* 站点卡片列表 */}
                <div className="space-y-3">
                  {sites.length === 0 ? (
                    <p className="text-xs text-muted-foreground">暂无可用站点。</p>
                  ) : (
                    sites.map((s) => {
                      const isEnabled = group.site_permissions?.[s.id] !== undefined
                      const currentPerms = group.site_permissions?.[s.id] || []
                      return (
                        <div
                          key={s.id}
                          className={cn(
                            "rounded-lg border p-3.5 transition-colors",
                            isEnabled ? "border-border bg-card shadow-xs" : "border-dashed bg-muted/15 opacity-70"
                          )}
                        >
                          <div className="flex flex-wrap items-center justify-between gap-2">
                            <label className="flex items-center gap-2 font-medium text-sm cursor-pointer select-none">
                              <input
                                type="checkbox"
                                checked={isEnabled}
                                onChange={(e) => toggleSiteInGroup(s.id, e.target.checked)}
                              />
                              <span>{s.name}</span>
                              <span className="text-xs text-muted-foreground font-mono">({s.code})</span>
                              {s.is_default && (
                                <Badge variant="secondary" className="text-[10px] px-1.5 py-0 font-normal">
                                  默认站点
                                </Badge>
                              )}
                            </label>
                            {isEnabled && (
                              <div className="flex items-center gap-2 text-xs">
                                <span className="text-muted-foreground">已选 {currentPerms.length} / {permissions.length} 项</span>
                                <span className="text-muted-foreground">·</span>
                                <button
                                  type="button"
                                  className="text-primary hover:underline"
                                  onClick={() => selectAllPermsForSite(s.id)}
                                >
                                  全选
                                </button>
                                <span className="text-muted-foreground">·</span>
                                <button
                                  type="button"
                                  className="text-muted-foreground hover:underline"
                                  onClick={() => clearAllPermsForSite(s.id)}
                                >
                                  清空
                                </button>
                              </div>
                            )}
                          </div>

                          {isEnabled ? (
                            <div className="mt-3 pt-3 border-t grid grid-cols-2 gap-2 sm:grid-cols-3">
                              {permissions.map((p) => {
                                const checked = currentPerms.includes(p.key)
                                return (
                                  <label
                                    key={p.key}
                                    className="flex items-center gap-2 text-xs cursor-pointer hover:text-foreground select-none"
                                  >
                                    <input
                                      type="checkbox"
                                      checked={checked}
                                      onChange={(e) => togglePermInGroupSite(s.id, p.key, e.target.checked)}
                                    />
                                    <span>{p.label}</span>
                                  </label>
                                )
                              })}
                            </div>
                          ) : (
                            <p className="mt-1 text-xs text-muted-foreground pl-6">
                              未启用此站点（属于该组的账号在【{s.name}】中彻底不出现、无任何操作权限）。
                            </p>
                          )}
                        </div>
                      )
                    })
                  )}
                </div>
              </div>

              <DialogFooter>
                <Button
                  disabled={busy}
                  type="button"
                  variant="outline"
                  onClick={() => {
                    setGroup(null)
                    setError("")
                  }}
                >
                  取消
                </Button>
                <Button disabled={busy} type="submit">
                  {busy ? "正在保存…" : "保存用户组"}
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>

      {/* 删除账号确认对话框 */}
      <ConfirmDialog
        open={deleteAccountTarget !== null}
        onOpenChange={(open) => {
          if (!open && !busy) setDeleteAccountTarget(null)
        }}
        title="删除账号"
        description={`确定要删除账号“${deleteAccountTarget?.username}”吗？该账号将无法继续登录。`}
        confirmLabel="确认删除"
        confirmVariant="destructive"
        pending={busy}
        onConfirm={() => {
          if (deleteAccountTarget) {
            const isCurrent = deleteAccountTarget.id === currentUserID
            void run(() => deleteAdminAccount(deleteAccountTarget.id), isCurrent).then(() => {
              setDeleteAccountTarget(null)
            })
          }
        }}
      />

      {/* 删除用户组确认对话框 */}
      <ConfirmDialog
        open={deleteGroupTarget !== null}
        onOpenChange={(open) => {
          if (!open && !busy) setDeleteGroupTarget(null)
        }}
        title="删除用户组"
        description={`确定要删除用户组“${deleteGroupTarget?.name}”吗？关联此用户组的账号将变为未分组状态。`}
        confirmLabel="确认删除"
        confirmVariant="destructive"
        pending={busy}
        onConfirm={() => {
          if (deleteGroupTarget) {
            void run(() => deleteAdminGroup(deleteGroupTarget.id)).then(() => {
              setDeleteGroupTarget(null)
            })
          }
        }}
      />
    </div>
  )
}
