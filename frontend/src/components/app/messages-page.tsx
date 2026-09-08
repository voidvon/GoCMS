import { useCallback, useEffect, useState } from "react"
import { Check, Inbox, LoaderCircle, MailOpen } from "lucide-react"

import { getMessages, updateMessageState, type MessageItem } from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { InlineAlert, TablePagination } from "@/components/app/app-ui"

const pageSize = 12

function formatDate(value: string) {
  return value ? value.slice(0, 16).replace("T", " ") : "暂无日期"
}

export function MessagesPage() {
  const [items, setItems] = useState<MessageItem[]>([])
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")
  const [selected, setSelected] = useState<MessageItem | null>(null)
  const [updating, setUpdating] = useState(false)

  const load = useCallback(() => {
    getMessages(page, pageSize)
      .then((response) => {
        setItems(response.items)
        setTotal(response.total)
        setError("")
      })
      .catch((loadError) => setError(loadError instanceof Error ? loadError.message : "留言加载失败"))
      .finally(() => setLoading(false))
  }, [page])

  useEffect(() => {
    void load()
  }, [load])

  function changePage(nextPage: number) {
    setLoading(true)
    setPage(nextPage)
  }

  async function markHandled(message: MessageItem) {
    setUpdating(true)
    try {
      await updateMessageState(message.id, message.state ? 0 : 1)
      setItems((current) => current.map((item) => item.id === message.id ? { ...item, state: item.state ? 0 : 1 } : item))
      setSelected((current) => current && current.id === message.id ? { ...current, state: current.state ? 0 : 1 } : current)
    } catch (updateError) {
      setError(updateError instanceof Error ? updateError.message : "更新失败")
    } finally {
      setUpdating(false)
    }
  }

  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  return (
    <div className="space-y-4">
      {error ? <InlineAlert>{error}</InlineAlert> : null}
      <Card>
        <CardHeader className="flex flex-row items-center justify-between border-b">
          <div>
            <CardTitle>客户留言</CardTitle>
            <CardDescription>{total.toLocaleString("zh-CN")} 条记录，按最新提交排序</CardDescription>
          </div>
          <Badge variant="outline" className="gap-1.5"><Inbox />收件箱</Badge>
        </CardHeader>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-4">主题</TableHead>
                <TableHead>联系人</TableHead>
                <TableHead>提交时间</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className="pr-4 text-right">查看</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading ? (
                <TableRow><TableCell colSpan={5} className="h-28 text-center text-muted-foreground"><LoaderCircle className="mx-auto size-5 animate-spin" /></TableCell></TableRow>
              ) : items.length === 0 ? (
                <TableRow><TableCell colSpan={5} className="h-28 text-center text-muted-foreground">暂无留言</TableCell></TableRow>
              ) : items.map((item) => (
                <TableRow key={item.id}>
                  <TableCell className="max-w-[360px] pl-4">
                    <p className="truncate font-medium">{item.title}</p>
                    <p className="mt-1 truncate text-xs text-muted-foreground">{item.content || "无留言内容"}</p>
                  </TableCell>
                  <TableCell><p>{item.name || "未填写"}</p><p className="text-xs text-muted-foreground">{item.phone}</p></TableCell>
                  <TableCell className="text-muted-foreground">{formatDate(item.created_at)}</TableCell>
                  <TableCell><Badge variant={item.state ? "outline" : "secondary"}>{item.state ? "已处理" : "待处理"}</Badge></TableCell>
                  <TableCell className="pr-4 text-right"><Button variant="ghost" size="sm" onClick={() => setSelected(item)}><MailOpen />查看</Button></TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          <TablePagination
            page={page}
            totalPages={totalPages}
            total={total}
            pageSize={pageSize}
            loading={loading}
            onPageChange={changePage}
          />
        </CardContent>
      </Card>

      <Dialog open={Boolean(selected)} onOpenChange={(open) => { if (!open) setSelected(null) }}>
        {selected ? (
          <DialogContent className="max-w-lg">
            <DialogHeader>
              <DialogTitle>{selected.title}</DialogTitle>
              <DialogDescription>{selected.name || "未填写联系人"} · {formatDate(selected.created_at)}</DialogDescription>
            </DialogHeader>
            <div className="space-y-4 text-sm">
              <div className="grid gap-3 rounded-lg border bg-muted/30 p-3 sm:grid-cols-2">
                <div><p className="text-xs text-muted-foreground">电话</p><p className="mt-1">{selected.phone || "未填写"}</p></div>
                <div><p className="text-xs text-muted-foreground">手机</p><p className="mt-1">{selected.mobile || "未填写"}</p></div>
                <div><p className="text-xs text-muted-foreground">邮箱</p><p className="mt-1 break-all">{selected.email || "未填写"}</p></div>
                <div><p className="text-xs text-muted-foreground">地址</p><p className="mt-1">{selected.address || "未填写"}</p></div>
              </div>
              <div className="rounded-lg border p-3 leading-6 whitespace-pre-wrap">{selected.content || "无留言内容"}</div>
            </div>
            <DialogFooter>
              <Button onClick={() => markHandled(selected)} disabled={updating} variant={selected.state ? "outline" : "default"}>
                {updating ? <LoaderCircle className="animate-spin" /> : <Check />}
                {selected.state ? "标记为待处理" : "标记为已处理"}
              </Button>
            </DialogFooter>
          </DialogContent>
        ) : null}
      </Dialog>
    </div>
  )
}
