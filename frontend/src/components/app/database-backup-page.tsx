import { useEffect, useState, type FormEvent } from "react"
import { DatabaseBackup, Download, LoaderCircle, RefreshCw, Trash2 } from "lucide-react"

import { showError, showSuccess } from "@/components/app/admin-notifications"
import { ConfirmDialog } from "@/components/app/app-ui"
import { TimeText } from "@/components/app/time-text"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Switch } from "@/components/ui/switch"
import {
  deleteDatabaseBackupObject,
  getDatabaseBackup,
  getDatabaseBackupObjects,
  prepareDatabaseBackupDownload,
  runDatabaseBackup,
  saveDatabaseBackup,
  testDatabaseBackup,
  type DatabaseBackupObjectList,
  type DatabaseBackupSettings,
  type DatabaseBackupStatus,
} from "@/lib/api"

function backupSettings(status: DatabaseBackupStatus): DatabaseBackupSettings {
  const { endpoint, bucket, region, access_key, automatic, daily_at, time_zone } = status
  return { endpoint, bucket, region, access_key, automatic, daily_at, time_zone }
}

function formatBackupSize(bytes: number) {
  const large = bytes >= 1_000_000
  return new Intl.NumberFormat("zh-CN", {
    style: "unit",
    unit: large ? "megabyte" : "kilobyte",
    maximumFractionDigits: 1,
  }).format(bytes / (large ? 1_000_000 : 1_000))
}

