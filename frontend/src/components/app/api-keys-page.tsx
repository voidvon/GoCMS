import { useCallback, useEffect, useState } from "react"
import {
  Check,
  Copy,
  History,
  KeyRound,
  LoaderCircle,
  Plus,
  RefreshCw,
  RotateCw,
  ShieldAlert,
  Trash2,
} from "lucide-react"

import {
  createApiKey,
  getApiKeyEvents,
  getApiKeys,
  revokeApiKey,
  rotateApiKey,
  type ApiKey,
  type ApiKeyEvent,
  type ApiKeyWithSecret,
} from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { ConfirmDialog, InlineAlert } from "@/components/app/app-ui"

function formatRelativeTime(dateStr?: string | null) {
  if (!dateStr) return "尚未使用"
  const date = new Date(dateStr)
  if (Number.isNaN(date.getTime())) return dateStr
  const now = new Date()
  const diffSec = Math.floor((now.getTime() - date.getTime()) / 1000)
  if (diffSec < 60) return "刚刚"
  if (diffSec < 3600) return `${Math.floor(diffSec / 60)} 分钟前`
  if (diffSec < 86400) return `${Math.floor(diffSec / 3600)} 小时前`
  if (diffSec < 2592000) return `${Math.floor(diffSec / 86400)} 天前`
  return date.toLocaleDateString("zh-CN")
}

