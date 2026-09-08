import { useEffect, useState, type FormEvent } from "react"
import { BookOpenText, LoaderCircle, Search } from "lucide-react"

import { getNews, type NewsItem } from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { InlineAlert, SearchField, TablePagination } from "@/components/app/app-ui"

const pageSize = 12

function formatDate(value: string) {
  return value ? value.slice(0, 10).replaceAll("-", "/") : "暂无日期"
}

export function NewsPage() {
  const [items, setItems] = useState<NewsItem[]>([])
  const [query, setQuery] = useState("")
  const [appliedQuery, setAppliedQuery] = useState("")
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")

  useEffect(() => {
    let active = true
    getNews(page, pageSize, appliedQuery)
      .then((response) => {
        if (!active) return
        setItems(response.items)
        setTotal(response.total)
        setError("")
      })
      .catch((loadError) => {
        if (active) setError(loadError instanceof Error ? loadError.message : "新闻加载失败")
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [appliedQuery, page])

  function submitSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const nextQuery = query.trim()
    if (page === 1 && appliedQuery === nextQuery) return
    setLoading(true)
    setPage(1)
    setAppliedQuery(nextQuery)
  }

  function changePage(nextPage: number) {
    setLoading(true)
    setPage(nextPage)
  }

  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  return (
    <div className="space-y-4">
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-center">
        <form className="flex w-full max-w-md gap-2" onSubmit={submitSearch}>
          <SearchField value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索新闻标题" />
          <Button type="submit" variant="outline"><Search />搜索</Button>
        </form>
        <Badge variant="outline" className="w-fit gap-1.5"><BookOpenText />内容只读</Badge>
      </div>
      {error ? <InlineAlert>{error}</InlineAlert> : null}
      <Card>
        <CardHeader className="border-b">
          <div>
            <CardTitle>新闻列表</CardTitle>
            <CardDescription>{total.toLocaleString("zh-CN")} 条记录</CardDescription>
          </div>
        </CardHeader>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-4">标题</TableHead>
                <TableHead>发布日期</TableHead>
                <TableHead>推荐</TableHead>
                <TableHead className="pr-4 text-right">编号</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading ? (
                <TableRow><TableCell colSpan={4} className="h-28 text-center text-muted-foreground"><LoaderCircle className="mx-auto size-5 animate-spin" /></TableCell></TableRow>
              ) : items.length === 0 ? (
                <TableRow><TableCell colSpan={4} className="h-28 text-center text-muted-foreground">没有匹配的新闻</TableCell></TableRow>
              ) : items.map((item) => (
                <TableRow key={item.id}>
                  <TableCell className="max-w-[620px] pl-4">
                    <p className="truncate font-medium">{item.title}</p>
                    <p className="mt-1 text-xs text-muted-foreground">新闻分类 #{item.category_id}</p>
                  </TableCell>
                  <TableCell className="text-muted-foreground">{formatDate(item.published_at)}</TableCell>
                  <TableCell><Badge variant={item.featured ? "secondary" : "outline"}>{item.featured ? "首页推荐" : "普通"}</Badge></TableCell>
                  <TableCell className="pr-4 text-right text-muted-foreground">#{item.id}</TableCell>
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
    </div>
  )
}
