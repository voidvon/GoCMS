import { useCallback, useEffect, useRef, useState, type FormEvent } from "react"
import { Download, RefreshCw, Upload } from "lucide-react"
import { activateGeography, downloadGeography, getGeographyStatus, uploadGeography, type GeographyStatus } from "@/lib/api"
import { showError, showSuccess } from "@/components/app/admin-notifications"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Badge } from "@/components/ui/badge"
import { ScrollArea } from "@/components/ui/scroll-area"

const source = "https://download.geonames.org/export/dump/"
export function GeographyPage() {
  const [status, setStatus] = useState<GeographyStatus | null>(null)
  const [busy, setBusy] = useState(false)
  const [failed, setFailed] = useState(false)
  const previousRunning = useRef(false)
  const mounted = useRef(true)
  const reload = useCallback(async () => {
    try {
      const next = await getGeographyStatus()
      if (!mounted.current) return
      if (previousRunning.current && !next.job.running) {
        if (next.job.error) showError(next.job.error)
        else showSuccess("地址目录已更新")
      }
      previousRunning.current = next.job.running
      setStatus(next)
      setFailed(false)
    } catch (error) {
      if (mounted.current) { setFailed(true); showError(error) }
    }
  }, [])
  useEffect(() => {
    mounted.current = true
    void reload()
    return () => { mounted.current = false }
  }, [reload])
  useEffect(() => {
    if (!status?.job.running || failed) return
    const timer = window.setInterval(() => void reload(), 2500)
    return () => window.clearInterval(timer)
  }, [status?.job.running, failed, reload])
  const disabled = busy || Boolean(status?.job.running) || !status
  async function run(action: () => Promise<unknown>, queued = false) {
    if (disabled) return
    setBusy(true)
    try {
      await action()
      if (!mounted.current) return
      if (queued) { previousRunning.current = true; showSuccess("任务已提交，可在此查看进度") }
      else showSuccess("已切换地址目录")
      await reload()
    } catch (error) { if (mounted.current) showError(error) }
    finally { if (mounted.current) setBusy(false) }
  }
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    void run(() => uploadGeography(form), true)
  }
  const current = status?.datasets.find(item => item.active)
  return <ScrollArea className="h-full" contentClassName="space-y-6 pb-8 pe-4">
    <section className="space-y-4 rounded-xl border p-5">
      <div className="flex flex-wrap items-start justify-between gap-3"><div><h2 className="font-medium">共享地址目录</h2><p className="mt-2 text-sm text-muted-foreground">仅超级管理员可更新，更新后供所有站点使用。新目录完整导入后才会启用，失败时继续使用当前目录，历史订单地址不变。</p></div><Button variant="outline" disabled={busy} onClick={() => void reload()}><RefreshCw className="size-4" />刷新</Button></div>
      {!status && <p role="status" className="text-sm text-muted-foreground">{failed ? "地址数据加载失败，请点击刷新重试。" : "正在读取地址目录…"}</p>}
      {current && <p className="text-sm">当前版本：{current.version} · {current.countries.toLocaleString()} 个国家或地区 · {current.regions.toLocaleString()} 个行政区 · {current.cities.toLocaleString()} 个城市</p>}
      {current?.cities === 0 && <p className="text-sm text-muted-foreground">当前仅有内置国家目录，请下载或上传 GeoNames 数据以启用行政区和城市选择。</p>}
      {status?.job.stage && <div role="status" className="rounded-lg bg-muted p-3 text-sm"><p>{status.job.running ? "处理中：" : "最近任务："}{status.job.stage}</p>{status.job.error && <p className="mt-2 break-words text-destructive">{status.job.error}</p>}</div>}
      <Button disabled={disabled} onClick={() => void run(downloadGeography, true)}><Download className="size-4" />下载并更新 GeoNames</Button>
      <p className="text-xs text-muted-foreground">服务端从 GeoNames 官方下载国家、一级行政区和 cities500 城市包，无需 API Key。下载或导入期间仍可使用当前目录；部署环境无法联网时可在下方上传。</p>
    </section>
    <section className="space-y-4 rounded-xl border p-5">
      <h2 className="font-medium">上传地址数据</h2>
      <p className="text-sm text-muted-foreground">使用 GeoNames 原始 TSV 文本及城市 ZIP；上传请求总大小上限 64 MiB。保留新版本与导入前使用的版本。城市包并不覆盖所有居民点，结算页始终支持手动填写。</p>
      <form onSubmit={submit} className="grid gap-4 sm:grid-cols-2">
        <div className="space-y-2 sm:col-span-2"><Label htmlFor="geo-version">版本名称</Label><Input id="geo-version" name="version" required maxLength={80} placeholder="例如 GeoNames 2026-11" disabled={disabled} /></div>
        {[{ name: "countries", label: "国家 countryInfo.txt（可选，留空使用内置国家目录）", file: "countryInfo.txt", accept: ".txt" }, { name: "regions", label: "一级行政区 admin1CodesASCII.txt", file: "admin1CodesASCII.txt", accept: ".txt" }, { name: "cities", label: "城市 cities500.zip 或解压后的 TXT", file: "cities500.zip", accept: ".zip,.txt" }].map(field => <div key={field.name} className="space-y-2"><Label htmlFor={`geo-${field.name}`}>{field.label}</Label><Input id={`geo-${field.name}`} type="file" name={field.name} required={field.name !== "countries"} accept={field.accept} disabled={disabled} /><a className="text-xs underline underline-offset-4" href={`${source}${field.file}`} target="_blank" rel="noreferrer">下载官方文件</a></div>)}
        <div className="sm:col-span-2"><Button type="submit" disabled={disabled}><Upload className="size-4" />上传并导入</Button></div>
      </form>
    </section>
    <section className="space-y-3 rounded-xl border p-5"><h2 className="font-medium">目录版本</h2>{status?.datasets.map(item => <div key={item.id} className="flex flex-wrap items-center justify-between gap-3 border-t pt-3 text-sm"><div className="space-y-1"><p>{item.version} {item.active && <Badge variant="secondary">使用中</Badge>}</p><p className="text-xs text-muted-foreground">{new Date(item.created_at).toLocaleString()} · 国家 {item.countries.toLocaleString()} / 行政区 {item.regions.toLocaleString()} / 城市 {item.cities.toLocaleString()}</p></div>{!item.active && <Button variant="outline" size="sm" disabled={disabled} onClick={() => void run(() => activateGeography(item.id))}>切换到此版本</Button>}</div>)}</section>
    <p className="text-xs text-muted-foreground">地址参考数据来自 <a className="underline" href="https://www.geonames.org/" target="_blank" rel="noreferrer">GeoNames</a>，遵循 <a className="underline" href="https://creativecommons.org/licenses/by/4.0/" target="_blank" rel="noreferrer">CC BY 4.0</a>；本系统将原始数据转换为可搜索目录。该目录用于辅助填写，不提供地址真实性或配送范围校验。</p>
  </ScrollArea>
}
