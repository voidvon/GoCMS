import { useCallback, useEffect, useMemo, useState } from "react"
import {
  Check,
  ChevronLeft,
  Database,
  Layers,
  LoaderCircle,
  Plus,
  Trash2,
} from "lucide-react"

import {
  deleteModelField,
  deleteModelTable,
  deleteSystemModel,
  getModelFields,
  getModelTables,
  getSystemModels,
  saveModelField,
  saveModelTable,
  saveSystemModel,
  type ModelField,
  type ModelTable,
  type SystemModel,
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
import { ConfirmDialog, IconButton, InlineAlert } from "@/components/app/app-ui"
import { ScrollArea } from "@/components/ui/scroll-area"

const fieldTypeLabels: Record<string, string> = {
  text: "单行文本 (text)",
  textarea: "多行文本 (textarea)",
  editor: "富文本编辑器 (editor)",
  select: "下拉选择 (select)",
  radio: "单选框 (radio)",
  checkbox: "复选框 (checkbox)",
  number: "数字 (number)",
  image: "图片上传 (image)",
  morepic: "图集/多图 (morepic)",
  multivalue: "多值文本 (multivalue)",
  date: "日期时间 (date)",
}

export function ModelsPage() {
  const [activeTab, setActiveTab] = useState("models")
  const [tables, setTables] = useState<ModelTable[]>([])
  const [fields, setFields] = useState<ModelField[]>([])
  const [models, setModels] = useState<SystemModel[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")

  // Dialog states for Model
  const [modelDialogOpen, setModelDialogOpen] = useState(false)
  const [editingModel, setEditingModel] = useState<Partial<SystemModel> | null>(null)
  const [modelSaving, setModelSaving] = useState(false)
  const [modelDeleting, setModelDeleting] = useState<SystemModel | null>(null)

  // Dialog states for Table
  const [tableDialogOpen, setTableDialogOpen] = useState(false)
  const [editingTable, setEditingTable] = useState<Partial<ModelTable> | null>(null)
  const [tableSaving, setTableSaving] = useState(false)
  const [tableDeleting, setTableDeleting] = useState<ModelTable | null>(null)

  // Dialog state for Field & Field Management Modal
  const [editingField, setEditingField] = useState<Partial<ModelField> | null>(null)
  const [fieldSaving, setFieldSaving] = useState(false)
  const [fieldDeleting, setFieldDeleting] = useState<ModelField | null>(null)
  const [managingTable, setManagingTable] = useState<ModelTable | null>(null)
  const [tableFieldSubView, setTableFieldSubView] = useState<"list" | "edit">("list")

  const loadData = useCallback(async () => {
    setLoading(true)
    try {
      const [tList, mList, fList] = await Promise.all([
        getModelTables(),
        getSystemModels(),
        getModelFields(),
      ])
      setTables(tList)
      setModels(mList)
      setFields(fList)
      setError("")
    } catch (err) {
      setError(err instanceof Error ? err.message : "加载数据失败")
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadData()
  }, [loadData])

  // Fields available for editingModel's selected table
  const availableFieldsForModel = useMemo(() => {
    if (!editingModel) return []
    const tid = editingModel.table_id || 1
    return fields.filter((f) => f.table_id === tid)
  }, [fields, editingModel])

  // Handlers for System Models
  function openCreateModel() {
    setEditingModel({
      name: "",
      table_id: tables[0]?.id || 1,
      description: "",
      entry_fields: [],
      must_fields: ["title"],
      sort_order: 10,
    })
    setModelDialogOpen(true)
  }

  function openEditModel(model: SystemModel) {
    setEditingModel({
      ...model,
      entry_fields: model.entry_fields ? [...model.entry_fields] : [],
      must_fields: model.must_fields ? [...model.must_fields] : [],
    })
    setModelDialogOpen(true)
  }

  async function handleSaveModel() {
    if (!editingModel || !editingModel.name) return
    setModelSaving(true)
    try {
      await saveSystemModel(editingModel)
      setModelDialogOpen(false)
      await loadData()
    } catch (err) {
      setError(err instanceof Error ? err.message : "保存系统模型失败")
    } finally {
      setModelSaving(false)
    }
  }

  async function handleDeleteModel() {
    if (!modelDeleting) return
    try {
      await deleteSystemModel(modelDeleting.id)
      setModelDeleting(null)
      await loadData()
    } catch (err) {
      setError(err instanceof Error ? err.message : "删除系统模型失败")
    }
  }

  // Handlers for Tables
  function openCreateTable() {
    setEditingTable({
      table_name: "",
      name: "",
      description: "",
    })
    setTableDialogOpen(true)
  }

  function openEditTable(table: ModelTable) {
    setEditingTable({ ...table })
    setTableDialogOpen(true)
  }

  async function handleSaveTable() {
    if (!editingTable || !editingTable.name || !editingTable.table_name) return
    setTableSaving(true)
    try {
      await saveModelTable(editingTable)
      setTableDialogOpen(false)
      await loadData()
    } catch (err) {
      setError(err instanceof Error ? err.message : "保存数据表失败")
    } finally {
      setTableSaving(false)
    }
  }

  async function handleDeleteTable() {
    if (!tableDeleting) return
    try {
      await deleteModelTable(tableDeleting.id)
      setTableDeleting(null)
      await loadData()
    } catch (err) {
      setError(err instanceof Error ? err.message : "删除数据表失败")
    }
  }

  async function handleDeleteField() {
    if (!fieldDeleting) return
    try {
      await deleteModelField(fieldDeleting.id)
      setFieldDeleting(null)
      await loadData()
    } catch (err) {
      setError(err instanceof Error ? err.message : "删除字段失败")
    }
  }

  const fieldsForManagingTable = useMemo(() => {
    if (!managingTable) return []
    return fields.filter((f) => f.table_id === managingTable.id)
  }, [fields, managingTable])

  async function handleSaveFieldInModal() {
    if (!editingField || !managingTable) return
    if (!editingField.field_name?.trim()) {
      setError("字段英文名不能为空")
      return
    }
    if (!editingField.field_label?.trim()) {
      setError("字段标识不能为空")
      return
    }
    setFieldSaving(true)
    try {
      await saveModelField({
        ...editingField,
        table_id: managingTable.id,
      })
      await loadData()
      setTableFieldSubView("list")
    } catch (err) {
      setError(err instanceof Error ? err.message : "保存字段失败")
    } finally {
      setFieldSaving(false)
    }
  }

  return (
    <div className="space-y-4">
      {error ? <InlineAlert>{error}</InlineAlert> : null}

      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">数据表与系统模型</h1>
          <p className="text-sm text-muted-foreground">
            对标帝国CMS核心架构：管理数据表结构、自定义字段及各系统模型录入项
          </p>
        </div>
      </div>

      <Tabs value={activeTab} onValueChange={setActiveTab} className="space-y-4">
        <TabsList>
          <TabsTrigger value="models" className="gap-2">
            <Layers className="size-4" />
            系统模型管理
          </TabsTrigger>
          <TabsTrigger value="tables" className="gap-2">
            <Database className="size-4" />
            数据表管理
          </TabsTrigger>
        </TabsList>

        {/* Tab 1: System Models */}
        <TabsContent value="models">
          <Card>
            <CardHeader className="flex flex-row items-center justify-between border-b pb-4">
              <div>
                <CardTitle>系统模型列表</CardTitle>
                <CardDescription>各栏目绑定系统模型，决定前后台录入项与自定义展示规则</CardDescription>
              </div>
              <Button onClick={openCreateModel} className="gap-1.5" size="sm">
                <Plus className="size-4" />
                新建系统模型
              </Button>
            </CardHeader>
            <CardContent className="p-0">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="pl-4">模型名称</TableHead>
                    <TableHead>绑定数据表</TableHead>
                    <TableHead>启用的录入项</TableHead>
                    <TableHead>必填校验项</TableHead>
                    <TableHead>排序</TableHead>
                    <TableHead>属性</TableHead>
                    <TableHead className="pr-4 text-right">操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {loading ? (
                    <TableRow>
                      <TableCell colSpan={7} className="h-28 text-center text-muted-foreground">
                        <LoaderCircle className="mx-auto size-5 animate-spin" />
                      </TableCell>
                    </TableRow>
                  ) : models.length === 0 ? (
                    <TableRow>
                      <TableCell colSpan={7} className="h-28 text-center text-muted-foreground">
                        暂无系统模型
                      </TableCell>
                    </TableRow>
                  ) : (
                    models.map((m) => {
                      const entryCount = m.entry_fields ? m.entry_fields.length : 0
                      const mustCount = m.must_fields ? m.must_fields.length : 0
                      return (
                        <TableRow key={m.id}>
                          <TableCell className="pl-4 font-medium">
                            {m.name}
                            {m.description ? (
                              <p className="text-xs text-muted-foreground">{m.description}</p>
                            ) : null}
                          </TableCell>
                          <TableCell>
                            <Badge variant="outline">{m.table_name || `表 ID: ${m.table_id}`}</Badge>
                          </TableCell>
                          <TableCell>{entryCount} 个字段</TableCell>
                          <TableCell>{mustCount} 个必填</TableCell>
                          <TableCell>{m.sort_order}</TableCell>
                          <TableCell>
                            {m.is_default ? <Badge variant="secondary">系统默认</Badge> : null}
                          </TableCell>
                          <TableCell className="pr-4 text-right">
                            <div className="flex justify-end gap-1">
                              <Button variant="ghost" size="sm" onClick={() => openEditModel(m)}>
                                配置
                              </Button>
                              {!m.is_default ? (
                                <IconButton
                                  variant="ghost"
                                  size="icon-sm"
                                  className="text-destructive hover:text-destructive"
                                  onClick={() => setModelDeleting(m)}
                                  label={`删除${m.name}`}
                                >
                                  <Trash2 />
                                </IconButton>
                              ) : null}
                            </div>
                          </TableCell>
                        </TableRow>
                      )
                    })
                  )}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </TabsContent>

        {/* Tab 2: Data Tables */}
        <TabsContent value="tables">
          <Card>
            <CardHeader className="flex flex-row items-center justify-between border-b pb-4">
              <div>
                <CardTitle>数据表管理</CardTitle>
                <CardDescription>管理内容与模块底层数据表存储标识</CardDescription>
              </div>
              <Button onClick={openCreateTable} className="gap-1.5" size="sm">
                <Plus className="size-4" />
                新建数据表
              </Button>
            </CardHeader>
            <CardContent className="p-0">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="pl-4">数据表名称</TableHead>
                    <TableHead>英文标识</TableHead>
                    <TableHead>说明</TableHead>
                    <TableHead>字段数</TableHead>
                    <TableHead>属性</TableHead>
                    <TableHead className="pr-4 text-right">操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {tables.map((t) => (
                    <TableRow key={t.id}>
                      <TableCell className="pl-4 font-medium">{t.name}</TableCell>
                      <TableCell>
                        <code className="rounded bg-muted px-1.5 py-0.5 text-xs">{t.table_name}</code>
                      </TableCell>
                      <TableCell className="text-muted-foreground">{t.description || "无"}</TableCell>
                      <TableCell>{t.field_count ?? 0}</TableCell>
                      <TableCell>
                        {t.is_default ? <Badge variant="secondary">系统默认</Badge> : null}
                      </TableCell>
                      <TableCell className="pr-4 text-right">
                        <div className="flex justify-end gap-1">
                          <Button variant="ghost" size="sm" onClick={() => openEditTable(t)}>
                            编辑
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => {
                              setManagingTable(t)
                              setTableFieldSubView("list")
                            }}
                          >
                            管理字段
                          </Button>
                          {!t.is_default ? (
                            <IconButton
                              variant="ghost"
                              size="icon-sm"
                              className="text-destructive hover:text-destructive"
                              onClick={() => setTableDeleting(t)}
                              label={`删除${t.name}`}
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

      {/* Dialog: Edit / Create System Model */}
      <Dialog open={modelDialogOpen} onOpenChange={setModelDialogOpen}>
        {editingModel ? (
          <DialogContent className="max-w-4xl max-h-[90vh] flex flex-col">
            <DialogHeader>
              <DialogTitle>{editingModel.id ? "编辑系统模型" : "新建系统模型"}</DialogTitle>
              <DialogDescription>
                配置模型名称、绑定的数据表以及该模型启用的录入项与必填项
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-4 flex-1 overflow-hidden flex flex-col min-h-0">
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-1.5">
                  <Label>模型名称</Label>
                  <Input
                    placeholder="如：文章系统模型、产品模型"
                    value={editingModel.name || ""}
                    onChange={(e) => setEditingModel({ ...editingModel, name: e.target.value })}
                  />
                </div>
                <div className="space-y-1.5">
                  <Label>绑定数据表</Label>
                  <Select
                    value={String(editingModel.table_id || 1)}
                    onValueChange={(val) =>
                      setEditingModel({ ...editingModel, table_id: Number(val) })
                    }
                    disabled={Boolean(editingModel.id)}
                  >
                    <SelectTrigger>
                      <SelectValue>
                        {(val) => {
                          const t = tables.find((item) => String(item.id) === String(val))
                          return t ? `${t.name} (${t.table_name})` : "选择数据表"
                        }}
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      {tables.map((t) => (
                        <SelectItem key={t.id} value={String(t.id)}>
                          {t.name} ({t.table_name})
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              </div>

              <div className="space-y-1.5">
                <Label>模型说明</Label>
                <Input
                  placeholder="模型功能用途说明"
                  value={editingModel.description || ""}
                  onChange={(e) => setEditingModel({ ...editingModel, description: e.target.value })}
                />
              </div>

              <div className="space-y-2 flex-1 flex flex-col min-h-0">
                <Label className="font-semibold">字段录入项配置 (前后台发布字段)</Label>
                <div className="rounded-lg border flex-1 overflow-hidden">
                  <ScrollArea className="h-[46vh]">
                    <Table>
                      <TableHeader>
                        <TableRow>
                          <TableHead className="w-16">录入</TableHead>
                          <TableHead className="w-16">必填</TableHead>
                          <TableHead>字段名</TableHead>
                          <TableHead>字段类型</TableHead>
                          <TableHead>展示别名</TableHead>
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {availableFieldsForModel.map((f) => {
                          const entryItem = editingModel.entry_fields?.find(
                            (item) => item.field === f.field_name,
                          )
                          const isEntered = Boolean(entryItem)
                          const isRequired = Boolean(
                            editingModel.must_fields?.includes(f.field_name),
                          )
                          return (
                            <TableRow key={f.id}>
                              <TableCell>
                                <input
                                  type="checkbox"
                                  checked={isEntered}
                                  onChange={(e) => {
                                    const currentEntries = editingModel.entry_fields || []
                                    if (e.target.checked) {
                                      setEditingModel({
                                        ...editingModel,
                                        entry_fields: [
                                          ...currentEntries,
                                          { field: f.field_name, label: f.field_label },
                                        ],
                                      })
                                    } else {
                                      setEditingModel({
                                        ...editingModel,
                                        entry_fields: currentEntries.filter(
                                          (item) => item.field !== f.field_name,
                                        ),
                                        must_fields: (editingModel.must_fields || []).filter(
                                          (item) => item !== f.field_name,
                                        ),
                                      })
                                    }
                                  }}
                                />
                              </TableCell>
                              <TableCell>
                                <input
                                  type="checkbox"
                                  checked={isRequired}
                                  disabled={!isEntered}
                                  onChange={(e) => {
                                    const currentMust = editingModel.must_fields || []
                                    if (e.target.checked) {
                                      setEditingModel({
                                        ...editingModel,
                                        must_fields: [...currentMust, f.field_name],
                                      })
                                    } else {
                                      setEditingModel({
                                        ...editingModel,
                                        must_fields: currentMust.filter(
                                          (item) => item !== f.field_name,
                                        ),
                                      })
                                    }
                                  }}
                                />
                              </TableCell>
                              <TableCell className="font-mono text-xs">{f.field_name}</TableCell>
                              <TableCell>
                                <Badge variant="outline">
                                  {fieldTypeLabels[f.field_type] || f.field_type}
                                </Badge>
                              </TableCell>
                              <TableCell>
                                <Input
                                  size={1}
                                  className="h-8 text-xs max-w-[200px]"
                                  placeholder={f.field_label}
                                  value={entryItem?.label ?? f.field_label}
                                  disabled={!isEntered}
                                  onChange={(e) => {
                                    const currentEntries = editingModel.entry_fields || []
                                    setEditingModel({
                                      ...editingModel,
                                      entry_fields: currentEntries.map((item) =>
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
              <Button variant="outline" onClick={() => setModelDialogOpen(false)}>
                取消
              </Button>
              <Button onClick={handleSaveModel} disabled={modelSaving}>
                {modelSaving ? <LoaderCircle className="size-4 animate-spin" /> : <Check />}
                保存模型
              </Button>
            </DialogFooter>
          </DialogContent>
        ) : null}
      </Dialog>

      {/* Dialog: Edit / Create Table */}
      <Dialog open={tableDialogOpen} onOpenChange={setTableDialogOpen}>
        {editingTable ? (
          <DialogContent className="max-w-2xl">
            <DialogHeader>
              <DialogTitle>{editingTable.id ? "编辑数据表" : "新建数据表"}</DialogTitle>
              <DialogDescription>创建或修改内容数据表标识</DialogDescription>
            </DialogHeader>
            <div className="space-y-4 py-2">
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-1.5">
                  <Label>数据表英文标识</Label>
                  <Input
                    placeholder="如：product, download, job"
                    value={editingTable.table_name || ""}
                    disabled={Boolean(editingTable.id)}
                    onChange={(e) =>
                      setEditingTable({ ...editingTable, table_name: e.target.value })
                    }
                  />
                  <p className="text-xs text-muted-foreground">存储数据表名称，不可重复</p>
                </div>
                <div className="space-y-1.5">
                  <Label>数据表中文名称</Label>
                  <Input
                    placeholder="如：产品数据表、招聘数据表"
                    value={editingTable.name || ""}
                    onChange={(e) => setEditingTable({ ...editingTable, name: e.target.value })}
                  />
                  <p className="text-xs text-muted-foreground">后台展示名称</p>
                </div>
              </div>
              <div className="space-y-1.5">
                <Label>说明</Label>
                <Input
                  placeholder="说明此数据表存放的数据类型"
                  value={editingTable.description || ""}
                  onChange={(e) =>
                    setEditingTable({ ...editingTable, description: e.target.value })
                  }
                />
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setTableDialogOpen(false)}>
                取消
              </Button>
              <Button onClick={handleSaveTable} disabled={tableSaving}>
                {tableSaving ? <LoaderCircle className="size-4 animate-spin" /> : <Check />}
                保存
              </Button>
            </DialogFooter>
          </DialogContent>
        ) : null}
      </Dialog>

      {/* Dialog: Manage Table Fields directly from Data Tables */}
      <Dialog
        open={Boolean(managingTable)}
        onOpenChange={(open) => {
          if (!open) {
            setManagingTable(null)
            setTableFieldSubView("list")
          }
        }}
      >
        {managingTable ? (
          <DialogContent className="max-w-4xl max-h-[90vh] flex flex-col">
            <DialogHeader>
              <DialogTitle>
                数据表字段管理：{managingTable.name} ({managingTable.table_name})
              </DialogTitle>
              <DialogDescription>
                管理该数据表下的内置系统字段与扩展自定义字段
              </DialogDescription>
            </DialogHeader>

            {tableFieldSubView === "list" ? (
              <>
                <div className="flex items-center justify-between py-1">
                  <span className="text-sm text-muted-foreground">
                    共 {fieldsForManagingTable.length} 个字段
                  </span>
                  <Button
                    size="sm"
                    className="gap-1.5"
                    onClick={() => {
                      setEditingField({
                        table_id: managingTable.id,
                        field_name: "",
                        field_label: "",
                        field_type: "text",
                        field_options: "",
                        description: "",
                        sort_order: 50,
                      })
                      setTableFieldSubView("edit")
                    }}
                  >
                    <Plus className="size-4" />
                    新建字段
                  </Button>
                </div>
                <ScrollArea className="max-h-[55vh] rounded-md border">
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead className="pl-4">排序</TableHead>
                        <TableHead>字段名 (英文)</TableHead>
                        <TableHead>字段标识 (中文)</TableHead>
                        <TableHead>表单控件类型</TableHead>
                        <TableHead>说明</TableHead>
                        <TableHead>属性</TableHead>
                        <TableHead className="pr-4 text-right">操作</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {fieldsForManagingTable.length === 0 ? (
                        <TableRow>
                          <TableCell colSpan={7} className="text-center py-6 text-muted-foreground">
                            暂无字段
                          </TableCell>
                        </TableRow>
                      ) : (
                        fieldsForManagingTable.map((f) => (
                          <TableRow key={f.id}>
                            <TableCell className="pl-4">{f.sort_order}</TableCell>
                            <TableCell>
                              <code className="rounded bg-muted px-1.5 py-0.5 text-xs font-semibold">
                                {f.field_name}
                              </code>
                            </TableCell>
                            <TableCell className="font-medium">{f.field_label}</TableCell>
                            <TableCell>
                              <Badge variant="outline">
                                {fieldTypeLabels[f.field_type] || f.field_type}
                              </Badge>
                            </TableCell>
                            <TableCell className="max-w-[180px] truncate text-muted-foreground">
                              {f.description || "无"}
                            </TableCell>
                            <TableCell>
                              {f.is_system ? (
                                <Badge variant="secondary">内置字段</Badge>
                              ) : (
                                <Badge variant="outline">自定义</Badge>
                              )}
                            </TableCell>
                            <TableCell className="pr-4 text-right">
                              <div className="flex justify-end gap-1">
                                <Button
                                  variant="ghost"
                                  size="sm"
                                  onClick={() => {
                                    setEditingField({ ...f })
                                    setTableFieldSubView("edit")
                                  }}
                                >
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
                        ))
                      )}
                    </TableBody>
                  </Table>
                </ScrollArea>
                <DialogFooter>
                  <Button variant="outline" onClick={() => setManagingTable(null)}>
                    关闭
                  </Button>
                </DialogFooter>
              </>
            ) : editingField ? (
              <>
                <div className="flex items-center gap-2 border-b pb-3">
                  <Button
                    variant="ghost"
                    size="sm"
                    className="gap-1 px-2"
                    onClick={() => setTableFieldSubView("list")}
                  >
                    <ChevronLeft className="size-4" />
                    返回字段列表
                  </Button>
                  <span className="text-sm font-semibold">
                    {editingField.id
                      ? `编辑字段：${editingField.field_label || editingField.field_name}`
                      : "新建数据表字段"}
                  </span>
                </div>

                <div className="space-y-4 py-2">
                  <div className="grid grid-cols-2 gap-4">
                    <div className="space-y-1.5">
                      <Label>字段英文名</Label>
                      <Input
                        placeholder="如：price, spec, brand, author"
                        value={editingField.field_name || ""}
                        disabled={Boolean(editingField.id)}
                        onChange={(e) =>
                          setEditingField({ ...editingField, field_name: e.target.value })
                        }
                      />
                      <p className="text-xs text-muted-foreground">
                        创建后不可修改，仅允许小写字母、数字与下划线
                      </p>
                    </div>
                    <div className="space-y-1.5">
                      <Label>字段标识 (中文名称)</Label>
                      <Input
                        placeholder="如：产品价格、规格型号"
                        value={editingField.field_label || ""}
                        onChange={(e) =>
                          setEditingField({ ...editingField, field_label: e.target.value })
                        }
                      />
                      <p className="text-xs text-muted-foreground">
                        在管理后台及表单中展示的中文标签
                      </p>
                    </div>
                  </div>

                  <div className="space-y-1.5">
                    <Label>表单控件类型</Label>
                    <Select
                      value={editingField.field_type || "text"}
                      onValueChange={(val) =>
                        setEditingField({ ...editingField, field_type: val ?? "text" })
                      }
                    >
                      <SelectTrigger>
                        <SelectValue>
                          {(val) => (val ? fieldTypeLabels[String(val)] || String(val) : "选择类型")}
                        </SelectValue>
                      </SelectTrigger>
                      <SelectContent>
                        {Object.entries(fieldTypeLabels).map(([type, label]) => (
                          <SelectItem key={type} value={type}>
                            {label}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    {editingField.field_type === "morepic" ? (
                      <p className="text-xs text-muted-foreground">多图/图集字段，内容发布时可批量添加图片、从媒体库选取、编辑标题及调整展示顺序。</p>
                    ) : editingField.field_type === "multivalue" ? (
                      <p className="text-xs text-muted-foreground">多值文本字段，支持录入多项参数或多行自定义文本数据。</p>
                    ) : null}
                  </div>

                  {["select", "radio", "checkbox"].includes(editingField.field_type || "") ? (
                    <div className="space-y-1.5">
                      <Label>选项列表 (每行一个，格式：值==标题 或 值)</Label>
                      <Textarea
                        placeholder={"red==红色\nblue==蓝色\ngreen==绿色"}
                        value={editingField.field_options || ""}
                        onChange={(e) =>
                          setEditingField({ ...editingField, field_options: e.target.value })
                        }
                        rows={4}
                      />
                    </div>
                  ) : null}

                  <div className="grid grid-cols-2 gap-4">
                    <div className="space-y-1.5">
                      <Label>排序号</Label>
                      <Input
                        type="number"
                        value={editingField.sort_order ?? 50}
                        onChange={(e) =>
                          setEditingField({
                            ...editingField,
                            sort_order: Number(e.target.value),
                          })
                        }
                      />
                    </div>
                    <div className="space-y-1.5">
                      <Label>字段说明 / 输入提示</Label>
                      <Input
                        placeholder="在表单输入框下方显示的提示说明"
                        value={editingField.description || ""}
                        onChange={(e) =>
                          setEditingField({ ...editingField, description: e.target.value })
                        }
                      />
                    </div>
                  </div>
                </div>

                <DialogFooter className="gap-2">
                  <Button variant="outline" onClick={() => setTableFieldSubView("list")}>
                    取消
                  </Button>
                  <Button onClick={handleSaveFieldInModal} disabled={fieldSaving}>
                    {fieldSaving ? <LoaderCircle className="size-4 animate-spin" /> : <Check />}
                    保存字段
                  </Button>
                </DialogFooter>
              </>
            ) : null}
          </DialogContent>
        ) : null}
      </Dialog>

      {/* Confirm Dialogs */}
      <ConfirmDialog
        open={Boolean(modelDeleting)}
        onOpenChange={(open) => {
          if (!open) setModelDeleting(null)
        }}
        title="删除系统模型"
        description={`确定删除系统模型“${modelDeleting?.name}”吗？`}
        confirmLabel="确认删除"
        onConfirm={handleDeleteModel}
      />
      <ConfirmDialog
        open={Boolean(tableDeleting)}
        onOpenChange={(open) => {
          if (!open) setTableDeleting(null)
        }}
        title="删除数据表"
        description={`确定删除数据表“${tableDeleting?.name}”吗？关联的字段将被同步清理。`}
        confirmLabel="确认删除"
        onConfirm={handleDeleteTable}
      />
      <ConfirmDialog
        open={Boolean(fieldDeleting)}
        onOpenChange={(open) => {
          if (!open) setFieldDeleting(null)
        }}
        title="删除字段"
        description={`确定删除字段“${fieldDeleting?.field_label} (${fieldDeleting?.field_name})”吗？`}
        confirmLabel="确认删除"
        onConfirm={handleDeleteField}
      />
    </div>
  )
}
