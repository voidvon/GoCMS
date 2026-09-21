import { useEffect, useState } from "react"
import { addMonths, format } from "date-fns"
import { MemberExpiryPicker } from "@/components/app/member-expiry-picker"
import { getSiteUsers, setSiteUserSessionLimit, revokeSiteUserSessions, setSiteUserStatus, deleteSiteUser, getMemberGroups, saveMemberGroup, deleteMemberGroup, getMemberGroupMembers, assignMemberGroup, removeMemberGroup, type SiteUser, type MemberGroup, type MemberGroupMember } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from "@/components/ui/dialog"

const statusNames = { active: "启用", disabled: "已禁用", pending: "待审核" }

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
    getSiteUsers(page, query).then((data) => {
      if (!active) return
      if (page > 1 && data.items.length === 0) {
        setPage(Math.max(1, Math.ceil(data.total / data.page_size)))
        return
      }
      setItems(data.items)
      setTotal(data.total)
      setSize(data.page_size)
    }).catch((e: Error) => { if (active) setError(e.message) })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [page, query, version])
  useEffect(() => { getMemberGroups().then((data) => { setGroups(data.items); setSelectedGroup((id) => data.items.some((g) => g.id === id) ? id : data.items[0]?.id ?? null) }).catch((e: Error) => setError(e.message)) }, [version])
  useEffect(() => { let active = true; setMembers([]); if (selectedGroup !== null) getMemberGroupMembers(selectedGroup).then((data) => { if (active) setMembers(data.items) }).catch((e: Error) => { if (active) setError(e.message) }); return () => { active = false } }, [selectedGroup, version])

  async function run(action: () => Promise<unknown>) {
    setBusy(true)
    setError("")
    setNotice("")
    try {
      await action()
      setNotice("操作已完成")
      setLoading(true)
      setVersion((value) => value + 1)
    } catch (e) { setError(e instanceof Error ? e.message : "操作失败") }
    finally { setBusy(false) }
  }

  return <section className="space-y-4" aria-label="前台用户管理">
    <h2 className="font-medium">前台用户</h2>
    <p className="text-sm text-muted-foreground">管理网站注册用户。禁用后会撤销登录会话，重新启用后需再次登录。</p>
    <form className="flex flex-wrap gap-2" onSubmit={(e) => { e.preventDefault(); setLoading(true); setError(""); setPage(1); setQuery(search.trim()); setVersion((v) => v + 1) }}>
      <Input className="max-w-sm" aria-label="搜索前台用户" placeholder="用户名、邮箱或显示名称" value={search} onChange={(e) => setSearch(e.target.value)} />
      <Button type="submit" disabled={loading || busy}>搜索</Button>
    </form>
    {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
    {notice && <p role="status" className="text-sm">{notice}</p>}
    {loading ? <p role="status">正在读取用户…</p> : <div className="overflow-x-auto rounded-md border">
      <table className="w-full min-w-[800px] text-left text-sm">
        <thead className="bg-muted/40"><tr>{["用户名", "显示名称", "邮箱", "状态", "注册时间", "最后登录", "操作"].map((label) => <th key={label} className="p-3">{label}</th>)}</tr></thead>
        <tbody>{items.map((item) => <tr key={item.id} className="border-t">
          <td className="p-3">{item.username}</td><td className="p-3">{item.display_name}</td><td className="p-3">{item.email || "—"}</td>
          <td className="p-3">{statusNames[item.status] ?? item.status}</td><td className="whitespace-nowrap p-3">{item.created_at}</td><td className="whitespace-nowrap p-3">{item.last_login_at || "尚未登录"}</td>
          <td className="space-x-2 whitespace-nowrap p-3">
            <Button size="sm" variant="outline" disabled={busy} onClick={() => void run(() => setSiteUserStatus(item.id, item.status === "active" ? "disabled" : "active"))}>{item.status === "active" ? "禁用" : item.status === "pending" ? "审核通过" : "启用"}</Button>
            <Button size="sm" variant="ghost" disabled={busy} onClick={() => { setError(""); setConfirmation({ title: `永久删除前台用户“${item.username}”？该操作无法撤销。`, action: () => deleteSiteUser(item.id) }) }}>删除</Button>
            <Button size="sm" variant="ghost" disabled={busy} onClick={() => { setError(""); setSessionUser(item); setSessionLimit(String(item.max_sessions)) }}>登录会话（上限 {item.max_sessions}）</Button>
            <Button size="sm" variant="outline" disabled={busy} onClick={() => { setError(""); setAssignUser(item.id); setAssignGroup(groups.find((g) => g.status === "active")?.id ?? null); const now = new Date(); setExpiryBase(now); setExpires(format(addMonths(now, 12), "yyyy-MM-dd'T'HH:mm")) }}>设置VIP</Button>
          </td>
        </tr>)}
        {items.length === 0 && <tr><td colSpan={7} className="p-6 text-center text-muted-foreground">{query ? "没有匹配的用户" : "暂无前台用户"}</td></tr>}</tbody>
      </table>
    </div>}
    <Dialog open={sessionUser !== null} onOpenChange={(open) => { if (!open && !busy) setSessionUser(null) }}><DialogContent showCloseButton={!busy} className="max-h-[85vh] overflow-y-auto sm:max-w-lg"><DialogHeader><DialogTitle>登录会话设置</DialogTitle><DialogDescription>调整登录数量或撤销用户的登录会话。</DialogDescription></DialogHeader>{sessionUser && <form className="space-y-3" onSubmit={(e) => {
      e.preventDefault()
      void run(async () => { await setSiteUserSessionLimit(sessionUser.id, Number(sessionLimit)); setSessionUser(null) })
    }}>
      <h3 className="font-medium">{sessionUser.username} 的登录会话</h3>
      <label className="block space-y-1 text-sm"><span>最大登录会话数（1–100）</span><Input className="max-w-xs" type="number" min={1} max={100} step={1} required value={sessionLimit} onChange={(e) => setSessionLimit(e.target.value)} /></label>
      <p className="text-sm text-muted-foreground">同一有效 Cookie 重复登录不增加数量。降低上限不会自动退出旧会话；全部撤销后，用户需重新登录。</p>
      <div className="flex flex-wrap gap-2">
        <Button type="submit" disabled={busy}>保存上限</Button>
        <Button type="button" variant="outline" disabled={busy} onClick={() => {
          setError(""); setConfirmation({ title: `撤销“${sessionUser.username}”的全部登录会话？用户需要重新登录。`, action: () => revokeSiteUserSessions(sessionUser.id) })
        }}>撤销全部会话</Button>
        <Button type="button" variant="ghost" disabled={busy} onClick={() => setSessionUser(null)}>取消</Button>
      </div>
    </form>}{error && <p role="alert" className="text-destructive">{error}</p>}</DialogContent></Dialog>
    <Dialog open={assignUser !== null} onOpenChange={(open) => { if (!open && !busy) setAssignUser(null) }}>
      <DialogContent showCloseButton={!busy} className="max-h-[85vh] overflow-y-auto sm:max-w-lg">
        <DialogHeader><DialogTitle>设置VIP</DialogTitle><DialogDescription>为用户分配会员组。再次分配同一组会更新有效期，其他会员组不受影响。</DialogDescription></DialogHeader>
        {groups.length === 0 ? <div className="space-y-3"><p>尚未创建会员组，请先新增会员组，再设置 VIP。</p><Button onClick={() => { setAssignUser(null); setGroupForm({ name: "", slug: "", description: "", sort_order: 0, status: "active" }) }}>新增会员组</Button></div> :
          <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); if (assignUser !== null && assignGroup !== null) void run(async () => { await assignMemberGroup(assignUser, assignGroup, expires ? new Date(expires).toISOString() : ""); setAssignUser(null) }) }}>
            <label className="block space-y-1 text-sm"><span>会员组</span><select required disabled={busy} className="block h-9 w-full rounded-md border bg-background px-3" value={assignGroup ?? ""} onChange={(e) => setAssignGroup(Number(e.target.value))}><option value="" disabled>请选择会员组</option>{groups.map((g) => <option key={g.id} value={g.id} disabled={g.status !== "active"}>{g.name}（{g.slug}）{g.status !== "active" ? " · 已停用" : ""}</option>)}</select></label>
            {assignUser !== null && <MemberExpiryPicker key={assignUser} base={expiryBase} value={expires} onChange={setExpires} disabled={busy} />}
            <p className="text-sm text-muted-foreground">选择过去的时间会使会员资格立即过期。</p>
            <div className="flex justify-end gap-2"><Button type="button" variant="outline" disabled={busy} onClick={() => setAssignUser(null)}>取消</Button><Button type="submit" disabled={busy || !groups.some((g) => g.id === assignGroup && g.status === "active")}>保存会员资格</Button></div>
          </form>}
        {error && <p role="alert" className="text-destructive">{error}</p>}
      </DialogContent>
    </Dialog>
    <section className="space-y-3 rounded-md border p-4"><div className="flex flex-wrap items-center justify-between gap-2"><h3 className="font-medium">会员组 / VIP</h3><Button size="sm" disabled={busy} onClick={() => { setError(""); setGroupForm({ name: "", slug: "", description: "", sort_order: 0, status: "active" }) }}>新增会员组</Button></div><p className="text-sm text-muted-foreground">VIP 通过会员组授予：先新增会员组，再点击用户行的“设置VIP”。会员组标识需与接入客户端配置一致。</p>{groups.length === 0 && <p className="text-sm text-muted-foreground">尚未创建会员组。</p>}<div className="flex flex-wrap gap-2">{groups.map((g) => <Button key={g.id} size="sm" variant={selectedGroup === g.id ? "default" : "outline"} onClick={() => setSelectedGroup(g.id)}>{g.name}{g.status === "disabled" ? "（停用）" : ""}</Button>)}</div>{selectedGroup !== null && <div className="space-y-2"><div className="flex items-center justify-between gap-2"><p className="text-sm text-muted-foreground">当前会员组成员 · 标识：{groups.find((g) => g.id === selectedGroup)?.slug}</p><Button size="sm" variant="outline" disabled={busy} onClick={() => { const group = groups.find((g) => g.id === selectedGroup); if (group) { setError(""); setGroupForm({ ...group }) } }}>编辑会员组</Button></div>{members.length === 0 ? <p className="text-sm text-muted-foreground">暂无成员</p> : members.map((m) => <div key={m.id} className="flex flex-wrap items-center justify-between gap-2 border-t py-2 text-sm"><span>{m.username} · {m.email || "无邮箱"} {m.expires_at ? `· 到期 ${m.expires_at}` : "· 永久"}</span><div className="flex gap-2"><Button size="sm" variant="outline" disabled={busy} onClick={() => { setError(""); setAssignUser(m.id); setAssignGroup(selectedGroup); setExpiryBase(new Date()); const date = m.expires_at ? new Date(m.expires_at) : null; setExpires(date && !Number.isNaN(date.getTime()) ? new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16) : "") }}>修改有效期 / 续期</Button><Button size="sm" variant="ghost" disabled={busy} onClick={() => { setError(""); setConfirmation({ title: `移除“${m.username}”的当前会员组资格？`, action: () => removeMemberGroup(m.id, selectedGroup) }) }}>移除</Button></div></div>)}</div>}</section>
    <Dialog open={groupForm !== null} onOpenChange={(open) => { if (!open && !busy) setGroupForm(null) }}>
      <DialogContent showCloseButton={!busy} className="max-h-[85vh] overflow-y-auto sm:max-w-lg">
        <DialogHeader><DialogTitle>{groupForm?.id ? "编辑会员组" : "新增会员组"}</DialogTitle><DialogDescription>标识供客户端校验会员权限。修改标识或停用会员组会影响已有成员权限。</DialogDescription></DialogHeader>
        {groupForm && <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); void run(async () => { await saveMemberGroup(groupForm); setGroupForm(null) }) }}>
          <label className="block space-y-1"><span>名称</span><Input required disabled={busy} value={groupForm.name ?? ""} onChange={(e) => setGroupForm({ ...groupForm, name: e.target.value })} /></label>
          <label className="block space-y-1"><span>标识</span><Input required disabled={busy} placeholder="例如 vip，需与客户端配置一致" value={groupForm.slug ?? ""} onChange={(e) => setGroupForm({ ...groupForm, slug: e.target.value })} /></label>
          <label className="block space-y-1"><span>说明</span><Input disabled={busy} value={groupForm.description ?? ""} onChange={(e) => setGroupForm({ ...groupForm, description: e.target.value })} /></label>
          <label className="block space-y-1"><span>排序</span><Input required type="number" step={1} disabled={busy} value={groupForm.sort_order ?? 0} onChange={(e) => setGroupForm({ ...groupForm, sort_order: Number(e.target.value) })} /></label>
          <label className="block space-y-1"><span>状态</span><select disabled={busy} className="block h-9 w-full rounded-md border bg-background px-3" value={groupForm.status} onChange={(e) => setGroupForm({ ...groupForm, status: e.target.value === "disabled" ? "disabled" : "active" })}><option value="active">启用</option><option value="disabled">停用</option></select></label>
          <div className="flex justify-end gap-2">
            {groupForm.id && <Button type="button" variant="destructive" disabled={busy} onClick={() => { setError(""); setConfirmation({ title: `删除“${groupForm.name}”及全部会员关联？该操作无法撤销。`, action: async () => { await deleteMemberGroup(groupForm.id!); setGroupForm(null) } }) }}>删除</Button>}
            <Button type="button" variant="outline" disabled={busy} onClick={() => setGroupForm(null)}>取消</Button><Button type="submit" disabled={busy}>保存</Button>
          </div>
        </form>}
        {error && <p role="alert" className="text-destructive">{error}</p>}
      </DialogContent>
    </Dialog>
    <Dialog open={confirmation !== null} onOpenChange={(open) => { if (!open && !busy) setConfirmation(null) }}>
      <DialogContent showCloseButton={!busy}>
        <DialogHeader><DialogTitle>确认操作</DialogTitle><DialogDescription>{confirmation?.title}</DialogDescription></DialogHeader>
        {error && <p role="alert" className="text-destructive">{error}</p>}
        <div className="flex justify-end gap-2"><Button variant="outline" disabled={busy} onClick={() => setConfirmation(null)}>取消</Button><Button variant="destructive" disabled={busy} onClick={() => { if (confirmation) void run(async () => { await confirmation.action(); setConfirmation(null) }) }}>确认</Button></div>
      </DialogContent>
    </Dialog>
    <div className="flex items-center gap-3 text-sm">
      <span>共 {total} 位用户 · 第 {page} / {Math.max(1, Math.ceil(total / size))} 页</span>
      <Button variant="outline" size="sm" disabled={loading || busy || page <= 1} onClick={() => { setLoading(true); setError(""); setPage(page - 1) }}>上一页</Button>
      <Button variant="outline" size="sm" disabled={loading || busy || page * size >= total} onClick={() => { setLoading(true); setError(""); setPage(page + 1) }}>下一页</Button>
    </div>
  </section>
}
