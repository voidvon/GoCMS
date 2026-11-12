import { CurrencySettings } from "./currency-settings"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import { Card } from "@/components/ui/card"
import { useEffect, useState, type ReactNode } from "react"
import { CreditCard, Coins, Plus, RefreshCw, Radio } from "lucide-react"
import {
  createPaymentAccount,
  getPaymentAccounts, getPaymentEvents, getPaymentStatus, getCommerceCurrencyInfo,
  updatePaymentAccount, initializePaymentKey,
  adoptPaymentKey, exportPaymentKey, installPaymentTemplate,
  type AdminUser, type PaymentAccount,
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
import { showError, showSuccess } from "@/components/app/admin-notifications"
import { TimeText } from "@/components/app/time-text"

const currencies = ["USD", "EUR", "GBP", "HKD", "CNY", "JPY", "AUD", "CAD", "CHF", "SGD", "NZD"]
const environments = { sandbox: "沙箱测试", "prod-fra": "生产 · 法兰克福", "prod-sg": "生产 · 新加坡", "prod-us": "生产 · 美国" }
const states: Record<string, string> = { unpaid: "待付款", paid: "已付款", refunded: "已退款", creating: "结果待确认", pending: "待处理", succeeded: "成功", failed: "失败", closed: "已关闭", processing: "处理中", completed: "已完成" }
const emptyAccount: PaymentAccountInput = { name: "", provider: "pingpong", environment: "sandbox", client_id: "", merchant_id: "", secret: "", currency: "", public_url: "", return_path: "/shop/orders", enabled: false }
// Public sandbox examples published by PingPong; never use these for production.
// https://acquirer-api-docs-v4-en.pingpongx.com/notes/zh/onlinePayment/sandbox/
const sandboxExample: PaymentAccountInput = {
  ...emptyAccount, currency: "USD", name: "PingPong 沙箱", client_id: "2018092714313010016",
  merchant_id: "2018092714313010016291", secret: "F78BC96A55548B2319EE68E0", enabled: true,
}

type Tab = "accounts" | "events" | "pricing"

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

function State({ value }: { value: string }) {
  return <Badge variant={value === "paid" || value === "succeeded" || value === "completed" ? "default" : "secondary"}>{states[value] ?? value}</Badge>
}
function Field({ label, children }: { label: string; children: ReactNode }) {
  return <div className="space-y-2"><Label>{label}</Label>{children}</div>
}
function Options({ value, onChange, options, label }: { value: string; onChange: (v: string) => void; options: Record<string, string>; label: string }) {
  return <Select value={value} onValueChange={(v) => { if (v) onChange(v) }}><SelectTrigger className="w-full" aria-label={label}><SelectValue placeholder={label}>{options[value] ?? (value || undefined)}</SelectValue></SelectTrigger><SelectContent>{Object.entries(options).map(([v, text]) => <SelectItem key={v} value={v}>{text}</SelectItem>)}</SelectContent></Select>
}

export function PaymentsPage({ user }: { user: AdminUser }) {
  const { activeSiteId } = useSite()
  const permissions = user.site_permissions?.[activeSiteId] ?? user.permissions
  const can = (key: string) => user.is_super || permissions.includes(key) || permissions.includes("*")
  const [tab, setTab] = useState<Tab>(can("payments.configure") ? "accounts" : "events")
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
  const [events, setEvents] = useState<PaymentEvent[]>([])
  const [accountForm, setAccountForm] = useState<PaymentAccountInput | null>(null)
  const loading = loadedKey !== `${tab}:${page}:${version}`

  useEffect(() => {
    let cancelled = false
    getPaymentStatus().then((r) => { if (!cancelled) setKeyStatus(r) }).catch((e: Error) => { if (!cancelled) showError(e) })
    return () => { cancelled = true }
  }, [])

  useEffect(() => {
    let cancelled = false
    const load = async () => {
      if (tab === "accounts") { const r = await getPaymentAccounts(); if (!cancelled) setAccounts(r.items) }
      if (tab === "events") { const r = await getPaymentEvents(page); if (!cancelled) setEvents(r.items) }
    }
    load().catch((e: Error) => { if (!cancelled) showError(e) }).finally(() => { if (!cancelled) setLoadedKey(`${tab}:${page}:${version}`) })
    return () => { cancelled = true }
  }, [tab, page, version])

  async function run(action: () => Promise<unknown>, success: string) {
    setBusy(true)
    try {
      await action()
      if (success) showSuccess(success)
      setVersion((v) => v + 1)
    } catch (e) { showError(e) }
    finally { setBusy(false) }
  }
  const rows = tab === "events" ? events.length : accounts.length
  const tabs = [...(can("payments.configure") ? [{ id: "accounts", label: "支付渠道", icon: CreditCard }, { id: "pricing", label: "币种与汇率", icon: Coins }] : []), { id: "events", label: "回调记录", icon: Radio }]

  return <Tabs value={tab} onValueChange={value => { if (value === "accounts" || value === "events" || value === "pricing") { setTab(value); setPage(1) } }} className="flex h-full min-h-0 flex-col gap-4">
    <div className="flex flex-wrap items-center justify-between gap-3">
      <TabsList aria-label="支付模块">{tabs.map(t => <TabsTrigger key={t.id} value={t.id}><t.icon className="size-4" />{t.label}</TabsTrigger>)}</TabsList>
      <div className="flex flex-wrap gap-2"><Button variant="outline" nativeButton={false} render={<a href="/shop/cart" target="_blank" rel="noopener noreferrer" />}>打开购物前台</Button>{can("theme") && <Button variant="outline" disabled={busy} onClick={() => run(() => installPaymentTemplate(), "购买模板已安装。请在分类中选择 payment_checkout.html 作为封面模板，设置发布路径并发布。")}>安装购买模板</Button>}<Button variant="outline" disabled={loading || busy} onClick={() => setVersion((v) => v + 1)}><RefreshCw className="size-4" />刷新</Button>{tab === "accounts" && <Button disabled={!configured || busy} onClick={() => void run(async () => { const info = await getCommerceCurrencyInfo(); setAccountForm({ ...emptyAccount, currency: info.base_currency }) }, "")}><Plus className="size-4" />添加 PingPong</Button>}</div>
    </div>
    <TabsContent value={tab} className="flex min-h-0 flex-1 flex-col gap-4">
    {tab === "pricing" ? <CurrencySettings key={`${activeSiteId}:${version}`} /> : <>
    <p className="text-sm text-muted-foreground">商品直接使用分类中的内容。可在<a href="/admin/content" className="underline underline-offset-4">内容管理</a>直接配置规格；需要购买功能时，再填写售价并同时开启内容与<a href="/admin/categories" className="underline underline-offset-4">所属分类</a>的在线销售。</p>
    {configured === false && <Alert role="status" className="flex flex-wrap items-center justify-between gap-3"><AlertDescription>{keyStatus?.key_requires_restore ? "已有支付渠道，请导入原加密密钥恢复支付功能。" : "支付凭据加密尚未配置，请由超级管理员初始化支付密钥。"}</AlertDescription>{keyStatus?.key_can_initialize && <Button variant="outline" onClick={() => { setKeyMode(keyStatus.key_requires_restore ? "import" : "generate"); setImportKey(""); setKeyDialog(true) }}>配置支付密钥</Button>}</Alert>}
    {configured && user.is_super && <div className="flex flex-wrap items-center justify-between gap-2"><p className="text-xs text-muted-foreground">支付加密密钥已配置 · {keyStatus?.key_source === "environment" ? "来自环境变量，可接管到后台管理" : "保存在服务端独立文件"}</p><Button variant="outline" size="sm" disabled={busy} onClick={() => { setImportKey(""); setKeyDialog(true) }}>密钥管理与备份</Button></div>}
    <ScrollArea orientation="both" className="min-h-0 flex-1 rounded-lg border" contentClassName="min-w-full">
      <Table containerClassName="overflow-visible!">
        <TableHeader><TableRow>
          {tab === "accounts" && <><TableHead>渠道名称</TableHead><TableHead>服务商 / 环境</TableHead><TableHead>币种</TableHead><TableHead>商户店铺</TableHead><TableHead>状态</TableHead><TableHead>操作</TableHead></>}
          {tab === "events" && <><TableHead>支付编号</TableHead><TableHead>类型</TableHead><TableHead>状态</TableHead><TableHead>接收时间</TableHead></>}
        </TableRow></TableHeader>
        <TableBody>
          {loading ? <TableRow><TableCell colSpan={7} className="h-40 text-center text-muted-foreground">正在加载...</TableCell></TableRow> : rows === 0 ? <TableRow><TableCell colSpan={7} className="h-48 text-center text-muted-foreground">{tab === "accounts" ? "尚未添加收款渠道，先配置一个沙箱账户开始测试。" : tab === "events" ? "尚未收到已验证的支付回调。" : "暂无订单，买家通过前台购买后，订单会显示在这里。"}</TableCell></TableRow> : <>
            {tab === "accounts" && accounts.map((a) => <TableRow key={a.id}><TableCell className="font-medium">{a.name}<p className="text-xs text-muted-foreground">{a.public_url}</p></TableCell><TableCell>PingPong Checkout v4<p className="text-xs text-muted-foreground">{environments[a.environment as keyof typeof environments]}</p></TableCell><TableCell>{a.currency}</TableCell><TableCell className="font-mono text-xs">{a.merchant_id}</TableCell><TableCell><Badge variant={a.enabled ? "default" : "secondary"}>{a.enabled ? "已启用" : "已停用"}</Badge></TableCell><TableCell><div className="flex flex-wrap gap-2"><Button variant="outline" size="sm" disabled={busy} onClick={() => { const draft = accountDraft(a); setAccountForm({ ...draft, name: a.name + "（新配置）", secret: "", enabled: false }) }}>复制为新渠道</Button><Button variant="outline" size="sm" disabled={busy} onClick={() => run(() => updatePaymentAccount({ ...a, enabled: !a.enabled }), a.enabled ? "渠道已停用，历史交易仍可同步及退款。" : "渠道已启用。")}>{a.enabled ? "停用" : "启用"}</Button></div><p className="mt-2 break-all text-xs text-muted-foreground">通知地址：{a.public_url}/api/payments/webhooks/{a.public_id}</p></TableCell></TableRow>)}
            {tab === "events" && events.map((e) => <TableRow key={e.id}><TableCell className="font-mono text-xs">{e.reference}</TableCell><TableCell>{e.kind === "refund" ? "退款" : "支付"}</TableCell><TableCell><State value={e.status} /></TableCell><TableCell><TimeText value={e.created_at} /></TableCell></TableRow>)}
          </>}
        </TableBody>
      </Table>
    </ScrollArea>
    <div className="flex flex-wrap items-center justify-between gap-2 text-sm text-muted-foreground"><span>{tab === "accounts" ? "每个渠道固定币种和凭据；更换凭据时新增渠道，保留历史账户。" : `第 ${page} 页 · 每页最多 50 条`}</span>{tab !== "accounts" && <div className="flex gap-2"><Button variant="outline" size="sm" disabled={page === 1 || loading} onClick={() => setPage((p) => p - 1)}>上一页</Button><Button variant="outline" size="sm" disabled={rows < 50 || loading} onClick={() => setPage((p) => p + 1)}>下一页</Button></div>}</div>

    </>}
    </TabsContent>
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
          <Card className="space-y-2 rounded-lg border p-3 gap-0"><Field label="导入渠道配置文件（JSON）"><Input type="file" accept=".json,application/json" disabled={busy} onChange={(e) => {
            const file = e.target.files?.[0]; e.target.value = ""
            if (file) void run(async () => { if (file.size > 16384) throw new Error("配置文件不能超过 16 KB"); setAccountForm(accountDraft(JSON.parse(await file.text()))) }, "配置已填入表单，请核对后保存。")
          }} /></Field><Button variant="outline" size="sm" onClick={() => downloadFile(new Blob([JSON.stringify(sandboxExample, null, 2)], { type: "application/json" }), "pingpong-account-example.json")}>下载配置示例</Button><p className="text-xs text-muted-foreground">文件仅填入表单，核对后再保存。含 Salt 的配置文件需要妥善保管。</p></Card>
          {accountForm.environment === "sandbox" && <Card className="space-y-3 rounded-lg border p-3 gap-0">
            <div className="flex flex-wrap items-center justify-between gap-2"><p className="text-sm font-medium">官方沙箱测试账号</p><Button variant="outline" size="sm" disabled={busy} onClick={() => {
              setAccountForm({ ...accountForm, client_id: sandboxExample.client_id, merchant_id: sandboxExample.merchant_id, secret: sandboxExample.secret, name: accountForm.name || sandboxExample.name, enabled: true })
              showSuccess("官方沙箱参数已填入，已选中启用渠道；请填写网站公开地址后保存。")
            }}>填入官方沙箱参数</Button></div>
            <p className="text-xs text-muted-foreground">使用 PingPong 官方公开测试参数，沙箱不会真实扣款。商品和渠道币种须一致，公开地址仍需填写本站可从公网访问的 HTTPS 地址；内网开发可通过临时 HTTPS 隧道转发 GoCMS 服务。</p>
            <p className="text-xs text-muted-foreground">测试卡号：<span dir="ltr" className="font-mono">5204740000001002</span>；有效期填写未来年月，CVV 填任意三位数字；如进入 3DS 验证页，验证码为 <span dir="ltr" className="font-mono">1234</span>。测试结果以沙箱返回为准。</p>
            <a className="text-xs underline underline-offset-4" href="https://acquirer-api-docs-v4-en.pingpongx.com/notes/zh/onlinePayment/sandbox/" target="_blank" rel="noopener noreferrer">查看官方沙箱说明</a>
          </Card>}
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

  </Tabs>
}