function formatDate(dateStr?: string | null) {
  if (!dateStr) return "-"
  const date = new Date(dateStr)
  if (Number.isNaN(date.getTime())) return dateStr
  return date.toLocaleString("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  })
}

function getStatusBadge(status: ApiKey["status"]) {
  switch (status) {
    case "active":
      return <Badge variant="outline" className="text-emerald-600 border-emerald-500/30 bg-emerald-500/10">有效</Badge>
    case "revoked":
      return <Badge variant="destructive">已撤销</Badge>
    case "expired":
      return <Badge variant="secondary">已过期</Badge>
    default:
      return <Badge variant="outline">{status}</Badge>
  }
}

export function ApiKeysPage() {
  const [keys, setKeys] = useState<ApiKey[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")

  // Create Dialog State
  const [createOpen, setCreateOpen] = useState(false)
  const [name, setName] = useState("")
  const [expiryPreset, setExpiryPreset] = useState("never")
  const [customExpiryDate, setCustomExpiryDate] = useState("")
  const [createPending, setCreatePending] = useState(false)
  const [createError, setCreateError] = useState("")

  // Secret display dialog (once on create or rotate)
  const [secretKey, setSecretKey] = useState<ApiKeyWithSecret | null>(null)
  const [copied, setCopied] = useState(false)

  // Revoke Dialog State
  const [revokeTarget, setRevokeTarget] = useState<ApiKey | null>(null)
  const [revokeReason, setRevokeReason] = useState("")
  const [revokePending, setRevokePending] = useState(false)

  // Rotate Dialog State
  const [rotateTarget, setRotateTarget] = useState<ApiKey | null>(null)
  const [rotatePending, setRotatePending] = useState(false)

  // Events Dialog State
  const [eventsTarget, setEventsTarget] = useState<ApiKey | null>(null)
  const [eventsList, setEventsList] = useState<ApiKeyEvent[]>([])
  const [eventsLoading, setEventsLoading] = useState(false)
  const [eventsError, setEventsError] = useState("")

  const loadKeys = useCallback(async () => {
    setLoading(true)
    setError("")
    try {
      const res = await getApiKeys()
      setKeys(res.data || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : "加载 API Key 列表失败")
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    let ignore = false
    getApiKeys()
      .then((res) => {
        if (!ignore) {
          setKeys(res.data || [])
        }
      })
      .catch((err) => {
        if (!ignore) {
          setError(err instanceof Error ? err.message : "加载 API Key 列表失败")
        }
      })
      .finally(() => {
        if (!ignore) {
          setLoading(false)
        }
      })
    return () => {
      ignore = true
    }
  }, [])

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault()
    const trimmedName = name.trim()
    if (!trimmedName) {
      setCreateError("请输入 API Key 名称")
      return
    }

    let expiresAt: string | null = null
    if (expiryPreset !== "never") {
      if (expiryPreset === "custom") {
        if (!customExpiryDate) {
          setCreateError("请选择自定义过期时间")
          return
        }
        expiresAt = new Date(customExpiryDate).toISOString()
      } else {
        const days = Number(expiryPreset)
        if (days > 0) {
          const target = new Date()
          target.setDate(target.getDate() + days)
          expiresAt = target.toISOString()
        }
      }
    }

    setCreatePending(true)
    setCreateError("")
    try {
      const res = await createApiKey({ name: trimmedName, expires_at: expiresAt })
      setCreateOpen(false)
      setName("")
      setExpiryPreset("never")
      setCustomExpiryDate("")
      if (res.data) {
        setSecretKey(res.data)
      }
      await loadKeys()
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : "创建 API Key 失败")
    } finally {
      setCreatePending(false)
    }
  }

  async function handleRotate() {
    if (!rotateTarget) return
    setRotatePending(true)
    try {
      const res = await rotateApiKey(rotateTarget.id)
      setRotateTarget(null)
      if (res.data) {
        setSecretKey(res.data)
      }
      await loadKeys()
    } catch (err) {
      setError(err instanceof Error ? err.message : "轮换 API Key 失败")
      setRotateTarget(null)
    } finally {
      setRotatePending(false)
    }
  }

  async function handleRevoke() {
    if (!revokeTarget) return
    setRevokePending(true)
    try {
      await revokeApiKey(revokeTarget.id, revokeReason.trim() || undefined)
      setRevokeTarget(null)
      setRevokeReason("")
      await loadKeys()
    } catch (err) {
      setError(err instanceof Error ? err.message : "撤销 API Key 失败")
      setRevokeTarget(null)
    } finally {
      setRevokePending(false)
    }
  }

  async function handleViewEvents(item: ApiKey) {
    setEventsTarget(item)
    setEventsLoading(true)
    setEventsError("")
    try {
      const res = await getApiKeyEvents(item.id)
      setEventsList(res.data || [])
    } catch (err) {
      setEventsError(err instanceof Error ? err.message : "加载审计日志失败")
    } finally {
      setEventsLoading(false)
    }
  }

  async function copyKey(text: string) {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // Fallback
    }
  }

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader>
          <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
            <div className="flex items-start gap-3">
              <div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted text-foreground">
                <KeyRound className="size-4" />
              </div>
              <div>
                <CardTitle>API Key</CardTitle>
                <CardDescription className="mt-1">
                  API Key 与所属管理员拥有相同的后台权限，默认长期有效。
                </CardDescription>
              </div>
            </div>
            <div className="flex shrink-0 items-center gap-2">
              <Button
                variant="outline"
                size="sm"
                onClick={() => void loadKeys()}
                disabled={loading}
              >
                <RefreshCw className={loading ? "animate-spin" : ""} />
                刷新
              </Button>
              <Button size="sm" onClick={() => setCreateOpen(true)}>
                <Plus />
                创建 Key
              </Button>
            </div>
          </div>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-start gap-2.5 rounded-lg border border-amber-500/30 bg-amber-500/10 p-3.5 text-sm text-amber-900 dark:text-amber-200">
            <ShieldAlert className="mt-0.5 size-4 shrink-0 text-amber-600 dark:text-amber-400" />
            <span>
              长期 Key 等同于管理员 API 凭据。建议为接口单独创建权限最小化的管理员账号，不要在浏览器或静态页面中使用。
            </span>
          </div>

          {error && <InlineAlert>{error}</InlineAlert>}

          <div className="rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>名称</TableHead>
                  <TableHead>所属管理员</TableHead>
                  <TableHead>Key 前缀</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>最后使用</TableHead>
                  <TableHead>过期时间</TableHead>
                  <TableHead>创建时间</TableHead>
                  <TableHead className="text-right">操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {loading && keys.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={8} className="h-28 text-center text-muted-foreground">
                      <LoaderCircle className="mx-auto size-5 animate-spin" />
                      <span className="mt-2 block text-xs">加载中...</span>
                    </TableCell>
                  </TableRow>
                ) : keys.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={8} className="h-28 text-center text-muted-foreground">
                      暂无 API Key
                    </TableCell>
                  </TableRow>
                ) : (
                  keys.map((item) => (
                    <TableRow key={item.id}>
                      <TableCell className="font-medium">{item.name}</TableCell>
                      <TableCell className="text-muted-foreground">
                        {item.admin_username || "-"}
                      </TableCell>
                      <TableCell>
                        <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs text-muted-foreground">
                          {item.key_prefix}...
                        </code>
                      </TableCell>
                      <TableCell>{getStatusBadge(item.status)}</TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {item.last_used_at ? (
                          <span title={`IP: ${item.last_used_ip || "未知"}`}>
                            {formatRelativeTime(item.last_used_at)}
                          </span>
                        ) : (
                          "尚未使用"
                        )}
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {item.expires_at ? formatDate(item.expires_at) : "永久"}
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {formatDate(item.created_at)}
                      </TableCell>
                      <TableCell className="text-right">
                        <div className="flex items-center justify-end gap-1.5">
                          {item.status === "active" && (
                            <>
                              <Button
                                variant="outline"
                                size="sm"
                                onClick={() => setRotateTarget(item)}
                                title="轮换此 Key"
                              >
                                <RotateCw />
                                轮换
                              </Button>
                              <Button
                                variant="destructive"
                                size="sm"
                                onClick={() => setRevokeTarget(item)}
                                title="撤销此 Key"
                              >
                                <Trash2 />
                                撤销
                              </Button>
                            </>
                          )}
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => void handleViewEvents(item)}
                            title="审计日志"
                          >
                            <History />
                            日志
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>
        </CardContent>
      </Card>

      {/* Create API Key Dialog */}
      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>创建 API Key</DialogTitle>
            <DialogDescription>
              新 Key 会继承当前管理员的全部后台权限，默认不过期。
            </DialogDescription>
          </DialogHeader>
          <form onSubmit={handleCreate} className="space-y-4">
            {createError && <InlineAlert>{createError}</InlineAlert>}
            <div className="space-y-2">
              <Label htmlFor="api-key-name">名称</Label>
              <Input
                id="api-key-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="例如：生产内容同步服务"
                maxLength={120}
                autoFocus
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="api-key-expiry">有效期</Label>
              <Select value={expiryPreset} onValueChange={(val) => val && setExpiryPreset(val)}>
                <SelectTrigger id="api-key-expiry" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="never">永不过期（默认）</SelectItem>
                  <SelectItem value="30">30 天</SelectItem>
                  <SelectItem value="90">90 天</SelectItem>
                  <SelectItem value="180">180 天</SelectItem>
                  <SelectItem value="365">1 年</SelectItem>
                  <SelectItem value="custom">自定义过期日期</SelectItem>
                </SelectContent>
              </Select>
            </div>
            {expiryPreset === "custom" && (
              <div className="space-y-2">
                <Label htmlFor="custom-expiry">选择到期日期</Label>
                <Input
                  id="custom-expiry"
                  type="date"
                  value={customExpiryDate}
                  onChange={(e) => setCustomExpiryDate(e.target.value)}
                />
              </div>
            )}
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => setCreateOpen(false)}
                disabled={createPending}
              >
                取消
              </Button>
              <Button type="submit" disabled={createPending}>
                {createPending ? (
                  <>
                    <LoaderCircle className="animate-spin" />
                    创建中...
                  </>
                ) : (
                  <>
                    <KeyRound />
                    创建
                  </>
                )}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Secret Key Display Dialog (one-time view) */}
      <Dialog
        open={Boolean(secretKey)}
        onOpenChange={(open) => {
          if (!open) {
            setSecretKey(null)
            setCopied(false)
          }
        }}
      >
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>请立即保存 API Key</DialogTitle>
            <DialogDescription>
              完整 Key 只显示这一次。关闭窗口后无法再次查看，只能重新轮换。
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            <div className="flex items-center gap-2 rounded-lg border bg-muted/50 p-3">
              <code className="min-w-0 flex-1 break-all font-mono text-xs select-all">
                {secretKey?.key}
              </code>
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="shrink-0"
                onClick={() => secretKey?.key && void copyKey(secretKey.key)}
              >
                {copied ? <Check className="text-emerald-500" /> : <Copy />}
                {copied ? "已复制" : "复制"}
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">
              外部系统调用时，可通过 HTTP Header <code className="font-mono">X-API-Key: {secretKey?.key_prefix}...</code> 或 <code className="font-mono">Authorization: Bearer {secretKey?.key_prefix}...</code> 进行身份验证。
            </p>
          </div>
          <DialogFooter>
            <Button
              type="button"
              onClick={() => {
                setSecretKey(null)
                setCopied(false)
              }}
            >
              我已安全保存
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Revoke Confirmation Dialog */}
      <Dialog
        open={Boolean(revokeTarget)}
        onOpenChange={(open) => {
          if (!open) {
            setRevokeTarget(null)
            setRevokeReason("")
          }
        }}
      >
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>撤销 API Key？</DialogTitle>
            <DialogDescription>
              “{revokeTarget?.name}” 撤销后会立即失效，使用该 Key 的服务将无法继续访问 API。
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="revoke-reason">撤销原因（可选）</Label>
            <Input
              id="revoke-reason"
              value={revokeReason}
              onChange={(e) => setRevokeReason(e.target.value)}
              placeholder="例如：凭据泄露或已下线"
              maxLength={200}
            />
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => {
                setRevokeTarget(null)
                setRevokeReason("")
              }}
              disabled={revokePending}
            >
              取消
            </Button>
            <Button
              variant="destructive"
              onClick={() => void handleRevoke()}
              disabled={revokePending}
            >
              {revokePending ? "撤销中..." : "确定撤销"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Rotate Confirmation Dialog */}
      <ConfirmDialog
        open={Boolean(rotateTarget)}
        onOpenChange={(open) => !open && setRotateTarget(null)}
        title="轮换 API Key？"
        description="轮换后旧 Key 会立即失效，并生成一个只能查看一次的新 Key。请先确认外部调用方可以及时切换。"
        confirmLabel="确认轮换"
        pending={rotatePending}
        onConfirm={() => void handleRotate()}
      />

      {/* Events / Audit Log Dialog */}
      <Dialog
        open={Boolean(eventsTarget)}
        onOpenChange={(open) => !open && setEventsTarget(null)}
      >
        <DialogContent className="max-w-xl">
          <DialogHeader>
            <DialogTitle>API Key 审计日志</DialogTitle>
            <DialogDescription>
              “{eventsTarget?.name}” 的生命周期操作记录。
            </DialogDescription>
          </DialogHeader>
          <div className="max-h-96 space-y-3 overflow-y-auto">
            {eventsError && <InlineAlert>{eventsError}</InlineAlert>}
            {eventsLoading ? (
              <div className="flex h-32 items-center justify-center text-muted-foreground">
                <LoaderCircle className="size-5 animate-spin" />
              </div>
            ) : eventsList.length === 0 ? (
              <div className="py-8 text-center text-sm text-muted-foreground">
                暂无事件记录
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>时间</TableHead>
                    <TableHead>事件</TableHead>
                    <TableHead>操作人</TableHead>
                    <TableHead>IP</TableHead>
                    <TableHead>详情</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {eventsList.map((evt) => (
                    <TableRow key={evt.id}>
                      <TableCell className="text-xs text-muted-foreground whitespace-nowrap">
                        {formatDate(evt.created_at)}
                      </TableCell>
                      <TableCell className="whitespace-nowrap">
                        {evt.event_type === "created" ? (
                          <Badge variant="outline" className="text-emerald-600 border-emerald-500/30">创建</Badge>
                        ) : evt.event_type === "rotated" ? (
                          <Badge variant="outline" className="text-blue-600 border-blue-500/30">轮换</Badge>
                        ) : evt.event_type === "revoked" ? (
                          <Badge variant="destructive">撤销</Badge>
                        ) : (
                          <Badge variant="secondary">{evt.event_type}</Badge>
                        )}
                      </TableCell>
                      <TableCell className="text-xs">{evt.actor_username || "-"}</TableCell>
                      <TableCell className="font-mono text-xs text-muted-foreground">
                        {evt.client_ip || "-"}
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {evt.metadata?.reason ? (
                          <span>原因: {String(evt.metadata.reason)}</span>
                        ) : evt.metadata?.replacement_id ? (
                          <span>已替换为 ID #{String(evt.metadata.replacement_id)}</span>
                        ) : evt.metadata?.replacement_for_id ? (
                          <span>替换旧 ID #{String(evt.metadata.replacement_for_id)}</span>
                        ) : (
                          "-"
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setEventsTarget(null)}>
              关闭
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
