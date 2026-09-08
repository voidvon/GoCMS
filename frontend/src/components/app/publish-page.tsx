import { useEffect, useState } from "react"
import { Globe, LoaderCircle, RefreshCw } from "lucide-react"
import { getPublication, publishSite, type Publication } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "@/components/ui/card"
import { InlineAlert } from "@/components/app/app-ui"

const labels = { idle: "尚未发布", running: "正在生成", success: "发布成功", failed: "发布失败" }
export function PublishPage() {
 const [report, setReport] = useState<Publication | null>(null)
 const [error,setError] = useState("")
 const [starting,setStarting] = useState(false)
 useEffect(() => {
  let active = true
  const refresh = () => getPublication().then(r => {if(active) {setReport(r);setError("")}}).catch(e => {if(active) setError(String(e))})
  void refresh();const timer = setInterval(refresh,2000)
  return () => {active=false;clearInterval(timer)}
 },[])
 async function publish() {
  setStarting(true);setError("")
  try {setReport(await publishSite())} catch(e){setError(e instanceof Error ? e.message : "发布失败")} finally {setStarting(false)}
 }
 const running = starting || report?.state === "running"
 return <div className="space-y-4">
  {error && <InlineAlert>{error}</InlineAlert>}
  {report?.error && <InlineAlert>{report.error}</InlineAlert>}
  <Card><CardHeader><CardTitle>网站发布</CardTitle><CardDescription>将已保存的内容生成网页，成功后更新公开站点。生成期间可继续访问已发布页面。</CardDescription></CardHeader>
   <CardContent className="space-y-5">
    <div className="flex flex-wrap items-center gap-3"><Badge variant={report?.state === "failed" ? "destructive" : "secondary"}>{report ? labels[report.state] : "加载中"}</Badge>
    <Button disabled={!report || running} onClick={publish}>{running ? <LoaderCircle className="animate-spin" /> : <RefreshCw />}{running ? "生成中…" : "全站重新生成"}</Button>
    <Button variant="outline" render={<a href={import.meta.env.DEV ? "http://127.0.0.1:18080/" : "/"} target="_blank" rel="noreferrer" />}><Globe />查看网站</Button></div>
    <p className="text-sm text-muted-foreground">覆盖首页、产品详情、产品分类与分页、新闻、技术文章、公司介绍、招聘、联系页面和网站地图。</p>
    {report?.state === "success" && <div className="grid gap-4 sm:grid-cols-3"><div><p className="text-sm text-muted-foreground">生成文件</p><p className="text-2xl font-semibold">{report.files}</p></div><div><p className="text-sm text-muted-foreground">公开产品</p><p className="text-2xl font-semibold">{report.products}</p></div><div><p className="text-sm text-muted-foreground">新闻与技术文章</p><p className="text-2xl font-semibold">{report.news}</p></div></div>}
    {report && report.finished && !report.finished.startsWith("0001") && <p className="text-xs text-muted-foreground">最近完成：{new Date(report.finished).toLocaleString("zh-CN")}</p>}
   </CardContent></Card>
 </div>
}
