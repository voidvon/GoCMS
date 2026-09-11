import { useCallback, useEffect, useState } from "react"
import {
  Check,
  Code2,
  Copy,
  Inbox,
  LoaderCircle,
  MailOpen,
  MessageSquare,
  Plus,
  Settings2,
  Trash2,
} from "lucide-react"

import {
  batchDeleteFeedbacks,
  deleteFeedback,
  deleteFeedbackClass,
  deleteFeedbackField,
  getFeedbackClasses,
  getFeedbackFields,
  getFeedbacks,
  saveFeedbackClass,
  saveFeedbackField,
  updateFeedbackState,
  type FeedbackClass,
  type FeedbackField,
  type FeedbackItem,
} from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Textarea } from "@/components/ui/textarea"
import { ConfirmDialog, IconButton, InlineAlert, TablePagination } from "@/components/app/app-ui"
import { ScrollArea } from "@/components/ui/scroll-area"

const pageSize = 20

function formatDate(value: string) {
  return value ? value.slice(0, 16).replace("T", " ") : "暂无日期"
}

export function MessagesPage() {
  const [activeTab, setActiveTab] = useState("feedbacks")
  const [error, setError] = useState("")

  // Feedbacks State
  const [items, setItems] = useState<FeedbackItem[]>([])
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [filterClass, setFilterClass] = useState<string>("all")
  const [filterState, setFilterState] = useState<string>("all")
  const [keyword, setKeyword] = useState("")
  const [selectedIds, setSelectedIds] = useState<number[]>([])

  // Feedback Item Dialogs
  const [viewingItem, setViewingItem] = useState<FeedbackItem | null>(null)
  const [updatingState, setUpdatingState] = useState(false)
  const [deletingItem, setDeletingItem] = useState<FeedbackItem | null>(null)
  const [batchDeleting, setBatchDeleting] = useState(false)

  // Classes State
  const [classes, setClasses] = useState<FeedbackClass[]>([])
  const [editingClass, setEditingClass] = useState<Partial<FeedbackClass> | null>(null)
  const [classDialogOpen, setClassDialogOpen] = useState(false)
  const [classSaving, setClassSaving] = useState(false)
  const [classDeleting, setClassDeleting] = useState<FeedbackClass | null>(null)
  const [codeModalClass, setCodeModalClass] = useState<FeedbackClass | null>(null)
  const [copiedCode, setCopiedCode] = useState(false)

  // Fields State
  const [fields, setFields] = useState<FeedbackField[]>([])
  const [editingField, setEditingField] = useState<Partial<FeedbackField> | null>(null)
  const [fieldDialogOpen, setFieldDialogOpen] = useState(false)
  const [fieldSaving, setFieldSaving] = useState(false)
  const [fieldDeleting, setFieldDeleting] = useState<FeedbackField | null>(null)

  // Load Feedbacks
  const loadFeedbacks = useCallback(() => {
    setLoading(true)
    const classId = filterClass !== "all" ? Number(filterClass) : undefined
    const state = filterState !== "all" ? filterState : undefined
    getFeedbacks(page, pageSize, classId, state, keyword)
      .then((res) => {
        setItems(res.items)
        setTotal(res.total)
        setError("")
      })
      .catch((err) => setError(err instanceof Error ? err.message : "加载反馈失败"))
      .finally(() => setLoading(false))
  }, [page, filterClass, filterState, keyword])

  // Load Classes & Fields
  const loadClassesAndFields = useCallback(async () => {
    try {
      const [cList, fList] = await Promise.all([getFeedbackClasses(), getFeedbackFields()])
      setClasses(cList)
      setFields(fList)
    } catch (err) {
      setError(err instanceof Error ? err.message : "加载分类与字段配置失败")
    }
  }, [])

  useEffect(() => {
    void loadFeedbacks()
  }, [loadFeedbacks])

  useEffect(() => {
    void loadClassesAndFields()
  }, [loadClassesAndFields])

  // Feedback Actions
  async function markHandled(item: FeedbackItem) {
    setUpdatingState(true)
    try {
      const newState = item.state ? 0 : 1
      await updateFeedbackState(item.id, newState)
      setItems((prev) =>
        prev.map((i) => (i.id === item.id ? { ...i, state: newState } : i)),
      )
      setViewingItem((prev) => (prev && prev.id === item.id ? { ...prev, state: newState } : prev))
    } catch (err) {
      setError(err instanceof Error ? err.message : "更新处理状态失败")
    } finally {
      setUpdatingState(false)
    }
  }

  async function confirmDeleteFeedback() {
    if (!deletingItem) return
    try {
      await deleteFeedback(deletingItem.id)
      setItems((prev) => prev.filter((i) => i.id !== deletingItem.id))
      setTotal((prev) => Math.max(0, prev - 1))
      setDeletingItem(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : "删除失败")
    }
  }

  async function confirmBatchDelete() {
    if (selectedIds.length === 0) return
    try {
      await batchDeleteFeedbacks(selectedIds)
      setSelectedIds([])
      setBatchDeleting(false)
      loadFeedbacks()
    } catch (err) {
      setError(err instanceof Error ? err.message : "批量删除失败")
    }
  }

  // Class Actions
  function openCreateClass() {
    setEditingClass({
      name: "",
      description: "",
      fields_config: fields.map((f) => ({ field: f.field_name, label: f.field_label })),
      must_fields: ["title", "name", "phone"],
      sort_order: 10,
    })
    setClassDialogOpen(true)
  }

  function openEditClass(c: FeedbackClass) {
    setEditingClass({
      ...c,
      fields_config: c.fields_config ? [...c.fields_config] : [],
      must_fields: c.must_fields ? [...c.must_fields] : [],
    })
    setClassDialogOpen(true)
  }

  async function handleSaveClass() {
    if (!editingClass || !editingClass.name) return
    setClassSaving(true)
    try {
      await saveFeedbackClass(editingClass)
      setClassDialogOpen(false)
      await loadClassesAndFields()
    } catch (err) {
      setError(err instanceof Error ? err.message : "保存分类失败")
    } finally {
      setClassSaving(false)
    }
  }

  async function confirmDeleteClass() {
    if (!classDeleting) return
    try {
      await deleteFeedbackClass(classDeleting.id)
      setClassDeleting(null)
      await loadClassesAndFields()
    } catch (err) {
      setError(err instanceof Error ? err.message : "删除分类失败")
    }
  }

  // Field Actions
  function openCreateField() {
    setEditingField({
      field_name: "",
      field_label: "",
      field_type: "text",
      field_options: "",
      description: "",
      sort_order: 50,
    })
    setFieldDialogOpen(true)
  }

  function openEditField(f: FeedbackField) {
    setEditingField({ ...f })
    setFieldDialogOpen(true)
  }

  async function handleSaveField() {
    if (!editingField || !editingField.field_name || !editingField.field_label) return
    setFieldSaving(true)
    try {
      await saveFeedbackField(editingField)
      setFieldDialogOpen(false)
      await loadClassesAndFields()
    } catch (err) {
      setError(err instanceof Error ? err.message : "保存字段失败")
    } finally {
      setFieldSaving(false)
    }
  }

  async function confirmDeleteField() {
    if (!fieldDeleting) return
    try {
      await deleteFeedbackField(fieldDeleting.id)
      setFieldDeleting(null)
      await loadClassesAndFields()
    } catch (err) {
      setError(err instanceof Error ? err.message : "删除字段失败")
    }
  }

  // Generate HTML Form snippet for a class
  function generateFormHTML(c: FeedbackClass) {
    const activeConfigs = c.fields_config || []
    const lines = [
      `<!-- 帝国CMS自定义反馈表单代码 (${c.name}) -->`,
      `<form id="feedback-form" method="post" action="/api/feedback">`,
      `  <input type="hidden" name="class_id" value="${c.id}">`,
    ]
    activeConfigs.forEach((cfg) => {
      const isReq = c.must_fields?.includes(cfg.field) ? ' required' : ''
      const star = isReq ? ' *' : ''
      if (cfg.field === 'content') {
        lines.push(`  <p><label>${cfg.label}${star}</label><textarea name="${cfg.field}" rows="5"${isReq}></textarea></p>`)
      } else {
        lines.push(`  <p><label>${cfg.label}${star}</label><input type="text" name="${cfg.field}"${isReq}></p>`)
      }
    })
    lines.push(`  <p><button type="submit">提交反馈</button></p>`)
    lines.push(`</form>`)
    return lines.join('\n')
  }

  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  return (
    <div className="space-y-4">
      {error ? <InlineAlert>{error}</InlineAlert> : null}

      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">信息反馈管理</h1>
          <p className="text-sm text-muted-foreground">
            对标帝国CMS自定义反馈系统：多分类表单、自定义录入字段与客户反馈处理
          </p>
        </div>
      </div>

      <Tabs value={activeTab} onValueChange={setActiveTab} className="space-y-4">
        <TabsList>
          <TabsTrigger value="feedbacks" className="gap-2">
            <Inbox className="size-4" />
            反馈信息列表
          </TabsTrigger>
          <TabsTrigger value="classes" className="gap-2">
            <MessageSquare className="size-4" />
            反馈分类管理
          </TabsTrigger>
          <TabsTrigger value="fields" className="gap-2">
            <Settings2 className="size-4" />
            反馈字段管理
          </TabsTrigger>
        </TabsList>

        {/* Tab 1: Feedback Records */}
        <TabsContent value="feedbacks">
          <Card>
            <CardHeader className="flex flex-col gap-4 border-b pb-4 sm:flex-row sm:items-center sm:justify-between">
              <div>
                <CardTitle>反馈与留言列表</CardTitle>
                <CardDescription>
                  {total.toLocaleString("zh-CN")} 条反馈记录，按最新提交排序
                </CardDescription>
              </div>

              <div className="flex flex-wrap items-center gap-2">
                <Select value={filterClass} onValueChange={(val) => setFilterClass(val ?? "all")}>
                  <SelectTrigger className="w-[140px]">
                    <SelectValue placeholder="全部分类">
                      {(val) => {
                        if (!val || val === "all") return "全部分类"
                        const c = classes.find((item) => String(item.id) === String(val))
                        return c ? c.name : "全部分类"
                      }}
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="all">全部分类</SelectItem>
                    {classes.map((c) => (
                      <SelectItem key={c.id} value={String(c.id)}>
                        {c.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>

                <Select value={filterState} onValueChange={(val) => setFilterState(val ?? "all")}>
                  <SelectTrigger className="w-[120px]">
                    <SelectValue placeholder="处理状态">
                      {(val) => {
                        if (val === "0") return "待处理"
                        if (val === "1") return "已处理"
                        return "全部状态"
                      }}
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="all">全部状态</SelectItem>
                    <SelectItem value="0">待处理</SelectItem>
                    <SelectItem value="1">已处理</SelectItem>
                  </SelectContent>
                </Select>

                <Input
                  className="w-[160px]"
                  placeholder="搜索标题/姓名/电话/IP"
                  value={keyword}
                  onChange={(e) => setKeyword(e.target.value)}
                />

                {selectedIds.length > 0 ? (
                  <Button
                    variant="destructive"
                    size="sm"
                    className="gap-1.5"
                    onClick={() => setBatchDeleting(true)}
                  >
                    <Trash2 className="size-4" />
                    批量删除 ({selectedIds.length})
                  </Button>
                ) : null}
              </div>
            </CardHeader>

            <CardContent className="p-0">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-10 pl-4">
                      <input
                        type="checkbox"
                        checked={items.length > 0 && selectedIds.length === items.length}
                        onChange={(e) => {
                          if (e.target.checked) {
                            setSelectedIds(items.map((i) => i.id))
                          } else {
                            setSelectedIds([])
                          }
                        }}
                      />
                    </TableHead>
                    <TableHead>主题</TableHead>
                    <TableHead>所属分类</TableHead>
                    <TableHead>提交人 / 联系方式</TableHead>
                    <TableHead>提交时间</TableHead>
                    <TableHead>访客 IP</TableHead>
                    <TableHead>状态</TableHead>
                    <TableHead className="pr-4 text-right">操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {loading ? (
                    <TableRow>
                      <TableCell colSpan={8} className="h-28 text-center text-muted-foreground">
                        <LoaderCircle className="mx-auto size-5 animate-spin" />
                      </TableCell>
                    </TableRow>
                  ) : items.length === 0 ? (
                    <TableRow>
                      <TableCell colSpan={8} className="h-28 text-center text-muted-foreground">
                        暂无反馈记录
                      </TableCell>
                    </TableRow>
                  ) : (
                    items.map((item) => (
                      <TableRow key={item.id}>
                        <TableCell className="pl-4">
                          <input
                            type="checkbox"
                            checked={selectedIds.includes(item.id)}
                            onChange={(e) => {
                              if (e.target.checked) {
                                setSelectedIds((prev) => [...prev, item.id])
                              } else {
                                setSelectedIds((prev) => prev.filter((id) => id !== item.id))
                              }
                            }}
                          />
                        </TableCell>
                        <TableCell className="max-w-[280px]">
                          <p className="truncate font-medium">{item.title}</p>
                          <p className="mt-0.5 truncate text-xs text-muted-foreground">
                            {item.content || "无留言内容"}
                          </p>
                        </TableCell>
                        <TableCell>
                          <Badge variant="outline">{item.class_name || "默认分类"}</Badge>
                        </TableCell>
                        <TableCell>
                          <p>{item.name || "未填写"}</p>
                          <p className="text-xs text-muted-foreground">{item.phone}</p>
                        </TableCell>
                        <TableCell className="text-xs text-muted-foreground">
                          {formatDate(item.created_at)}
                        </TableCell>
                        <TableCell className="text-xs text-muted-foreground">
                          {item.ip || "—"}
                        </TableCell>
                        <TableCell>
                          <Badge variant={item.state ? "outline" : "secondary"}>
                            {item.state ? "已处理" : "待处理"}
                          </Badge>
                        </TableCell>
                        <TableCell className="pr-4 text-right">
                          <div className="flex justify-end gap-1">
                            <Button variant="ghost" size="sm" onClick={() => setViewingItem(item)}>
                              <MailOpen className="size-4" />
                              查看
                            </Button>
                            <IconButton
                              variant="ghost"
                              size="icon-sm"
                              className="text-destructive hover:text-destructive"
                              onClick={() => setDeletingItem(item)}
                              label={`删除${item.title}`}
                            >
                              <Trash2 />
                            </IconButton>
                          </div>
                        </TableCell>
                      </TableRow>
                    ))
                  )}
                </TableBody>
              </Table>
              <TablePagination
                page={page}
                totalPages={totalPages}
                total={total}
                pageSize={pageSize}
                loading={loading}
                onPageChange={setPage}
              />
            </CardContent>
          </Card>
        </TabsContent>

        {/* Tab 2: Feedback Classes */}
        <TabsContent value="classes">
          <Card>
            <CardHeader className="flex flex-row items-center justify-between border-b pb-4">
              <div>
                <CardTitle>反馈分类管理</CardTitle>
                <CardDescription>
                  针对不同业务场景配置独立的反馈表单、录入项与必填验证规则
                </CardDescription>
              </div>
              <Button onClick={openCreateClass} className="gap-1.5" size="sm">
                <Plus className="size-4" />
                新建反馈分类
              </Button>
            </CardHeader>
            <CardContent className="p-0">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="pl-4">分类名称</TableHead>
                    <TableHead>说明</TableHead>
                    <TableHead>启用的字段数</TableHead>
                    <TableHead>必填校验数</TableHead>
                    <TableHead>排序</TableHead>
                    <TableHead>累计反馈</TableHead>
                    <TableHead className="pr-4 text-right">操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {classes.map((c) => {
                    const fieldsCount = c.fields_config ? c.fields_config.length : 0
                    const mustCount = c.must_fields ? c.must_fields.length : 0
                    return (
                      <TableRow key={c.id}>
                        <TableCell className="pl-4 font-medium">{c.name}</TableCell>
                        <TableCell className="text-muted-foreground">{c.description || "无"}</TableCell>
                        <TableCell>{fieldsCount} 项</TableCell>
                        <TableCell>{mustCount} 项</TableCell>
                        <TableCell>{c.sort_order}</TableCell>
                        <TableCell>{c.item_count ?? 0} 条</TableCell>
                        <TableCell className="pr-4 text-right">
                          <div className="flex justify-end gap-1">
                            <Button
                              variant="ghost"
                              size="sm"
                              className="gap-1 text-xs"
                              onClick={() => {
                                setCodeModalClass(c)
                                setCopiedCode(false)
                              }}
                            >
                              <Code2 className="size-3.5" />
                              表单代码
                            </Button>
                            <Button variant="ghost" size="sm" onClick={() => openEditClass(c)}>
                              配置
                            </Button>
                            {c.id !== 1 ? (
                              <IconButton
                                variant="ghost"
                                size="icon-sm"
                                className="text-destructive hover:text-destructive"
                                onClick={() => setClassDeleting(c)}
                                label={`删除${c.name}`}
                              >
                                <Trash2 />
                              </IconButton>
                            ) : null}
                          </div>
                        </TableCell>
                      </TableRow>
                    )
                  })}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </TabsContent>

        {/* Tab 3: Feedback Fields */}
        <TabsContent value="fields">
          <Card>
            <CardHeader className="flex flex-row items-center justify-between border-b pb-4">
              <div>
                <CardTitle>反馈字段管理</CardTitle>
                <CardDescription>
                  管理全站反馈系统可选用的字段池，支持单行、多行、单选、下拉等多种输入控件
                </CardDescription>
              </div>
              <Button onClick={openCreateField} className="gap-1.5" size="sm">
                <Plus className="size-4" />
                新建反馈字段
              </Button>
            </CardHeader>
            <CardContent className="p-0">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="pl-4">排序</TableHead>
                    <TableHead>字段英文名</TableHead>
                    <TableHead>字段标识 (中文)</TableHead>
                    <TableHead>控件类型</TableHead>
                    <TableHead>说明</TableHead>
                    <TableHead>属性</TableHead>
                    <TableHead className="pr-4 text-right">操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {fields.map((f) => (
                    <TableRow key={f.id}>
                      <TableCell className="pl-4">{f.sort_order}</TableCell>
                      <TableCell>
                        <code className="rounded bg-muted px-1.5 py-0.5 text-xs font-semibold">{f.field_name}</code>
                      </TableCell>
                      <TableCell className="font-medium">{f.field_label}</TableCell>
                      <TableCell>
                        <Badge variant="outline">{f.field_type}</Badge>
                      </TableCell>
                      <TableCell className="text-muted-foreground">{f.description || "无"}</TableCell>
                      <TableCell>
                        {f.is_system ? <Badge variant="secondary">系统内置</Badge> : <Badge variant="outline">自定义</Badge>}
                      </TableCell>
                      <TableCell className="pr-4 text-right">
                        <div className="flex justify-end gap-1">
                          <Button variant="ghost" size="sm" onClick={() => openEditField(f)}>
                            编辑
                          </Button>
                          {!f.is_system ? (
                            <IconButton
                              variant="ghost"
                              size="icon-sm"
                              className="text-destructive hover:text-destructive"
                              onClick={() => setFieldDeleting(f)}
                              label={`删除${f.field_label}`}
                            >
                              <Trash2 />
                            </IconButton>
                          ) : null}
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>

      {/* Dialog: View Feedback Item Detail */}
      <Dialog open={Boolean(viewingItem)} onOpenChange={(open) => { if (!open) setViewingItem(null) }}>
        {viewingItem ? (
          <DialogContent className="max-w-lg">
            <DialogHeader>
              <DialogTitle>{viewingItem.title}</DialogTitle>
              <DialogDescription>
                {viewingItem.name || "未填写联系人"} · {formatDate(viewingItem.created_at)} · IP: {viewingItem.ip || "未知"}
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-4 text-sm">
              <div className="grid gap-3 rounded-lg border bg-muted/30 p-3 sm:grid-cols-2">
                <div>
                  <p className="text-xs text-muted-foreground">分类</p>
                  <p className="mt-1 font-medium">{viewingItem.class_name || "默认分类"}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">联系电话</p>
                  <p className="mt-1">{viewingItem.phone || "未填写"}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">电子邮箱</p>
                  <p className="mt-1 break-all">{viewingItem.email || "未填写"}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">联系地址</p>
                  <p className="mt-1">{viewingItem.address || "未填写"}</p>
                </div>
              </div>

              {/* Extra Custom Fields */}
              {viewingItem.extra_data && Object.keys(viewingItem.extra_data).length > 0 ? (
                <div className="space-y-2 rounded-lg border bg-muted/20 p-3">
                  <p className="text-xs font-semibold text-muted-foreground">扩展自定义字段</p>
                  <div className="grid gap-2 sm:grid-cols-2">
                    {Object.entries(viewingItem.extra_data).map(([k, v]) => (
                      <div key={k}>
                        <p className="text-xs text-muted-foreground">{k}</p>
                        <p className="mt-0.5 text-sm">{String(v)}</p>
                      </div>
                    ))}
                  </div>
                </div>
              ) : null}

              <div className="rounded-lg border p-3 leading-6 whitespace-pre-wrap">
                {viewingItem.content || "无反馈内容"}
              </div>
            </div>
            <DialogFooter>
              <Button
                variant={viewingItem.state ? "outline" : "default"}
                onClick={() => markHandled(viewingItem)}
                disabled={updatingState}
              >
                {updatingState ? <LoaderCircle className="animate-spin" /> : <Check />}
                {viewingItem.state ? "标记为待处理" : "标记为已处理"}
              </Button>
            </DialogFooter>
          </DialogContent>
        ) : null}
      </Dialog>

      {/* Dialog: Edit / Create Feedback Class */}
      <Dialog open={classDialogOpen} onOpenChange={setClassDialogOpen}>
        {editingClass ? (
          <DialogContent className="max-w-2xl">
            <DialogHeader>
              <DialogTitle>{editingClass.id ? "配置反馈分类" : "新建反馈分类"}</DialogTitle>
              <DialogDescription>
                设置分类名称及此分类启用的前台录入字段、必填校验规则
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-4">
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-1.5">
                  <Label>分类名称</Label>
                  <Input
                    placeholder="如：业务询盘、产品售后、商务合作"
                    value={editingClass.name || ""}
                    onChange={(e) => setEditingClass({ ...editingClass, name: e.target.value })}
                  />
                </div>
                <div className="space-y-1.5">
                  <Label>排序号</Label>
                  <Input
                    type="number"
                    value={editingClass.sort_order ?? 10}
                    onChange={(e) =>
                      setEditingClass({ ...editingClass, sort_order: Number(e.target.value) })
                    }
                  />
                </div>
              </div>

              <div className="space-y-1.5">
                <Label>分类说明</Label>
                <Input
                  placeholder="该分类的应用场景说明"
                  value={editingClass.description || ""}
                  onChange={(e) => setEditingClass({ ...editingClass, description: e.target.value })}
                />
              </div>

              <div className="space-y-2">
                <Label className="font-semibold">字段录入项配置 (该分类启用的表单字段)</Label>
                <div className="rounded-lg border">
                  <ScrollArea className="max-h-60">
                    <Table>
                      <TableHeader>
                        <TableRow>
                          <TableHead className="w-16">启用</TableHead>
                          <TableHead className="w-16">必填</TableHead>
                          <TableHead>字段名</TableHead>
                          <TableHead>控件类型</TableHead>
                          <TableHead>展示别名</TableHead>
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {fields.map((f) => {
                          const configItem = editingClass.fields_config?.find(
                            (c) => c.field === f.field_name,
                          )
                          const isEnabled = Boolean(configItem)
                          const isMust = Boolean(
                            editingClass.must_fields?.includes(f.field_name),
                          )
                          return (
                            <TableRow key={f.id}>
                              <TableCell>
                                <input
                                  type="checkbox"
                                  checked={isEnabled}
                                  onChange={(e) => {
                                    const cur = editingClass.fields_config || []
                                    if (e.target.checked) {
                                      setEditingClass({
                                        ...editingClass,
                                        fields_config: [
                                          ...cur,
                                          { field: f.field_name, label: f.field_label },
                                        ],
                                      })
                                    } else {
                                      setEditingClass({
                                        ...editingClass,
                                        fields_config: cur.filter(
                                          (c) => c.field !== f.field_name,
                                        ),
                                        must_fields: (editingClass.must_fields || []).filter(
                                          (name) => name !== f.field_name,
                                        ),
                                      })
                                    }
                                  }}
                                />
                              </TableCell>
                              <TableCell>
                                <input
                                  type="checkbox"
                                  disabled={!isEnabled}
                                  checked={isMust}
                                  onChange={(e) => {
                                    const curMust = editingClass.must_fields || []
                                    if (e.target.checked) {
                                      setEditingClass({
                                        ...editingClass,
                                        must_fields: [...curMust, f.field_name],
                                      })
                                    } else {
                                      setEditingClass({
                                        ...editingClass,
                                        must_fields: curMust.filter((n) => n !== f.field_name),
                                      })
                                    }
                                  }}
                                />
                              </TableCell>
                              <TableCell>
                                <code>{f.field_name}</code>
                              </TableCell>
                              <TableCell>
                                <span className="text-xs text-muted-foreground">{f.field_type}</span>
                              </TableCell>
                              <TableCell>
                                <Input
                                  className="h-7 text-xs"
                                  value={configItem?.label ?? f.field_label}
                                  disabled={!isEnabled}
                                  onChange={(e) => {
                                    const cur = editingClass.fields_config || []
                                    setEditingClass({
                                      ...editingClass,
                                      fields_config: cur.map((item) =>
                                        item.field === f.field_name
                                          ? { ...item, label: e.target.value }
                                          : item,
                                      ),
                                    })
                                  }}
                                />
                              </TableCell>
                            </TableRow>
                          )
                        })}
                      </TableBody>
                    </Table>
                  </ScrollArea>
                </div>
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setClassDialogOpen(false)}>
                取消
              </Button>
              <Button onClick={handleSaveClass} disabled={classSaving}>
                {classSaving ? <LoaderCircle className="size-4 animate-spin" /> : <Check />}
                保存分类
              </Button>
            </DialogFooter>
          </DialogContent>
        ) : null}
      </Dialog>

      {/* Dialog: View Form Code */}
      <Dialog open={Boolean(codeModalClass)} onOpenChange={(open) => { if (!open) setCodeModalClass(null) }}>
        {codeModalClass ? (
          <DialogContent className="max-w-xl">
            <DialogHeader>
              <DialogTitle>前台表单 HTML 代码 ({codeModalClass.name})</DialogTitle>
              <DialogDescription>
                将以下表单代码复制并粘贴到您的主题模板（如 msg.html 或 contact.html）中即可
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-3">
              <Textarea
                className="font-mono text-xs leading-5"
                rows={10}
                readOnly
                value={generateFormHTML(codeModalClass)}
              />
            </div>
            <DialogFooter>
              <Button
                onClick={() => {
                  navigator.clipboard.writeText(generateFormHTML(codeModalClass))
                  setCopiedCode(true)
                  setTimeout(() => setCopiedCode(false), 2000)
                }}
                className="gap-1.5"
              >
                {copiedCode ? <Check className="size-4 text-green-500" /> : <Copy className="size-4" />}
                {copiedCode ? "已复制到剪贴板" : "复制代码"}
              </Button>
            </DialogFooter>
          </DialogContent>
        ) : null}
      </Dialog>

      {/* Dialog: Edit / Create Feedback Field */}
      <Dialog open={fieldDialogOpen} onOpenChange={setFieldDialogOpen}>
        {editingField ? (
          <DialogContent className="max-w-md">
            <DialogHeader>
              <DialogTitle>{editingField.id ? "编辑反馈字段" : "新建反馈字段"}</DialogTitle>
              <DialogDescription>定义字段标识、控件类型及选项值</DialogDescription>
            </DialogHeader>
            <div className="space-y-4">
              <div className="space-y-1.5">
                <Label>字段英文名</Label>
                <Input
                  placeholder="如：budget, wechat, product_model"
                  value={editingField.field_name || ""}
                  disabled={Boolean(editingField.id)}
                  onChange={(e) =>
                    setEditingField({ ...editingField, field_name: e.target.value })
                  }
                />
              </div>
              <div className="space-y-1.5">
                <Label>字段标识 (中文名称)</Label>
                <Input
                  placeholder="如：意向预算、微信号、采购型号"
                  value={editingField.field_label || ""}
                  onChange={(e) =>
                    setEditingField({ ...editingField, field_label: e.target.value })
                  }
                />
              </div>
              <div className="space-y-1.5">
                <Label>表单控件类型</Label>
                <Select
                  value={editingField.field_type || "text"}
                  onValueChange={(val) => setEditingField({ ...editingField, field_type: val ?? "text" })}
                >
                  <SelectTrigger>
                    <SelectValue>
                      {(val) => {
                        const map: Record<string, string> = {
                          text: "单行文本 (text)",
                          textarea: "多行文本 (textarea)",
                          select: "下拉选择 (select)",
                          radio: "单选框 (radio)",
                          checkbox: "复选框 (checkbox)",
                        }
                        return map[val] || val || "选择控件类型"
                      }}
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="text">单行文本 (text)</SelectItem>
                    <SelectItem value="textarea">多行文本 (textarea)</SelectItem>
                    <SelectItem value="select">下拉选择 (select)</SelectItem>
                    <SelectItem value="radio">单选框 (radio)</SelectItem>
                    <SelectItem value="checkbox">复选框 (checkbox)</SelectItem>
                  </SelectContent>
                </Select>
              </div>

              {["select", "radio", "checkbox"].includes(editingField.field_type || "") ? (
                <div className="space-y-1.5">
                  <Label>选项列表 (每行一个或逗号隔开)</Label>
                  <Textarea
                    placeholder={"选项1\n选项2\n选项3"}
                    value={editingField.field_options || ""}
                    onChange={(e) =>
                      setEditingField({ ...editingField, field_options: e.target.value })
                    }
                    rows={3}
                  />
                </div>
              ) : null}

              <div className="grid grid-cols-2 gap-3">
                <div className="space-y-1.5">
                  <Label>排序号</Label>
                  <Input
                    type="number"
                    value={editingField.sort_order ?? 50}
                    onChange={(e) =>
                      setEditingField({ ...editingField, sort_order: Number(e.target.value) })
                    }
                  />
                </div>
                <div className="space-y-1.5">
                  <Label>说明</Label>
                  <Input
                    placeholder="输入提示"
                    value={editingField.description || ""}
                    onChange={(e) =>
                      setEditingField({ ...editingField, description: e.target.value })
                    }
                  />
                </div>
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setFieldDialogOpen(false)}>
                取消
              </Button>
              <Button onClick={handleSaveField} disabled={fieldSaving}>
                {fieldSaving ? <LoaderCircle className="size-4 animate-spin" /> : <Check />}
                保存字段
              </Button>
            </DialogFooter>
          </DialogContent>
        ) : null}
      </Dialog>

      {/* Confirm Dialogs */}
      <ConfirmDialog
        open={Boolean(deletingItem)}
        onOpenChange={(open) => {
          if (!open) setDeletingItem(null)
        }}
        title="删除反馈"
        description={`确定删除“${deletingItem?.title}”这条记录吗？`}
        confirmLabel="确认删除"
        onConfirm={confirmDeleteFeedback}
      />
      <ConfirmDialog
        open={batchDeleting}
        onOpenChange={setBatchDeleting}
        title="批量删除反馈"
        description={`确定删除选中的 ${selectedIds.length} 条反馈记录吗？此操作不可撤销。`}
        confirmLabel="确认删除"
        onConfirm={confirmBatchDelete}
      />
      <ConfirmDialog
        open={Boolean(classDeleting)}
        onOpenChange={(open) => {
          if (!open) setClassDeleting(null)
        }}
        title="删除反馈分类"
        description={`确定删除分类“${classDeleting?.name}”吗？`}
        confirmLabel="确认删除"
        onConfirm={confirmDeleteClass}
      />
      <ConfirmDialog
        open={Boolean(fieldDeleting)}
        onOpenChange={(open) => {
          if (!open) setFieldDeleting(null)
        }}
        title="删除反馈字段"
        description={`确定删除字段“${fieldDeleting?.field_label}”吗？`}
        confirmLabel="确认删除"
        onConfirm={confirmDeleteField}
      />
    </div>
  )
}
