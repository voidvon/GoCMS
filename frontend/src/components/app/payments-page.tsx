import { useEffect, useState, type ReactNode } from "react"
import { CreditCard, Plus, RefreshCw, ReceiptText, Radio } from "lucide-react"
import {
  createPaymentAccount, fulfillCommerceOrder, getCommerceOrder,
  getCommerceOrders, getPaymentAccounts, getPaymentEvents, getPaymentStatus,
  refundPayment, syncPayment, syncPaymentRefund, updatePaymentAccount, initializePaymentKey,
  adoptPaymentKey, exportPaymentKey, installPaymentTemplate,
  type AdminUser, type CommerceOrder, type PaymentAccount,
  type PaymentAccountInput, type PaymentEvent, type PaymentStatus,
} from "@/lib/api"
import { useSite } from "@/lib/site-context"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { ConfirmDialog } from "@/components/app/app-ui"
import { showError, showSuccess } from "@/components/app/admin-notifications"
import { TimeText } from "@/components/app/time-text"

const currencies = ["USD", "EUR", "GBP", "HKD", "CNY", "JPY", "AUD", "CAD", "CHF", "SGD", "NZD"]
const environments = { sandbox: "沙箱测试", "prod-fra": "生产 · 法兰克福", "prod-sg": "生产 · 新加坡", "prod-us": "生产 · 美国" }
const states: Record<string, string> = { unpaid: "待付款", paid: "已付款", refunded: "已退款", creating: "结果待确认", pending: "待处理", succeeded: "成功", failed: "失败", closed: "已关闭", processing: "处理中", completed: "已完成" }
const emptyAccount: PaymentAccountInput = { name: "", provider: "pingpong", environment: "sandbox", client_id: "", merchant_id: "", secret: "", currency: "USD", public_url: "", return_path: "/shop/orders", enabled: false }
// Public sandbox examples published by PingPong; never use these for production.
// https://acquirer-api-docs-v4-en.pingpongx.com/notes/zh/onlinePayment/sandbox/
const sandboxExample: PaymentAccountInput = {
  ...emptyAccount, name: "PingPong 沙箱", client_id: "2018092714313010016",
  merchant_id: "2018092714313010016291", secret: "F78BC96A55548B2319EE68E0", enabled: true,
}

type Tab = "orders" | "accounts" | "events"

function downloadFile(blob: Blob, name: string) {
  const url = URL.createObjectURL(blob)
  const link = document.createElement("a")
  link.href = url
  link.download = name
  document.body.appendChild(link)
  link.click()
  link.remove()
  window.setTimeout(() => URL.revokeObjectURL(url), 1000)
}

// Allow only editable fields: IDs and site ownership never come from a file.
function accountDraft(value: unknown): PaymentAccountInput {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("配置文件必须是 JSON 对象")
  const input = value as Record<string, unknown>
  const result = { ...emptyAccount }
  for (const key of Object.keys(emptyAccount) as (keyof PaymentAccountInput)[]) {
    if (!(key in input)) continue
    if (typeof input[key] !== typeof emptyAccount[key]) throw new Error(`配置字段 ${key} 类型不正确`)
    Object.assign(result, { [key]: input[key] })
  }
  if (result.provider !== "pingpong" || !Object.hasOwn(environments, result.environment) || !currencies.includes(result.currency)) throw new Error("不支持的服务商、环境或币种")
  return result
}

function money(amount: number, currency: string) {
  return new Intl.NumberFormat("zh-CN", { style: "currency", currency }).format(amount / (currency === "JPY" ? 1 : 100))
}
function State({ value }: { value: string }) {
  return <Badge variant={value === "paid" || value === "succeeded" || value === "completed" ? "default" : "secondary"}>{states[value] ?? value}</Badge>
}
function Field({ label, children }: { label: string; children: ReactNode }) {
  return <div className="space-y-2"><Label>{label}</Label>{children}</div>
}
function Options({ value, onChange, options, label }: { value: string; onChange: (v: string) => void; options: Record<string, string>; label: string }) {
  return <Select value={value} onValueChange={(v) => { if (v) onChange(v) }}><SelectTrigger className="w-full" aria-label={label}><SelectValue>{options[value] ?? value}</SelectValue></SelectTrigger><SelectContent>{Object.entries(options).map(([v, text]) => <SelectItem key={v} value={v}>{text}</SelectItem>)}</SelectContent></Select>
}

