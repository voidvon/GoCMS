import { useEffect, useState } from "react"
import { getAdminAccounts, getAdminGroups, saveAdminAccount, deleteAdminAccount, saveAdminGroup, deleteAdminGroup, type AdminAccount, type AdminGroup } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { getCategories, type CategoryItem } from "@/lib/api"

export function UsersPage({ currentUserID }: { currentUserID: number }) {
  const [accounts, setAccounts] = useState<AdminAccount[]>([])
  const [categories, setCategories] = useState<CategoryItem[]>([])
  const [groups, setGroups] = useState<AdminGroup[]>([])
  const [permissions, setPermissions] = useState<{ key: string; label: string }[]>([])
  const [account, setAccount] = useState<AdminAccount | null>(null)
  const [group, setGroup] = useState<AdminGroup | null>(null)
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const [loaded, setLoaded] = useState(false)

  async function refresh() {
    const [users, data, categoryList] = await Promise.all([getAdminAccounts(), getAdminGroups(), getCategories()])
    setCategories(categoryList)
    setAccounts(users)
    setGroups(data.items)
    setPermissions(data.permissions)
    setLoaded(true)
  }
  useEffect(() => { void refresh().catch((e: Error) => setError(e.message)) }, [])

  async function run(action: () => Promise<unknown>, reloadSession = false) {
    setBusy(true)
    setError("")
    try {
      await action()
      if (reloadSession) { window.location.reload(); return }
      setAccount(null)
      setGroup(null)
      await refresh()
    } catch (e) { setError(e instanceof Error ? e.message : "操作失败") }
    finally { setBusy(false) }
  }

  return (
    <div className="h-full space-y-6 overflow-auto pb-6">
      {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
      {!loaded && <p className="text-sm text-muted-foreground">正在读取账号和用户组…</p>}
      <section className="space-y-3">
        <div className="flex items-center justify-between"><h2 className="font-medium">后台账号</h2><Button disabled={busy || !loaded} onClick={() => { setGroup(null); setAccount({ id: 0, username: "", password: "", group_id: groups[0]?.id ?? 0, is_super: false, disabled: false }) }}>新增账号</Button></div>
        <p className="text-sm text-muted-foreground">普通账号按用户组获得模块权限；超级管理员可管理账号、用户组及全部模块。</p>
        <div className="overflow-x-auto rounded-md border">
          <table className="w-full text-left text-sm"><thead className="bg-muted/40"><tr><th className="p-3">账号</th><th className="p-3">用户组</th><th className="p-3">状态</th><th className="p-3">操作</th></tr></thead><tbody>
            {accounts.map((item) => <tr key={item.id} className="border-t"><td className="p-3">{item.username}{item.id === currentUserID && "（当前账号）"}</td><td className="p-3">{item.is_super ? "超级管理员" : groups.find((g) => g.id === item.group_id)?.name ?? "未分组"}</td><td className="p-3">{item.disabled ? "已停用" : "启用"}</td><td className="space-x-2 whitespace-nowrap p-3"><Button variant="outline" size="sm" disabled={busy} onClick={() => { setGroup(null); setAccount({ ...item, password: "" }) }}>编辑</Button><Button variant="ghost" size="sm" disabled={busy} onClick={() => { if (window.confirm(`删除账号“${item.username}”？该账号将无法继续登录。`)) void run(() => deleteAdminAccount(item.id), item.id === currentUserID) }}>删除</Button></td></tr>)}
          </tbody></table>
        </div>
        {account && <form className="space-y-4 rounded-md border p-4" onSubmit={(e) => { e.preventDefault(); void run(() => saveAdminAccount(account), account.id === currentUserID) }}>
          <h3 className="font-medium">{account.id ? "编辑账号" : "新增账号"}</h3>
          <label className="block space-y-1 text-sm"><span>账号名称</span><Input required value={account.username} autoComplete="off" onChange={(e) => setAccount({ ...account, username: e.target.value })} /></label>
          <label className="block space-y-1 text-sm"><span>{account.id ? "新密码（留空保留原密码）" : "密码（至少 8 字节）"}</span><Input type="password" autoComplete="new-password" required={!account.id} value={account.password ?? ""} onChange={(e) => setAccount({ ...account, password: e.target.value })} /></label>
          <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={account.is_super} onChange={(e) => setAccount({ ...account, is_super: e.target.checked })} />超级管理员</label>
          {!account.is_super && <label className="block space-y-1 text-sm"><span>用户组</span><select className="block h-9 w-full rounded-md border bg-background px-3" required value={account.group_id || ""} onChange={(e) => setAccount({ ...account, group_id: Number(e.target.value) })}><option value="">请选择用户组</option>{groups.map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}</select>{groups.length === 0 && <span className="text-muted-foreground">请先在下方新增用户组。</span>}</label>}
          {!account.is_super && <fieldset className="space-y-2"><legend className="text-sm">可管理内容的栏目</legend><label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={account.category_ids == null} onChange={(e) => setAccount({ ...account, category_ids: e.target.checked ? null : [] })} />全部栏目（包含以后新增的栏目）</label>{account.category_ids != null && <div className="grid grid-cols-2 gap-2">{categories.map((c) => <label key={c.id} className="flex items-center gap-2 text-sm"><input type="checkbox" checked={account.category_ids?.includes(c.id) ?? false} onChange={(e) => setAccount({ ...account, category_ids: e.target.checked ? [...(account.category_ids ?? []), c.id] : account.category_ids?.filter((id) => id !== c.id) })} />{c.name}</label>)}</div>}<p className="text-xs text-muted-foreground">逐个授权，不自动包含下级栏目；未勾选任何栏目时不能管理内容。栏目配置管理由用户组权限独立控制。</p></fieldset>}
          <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={account.disabled} onChange={(e) => setAccount({ ...account, disabled: e.target.checked })} />停用账号</label>
          {account.id !== 0 && <p className="text-sm text-muted-foreground">保存后该账号需重新登录，已有 API Key 将撤销。</p>}
          <div className="flex gap-2"><Button disabled={busy} type="submit">{busy ? "正在保存…" : "保存账号"}</Button><Button disabled={busy} type="button" variant="outline" onClick={() => setAccount(null)}>取消</Button></div>
        </form>}
      </section>
      <section className="space-y-3">
        <div className="flex items-center justify-between"><h2 className="font-medium">用户组</h2><Button disabled={busy || !loaded} onClick={() => { setAccount(null); setGroup({ id: 0, name: "", permissions: [] }) }}>新增用户组</Button></div>
        <p className="text-sm text-muted-foreground">内容管理允许浏览内容，新增、修改、删除、审核需分别授权。审核人员还需修改权限。无审核权限只能保存隐藏内容，不能修改已公开内容。栏目、模型和语言的基本信息可供编辑内容时读取。</p>
        {groups.length === 0 && loaded && <p className="text-sm text-muted-foreground">暂无用户组。</p>}
        {groups.map((item) => <div key={item.id} className="flex flex-wrap items-center justify-between gap-3 rounded-md border p-3"><div><p className="text-sm font-medium">{item.name}</p><p className="text-xs text-muted-foreground">{item.permissions.map((key) => permissions.find((p) => p.key === key)?.label ?? key).join("、") || "未授权业务模块"}</p></div><div className="flex gap-2"><Button disabled={busy} variant="outline" size="sm" onClick={() => { setAccount(null); setGroup({ ...item }) }}>编辑</Button><Button disabled={busy} variant="ghost" size="sm" onClick={() => { if (window.confirm(`删除用户组“${item.name}”？`)) void run(() => deleteAdminGroup(item.id)) }}>删除</Button></div></div>)}
        {group && <form className="space-y-4 rounded-md border p-4" onSubmit={(e) => { e.preventDefault(); void run(() => saveAdminGroup(group)) }}>
          <h3 className="font-medium">{group.id ? "编辑用户组" : "新增用户组"}</h3>
          <label className="block space-y-1 text-sm"><span>用户组名称</span><Input required value={group.name} onChange={(e) => setGroup({ ...group, name: e.target.value })} /></label>
          <fieldset><legend className="mb-2 text-sm">模块权限</legend><div className="grid grid-cols-2 gap-3 sm:grid-cols-4">{permissions.map((p) => <label key={p.key} className="flex items-center gap-2 text-sm"><input type="checkbox" checked={group.permissions.includes(p.key)} onChange={(e) => setGroup({ ...group, permissions: e.target.checked ? [...group.permissions, p.key] : group.permissions.filter((key) => key !== p.key) })} />{p.label}</label>)}</div></fieldset>
          <div className="flex gap-2"><Button disabled={busy} type="submit">{busy ? "正在保存…" : "保存用户组"}</Button><Button disabled={busy} type="button" variant="outline" onClick={() => setGroup(null)}>取消</Button></div>
        </form>}
      </section>
    </div>
  )
}
