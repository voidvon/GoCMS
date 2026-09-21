import { useEffect, useState } from "react"
import { addMonths, format } from "date-fns"
import { ChevronLeft } from "lucide-react"
import { MemberExpiryPicker } from "@/components/app/member-expiry-picker"
import {
  getSiteUsers,
  setSiteUserSessionLimit,
  revokeSiteUserSessions,
  setSiteUserStatus,
  deleteSiteUser,
  getMemberGroups,
  saveMemberGroup,
  deleteMemberGroup,
  getMemberGroupMembers,
  assignMemberGroup,
  removeMemberGroup,
  type SiteUser,
  type MemberGroup,
  type MemberGroupMember,
} from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog"
import { InlineAlert } from "@/components/app/app-ui"


export function SiteUsersPage() {
  const [items, setItems] = useState<SiteUser[]>([])
  const [total, setTotal] = useState(0)
  const [size, setSize] = useState(30)
  const [page, setPage] = useState(1)
  const [search, setSearch] = useState("")
  const [query, setQuery] = useState("")
  const [version, setVersion] = useState(0)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const [groups, setGroups] = useState<MemberGroup[]>([])
  const [selectedGroup, setSelectedGroup] = useState<number | null>(null)
  const [members, setMembers] = useState<MemberGroupMember[]>([])
  const [groupsDialogOpen, setGroupsDialogOpen] = useState(false)
  const [viewingGroupMembers, setViewingGroupMembers] = useState<MemberGroup | null>(null)
  const [groupForm, setGroupForm] = useState<Partial<MemberGroup> | null>(null)
  const [assignUser, setAssignUser] = useState<number | null>(null)
  const [sessionUser, setSessionUser] = useState<SiteUser | null>(null)
  const [sessionLimit, setSessionLimit] = useState("2")
  const [assignGroup, setAssignGroup] = useState<number | null>(null)
  const [confirmation, setConfirmation] = useState<{ title: string; action: () => Promise<unknown> } | null>(null)
  const [expiryBase, setExpiryBase] = useState(() => new Date())
  const [expires, setExpires] = useState("")

  useEffect(() => {
    let active = true
    getSiteUsers(page, query)
      .then((data) => {
        if (!active) return
        if (page > 1 && data.items.length === 0) {
          setPage(Math.max(1, Math.ceil(data.total / data.page_size)))
          return
        }
        setItems(data.items)
        setTotal(data.total)
        setSize(data.page_size)
      })
      .catch((e: Error) => {
        if (active) setError(e.message)
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [page, query, version])

  useEffect(() => {
    getMemberGroups()
      .then((data) => {
        setGroups(data.items)
        setSelectedGroup((id) => (data.items.some((g) => g.id === id) ? id : data.items[0]?.id ?? null))
      })
      .catch((e: Error) => setError(e.message))
  }, [version])

  useEffect(() => {
    let active = true
    setMembers([])
    if (selectedGroup !== null) {
      getMemberGroupMembers(selectedGroup)
        .then((data) => {
          if (active) setMembers(data.items)
        })
        .catch((e: Error) => {
          if (active) setError(e.message)
        })
    }
    return () => {
      active = false
    }
  }, [selectedGroup, version])

  async function run(action: () => Promise<unknown>) {
    setBusy(true)
    setError("")
    setNotice("")
    try {
      await action()
      setNotice("操作已完成")
      setLoading(true)
      setVersion((value) => value + 1)
    } catch (e) {
      setError(e instanceof Error ? e.message : "操作失败")
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="space-y-4" aria-label="前台用户管理">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <h2 className="font-medium text-base">前台用户</h2>
          <Button
            variant="outline"
            size="sm"
            disabled={busy}
            onClick={() => {
              setError("")
              setViewingGroupMembers(null)
              setGroupsDialogOpen(true)
            }}
          >
            会员组管理
          </Button>
        </div>
      </div>
      <p className="text-sm text-muted-foreground">
        管理网站注册用户。禁用后会撤销登录会话，重新启用后需再次登录。
      </p>

      <form
        className="flex flex-wrap gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          setLoading(true)
          setError("")
          setPage(1)
          setQuery(search.trim())
          setVersion((v) => v + 1)
        }}
      >
        <Input
          className="max-w-sm"
          aria-label="搜索前台用户"
          placeholder="用户名、邮箱或显示名称"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <Button type="submit" disabled={loading || busy}>
          搜索
        </Button>
      </form>

      {error && !groupsDialogOpen && !sessionUser && !assignUser && !groupForm && !confirmation && (
        <InlineAlert>{error}</InlineAlert>
      )}
      {notice && <p role="status" className="text-sm text-emerald-600 dark:text-emerald-400">{notice}</p>}

      {loading ? (
        <p role="status" className="text-sm text-muted-foreground">正在读取用户…</p>
      ) : (
        <div className="overflow-x-auto rounded-md border">
          <table className="w-full min-w-[800px] text-left text-sm">
            <thead className="bg-muted/40">
              <tr>
                {["用户名", "显示名称", "邮箱", "状态", "注册时间", "最后登录", "操作"].map((label) => (
                  <th key={label} className="p-3 font-medium">{label}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {items.map((item) => (
                <tr key={item.id} className="border-t hover:bg-muted/10 transition-colors">
                  <td className="p-3 font-medium">{item.username}</td>
                  <td className="p-3">{item.display_name}</td>
                  <td className="p-3 text-muted-foreground">{item.email || "—"}</td>
                  <td className="p-3">
                    {item.status === "active" ? (
                      <Badge variant="outline" className="text-xs text-emerald-600 dark:text-emerald-400 border-emerald-200 dark:border-emerald-800">
                        启用
                      </Badge>
                    ) : item.status === "pending" ? (
                      <Badge variant="secondary" className="text-xs">
                        待审核
                      </Badge>
                    ) : (
                      <Badge variant="destructive" className="text-xs">
                        已禁用
                      </Badge>
                    )}
                  </td>
                  <td className="whitespace-nowrap p-3 text-xs text-muted-foreground">{item.created_at}</td>
                  <td className="whitespace-nowrap p-3 text-xs text-muted-foreground">{item.last_login_at || "尚未登录"}</td>
                  <td className="space-x-2 whitespace-nowrap p-3 text-right">
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={busy}
                      onClick={() =>
                        void run(() =>
                          setSiteUserStatus(item.id, item.status === "active" ? "disabled" : "active")
                        )
                      }
                    >
                      {item.status === "active" ? "禁用" : item.status === "pending" ? "审核通过" : "启用"}
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      className="text-destructive hover:text-destructive hover:bg-destructive/10"
                      disabled={busy}
                      onClick={() => {
                        setError("")
                        setConfirmation({
                          title: `永久删除前台用户“${item.username}”？该操作无法撤销。`,
                          action: () => deleteSiteUser(item.id),
                        })
                      }}
                    >
                      删除
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      disabled={busy}
                      onClick={() => {
                        setError("")
                        setSessionUser(item)
                        setSessionLimit(String(item.max_sessions))
                      }}
                    >
                      登录会话（上限 {item.max_sessions}）
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={busy}
                      onClick={() => {
                        setError("")
                        setAssignUser(item.id)
                        setAssignGroup(groups.find((g) => g.status === "active")?.id ?? null)
                        const now = new Date()
                        setExpiryBase(now)
                        setExpires(format(addMonths(now, 12), "yyyy-MM-dd'T'HH:mm"))
                      }}
                    >
                      设置VIP
                    </Button>
                  </td>
                </tr>
              ))}
              {items.length === 0 && (
                <tr>
                  <td colSpan={7} className="p-6 text-center text-sm text-muted-foreground">
                    {query ? "没有匹配的用户" : "暂无前台用户"}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      )}

      <div className="flex items-center gap-3 text-sm">
        <span className="text-muted-foreground">
          共 {total} 位用户 · 第 {page} / {Math.max(1, Math.ceil(total / size))} 页
        </span>
        <Button
          variant="outline"
          size="sm"
          disabled={loading || busy || page <= 1}
          onClick={() => {
            setLoading(true)
            setError("")
            setPage(page - 1)
          }}
        >
          上一页
        </Button>
        <Button
          variant="outline"
          size="sm"
          disabled={loading || busy || page * size >= total}
          onClick={() => {
            setLoading(true)
            setError("")
            setPage(page + 1)
          }}
        >
          下一页
        </Button>
      </div>

      {/* 会员组管理 Dialog */}
      <Dialog
        open={groupsDialogOpen}
        onOpenChange={(open) => {
          if (!open && !busy) {
            setGroupsDialogOpen(false)
            setViewingGroupMembers(null)
            setError("")
          }
        }}
      >
        <DialogContent showCloseButton={!busy} className="max-h-[90vh] overflow-y-auto sm:max-w-4xl">
          {viewingGroupMembers ? (
            <div className="space-y-4">
              <DialogHeader>
                <div className="flex flex-wrap items-center justify-between gap-3 pr-6">
                  <div className="flex items-center gap-2">
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => setViewingGroupMembers(null)}
                    >
                      <ChevronLeft className="size-4 mr-1" />
                      返回会员组列表
                    </Button>
                    <div>
                      <DialogTitle>{viewingGroupMembers.name} · 成员列表</DialogTitle>
                      <DialogDescription className="mt-0.5">
                        标识：<code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">{viewingGroupMembers.slug}</code> · 供客户端校验 VIP 权限
                      </DialogDescription>
                    </div>
                  </div>
                </div>
              </DialogHeader>

              {error && <InlineAlert>{error}</InlineAlert>}

              <div className="overflow-x-auto rounded-md border">
                <table className="w-full text-left text-sm">
                  <thead className="bg-muted/40">
                    <tr>
                      <th className="p-3 font-medium">用户名</th>
                      <th className="p-3 font-medium">邮箱</th>
                      <th className="p-3 font-medium">显示名称</th>
                      <th className="p-3 font-medium">有效期</th>
                      <th className="p-3 font-medium text-right">操作</th>
                    </tr>
                  </thead>
                  <tbody>
                    {members.length === 0 ? (
                      <tr>
                        <td colSpan={5} className="p-8 text-center text-sm text-muted-foreground">
                          该会员组暂无成员，可在前台用户列表中点击“设置VIP”添加成员
                        </td>
                      </tr>
                    ) : (
                      members.map((m) => (
                        <tr key={m.id} className="border-t hover:bg-muted/10 transition-colors">
                          <td className="p-3 font-medium">{m.username}</td>
                          <td className="p-3 text-muted-foreground">{m.email || "—"}</td>
                          <td className="p-3">{m.display_name || "—"}</td>
                          <td className="p-3 whitespace-nowrap">
                            {m.expires_at ? (
                              <span className="text-xs">到期：{m.expires_at}</span>
                            ) : (
                              <Badge variant="secondary" className="text-xs">永久有效</Badge>
                            )}
                          </td>
                          <td className="whitespace-nowrap p-3 text-right space-x-2">
                            <Button
                              size="sm"
                              variant="outline"
                              disabled={busy}
                              onClick={() => {
                                setError("")
                                setAssignUser(m.id)
                                setAssignGroup(viewingGroupMembers.id)
                                setExpiryBase(new Date())
                                const date = m.expires_at ? new Date(m.expires_at) : null
                                setExpires(
                                  date && !Number.isNaN(date.getTime())
                                    ? new Date(date.getTime() - date.getTimezoneOffset() * 60000)
                                        .toISOString()
                                        .slice(0, 16)
                                    : ""
                                )
                              }}
                            >
                              修改有效期 / 续期
                            </Button>
                            <Button
                              size="sm"
                              variant="ghost"
                              className="text-destructive hover:text-destructive hover:bg-destructive/10"
                              disabled={busy}
                              onClick={() => {
                                setError("")
                                setConfirmation({
                                  title: `移除“${m.username}”的【${viewingGroupMembers.name}】会员组资格？`,
                                  action: () => removeMemberGroup(m.id, viewingGroupMembers.id),
                                })
                              }}
                            >
                              移除
                            </Button>
                          </td>
                        </tr>
                      ))
                    )}
                  </tbody>
                </table>
              </div>

              <DialogFooter>
                <Button variant="outline" onClick={() => setViewingGroupMembers(null)}>
                  返回会员组列表
                </Button>
              </DialogFooter>
            </div>
          ) : (
            <div className="space-y-4">
              <DialogHeader>
                <div className="flex flex-wrap items-center justify-between gap-3 pr-6">
                  <div>
                    <DialogTitle>会员组管理</DialogTitle>
                    <DialogDescription className="mt-1">
                      VIP 通过会员组授予。会员组标识需与接入客户端配置一致。
                    </DialogDescription>
                  </div>
                  <Button
                    size="sm"
                    disabled={busy}
                    onClick={() => {
                      setError("")
                      setGroupForm({
                        name: "",
                        slug: "",
                        description: "",
                        sort_order: 0,
                        status: "active",
                      })
                    }}
                  >
                    新增会员组
                  </Button>
                </div>
              </DialogHeader>

              {error && !groupForm && !confirmation && <InlineAlert>{error}</InlineAlert>}

              <div className="overflow-x-auto rounded-md border">
                <table className="w-full text-left text-sm">
                  <thead className="bg-muted/40">
                    <tr>
                      <th className="p-3 font-medium">会员组名称</th>
                      <th className="p-3 font-medium">标识</th>
                      <th className="p-3 font-medium">说明</th>
                      <th className="p-3 font-medium">排序</th>
                      <th className="p-3 font-medium">状态</th>
                      <th className="p-3 font-medium text-right">操作</th>
                    </tr>
                  </thead>
                  <tbody>
                    {groups.length === 0 ? (
                      <tr>
                        <td colSpan={6} className="p-8 text-center text-sm text-muted-foreground">
                          暂无会员组，可点击右上角“新增会员组”创建
                        </td>
                      </tr>
                    ) : (
                      groups.map((item) => (
                        <tr key={item.id} className="border-t hover:bg-muted/10 transition-colors">
                          <td className="p-3 font-medium">{item.name}</td>
                          <td className="p-3">
                            <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">{item.slug}</code>
                          </td>
                          <td className="p-3 text-xs text-muted-foreground max-w-xs truncate">
                            {item.description || "—"}
                          </td>
                          <td className="p-3 text-xs tabular-nums">{item.sort_order}</td>
                          <td className="p-3">
                            {item.status === "active" ? (
                              <Badge variant="outline" className="text-xs text-emerald-600 dark:text-emerald-400 border-emerald-200 dark:border-emerald-800">
                                启用
                              </Badge>
                            ) : (
                              <Badge variant="destructive" className="text-xs">
                                已停用
                              </Badge>
                            )}
                          </td>
                          <td className="whitespace-nowrap p-3 text-right space-x-2">
                            <Button
                              variant="outline"
                              size="sm"
                              disabled={busy}
                              onClick={() => {
                                setSelectedGroup(item.id)
                                setViewingGroupMembers(item)
                              }}
                            >
                              成员
                            </Button>
                            <Button
                              variant="outline"
                              size="sm"
                              disabled={busy}
                              onClick={() => {
                                setError("")
                                setGroupForm({ ...item })
                              }}
                            >
                              编辑
                            </Button>
                            <Button
                              variant="ghost"
                              size="sm"
                              className="text-destructive hover:text-destructive hover:bg-destructive/10"
                              disabled={busy}
                              onClick={() => {
                                setError("")
                                setConfirmation({
                                  title: `删除会员组“${item.name}”及全部会员关联？该操作无法撤销。`,
                                  action: async () => {
                                    await deleteMemberGroup(item.id)
                                    if (selectedGroup === item.id) setSelectedGroup(null)
                                  },
                                })
                              }}
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

              <DialogFooter>
                <Button
                  variant="outline"
                  onClick={() => {
                    setGroupsDialogOpen(false)
                    setError("")
                  }}
                >
                  关闭
                </Button>
              </DialogFooter>
            </div>
          )}
        </DialogContent>
      </Dialog>

      {/* 登录会话设置 Dialog */}
      <Dialog
        open={sessionUser !== null}
        onOpenChange={(open) => {
          if (!open && !busy) setSessionUser(null)
        }}
      >
        <DialogContent showCloseButton={!busy} className="max-h-[85vh] overflow-y-auto sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>登录会话设置</DialogTitle>
            <DialogDescription>调整登录数量或撤销用户的登录会话。</DialogDescription>
          </DialogHeader>
          {sessionUser && (
            <form
              className="space-y-3"
              onSubmit={(e) => {
                e.preventDefault()
                void run(async () => {
                  await setSiteUserSessionLimit(sessionUser.id, Number(sessionLimit))
                  setSessionUser(null)
                })
              }}
            >
              <h3 className="font-medium">{sessionUser.username} 的登录会话</h3>
              <label className="block space-y-1 text-sm">
                <span>最大登录会话数（1–100）</span>
                <Input
                  className="max-w-xs"
                  type="number"
                  min={1}
                  max={100}
                  step={1}
                  required
                  value={sessionLimit}
                  onChange={(e) => setSessionLimit(e.target.value)}
                />
              </label>
              <p className="text-sm text-muted-foreground">
                同一有效 Cookie 重复登录不增加数量。降低上限不会自动退出旧会话；全部撤销后，用户需重新登录。
              </p>
              <div className="flex flex-wrap gap-2">
                <Button type="submit" disabled={busy}>
                  保存上限
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  disabled={busy}
                  onClick={() => {
                    setError("")
                    setConfirmation({
                      title: `撤销“${sessionUser.username}”的全部登录会话？用户需要重新登录。`,
                      action: () => revokeSiteUserSessions(sessionUser.id),
                    })
                  }}
                >
                  撤销全部会话
                </Button>
                <Button type="button" variant="ghost" disabled={busy} onClick={() => setSessionUser(null)}>
                  取消
                </Button>
              </div>
            </form>
          )}
          {error && <InlineAlert>{error}</InlineAlert>}
        </DialogContent>
      </Dialog>

      {/* 设置VIP Dialog */}
      <Dialog
        open={assignUser !== null}
        onOpenChange={(open) => {
          if (!open && !busy) setAssignUser(null)
        }}
      >
        <DialogContent showCloseButton={!busy} className="max-h-[85vh] overflow-y-auto sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>设置VIP</DialogTitle>
            <DialogDescription>
              为用户分配会员组。再次分配同一组会更新有效期，其他会员组不受影响。
            </DialogDescription>
          </DialogHeader>
          {groups.length === 0 ? (
            <div className="space-y-3">
              <p className="text-sm text-muted-foreground">尚未创建会员组，请先新增会员组，再设置 VIP。</p>
              <Button
                onClick={() => {
                  setAssignUser(null)
                  setGroupsDialogOpen(true)
                  setGroupForm({
                    name: "",
                    slug: "",
                    description: "",
                    sort_order: 0,
                    status: "active",
                  })
                }}
              >
                新增会员组
              </Button>
            </div>
          ) : (
            <form
              className="space-y-4"
              onSubmit={(e) => {
                e.preventDefault()
                if (assignUser !== null && assignGroup !== null)
                  void run(async () => {
                    await assignMemberGroup(
                      assignUser,
                      assignGroup,
                      expires ? new Date(expires).toISOString() : ""
                    )
                    setAssignUser(null)
                  })
              }}
            >
              <label className="block space-y-1 text-sm">
                <span>会员组</span>
                <select
                  required
                  disabled={busy}
                  className="block h-9 w-full rounded-md border bg-background px-3 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
                  value={assignGroup ?? ""}
                  onChange={(e) => setAssignGroup(Number(e.target.value))}
                >
                  <option value="" disabled>
                    请选择会员组
                  </option>
                  {groups.map((g) => (
                    <option key={g.id} value={g.id} disabled={g.status !== "active"}>
                      {g.name}（{g.slug}）{g.status !== "active" ? " · 已停用" : ""}
                    </option>
                  ))}
                </select>
              </label>
              {assignUser !== null && (
                <MemberExpiryPicker
                  key={assignUser}
                  base={expiryBase}
                  value={expires}
                  onChange={setExpires}
                  disabled={busy}
                />
              )}
              <p className="text-sm text-muted-foreground">选择过去的时间会使会员资格立即过期。</p>
              <div className="flex justify-end gap-2">
                <Button type="button" variant="outline" disabled={busy} onClick={() => setAssignUser(null)}>
                  取消
                </Button>
                <Button
                  type="submit"
                  disabled={busy || !groups.some((g) => g.id === assignGroup && g.status === "active")}
                >
                  保存会员资格
                </Button>
              </div>
            </form>
          )}
          {error && <InlineAlert>{error}</InlineAlert>}
        </DialogContent>
      </Dialog>

      {/* 会员组编辑/新建 Dialog */}
      <Dialog
        open={groupForm !== null}
        onOpenChange={(open) => {
          if (!open && !busy) setGroupForm(null)
        }}
      >
        <DialogContent showCloseButton={!busy} className="max-h-[85vh] overflow-y-auto sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{groupForm?.id ? "编辑会员组" : "新增会员组"}</DialogTitle>
            <DialogDescription>
              标识供客户端校验会员权限。修改标识或停用会员组会影响已有成员权限。
            </DialogDescription>
          </DialogHeader>
          {error && <InlineAlert>{error}</InlineAlert>}
          {groupForm && (
            <form
              className="space-y-4"
              onSubmit={(e) => {
                e.preventDefault()
                void run(async () => {
                  await saveMemberGroup(groupForm)
                  setGroupForm(null)
                })
              }}
            >
              <label className="block space-y-1 text-sm">
                <span className="font-medium">名称</span>
                <Input
                  required
                  disabled={busy}
                  placeholder="例如：VIP会员、高级会员"
                  value={groupForm.name ?? ""}
                  onChange={(e) => setGroupForm({ ...groupForm, name: e.target.value })}
                />
              </label>
              <label className="block space-y-1 text-sm">
                <span className="font-medium">标识</span>
                <Input
                  required
                  disabled={busy}
                  placeholder="例如 vip，需与客户端配置一致"
                  value={groupForm.slug ?? ""}
                  onChange={(e) => setGroupForm({ ...groupForm, slug: e.target.value })}
                />
              </label>
              <label className="block space-y-1 text-sm">
                <span className="font-medium">说明</span>
                <Input
                  disabled={busy}
                  placeholder="可选描述说明"
                  value={groupForm.description ?? ""}
                  onChange={(e) => setGroupForm({ ...groupForm, description: e.target.value })}
                />
              </label>
              <label className="block space-y-1 text-sm">
                <span className="font-medium">排序</span>
                <Input
                  required
                  type="number"
                  step={1}
                  disabled={busy}
                  value={groupForm.sort_order ?? 0}
                  onChange={(e) => setGroupForm({ ...groupForm, sort_order: Number(e.target.value) })}
                />
              </label>
              <label className="block space-y-1 text-sm">
                <span className="font-medium">状态</span>
                <select
                  disabled={busy}
                  className="block h-9 w-full rounded-md border bg-background px-3 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
                  value={groupForm.status}
                  onChange={(e) =>
                    setGroupForm({
                      ...groupForm,
                      status: e.target.value === "disabled" ? "disabled" : "active",
                    })
                  }
                >
                  <option value="active">启用</option>
                  <option value="disabled">停用</option>
                </select>
              </label>
              <DialogFooter className="pt-2">
                {groupForm.id && (
                  <Button
                    type="button"
                    variant="destructive"
                    disabled={busy}
                    onClick={() => {
                      setError("")
                      setConfirmation({
                        title: `删除会员组“${groupForm.name}”及全部会员关联？该操作无法撤销。`,
                        action: async () => {
                          await deleteMemberGroup(groupForm.id!)
                          if (selectedGroup === groupForm.id) setSelectedGroup(null)
                          setGroupForm(null)
                        },
                      })
                    }}
                  >
                    删除
                  </Button>
                )}
                <Button type="button" variant="outline" disabled={busy} onClick={() => setGroupForm(null)}>
                  取消
                </Button>
                <Button type="submit" disabled={busy}>
                  保存
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>

      {/* 确认操作 Dialog */}
      <Dialog
        open={confirmation !== null}
        onOpenChange={(open) => {
          if (!open && !busy) setConfirmation(null)
        }}
      >
        <DialogContent showCloseButton={!busy} className="max-w-sm">
          <DialogHeader>
            <DialogTitle>确认操作</DialogTitle>
            <DialogDescription>{confirmation?.title}</DialogDescription>
          </DialogHeader>
          {error && <InlineAlert>{error}</InlineAlert>}
          <DialogFooter>
            <Button variant="outline" disabled={busy} onClick={() => setConfirmation(null)}>
              取消
            </Button>
            <Button
              variant="destructive"
              disabled={busy}
              onClick={() => {
                if (confirmation)
                  void run(async () => {
                    await confirmation.action()
                    setConfirmation(null)
                  })
              }}
            >
              确认
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  )
}