export function PaymentsPage({ user }: { user: AdminUser }) {
  const { activeSiteId } = useSite()
  const permissions = user.site_permissions?.[activeSiteId] ?? user.permissions
  const can = (key: string) => user.is_super || permissions.includes(key) || permissions.includes("*")
  const [tab, setTab] = useState<Tab>("orders")
  const [page, setPage] = useState(1)
  const [version, setVersion] = useState(0)
  const [loadedKey, setLoadedKey] = useState("")
  const [busy, setBusy] = useState(false)
  const [keyStatus, setKeyStatus] = useState<PaymentStatus | null>(null)
  const [keyDialog, setKeyDialog] = useState(false)
  const [keyMode, setKeyMode] = useState<"generate" | "import">("generate")
  const [importKey, setImportKey] = useState("")
  const configured = keyStatus?.key_configured
  const [accounts, setAccounts] = useState<PaymentAccount[]>([])
  const [orders, setOrders] = useState<CommerceOrder[]>([])
  const [events, setEvents] = useState<PaymentEvent[]>([])
  const [accountForm, setAccountForm] = useState<PaymentAccountInput | null>(null)
  const [detail, setDetail] = useState<CommerceOrder | null>(null)
  const [refundID, setRefundID] = useState<string | null>(null)
  const loading = loadedKey !== `${tab}:${page}:${version}`

  useEffect(() => {
    let cancelled = false
    getPaymentStatus().then((r) => { if (!cancelled) setKeyStatus(r) }).catch((e: Error) => { if (!cancelled) showError(e) })
    return () => { cancelled = true }
  }, [])

  useEffect(() => {
    let cancelled = false
    const load = async () => {
      if (tab === "orders") { const r = await getCommerceOrders(page); if (!cancelled) setOrders(r.items) }
      if (tab === "accounts") { const r = await getPaymentAccounts(); if (!cancelled) setAccounts(r.items) }
      if (tab === "events") { const r = await getPaymentEvents(page); if (!cancelled) setEvents(r.items) }
    }
    load().catch((e: Error) => { if (!cancelled) showError(e) }).finally(() => { if (!cancelled) setLoadedKey(`${tab}:${page}:${version}`) })
    return () => { cancelled = true }
  }, [tab, page, version])

  async function run(action: () => Promise<unknown>, success: string, refreshDetail = false) {
    setBusy(true)
    try {
      await action()
      if (refreshDetail && detail) setDetail(await getCommerceOrder(detail.id))
      if (success) showSuccess(success)
      setVersion((v) => v + 1)
    } catch (e) { showError(e) }
    finally { setBusy(false) }
  }
  async function viewOrder(id: string) {
    await run(async () => setDetail(await getCommerceOrder(id)), "")
  }
  const rows = tab === "orders" ? orders.length : tab === "events" ? events.length : accounts.length
  const tabs = [{ id: "orders", label: "交易订单", icon: ReceiptText }, ...(can("payments.configure") ? [{ id: "accounts", label: "支付渠道", icon: CreditCard }] : []), { id: "events", label: "回调记录", icon: Radio }]

  return <div className="flex h-full min-h-0 flex-col gap-4">
    <div className="flex flex-wrap items-center justify-between gap-3">
      <div className="flex flex-wrap gap-1" role="tablist" aria-label="支付模块">{tabs.map((t) => <Button key={t.id} role="tab" aria-selected={tab === t.id} variant={tab === t.id ? "secondary" : "ghost"} onClick={() => { setTab(t.id as Tab); setPage(1) }}><t.icon className="size-4" />{t.label}</Button>)}</div>
      <div className="flex flex-wrap gap-2"><Button variant="outline" nativeButton={false} render={<a href="/shop/cart" target="_blank" rel="noopener noreferrer" />}>打开购物前台</Button>{can("theme") && <Button variant="outline" disabled={busy} onClick={() => run(() => installPaymentTemplate(), "购买模板已安装。请在分类中选择 payment_checkout.html 作为封面模板，设置发布路径并发布。")}>安装购买模板</Button>}<Button variant="outline" disabled={loading || busy} onClick={() => setVersion((v) => v + 1)}><RefreshCw className="size-4" />刷新</Button>{tab === "accounts" && <Button disabled={!configured} onClick={() => setAccountForm({ ...emptyAccount })}><Plus className="size-4" />添加 PingPong</Button>}</div>
    </div>
    <p className="text-sm text-muted-foreground">商品直接使用分类中的内容。请在<a href="/admin/categories" className="underline underline-offset-4">分类管理</a>开启在线销售，再到<a href="/admin/content" className="underline underline-offset-4">内容管理</a>配置规格和价格。</p>
    {configured === false && <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-amber-300/50 bg-amber-50/40 p-3 text-sm text-amber-800 dark:bg-amber-950/20 dark:text-amber-200"><p>{keyStatus?.key_requires_restore ? "已有支付渠道，请导入原加密密钥恢复支付功能。" : "支付凭据加密尚未配置，请由超级管理员初始化支付密钥。"}</p>{keyStatus?.key_can_initialize && <Button variant="outline" onClick={() => { setKeyMode(keyStatus.key_requires_restore ? "import" : "generate"); setImportKey(""); setKeyDialog(true) }}>配置支付密钥</Button>}</div>}
    {configured && user.is_super && <div className="flex flex-wrap items-center justify-between gap-2"><p className="text-xs text-muted-foreground">支付加密密钥已配置 · {keyStatus?.key_source === "environment" ? "来自环境变量，可接管到后台管理" : "保存在服务端独立文件"}</p><Button variant="outline" size="sm" disabled={busy} onClick={() => { setImportKey(""); setKeyDialog(true) }}>密钥管理与备份</Button></div>}
    <ScrollArea orientation="both" className="min-h-0 flex-1 rounded-lg border" contentClassName="min-w-full">
      <Table containerClassName="overflow-visible!">
        <TableHeader><TableRow>
          {tab === "orders" && <><TableHead>订单 / 商品</TableHead><TableHead>买家</TableHead><TableHead>金额</TableHead><TableHead>支付状态</TableHead><TableHead>交付状态</TableHead><TableHead>创建时间</TableHead><TableHead>操作</TableHead></>}
          {tab === "accounts" && <><TableHead>渠道名称</TableHead><TableHead>服务商 / 环境</TableHead><TableHead>币种</TableHead><TableHead>商户店铺</TableHead><TableHead>状态</TableHead><TableHead>操作</TableHead></>}
          {tab === "events" && <><TableHead>支付编号</TableHead><TableHead>类型</TableHead><TableHead>状态</TableHead><TableHead>接收时间</TableHead></>}
        </TableRow></TableHeader>
        <TableBody>
          {loading ? <TableRow><TableCell colSpan={7} className="h-40 text-center text-muted-foreground">正在加载...</TableCell></TableRow> : rows === 0 ? <TableRow><TableCell colSpan={7} className="h-48 text-center text-muted-foreground">{tab === "accounts" ? "尚未添加收款渠道，先配置一个沙箱账户开始测试。" : tab === "events" ? "尚未收到已验证的支付回调。" : "暂无订单，买家通过前台购买后，订单会显示在这里。"}</TableCell></TableRow> : <>
            {tab === "orders" && orders.map((o) => <TableRow key={o.id}><TableCell><p className="font-medium">{o.items?.[0]?.name || o.item.name}{(o.items?.length ?? 1) > 1 ? ` 等 ${o.items.length} 种规格` : ` × ${o.quantity}`}</p><p className="text-xs text-muted-foreground">共 {o.quantity} 件</p><p className="mt-1 font-mono text-xs text-muted-foreground">{o.id}</p></TableCell><TableCell>{o.customer_name}<p className="text-xs text-muted-foreground">{o.email}</p></TableCell><TableCell>{money(o.amount, o.currency)}</TableCell><TableCell><State value={o.status} /></TableCell><TableCell><State value={o.fulfillment_status} /></TableCell><TableCell><TimeText value={o.created_at} /></TableCell><TableCell><Button variant="outline" size="sm" disabled={busy} onClick={() => viewOrder(o.id)}>查看详情</Button></TableCell></TableRow>)}
            {tab === "accounts" && accounts.map((a) => <TableRow key={a.id}><TableCell className="font-medium">{a.name}<p className="text-xs text-muted-foreground">{a.public_url}</p></TableCell><TableCell>PingPong Checkout v4<p className="text-xs text-muted-foreground">{environments[a.environment as keyof typeof environments]}</p></TableCell><TableCell>{a.currency}</TableCell><TableCell className="font-mono text-xs">{a.merchant_id}</TableCell><TableCell><Badge variant={a.enabled ? "default" : "secondary"}>{a.enabled ? "已启用" : "已停用"}</Badge></TableCell><TableCell><div className="flex flex-wrap gap-2"><Button variant="outline" size="sm" disabled={busy} onClick={() => { const draft = accountDraft(a); setAccountForm({ ...draft, name: a.name + "（新配置）", secret: "", enabled: false }) }}>复制为新渠道</Button><Button variant="outline" size="sm" disabled={busy} onClick={() => run(() => updatePaymentAccount({ ...a, enabled: !a.enabled }), a.enabled ? "渠道已停用，历史交易仍可同步及退款。" : "渠道已启用。")}>{a.enabled ? "停用" : "启用"}</Button></div><p className="mt-2 break-all text-xs text-muted-foreground">通知地址：{a.public_url}/api/payments/webhooks/{a.public_id}</p></TableCell></TableRow>)}
            {tab === "events" && events.map((e) => <TableRow key={e.id}><TableCell className="font-mono text-xs">{e.reference}</TableCell><TableCell>{e.kind === "refund" ? "退款" : "支付"}</TableCell><TableCell><State value={e.status} /></TableCell><TableCell><TimeText value={e.created_at} /></TableCell></TableRow>)}
          </>}
        </TableBody>
      </Table>
    </ScrollArea>
    <div className="flex flex-wrap items-center justify-between gap-2 text-sm text-muted-foreground"><span>{tab === "accounts" ? "每个渠道固定币种和凭据；更换凭据时新增渠道，保留历史账户。" : `第 ${page} 页 · 每页最多 50 条`}</span>{tab !== "accounts" && <div className="flex gap-2"><Button variant="outline" size="sm" disabled={page === 1 || loading} onClick={() => setPage((p) => p - 1)}>上一页</Button><Button variant="outline" size="sm" disabled={rows < 50 || loading} onClick={() => setPage((p) => p + 1)}>下一页</Button></div>}</div>

    <Dialog open={keyDialog} onOpenChange={(open) => { if (!busy) { setKeyDialog(open); if (!open) setImportKey("") } }}>
      <DialogContent className="flex max-h-[90svh] flex-col sm:max-w-lg"><DialogHeader><DialogTitle>配置支付加密密钥</DialogTitle><DialogDescription>这是所有站点共用的凭据加密密钥，由超级管理员管理。保存后立即生效。</DialogDescription></DialogHeader>
        <ScrollArea className="min-h-0 flex-1" contentClassName="space-y-4 px-1 py-2">
          {!configured && <><Field label="配置方式"><Options label="密钥配置方式" value={keyMode} options={keyStatus?.key_requires_restore ? { import: "导入原密钥" } : { generate: "自动生成（推荐）", import: "导入已有密钥" }} onChange={(mode) => { setKeyMode(mode as "generate" | "import"); setImportKey("") }} /></Field>
          {keyMode === "import" && <Field label="加密密钥"><Input type="password" aria-label="支付加密密钥" autoComplete="new-password" value={importKey} onChange={(e) => setImportKey(e.target.value)} placeholder="32 字节随机密钥的 Base64 编码" /></Field>}
          {keyMode === "import" && <Field label="从备份文件导入"><Input type="file" accept=".key,.txt" disabled={busy} aria-label="导入支付密钥文件" onChange={(e) => {
            const file = e.target.files?.[0]; e.target.value = ""
            if (file) void run(async () => { if (file.size > 256) throw new Error("密钥文件不能超过 256 字节"); setImportKey((await file.text()).trim()) }, "已读取密钥文件，请保存以完成导入。")
          }} /></Field>}</>}
          {configured && <div className="flex flex-wrap gap-2">{keyStatus?.key_can_export && <Button variant="outline" disabled={busy} onClick={() => run(async () => downloadFile(await exportPaymentKey(), "gocms-payment.key"), "密钥备份已下载，请与数据库分别保管。")}>下载密钥备份</Button>}{keyStatus?.key_can_adopt && <Button disabled={busy} onClick={() => run(async () => { await adoptPaymentKey(); setKeyStatus(await getPaymentStatus()) }, "已将当前密钥保存到独立文件，重启后继续使用该文件，无需修改环境变量。")}>接管到后台管理</Button>}</div>}
          <p className="text-sm text-muted-foreground">密钥保存在服务端数据目录的 payment.key 文件中。可在此下载备份，导入时可选择备份文件或粘贴原值。备份包含明文密钥，请与数据库分别保管。已有密钥不提供直接覆盖操作。</p>
        </ScrollArea>
        <DialogFooter><Button variant="outline" disabled={busy} onClick={() => { setKeyDialog(false); setImportKey("") }}>取消</Button>{!configured && <Button disabled={busy || (keyMode === "import" && !importKey.trim())} onClick={() => run(async () => { await initializePaymentKey({ mode: keyMode, ...(keyMode === "import" ? { key: importKey.trim() } : {}) }); setImportKey(""); setKeyStatus(await getPaymentStatus()) }, "支付密钥已保存并生效，请下载密钥备份。")}>{busy ? "保存中..." : keyMode === "generate" ? "生成并保存" : "保存密钥"}</Button>}</DialogFooter>
      </DialogContent>
    </Dialog>

    <Dialog open={accountForm !== null} onOpenChange={(open) => { if (!open && !busy) setAccountForm(null) }}>
      <DialogContent className="flex max-h-[90svh] flex-col sm:max-w-xl"><DialogHeader><DialogTitle>添加 PingPong 收款渠道</DialogTitle><DialogDescription>使用 Checkout v4 的商户参数。先在沙箱完成支付与退款测试，再添加生产渠道。</DialogDescription></DialogHeader>
        {accountForm && <><ScrollArea className="min-h-0 flex-1" contentClassName="space-y-4 px-1 py-2">
          <div className="space-y-2 rounded-lg border p-3"><Field label="导入渠道配置文件（JSON）"><Input type="file" accept=".json,application/json" disabled={busy} onChange={(e) => {
            const file = e.target.files?.[0]; e.target.value = ""
            if (file) void run(async () => { if (file.size > 16384) throw new Error("配置文件不能超过 16 KB"); setAccountForm(accountDraft(JSON.parse(await file.text()))) }, "配置已填入表单，请核对后保存。")
          }} /></Field><Button variant="outline" size="sm" onClick={() => downloadFile(new Blob([JSON.stringify(sandboxExample, null, 2)], { type: "application/json" }), "pingpong-account-example.json")}>下载配置示例</Button><p className="text-xs text-muted-foreground">文件仅填入表单，核对后再保存。含 Salt 的配置文件需要妥善保管。</p></div>
          {accountForm.environment === "sandbox" && <div className="space-y-3 rounded-lg border p-3">
            <div className="flex flex-wrap items-center justify-between gap-2"><p className="text-sm font-medium">官方沙箱测试账号</p><Button variant="outline" size="sm" disabled={busy} onClick={() => {
              setAccountForm({ ...accountForm, client_id: sandboxExample.client_id, merchant_id: sandboxExample.merchant_id, secret: sandboxExample.secret, name: accountForm.name || sandboxExample.name, enabled: true })
              showSuccess("官方沙箱参数已填入，已选中启用渠道；请填写网站公开地址后保存。")
            }}>填入官方沙箱参数</Button></div>
            <p className="text-xs text-muted-foreground">使用 PingPong 官方公开测试参数，沙箱不会真实扣款。商品和渠道币种须一致，公开地址仍需填写本站可从公网访问的 HTTPS 地址；内网开发可通过临时 HTTPS 隧道转发 GoCMS 服务。</p>
            <p className="text-xs text-muted-foreground">测试卡号：<span dir="ltr" className="font-mono">5204740000001002</span>；有效期填写未来年月，CVV 填任意三位数字；如进入 3DS 验证页，验证码为 <span dir="ltr" className="font-mono">1234</span>。测试结果以沙箱返回为准。</p>
            <a className="text-xs underline underline-offset-4" href="https://acquirer-api-docs-v4-en.pingpongx.com/notes/zh/onlinePayment/sandbox/" target="_blank" rel="noopener noreferrer">查看官方沙箱说明</a>
          </div>}
          <div className="grid gap-4 sm:grid-cols-2">
          <Field label="渠道名称"><Input aria-label="渠道名称" value={accountForm.name} onChange={(e) => setAccountForm({ ...accountForm, name: e.target.value })} placeholder="例如：国际信用卡" /></Field>
          <Field label="环境"><Options label="环境" value={accountForm.environment} options={environments} onChange={(environment) => setAccountForm({ ...accountForm, environment })} /></Field>
          <Field label="Client ID"><Input aria-label="Client ID" value={accountForm.client_id} onChange={(e) => setAccountForm({ ...accountForm, client_id: e.target.value })} /></Field>
          <Field label="店铺编号 accId"><Input aria-label="店铺编号 accId" value={accountForm.merchant_id} onChange={(e) => setAccountForm({ ...accountForm, merchant_id: e.target.value })} /></Field>
        </div><Field label="签名密钥 Salt"><Input aria-label="签名密钥 Salt" type="password" autoComplete="new-password" value={accountForm.secret} onChange={(e) => setAccountForm({ ...accountForm, secret: e.target.value })} /></Field>
          <Field label="收款币种"><Options label="收款币种" value={accountForm.currency} options={Object.fromEntries(currencies.map((c) => [c, c]))} onChange={(currency) => setAccountForm({ ...accountForm, currency })} /></Field>
          <Field label="网站公开地址"><Input aria-label="网站公开地址" placeholder="https://www.example.com" value={accountForm.public_url} onChange={(e) => setAccountForm({ ...accountForm, public_url: e.target.value })} /><p className="text-xs text-muted-foreground">填写本站 HTTPS 地址，用于接收服务商通知。</p></Field>
          <Field label="付款返回页面路径"><Input aria-label="付款返回页面路径" placeholder="/shop/orders" value={accountForm.return_path} onChange={(e) => setAccountForm({ ...accountForm, return_path: e.target.value })} /><p className="text-xs text-muted-foreground">独立购物前台使用 /shop/orders；也可填写自定义主题结果页路径。系统会附加订单编号供页面查询。</p></Field>
          <div className="flex items-center justify-between"><Label htmlFor="payment-account-enabled">立即启用</Label><Switch id="payment-account-enabled" checked={accountForm.enabled} onCheckedChange={(enabled) => setAccountForm({ ...accountForm, enabled })} /></div>
        </ScrollArea><DialogFooter><Button variant="outline" disabled={busy} onClick={() => setAccountForm(null)}>取消</Button><Button disabled={busy || !accountForm.name || !accountForm.secret || !accountForm.public_url || !accountForm.client_id || !accountForm.merchant_id} onClick={() => run(async () => { await createPaymentAccount(accountForm); setAccountForm(null) }, "收款渠道已保存。")}>{busy ? "保存中..." : "保存渠道"}</Button></DialogFooter></>}
      </DialogContent>
    </Dialog>

    <Dialog open={detail !== null} onOpenChange={(open) => { if (!open && !busy) setDetail(null) }}>
      <DialogContent className="flex max-h-[90svh] flex-col sm:max-w-3xl"><DialogHeader><DialogTitle>订单详情</DialogTitle><DialogDescription className="break-all font-mono">{detail?.id}</DialogDescription></DialogHeader>
        {detail && <ScrollArea className="min-h-0 flex-1" contentClassName="space-y-5 px-1 py-2">
          <div className="grid gap-3 rounded-lg border p-4 sm:grid-cols-2"><div><p className="font-medium">共 {detail.quantity} 件 · {detail.items?.length || 1} 种规格</p><p className="mt-2 text-xl font-semibold">{money(detail.amount, detail.currency)}</p></div><div className="space-y-2"><p>{detail.customer_name} · {detail.email}</p><div className="flex gap-2"><State value={detail.status} /><State value={detail.fulfillment_status} /></div></div>{detail.address.street && <p className="text-sm text-muted-foreground sm:col-span-2">收货地址：{Object.values(detail.address).filter(Boolean).join(" · ")}</p>}</div>
          <section className="space-y-3"><h3 className="text-sm font-semibold">商品明细</h3>{(detail.items?.length ? detail.items : [{ ...detail.item, quantity: detail.quantity }]).map((item, index) => <div key={index} className="flex justify-between gap-4 rounded-lg border p-3"><div><p className="text-sm font-medium">{item.name} × {item.quantity}</p><p className="mt-1 text-xs text-muted-foreground">{item.sku && `规格编号：${item.sku}`} {item.specifications?.map((spec) => `${spec.name}：${spec.value}`).join(" / ")}</p><p className="mt-1 text-xs text-muted-foreground">单价 {money(item.amount, detail.currency)}</p></div><p className="shrink-0 text-sm">{money(item.amount * item.quantity, detail.currency)}</p></div>)}</section>
          <section className="space-y-3"><h3 className="text-sm font-semibold">支付流水</h3>{!detail.attempts?.length && <p className="text-sm text-muted-foreground">买家尚未发起支付。</p>}{detail.attempts?.map((a) => <div key={a.id} className="space-y-3 rounded-lg border p-3"><div className="flex flex-wrap items-center justify-between gap-2"><p className="break-all font-mono text-xs">{a.id}</p><State value={a.status} /></div><p className="break-all text-xs text-muted-foreground">服务商交易号：{a.transaction_id || "待确认"} · 渠道 #{a.account_id}</p>{a.error && <p className="text-sm text-amber-700 dark:text-amber-300">{a.error}</p>}<div className="flex flex-wrap gap-2"><Button size="sm" variant="outline" disabled={busy} onClick={() => run(() => syncPayment(a.id), "支付状态已同步。", true)}>同步状态</Button>{can("payments.refund") && a.status === "succeeded" && !detail.refunds?.some((r) => r.attempt_id === a.id && (r.status === "succeeded" || r.status === "pending" || r.status === "creating")) && <Button size="sm" variant="outline" disabled={busy} onClick={() => setRefundID(a.id)}>全额退款</Button>}</div></div>)}</section>
          {!!detail.refunds?.length && <section className="space-y-3"><h3 className="text-sm font-semibold">退款记录</h3>{detail.refunds.map((r) => <div key={r.id} className="space-y-2 rounded-lg border p-3"><div className="flex flex-wrap justify-between gap-2"><span className="break-all font-mono text-xs">{r.id}</span><State value={r.status} /></div><p className="text-sm">{money(r.amount, r.currency)} · {r.refund_id || "等待服务商确认"}</p>{r.error && <p className="text-sm text-amber-700 dark:text-amber-300">{r.error}</p>}<Button size="sm" variant="outline" disabled={busy} onClick={() => run(() => syncPaymentRefund(r.id), "退款状态已同步。", true)}>同步退款</Button></div>)}</section>}
          {detail.status === "paid" && can("payments.fulfill") && <section className="space-y-3"><h3 className="text-sm font-semibold">交付处理</h3><div className="flex flex-wrap gap-2">{["processing", "completed", "failed"].map((status) => <Button key={status} variant="outline" disabled={busy || detail.fulfillment_status === status} onClick={() => run(() => fulfillCommerceOrder(detail.id, status), "交付状态已更新。", true)}>{states[status]}</Button>)}</div></section>}
        </ScrollArea>}
      </DialogContent>
    </Dialog>
    <ConfirmDialog open={refundID !== null} onOpenChange={(open) => { if (!open && !busy) setRefundID(null) }} title="确认全额退款" description="将向原支付渠道退还本次付款的全部金额。退款提交后可能需要等待服务商处理，交付状态需单独核查。" confirmLabel="提交退款" pending={busy} onConfirm={() => { if (refundID) void run(async () => { try { await refundPayment(refundID) } finally { setRefundID(null) } }, "退款请求已提交，请查看退款记录确认结果。", true) }} />
  </div>
}
