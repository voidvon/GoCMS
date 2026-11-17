import { useEffect, useRef, useState } from "react"
import { Copy, FileText, RefreshCw } from "lucide-react"
import { getSecurityNginxFile, type SecurityNginxFile } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { ScrollArea } from "@/components/ui/scroll-area"
import { showError, showSuccess } from "./admin-notifications"
import { TimeText } from "./time-text"

export function NginxConfigDialog() {
  const [open, setOpen] = useState(false)
  const [file, setFile] = useState<SecurityNginxFile | null>(null)
  const [loading, setLoading] = useState(false)
  const [copying, setCopying] = useState(false)
  const sequence = useRef(0)
  const mounted = useRef(true)

  useEffect(() => {
    mounted.current = true
    return () => { mounted.current = false }
  }, [])

  function changeOpen(value: boolean) {
    setOpen(value)
    if (!value) {
      sequence.current++
      setFile(null)
      setLoading(false)
    }
  }

  async function readFile() {
    const current = ++sequence.current
    setLoading(true)
    try {
      const value = await getSecurityNginxFile()
      if (mounted.current && current === sequence.current) setFile(value)
    } catch (error) {
      if (mounted.current && current === sequence.current) showError(error, "读取配置文件失败")
    } finally {
      if (mounted.current && current === sequence.current) setLoading(false)
    }
  }

  async function copyFile() {
    if (!file || copying) return
    setCopying(true)
    try {
      await navigator.clipboard.writeText(file.content)
      if (mounted.current) showSuccess("完整配置已复制")
    } catch (error) {
      if (mounted.current) showError(error, "复制配置失败")
    } finally {
      if (mounted.current) setCopying(false)
    }
  }

  return <>
    <Button type="button" variant="outline" onClick={() => { changeOpen(true); void readFile() }}><FileText className="size-4" />查看完整配置</Button>
    <Dialog open={open} onOpenChange={changeOpen}><DialogContent className="h-[80dvh] min-h-0 min-w-0 grid-rows-[auto_auto_minmax(0,1fr)_auto] overflow-hidden sm:max-w-5xl">
      <DialogHeader><DialogTitle>完整配置文件</DialogTitle><DialogDescription className="break-all">{file ? file.path : "读取服务器上已保存的 GoCMS 安全规则文件全文。"}</DialogDescription></DialogHeader>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-xs text-muted-foreground">{loading ? <span role="status">正在读取配置文件…</span> : file ? <>文件修改时间：<TimeText value={file.modified_at} /></> : "配置文件尚未加载"}</p>
        <Button type="button" variant="outline" size="sm" disabled={loading} onClick={() => void readFile()}><RefreshCw className={loading ? "size-4 animate-spin" : "size-4"} />重新读取</Button>
      </div>
      <ScrollArea orientation="both" className="min-h-0 rounded-lg border bg-muted/40" contentClassName="p-4" aria-label="配置文件全文" aria-busy={loading}>
        {file ? file.content ? <pre className="m-0 font-mono text-xs leading-5"><code>{file.content}</code></pre> : <p className="text-sm text-muted-foreground">文件为空</p> : <p className="text-sm text-muted-foreground">{loading ? "读取中…" : "可点击“重新读取”查看配置。"}</p>}
      </ScrollArea>
      <DialogFooter><Button type="button" variant="outline" onClick={() => changeOpen(false)}>关闭</Button><Button type="button" disabled={!file || loading || copying} onClick={() => void copyFile()}><Copy className="size-4" />{copying ? "复制中…" : "复制全部"}</Button></DialogFooter>
    </DialogContent></Dialog>
  </>
}