export function DatabaseBackupPage() {
  const [form, setForm] = useState<DatabaseBackupSettings | null>(null)
  const [status, setStatus] = useState<DatabaseBackupStatus | null>(null)
  const [secret, setSecret] = useState("")
  const [pending, setPending] = useState<"save" | "test" | "run" | null>(null)
  const [loadFailed, setLoadFailed] = useState(false)
  const [retry, setRetry] = useState(0)
  const [manualRunAfter, setManualRunAfter] = useState<string | null>(null)
  const [objectList, setObjectList] = useState<DatabaseBackupObjectList | null>(null)
  const [listFailed, setListFailed] = useState(false)
  const [refresh, setRefresh] = useState(0)
  const [downloading, setDownloading] = useState<string | null>(null)
  const [removing, setRemoving] = useState<{ key: string; target: string; name: string } | null>(null)
  const [deleting, setDeleting] = useState(false)
  const running = status?.running ?? false
  const lastRun = status?.last_run

  useEffect(() => {
    let active = true
    getDatabaseBackup().then((data) => {
      if (!active) return
      setStatus(data)
      setForm(backupSettings(data))
    }).catch((error) => {
      if (!active) return
      setLoadFailed(true)
      showError(error, "读取备份设置失败")
    })
    return () => { active = false }
  }, [retry])

  const loaded = status !== null
  useEffect(() => {
    if (!loaded || pending) return
    let active = true
    let timer: number
    async function poll() {
      try {
        const data = await getDatabaseBackup()
        if (!active) return
        setStatus(data)
        if (manualRunAfter !== null && !data.running && data.last_run.at && data.last_run.at !== manualRunAfter) {
          setManualRunAfter(null)
          if (data.last_run.state === "success") showSuccess(data.last_run.message)
          else showError(data.last_run.message)
        }
      } catch (error) {
        if (active && manualRunAfter !== null) {
          setManualRunAfter(null)
          showError(error, "查询备份进度失败")
        }
      } finally {
        if (active) timer = window.setTimeout(poll, running ? 1500 : 15000)
      }
    }
    timer = window.setTimeout(poll, running ? 800 : 15000)
    return () => { active = false; window.clearTimeout(timer) }
  }, [loaded, manualRunAfter, pending, running])

  const configured = status?.has_secret ?? false
  const savedEndpoint = status?.endpoint
  const savedBucket = status?.bucket
  const savedZone = status?.time_zone
  const lastRunAt = lastRun?.at
  useEffect(() => {
    if (!configured) return
    let active = true
    getDatabaseBackupObjects().then((list) => {
      if (!active) return
      setObjectList(list)
      setListFailed(false)
    }).catch((error) => {
      if (!active) return
      setListFailed(true)
      showError(error, "读取备份列表失败")
    })
    return () => { active = false }
  }, [configured, savedEndpoint, savedBucket, savedZone, lastRunAt, refresh])

  async function submit(action: "save" | "test" | "run") {
    if (!form) return
    setPending(action)
    try {
      if (action === "test") {
        await testDatabaseBackup({ ...form, secret })
        showSuccess("S3 连接测试通过")
      } else if (action === "save") {
        const updated = await saveDatabaseBackup({ ...form, secret })
        setStatus(updated)
        setForm(backupSettings(updated))
        setSecret("")
        setObjectList(null)
        setListFailed(false)
        setRefresh((value) => value + 1)
        showSuccess("备份设置已保存")
      } else {
        await runDatabaseBackup()
        setStatus((current) => current && { ...current, running: true })
        setManualRunAfter(status?.last_run.at ?? "")
      }
    } catch (error) {
      showError(error, "备份操作失败")
    } finally {
      setPending(null)
    }
  }

  async function download(key: string, target: string, name: string) {
    setDownloading(key)
    try {
      const link = document.createElement("a")
      link.href = await prepareDatabaseBackupDownload({ key, target })
      link.download = name
      document.body.append(link)
      link.click()
      link.remove()
      showSuccess("已请求下载备份")
    } catch (error) {
      showError(error, "下载备份失败")
    } finally {
      setDownloading(null)
    }
  }

  async function remove() {
    if (!removing) return
    const { key, target } = removing
    setDeleting(true)
    try {
      await deleteDatabaseBackupObject({ key, target })
      setObjectList((list) => list?.target === target
        ? { ...list, items: list.items.filter((item) => item.key !== key) }
        : list)
      setRefresh((value) => value + 1)
      setRemoving(null)
      showSuccess("备份已删除")
    } catch (error) {
      showError(error, "删除备份失败")
    } finally {
      setDeleting(false)
    }
  }

  function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    void submit("save")
  }

  if (!status || !form) {
    return loadFailed
      ? <Button variant="outline" onClick={() => { setLoadFailed(false); setRetry((value) => value + 1) }}>重新加载</Button>
      : <div className="flex justify-center p-8" role="status" aria-label="正在加载备份设置"><LoaderCircle className="size-5 animate-spin" /></div>
  }

  const busy = pending !== null || running
  const storageChanged = form.endpoint.trim().replace(/\/+$/, "") !== status.endpoint || form.access_key.trim() !== status.access_key

  return (
    <div className="mx-auto grid w-full max-w-6xl items-start gap-4 xl:grid-cols-2">
      <Card>
        <CardHeader>
          <CardTitle>备份设置</CardTitle>
          <CardDescription>使用私有存储桶，凭据需有列出、读取、写入和删除对象的权限。</CardDescription>
        </CardHeader>
        <CardContent>
          <form className="space-y-5" onSubmit={save}>
            <fieldset className="grid gap-4 sm:grid-cols-2" disabled={busy}>
              <div className="space-y-2 sm:col-span-2">
                <Label htmlFor="backup-endpoint">S3 Endpoint</Label>
                <Input id="backup-endpoint" type="url" required maxLength={512} value={form.endpoint} placeholder="https://<Account ID>.r2.cloudflarestorage.com" onChange={(e) => setForm({ ...form, endpoint: e.target.value })} />
              </div>
              <div className="space-y-2">
                <Label htmlFor="backup-bucket">存储桶名称</Label>
                <Input id="backup-bucket" required maxLength={63} value={form.bucket} onChange={(e) => setForm({ ...form, bucket: e.target.value })} />
              </div>
              <div className="space-y-2">
                <Label htmlFor="backup-region">区域</Label>
                <Input id="backup-region" required maxLength={63} value={form.region} onChange={(e) => setForm({ ...form, region: e.target.value })} />
                <p className="text-xs text-muted-foreground">Cloudflare R2 使用 auto</p>
              </div>
              <div className="space-y-2">
                <Label htmlFor="backup-access-key">Access Key ID</Label>
                <Input id="backup-access-key" required autoComplete="off" maxLength={128} value={form.access_key} onChange={(e) => setForm({ ...form, access_key: e.target.value })} />
              </div>
              <div className="space-y-2">
                <Label htmlFor="backup-secret">Secret Access Key</Label>
                <Input id="backup-secret" type="password" autoComplete="new-password" maxLength={256} value={secret} required={!configured || storageChanged} placeholder={configured && !storageChanged ? "已保存，留空继续使用" : undefined} onChange={(e) => setSecret(e.target.value)} />
              </div>
            </fieldset>
            <fieldset className="space-y-4 border-t pt-4" disabled={busy}>
              <div className="flex items-center justify-between gap-3">
                <Label htmlFor="backup-automatic">每日自动备份</Label>
                <Switch id="backup-automatic" checked={form.automatic} onCheckedChange={(automatic) => setForm({ ...form, automatic })} />
              </div>
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="backup-time">备份时间</Label>
                  <Input id="backup-time" type="time" required value={form.daily_at} onChange={(e) => setForm({ ...form, daily_at: e.target.value })} />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="backup-time-zone">时区</Label>
                  <Input id="backup-time-zone" required maxLength={128} value={form.time_zone} placeholder="Asia/Shanghai" onChange={(e) => setForm({ ...form, time_zone: e.target.value })} />
                  <p className="text-xs text-muted-foreground">Local 使用服务器时区</p>
                </div>
              </div>
            </fieldset>
            <div className="flex flex-wrap gap-2">
              <Button type="button" variant="outline" disabled={busy || !form.endpoint.trim()} onClick={() => void submit("test")}>
                {pending === "test" && <LoaderCircle className="animate-spin" />}测试连接
              </Button>
              <Button type="submit" disabled={busy}>
                {pending === "save" && <LoaderCircle className="animate-spin" />}保存设置
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>备份记录</CardTitle>
          <CardDescription>手动和自动备份都会先回收业务数据库快照中的空闲空间，再压缩上传。独立的访问日志库不包含在此备份中。保留最新 10 份，以及过去 36 个已结束的自然月各自的最后一份。月份按设置的时区计算。</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex flex-wrap items-center gap-2">
            <Button disabled={!configured || busy} onClick={() => void submit("run")}>
              {running || pending === "run" ? <LoaderCircle className="animate-spin" /> : <DatabaseBackup />}
              {running ? "备份中" : "立即备份"}
            </Button>
            {configured && (
              <Button variant="outline" disabled={objectList === null && !listFailed} onClick={() => {
                setObjectList(null)
                setListFailed(false)
                setRefresh((value) => value + 1)
              }}>
                <RefreshCw />刷新列表
              </Button>
            )}
          </div>
          <div role="status" className="space-y-1 text-sm">
            {running ? <p>正在创建或上传数据库快照</p> : lastRun?.at ? (
              <>
                <p>
                  <Badge variant={lastRun.state === "failed" ? "destructive" : "secondary"}>
                    {lastRun.state === "success" ? "成功" : lastRun.state === "warning" ? "清理未完成" : "失败"}
                  </Badge>{" "}
                  <span className="text-muted-foreground">
                    {lastRun.source === "automatic" ? "自动备份" : "手动备份"} · <TimeText value={lastRun.at} mode="datetime" />
                  </span>
                </p>
                {lastRun.state !== "success" && <p className="text-muted-foreground">{lastRun.message}</p>}
              </>
            ) : <p className="text-muted-foreground">{configured ? "尚无备份任务记录" : "保存存储设置后即可开始备份"}</p>}
          </div>
          {configured && (
            <div className="space-y-3 border-t pt-4">
              <div className="text-xs text-muted-foreground">
                <p>存储目录</p>
                <p className="mt-1 font-mono [overflow-wrap:anywhere]">{status.prefix}</p>
              </div>
              {listFailed ? null : objectList === null ? (
                <div className="p-2" role="status" aria-label="正在读取备份列表"><LoaderCircle className="size-4 animate-spin" /></div>
              ) : objectList.items.length === 0 ? <p className="text-sm text-muted-foreground">存储桶中暂无备份</p> : (
                <ScrollArea className="max-h-[26rem] rounded-lg border">
                  <ul className="divide-y">
                    {objectList.items.map((item) => {
                      const name = item.key.slice(item.key.lastIndexOf("/") + 1)
                      return (
                        <li key={item.key} className="grid gap-2 px-3 py-2 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center">
                          <div className="space-y-1">
                            <p className="font-mono text-xs [overflow-wrap:anywhere]">{name}</p>
                            <p className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                              <TimeText value={item.at} mode="datetime" /> · {formatBackupSize(item.size)}
                              {item.recent && <Badge variant="secondary">近期</Badge>}
                              {item.monthly && <Badge variant="outline">月度</Badge>}
                            </p>
                          </div>
                          <div className="flex flex-wrap gap-1">
                            <Button variant="outline" size="sm" aria-label={`下载 ${name}`} disabled={downloading !== null}
                              onClick={() => void download(item.key, objectList.target, name)}>
                              {downloading === item.key ? <LoaderCircle className="animate-spin" /> : <Download />}下载
                            </Button>
                            <Button variant="ghost" size="sm" aria-label={`删除 ${name}`} disabled={busy || deleting}
                              onClick={() => setRemoving({ key: item.key, target: objectList.target, name })}>
                              <Trash2 />删除
                            </Button>
                          </div>
                        </li>
                      )
                    })}
                  </ul>
                </ScrollArea>
              )}
            </div>
          )}
        </CardContent>
      </Card>
      <ConfirmDialog
        open={removing !== null}
        onOpenChange={(open) => {
          if (!open && !deleting) setRemoving(null)
        }}
        title="删除备份"
        description={<>
          <span className="mb-2 block font-mono [overflow-wrap:anywhere]">{removing?.name}</span>
          这份备份将从存储桶中删除，删除后无法在后台撤销。
        </>}
        confirmLabel="确认删除"
        pending={deleting}
        onConfirm={() => void remove()}
      />
    </div>
  )
}
