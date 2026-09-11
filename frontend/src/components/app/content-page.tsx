import { useEffect, useMemo, useRef, useState, type ChangeEvent, type FormEvent } from "react"
import { ArrowDown, ArrowUp, FileText, ImagePlus, Languages, Library, LoaderCircle, Pencil, Plus, Search, Trash2, Upload, X } from "lucide-react"

import {
  createContent,
  deleteContent,
  getCategories,
  getContent,
  getContentItem,
  getModelFields,
  getSystemModels,
  uploadMedia,
  type CategoryItem,
  type Content,
  type ContentInput,
  type MediaAsset,
  type ModelField,
  type SaveResponse,
  type SystemModel,
  updateContent,
} from "@/lib/api"
import { useLanguage } from "@/lib/language-context"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Textarea } from "@/components/ui/textarea"
import { ScrollArea } from "@/components/ui/scroll-area"
import { ConfirmDialog, IconButton, InlineAlert, SearchField, TablePagination } from "@/components/app/app-ui"
import { MediaPickerDialog } from "@/components/app/media-picker-dialog"
import { RichTextEditor } from "@/components/app/rich-text-editor"
import { flattenCategoryTree } from "@/lib/category-tree"

function FieldLabel({
  label,
  isRequired,
  isTranslatable,
  isMultiLangActive,
  htmlFor,
}: {
  label: string
  isRequired?: boolean
  isTranslatable?: boolean
  isMultiLangActive?: boolean
  htmlFor?: string
}) {
  return (
    <div className="flex items-center gap-1.5">
      <Label htmlFor={htmlFor} className="cursor-pointer">
        {label}
        {isRequired ? <span className="text-destructive"> *</span> : null}
      </Label>
      {isMultiLangActive && (
        isTranslatable ? (
          <span className="text-[10px] leading-tight text-blue-600 dark:text-blue-400 bg-blue-50 dark:bg-blue-950/60 px-1 py-0.5 rounded border border-blue-200/60 dark:border-blue-800/60 font-normal">
            多语言
          </span>
        ) : (
          <span className="text-[10px] leading-tight text-muted-foreground bg-muted px-1 py-0.5 rounded font-normal">
            通用
          </span>
        )
      )}
    </div>
  )
}

const pageSize = 20

const emptyContent: ContentInput = {
  title: "",
  code: "",
  category_id: 0,
  summary: "",
  content: "",
  cover_image: "",
  published_at: "",
  source: "",
  keywords: "",
  description: "",
  order_id: 0,
  featured: 0,
  visible: 1,
  model_id: 1,
  extra_data: {},
}

function categoryName(categories: CategoryItem[], categoryID: number) {
  return categories.find((category) => category.id === categoryID)?.name ?? "未分类"
}

function formatDate(value: string) {
  return value ? value.slice(0, 10).replaceAll("-", "/") : "暂无日期"
}

function dateTimeInput(value: string) {
  const normalized = value.trim().replace(" ", "T")
  return normalized.length >= 16 ? normalized.slice(0, 16) : ""
}

function dateTimeValue(value: string) {
  return value ? `${value.replace("T", " ")}:00` : ""
}

function publicationMessage(result: SaveResponse) {
  if (!result.publication) return "内容已保存，发布后会更新公开网站。"
  return result.publish_started
    ? "内容已保存，正在生成网站；请在网站发布中查看结果。"
    : "内容已保存并加入发布队列，将在当前任务完成后自动生成。"
}

function parseFieldOptions(text: string) {
  if (!text) return []
  return text
    .split("\n")
    .map((l) => l.trim())
    .filter(Boolean)
    .map((line) => {
      const parts = line.split("==")
      return {
        value: parts[0].trim(),
        label: (parts[1] || parts[0]).trim(),
      }
    })
}

type PhotoItem = {
  url: string
  title: string
}

function parsePhotos(raw: any): PhotoItem[] {
  if (Array.isArray(raw)) {
    return raw.map((item) => {
      if (typeof item === "string") return { url: item, title: "" }
      return { url: item?.url || "", title: item?.title || "" }
    })
  }
  if (typeof raw === "string") {
    const trimmed = raw.trim()
    if (!trimmed) return []
    if (trimmed.startsWith("[") && trimmed.endsWith("]")) {
      try {
        const parsed = JSON.parse(trimmed)
        if (Array.isArray(parsed)) {
          return parsed.map((item) => {
            if (typeof item === "string") return { url: item, title: "" }
            return { url: item?.url || "", title: item?.title || "" }
          })
        }
      } catch {
        // fallback to delimited format
      }
    }
    return trimmed
      .split("\n")
      .map((line) => line.trim())
      .filter(Boolean)
      .map((line) => {
        const parts = line.split("::::::")
        if (parts.length >= 3) {
          return { url: parts[0].trim(), title: parts[2].trim() }
        } else if (parts.length === 2) {
          return { url: parts[0].trim(), title: parts[1].trim() }
        }
        return { url: parts[0].trim(), title: "" }
      })
  }
  return []
}

function MultiImageField({
  field,
  value,
  isRequired,
  isMultiLangActive,
  onChange,
}: {
  field: { field_name: string; field_label: string; description?: string; is_translatable?: boolean }
  value: any
  isRequired: boolean
  isMultiLangActive?: boolean
  onChange: (value: PhotoItem[]) => void
}) {
  const photos = useMemo(() => parsePhotos(value), [value])
  const [mediaPickerOpen, setMediaPickerOpen] = useState(false)
  const [uploading, setUploading] = useState(false)
  const fileInputRef = useRef<HTMLInputElement>(null)

  function updateItem(index: number, key: keyof PhotoItem, val: string) {
    const next = photos.map((item, i) => (i === index ? { ...item, [key]: val } : item))
    onChange(next)
  }

  function removeItem(index: number) {
    onChange(photos.filter((_, i) => i !== index))
  }

  function moveItem(index: number, direction: -1 | 1) {
    const target = index + direction
    if (target < 0 || target >= photos.length) return
    const next = [...photos]
    const [moved] = next.splice(index, 1)
    next.splice(target, 0, moved)
    onChange(next)
  }

  function addItem(url = "", title = "") {
    onChange([...photos, { url, title }])
  }

  async function handleFileUpload(e: ChangeEvent<HTMLInputElement>) {
    const files = e.target.files
    if (!files || files.length === 0) return
    setUploading(true)
    try {
      const added: PhotoItem[] = []
      for (let i = 0; i < files.length; i++) {
        const file = files[i]
        const res = await uploadMedia(file)
        if (res.asset) {
          added.push({ url: res.asset.url, title: res.asset.original_name || "" })
        }
      }
      onChange([...photos, ...added])
    } catch (err) {
      alert(err instanceof Error ? err.message : "图片上传失败")
    } finally {
      setUploading(false)
      if (fileInputRef.current) fileInputRef.current.value = ""
    }
  }

  return (
    <div className="space-y-3 rounded-md border bg-background/50 p-3 sm:col-span-2">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b pb-2">
        <div>
          <FieldLabel
            label={field.field_label}
            isRequired={isRequired}
            isTranslatable={field.is_translatable}
            isMultiLangActive={isMultiLangActive}
          />
          {field.description ? (
            <p className="text-xs text-muted-foreground mt-0.5">{field.description}</p>
          ) : null}
        </div>
        <div className="flex items-center gap-2">
          <input
            ref={fileInputRef}
            type="file"
            multiple
            accept="image/jpeg,image/png,image/gif,image/webp,image/svg+xml"
            className="hidden"
            onChange={handleFileUpload}
          />
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={uploading}
            onClick={() => fileInputRef.current?.click()}
          >
            {uploading ? <LoaderCircle className="size-3.5 animate-spin" /> : <Upload className="size-3.5" />}
            本地上传
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => setMediaPickerOpen(true)}
          >
            <Library className="size-3.5" />
            媒体库选取
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => addItem()}
          >
            <Plus className="size-3.5" />
            添加图片项
          </Button>
        </div>
      </div>

      {photos.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-6 text-center text-xs text-muted-foreground">
          <ImagePlus className="size-8 mb-1 opacity-50" />
          <span>暂无图片，点击上方按钮上传或选取</span>
        </div>
      ) : (
        <div className="space-y-2">
          {photos.map((item, index) => (
            <div
              key={index}
              className="flex items-center gap-3 rounded-lg border bg-muted/30 p-2.5 transition-colors hover:bg-muted/50"
            >
              <div className="flex size-14 shrink-0 items-center justify-center overflow-hidden rounded border bg-background">
                {item.url ? (
                  <img
                    src={item.url}
                    alt={item.title || `图片 ${index + 1}`}
                    className="size-full object-cover"
                    onError={(e) => {
                      ;(e.target as HTMLElement).style.display = "none"
                    }}
                  />
                ) : (
                  <ImagePlus className="size-5 text-muted-foreground/40" />
                )}
              </div>
              <div className="grid flex-1 gap-2 sm:grid-cols-2">
                <div className="space-y-1">
                  <span className="text-[11px] text-muted-foreground">图片地址 (URL)</span>
                  <Input
                    value={item.url}
                    onChange={(e) => updateItem(index, "url", e.target.value)}
                    placeholder="/images/example.jpg"
                    className="h-8 text-xs"
                  />
                </div>
                <div className="space-y-1">
                  <span className="text-[11px] text-muted-foreground">图片名称/说明</span>
                  <Input
                    value={item.title}
                    onChange={(e) => updateItem(index, "title", e.target.value)}
                    placeholder="如：外观正视图"
                    className="h-8 text-xs"
                  />
                </div>
              </div>
              <div className="flex shrink-0 items-center gap-1">
                <IconButton
                  variant="ghost"
                  size="icon-xs"
                  label="上移"
                  disabled={index === 0}
                  onClick={() => moveItem(index, -1)}
                >
                  <ArrowUp className="size-3.5" />
                </IconButton>
                <IconButton
                  variant="ghost"
                  size="icon-xs"
                  label="下移"
                  disabled={index === photos.length - 1}
                  onClick={() => moveItem(index, 1)}
                >
                  <ArrowDown className="size-3.5" />
                </IconButton>
                <IconButton
                  variant="ghost"
                  size="icon-xs"
                  className="text-destructive hover:text-destructive"
                  label="删除"
                  onClick={() => removeItem(index)}
                >
                  <Trash2 className="size-3.5" />
                </IconButton>
              </div>
            </div>
          ))}
        </div>
      )}

      <MediaPickerDialog
        open={mediaPickerOpen}
        onOpenChange={setMediaPickerOpen}
        onSelect={(media) => {
          addItem(media.url, media.original_name || "")
          setMediaPickerOpen(false)
        }}
      />
    </div>
  )
}

function parseMultiValue(raw: any): string[] {
  if (Array.isArray(raw)) return raw.map(String)
  if (typeof raw === "string") {
    const trimmed = raw.trim()
    if (!trimmed) return []
    if (trimmed.startsWith("[") && trimmed.endsWith("]")) {
      try {
        const parsed = JSON.parse(trimmed)
        if (Array.isArray(parsed)) return parsed.map(String)
      } catch {}
    }
    return trimmed.split("\n").map((l) => l.trim()).filter(Boolean)
  }
  return []
}

function MultiValueField({
  field,
  value,
  isRequired,
  isMultiLangActive,
  onChange,
}: {
  field: { field_name: string; field_label: string; description?: string; is_translatable?: boolean }
  value: any
  isRequired: boolean
  isMultiLangActive?: boolean
  onChange: (value: string[]) => void
}) {
  const items = useMemo(() => parseMultiValue(value), [value])

  function updateItem(index: number, val: string) {
    const next = items.map((item, i) => (i === index ? val : item))
    onChange(next)
  }

  function removeItem(index: number) {
    onChange(items.filter((_, i) => i !== index))
  }

  function addItem() {
    onChange([...items, ""])
  }

  return (
    <div className="space-y-2 rounded-md border bg-background/50 p-3 sm:col-span-2">
      <div className="flex items-center justify-between border-b pb-2">
        <div>
          <FieldLabel
            label={field.field_label}
            isRequired={isRequired}
            isTranslatable={field.is_translatable}
            isMultiLangActive={isMultiLangActive}
          />
          {field.description ? (
            <p className="text-xs text-muted-foreground mt-0.5">{field.description}</p>
          ) : null}
        </div>
        <Button type="button" variant="outline" size="sm" onClick={addItem}>
          <Plus className="size-3.5" />
          添加项
        </Button>
      </div>
      {items.length === 0 ? (
        <p className="py-3 text-center text-xs text-muted-foreground">暂无项目，点击上方“添加项”输入</p>
      ) : (
        <div className="space-y-1.5">
          {items.map((item, index) => (
            <div key={index} className="flex items-center gap-2">
              <span className="w-6 text-center text-xs text-muted-foreground font-mono">{index + 1}.</span>
              <Input
                value={item}
                onChange={(e) => updateItem(index, e.target.value)}
                placeholder={`输入${field.field_label}项目`}
                className="h-8 text-xs flex-1"
              />
              <IconButton
                variant="ghost"
                size="icon-xs"
                className="text-destructive hover:text-destructive"
                label="删除"
                onClick={() => removeItem(index)}
              >
                <Trash2 className="size-3.5" />
              </IconButton>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

function isSystemField(name: string): boolean {
  return [
    "title",
    "code",
    "summary",
    "body",
    "content",
    "cover_image",
    "published_at",
    "source",
    "keywords",
    "description",
  ].includes(name)
}

function defaultFieldLabel(name: string): string {
  const map: Record<string, string> = {
    title: "信息标题",
    code: "编号/型号",
    summary: "内容摘要",
    body: "正文内容",
    content: "正文内容",
    cover_image: "缩略图",
    published_at: "发布时间",
    source: "信息来源",
    keywords: "关键词",
    description: "描述",
  }
  return map[name] || name
}

function defaultFieldType(name: string): string {
  if (name === "body" || name === "content") return "editor"
  if (name === "cover_image") return "image"
  if (name === "summary" || name === "description") return "textarea"
  if (name === "published_at") return "date"
  return "text"
}

function SingleImageField({
  label,
  value,
  isRequired,
  description,
  isTranslatable,
  isMultiLangActive,
  onChange,
}: {
  label: string
  value: string
  isRequired: boolean
  description?: string
  isTranslatable?: boolean
  isMultiLangActive?: boolean
  onChange: (url: string) => void
}) {
  const [uploading, setUploading] = useState(false)
  const [error, setError] = useState("")
  const [pickerOpen, setPickerOpen] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  async function handleUpload(e: ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    e.target.value = ""
    if (!file) return
    setUploading(true)
    setError("")
    try {
      const res = await uploadMedia(file)
      if (res.asset) onChange(res.asset.url)
    } catch (err) {
      setError(err instanceof Error ? err.message : "图片上传失败")
    } finally {
      setUploading(false)
    }
  }

  return (
    <div className="space-y-2 sm:col-span-2">
      <div className="flex items-center justify-between">
        <FieldLabel
          label={label}
          isRequired={isRequired}
          isTranslatable={isTranslatable}
          isMultiLangActive={isMultiLangActive}
        />
        {description ? <span className="text-xs text-muted-foreground">{description}</span> : null}
      </div>
      <input
        ref={inputRef}
        type="file"
        accept="image/jpeg,image/png,image/gif,image/webp"
        className="hidden"
        onChange={handleUpload}
      />
      <div className="overflow-hidden rounded-lg border bg-muted">
        {value ? (
          <img src={value} alt={label} className="aspect-[3/1] max-h-48 w-full object-contain" />
        ) : (
          <div className="flex aspect-[3/1] max-h-36 items-center justify-center gap-2 text-sm text-muted-foreground">
            <ImagePlus className="size-5" />
            暂未选择{label}
          </div>
        )}
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <Button type="button" variant="outline" size="sm" onClick={() => inputRef.current?.click()} disabled={uploading}>
          {uploading ? <LoaderCircle className="size-3.5 animate-spin" /> : <Upload className="size-3.5" />}
          {uploading ? "上传中" : `上传${label}`}
        </Button>
        <Button type="button" variant="outline" size="sm" onClick={() => setPickerOpen(true)} disabled={uploading}>
          <Library className="size-3.5" />素材库
        </Button>
        {value ? (
          <IconButton label={`清除${label}`} variant="ghost" size="icon-sm" onClick={() => onChange("")}>
            <X />
          </IconButton>
        ) : null}
      </div>
      {error ? <p role="alert" className="text-xs text-destructive">{error}</p> : null}
      <MediaPickerDialog
        open={pickerOpen}
        onOpenChange={setPickerOpen}
        onSelect={(media: MediaAsset) => {
          onChange(media.url)
          setPickerOpen(false)
        }}
      />
    </div>
  )
}

function ContentEditor({
  content,
  categories,
  models,
  modelFields,
  onSave,
  onCancel,
  saving,
}: {
  content: ContentInput
  categories: CategoryItem[]
  models: SystemModel[]
  modelFields: ModelField[]
  onSave: (content: ContentInput, publish?: boolean) => void
  onCancel: () => void
  saving: boolean
}) {
  const [form, setForm] = useState(content)
  const [customError, setCustomError] = useState("")
  const categoryOptions = flattenCategoryTree(categories)
  const [bodyUploading, setBodyUploading] = useState(false)
  const { activeLang, defaultLang, currentLanguage } = useLanguage()
  const isMultiLangActive = activeLang !== defaultLang

  function update<K extends keyof ContentInput>(key: K, value: ContentInput[K]) {
    setForm((current) => ({ ...current, [key]: value }))
  }

  function updateExtra(field: string, value: any) {
    setForm((current) => ({
      ...current,
      extra_data: {
        ...(current.extra_data || {}),
        [field]: value,
      },
    }))
  }

  const selectedCategory = categoryOptions.find((c) => c.id === form.category_id)
  const currentModelId = selectedCategory?.model_id || form.model_id || 1
  const currentModel = models.find((m) => m.id === currentModelId) || models[0]

  const mustFieldSet = useMemo(() => {
    return new Set(currentModel?.must_fields || ["title"])
  }, [currentModel])

  const activeFields = useMemo(() => {
    if (!currentModel) return []
    const tableFields = modelFields.filter((f) => f.table_id === currentModel.table_id)
    const tableFieldMap = new Map(tableFields.map((f) => [f.field_name, f]))

    const checkTranslatable = (fieldName: string, fieldDef?: ModelField) => {
      if (fieldDef?.is_translatable !== undefined) {
        return fieldDef.is_translatable === 1
      }
      return ["title", "summary", "body", "content", "keywords", "description"].includes(fieldName)
    }

    if (currentModel.entry_fields && currentModel.entry_fields.length > 0) {
      return currentModel.entry_fields.map((ef) => {
        const fieldDef = tableFieldMap.get(ef.field)
        return {
          field_name: ef.field,
          field_label: ef.label || fieldDef?.field_label || defaultFieldLabel(ef.field),
          field_type: fieldDef?.field_type || defaultFieldType(ef.field),
          field_options: fieldDef?.field_options || "",
          description: fieldDef?.description || "",
          is_system: fieldDef?.is_system ?? (isSystemField(ef.field) ? 1 : 0),
          is_translatable: checkTranslatable(ef.field, fieldDef),
        }
      })
    }

    return [...tableFields].sort((a, b) => a.sort_order - b.sort_order).map((f) => ({
      field_name: f.field_name,
      field_label: f.field_label,
      field_type: f.field_type,
      field_options: f.field_options,
      description: f.description,
      is_system: f.is_system,
      is_translatable: checkTranslatable(f.field_name, f),
    }))
  }, [currentModel, modelFields])

  function getFieldValue(fieldName: string): any {
    if (fieldName === "body" || fieldName === "content") return form.content
    if (fieldName in form && fieldName !== "extra_data") {
      return (form as any)[fieldName] ?? ""
    }
    return form.extra_data?.[fieldName] ?? ""
  }

  function setFieldValue(fieldName: string, value: any) {
    if (fieldName === "body" || fieldName === "content") {
      update("content", value)
    } else if (fieldName in form && fieldName !== "extra_data") {
      update(fieldName as keyof ContentInput, value)
    } else {
      updateExtra(fieldName, value)
    }
  }

  function handleSave(publish = false) {
    for (const f of activeFields) {
      if (mustFieldSet.has(f.field_name)) {
        const val = getFieldValue(f.field_name)
        const isEmpty =
          val === undefined ||
          val === null ||
          (Array.isArray(val) ? val.length === 0 : String(val).trim() === "")
        if (isEmpty) {
          setCustomError(`请填写必填字段：${f.field_label}`)
          return
        }
      }
    }
    setCustomError("")
    onSave(
      {
        ...form,
        model_id: currentModelId,
        extra_data: form.extra_data || {},
      },
      publish,
    )
  }

  return (
    <>
      <DialogHeader>
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex items-center gap-2">
            <DialogTitle>{content.title ? "编辑内容" : "新增内容"}</DialogTitle>
            {activeLang && (
              <Badge variant="outline" className="text-xs">
                {currentLanguage?.name || activeLang}
              </Badge>
            )}
          </div>
          <Badge variant="outline" className="text-xs font-normal">
            模型：{currentModel?.name || "通用模型"}
          </Badge>
        </div>
        <DialogDescription>
          根据所属分类绑定的系统模型动态配置录入表单。
        </DialogDescription>
      </DialogHeader>

      {isMultiLangActive && (
        <div className="bg-amber-500/10 text-amber-900 dark:text-amber-200 border border-amber-500/20 rounded-md px-4 py-2.5 text-xs flex items-center gap-2">
          <Languages className="size-4 shrink-0 text-amber-600 dark:text-amber-400" />
          <span>当前正在编辑 <strong>{currentLanguage?.name || activeLang}</strong> 语言的内容。标有“多语言”的字段独立保存并支持兜底；标有“通用”的字段在所有语言间共享。</span>
        </div>
      )}

      <div className="space-y-4 py-2">
        {customError ? <InlineAlert>{customError}</InlineAlert> : null}

        {/* 顶部栏目与模型联动控制 */}
        <div className="rounded-lg border bg-muted/40 p-3.5 space-y-2">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <Label className="font-semibold text-sm">所属栏目分类</Label>
            <span className="text-xs text-muted-foreground">
              关联模型：<strong className="text-foreground">{currentModel?.name || "默认模型"}</strong> ({activeFields.length} 个字段)
            </span>
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5 sm:col-span-2">
              <Select
                value={String(form.category_id)}
                onValueChange={(value) => {
                  const catId = Number(value ?? 0)
                  const cat = categoryOptions.find((c) => c.id === catId)
                  setForm((curr) => ({
                    ...curr,
                    category_id: catId,
                    model_id: cat?.model_id || curr.model_id || 1,
                  }))
                }}
              >
                <SelectTrigger className="w-full bg-background">
                  <SelectValue>
                    {(value) => {
                      const selectedID = Number(value ?? 0)
                      return selectedID > 0
                        ? categoryOptions.find((category) => category.id === selectedID)?.name ?? "选择分类"
                        : "未分类"
                    }}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="0">未分类</SelectItem>
                  {categoryOptions.map((category) => (
                    <SelectItem key={category.id} value={String(category.id)}>
                      <span className="whitespace-pre">{"  ".repeat(category.depth)}{category.name}</span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
        </div>

        {/* 核心动态字段流：依据当前模型的 entry_fields 顺序与别名渲染 */}
        <div className="grid gap-5 sm:grid-cols-2">
          {activeFields.map((field) => {
            const isRequired = mustFieldSet.has(field.field_name)
            const val = getFieldValue(field.field_name)

            if (field.field_type === "morepic") {
              return (
                <MultiImageField
                  key={field.field_name}
                  field={field}
                  value={val}
                  isRequired={isRequired}
                  isMultiLangActive={isMultiLangActive}
                  onChange={(nextVal) => setFieldValue(field.field_name, nextVal)}
                />
              )
            }

            if (field.field_type === "multivalue") {
              return (
                <MultiValueField
                  key={field.field_name}
                  field={field}
                  value={val}
                  isRequired={isRequired}
                  isMultiLangActive={isMultiLangActive}
                  onChange={(nextVal) => setFieldValue(field.field_name, nextVal)}
                />
              )
            }

            if (field.field_type === "image") {
              return (
                <SingleImageField
                  key={field.field_name}
                  label={field.field_label}
                  value={val || ""}
                  isRequired={isRequired}
                  description={field.description}
                  isTranslatable={field.is_translatable}
                  isMultiLangActive={isMultiLangActive}
                  onChange={(url) => setFieldValue(field.field_name, url)}
                />
              )
            }

            if (field.field_type === "editor") {
              return (
                <div key={field.field_name} className="space-y-2 sm:col-span-2">
                  <FieldLabel
                    label={field.field_label}
                    isRequired={isRequired}
                    isTranslatable={field.is_translatable}
                    isMultiLangActive={isMultiLangActive}
                    htmlFor={`field-${field.field_name}`}
                  />
                  <RichTextEditor
                    id={`field-${field.field_name}`}
                    value={val || ""}
                    onChange={(content) => setFieldValue(field.field_name, content)}
                    onUploadingChange={setBodyUploading}
                  />
                  {field.description ? (
                    <p className="text-xs text-muted-foreground">{field.description}</p>
                  ) : null}
                </div>
              )
            }

            if (field.field_type === "textarea") {
              return (
                <div key={field.field_name} className="space-y-2 sm:col-span-2">
                  <FieldLabel
                    label={field.field_label}
                    isRequired={isRequired}
                    isTranslatable={field.is_translatable}
                    isMultiLangActive={isMultiLangActive}
                    htmlFor={`field-${field.field_name}`}
                  />
                  <Textarea
                    id={`field-${field.field_name}`}
                    value={val || ""}
                    onChange={(e) => setFieldValue(field.field_name, e.target.value)}
                    placeholder={field.description || `请输入${field.field_label}`}
                    className="min-h-20"
                    required={isRequired}
                  />
                </div>
              )
            }

            if (field.field_type === "select" || field.field_type === "radio") {
              const options = parseFieldOptions(field.field_options)
              return (
                <div key={field.field_name} className="space-y-2">
                  <FieldLabel
                    label={field.field_label}
                    isRequired={isRequired}
                    isTranslatable={field.is_translatable}
                    isMultiLangActive={isMultiLangActive}
                  />
                  <Select
                    value={val ? String(val) : ""}
                    onValueChange={(value) => setFieldValue(field.field_name, value ?? "")}
                  >
                    <SelectTrigger className="w-full">
                      <SelectValue placeholder={`选择${field.field_label}`} />
                    </SelectTrigger>
                    <SelectContent>
                      {options.map((opt) => (
                        <SelectItem key={opt.value} value={opt.value}>
                          {opt.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  {field.description ? (
                    <p className="text-xs text-muted-foreground">{field.description}</p>
                  ) : null}
                </div>
              )
            }

            if (field.field_type === "date") {
              return (
                <div key={field.field_name} className="space-y-2">
                  <FieldLabel
                    label={field.field_label}
                    isRequired={isRequired}
                    isTranslatable={field.is_translatable}
                    isMultiLangActive={isMultiLangActive}
                    htmlFor={`field-${field.field_name}`}
                  />
                  <Input
                    id={`field-${field.field_name}`}
                    type="datetime-local"
                    value={dateTimeInput(val || "")}
                    onChange={(e) => setFieldValue(field.field_name, dateTimeValue(e.target.value))}
                    required={isRequired}
                  />
                  {field.description ? (
                    <p className="text-xs text-muted-foreground">{field.description}</p>
                  ) : null}
                </div>
              )
            }

            if (field.field_type === "number") {
              return (
                <div key={field.field_name} className="space-y-2">
                  <FieldLabel
                    label={field.field_label}
                    isRequired={isRequired}
                    isTranslatable={field.is_translatable}
                    isMultiLangActive={isMultiLangActive}
                    htmlFor={`field-${field.field_name}`}
                  />
                  <Input
                    id={`field-${field.field_name}`}
                    type="number"
                    value={val ?? ""}
                    onChange={(e) => setFieldValue(field.field_name, e.target.value ? Number(e.target.value) : "")}
                    placeholder={field.description || `请输入${field.field_label}`}
                    required={isRequired}
                  />
                  {field.description ? (
                    <p className="text-xs text-muted-foreground">{field.description}</p>
                  ) : null}
                </div>
              )
            }

            const isFullWidth = field.field_name === "title" || field.field_name === "keywords"
            return (
              <div key={field.field_name} className={`space-y-2 ${isFullWidth ? "sm:col-span-2" : ""}`}>
                <FieldLabel
                  label={field.field_label}
                  isRequired={isRequired}
                  isTranslatable={field.is_translatable}
                  isMultiLangActive={isMultiLangActive}
                  htmlFor={`field-${field.field_name}`}
                />
                <Input
                  id={`field-${field.field_name}`}
                  type="text"
                  value={val || ""}
                  onChange={(e) => setFieldValue(field.field_name, e.target.value)}
                  placeholder={
                    field.field_name === "keywords"
                      ? "用 | 分隔关键词"
                      : field.description || `请输入${field.field_label}`
                  }
                  required={isRequired}
                />
                {field.description && field.field_name !== "keywords" ? (
                  <p className="text-xs text-muted-foreground">{field.description}</p>
                ) : null}
              </div>
            )
          })}

          {/* 发布属性设置 */}
          <div className="space-y-3 sm:col-span-2 rounded-lg border p-3.5 bg-card">
            <p className="text-sm font-semibold text-muted-foreground border-b pb-1.5">发布与展示属性</p>
            <div className="grid gap-3 sm:grid-cols-3 items-center">
              <div className="space-y-1">
                <Label htmlFor="content-order">显示排序权重</Label>
                <Input
                  id="content-order"
                  type="number"
                  min="0"
                  value={form.order_id}
                  onChange={(e) => update("order_id", Number(e.target.value))}
                  placeholder="数字越大越靠前"
                />
              </div>
              <div className="flex items-center justify-between rounded-md border p-2.5">
                <div>
                  <p className="text-sm font-medium">公开展示</p>
                  <p className="text-xs text-muted-foreground">生成页面及链接</p>
                </div>
                <Switch
                  checked={form.visible === 1}
                  onCheckedChange={(checked) => update("visible", checked ? 1 : 0)}
                  aria-label="公开展示"
                />
              </div>
              <div className="flex items-center justify-between rounded-md border p-2.5">
                <div>
                  <p className="text-sm font-medium">首页推荐</p>
                  <p className="text-xs text-muted-foreground">主题首页推荐标</p>
                </div>
                <Switch
                  checked={form.featured === 1}
                  onCheckedChange={(checked) => update("featured", checked ? 1 : 0)}
                  aria-label="首页推荐"
                />
              </div>
            </div>
          </div>
        </div>
      </div>

      <DialogFooter>
        <Button variant="outline" onClick={onCancel} disabled={saving || bodyUploading}>
          取消
        </Button>
        <Button onClick={() => handleSave(false)} disabled={saving || bodyUploading || !String(getFieldValue("title") || "").trim()}>
          {saving ? <LoaderCircle className="animate-spin" /> : null}
          仅保存
        </Button>
        <Button onClick={() => handleSave(true)} disabled={saving || bodyUploading || !String(getFieldValue("title") || "").trim()}>
          保存并发布
        </Button>
      </DialogFooter>
    </>
  )
}

export function ContentPage() {
  const [items, setItems] = useState<Content[]>([])
  const [categories, setCategories] = useState<CategoryItem[]>([])
  const [models, setModels] = useState<SystemModel[]>([])
  const [modelFields, setModelFields] = useState<ModelField[]>([])
  const [query, setQuery] = useState("")
  const [appliedQuery, setAppliedQuery] = useState("")
  const [categoryID, setCategoryID] = useState(0)
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const [editorOpen, setEditorOpen] = useState(false)
  const [editing, setEditing] = useState<Content | null>(null)
  const [editingDetail, setEditingDetail] = useState<Content | null>(null)
  const [editorLoading, setEditorLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [deleting, setDeleting] = useState<Content | null>(null)
  const [deleteSaving, setDeleteSaving] = useState(false)

  const { activeLang, setActiveLang, languages, currentLanguage } = useLanguage()

  const categoryOptions = useMemo(() => flattenCategoryTree(categories), [categories])

  useEffect(() => {
    getCategories(activeLang).then(setCategories).catch(() => setCategories([]))
    getSystemModels().then(setModels).catch(() => setModels([]))
    getModelFields().then(setModelFields).catch(() => setModelFields([]))
  }, [activeLang])

  useEffect(() => {
    let active = true
    getContent(page, pageSize, appliedQuery, categoryID, activeLang)
      .then((response) => {
        if (!active) return
        setItems(response.items)
        setTotal(response.total)
        setError("")
      })
      .catch((loadError) => {
        if (active) setError(loadError instanceof Error ? loadError.message : "内容加载失败")
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [appliedQuery, categoryID, page, activeLang])

  const editingID = editing?.id
  useEffect(() => {
    let active = true
    if (editingID && editorOpen) {
      void getContentItem(editingID, activeLang)
        .then((detail) => {
          if (active) setEditingDetail(detail)
        })
        .catch(() => {})
    }
    return () => {
      active = false
    }
  }, [activeLang, editingID, editorOpen])

  const formContent = useMemo<ContentInput>(() => {
    if (!editingDetail) {
      if (categoryID > 0) {
        const cat = categoryOptions.find((c) => c.id === categoryID)
        return {
          ...emptyContent,
          category_id: categoryID,
          model_id: cat?.model_id || 1,
        }
      }
      return emptyContent
    }
    return {
      title: editingDetail.title,
      code: editingDetail.code,
      category_id: editingDetail.category_id,
      summary: editingDetail.summary,
      content: editingDetail.content,
      cover_image: editingDetail.cover_image,
      published_at: editingDetail.published_at,
      source: editingDetail.source,
      keywords: editingDetail.keywords,
      description: editingDetail.description,
      order_id: editingDetail.order_id,
      featured: editingDetail.featured,
      visible: editingDetail.visible,
      model_id: editingDetail.model_id ?? 1,
      extra_data: editingDetail.extra_data ?? {},
    }
  }, [editingDetail, categoryID, categoryOptions])

  function submitSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const nextQuery = query.trim()
    setLoading(true)
    setPage(1)
    setAppliedQuery(nextQuery)
  }

  function openNew() {
    setEditing(null)
    setEditingDetail(null)
    setEditorOpen(true)
  }

  async function openEdit(item: Content) {
    setEditing(item)
    setEditingDetail(null)
    setEditorOpen(true)
    setEditorLoading(true)
    try {
      setEditingDetail(await getContentItem(item.id, activeLang))
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : "内容详情加载失败")
      setEditorOpen(false)
    } finally {
      setEditorLoading(false)
    }
  }

  function closeEditor(open: boolean) {
    setEditorOpen(open)
    if (!open) {
      setEditing(null)
      setEditingDetail(null)
    }
  }

  async function save(payload: ContentInput, publish = false) {
    setSaving(true)
    try {
      const result = editing
        ? await updateContent(editing.id, payload, publish, activeLang)
        : await createContent(payload, publish, activeLang)
      setNotice(publicationMessage(result))
      closeEditor(false)
      setLoading(true)
      const targetPage = editing ? page : 1
      if (!editing) setPage(1)
      const response = await getContent(targetPage, pageSize, appliedQuery, categoryID, activeLang)
      setItems(response.items)
      setTotal(response.total)
    } catch (saveError) {
      setError(saveError instanceof Error ? saveError.message : "保存失败")
    } finally {
      setSaving(false)
      setLoading(false)
    }
  }

  async function confirmDelete() {
    if (!deleting) return
    setDeleteSaving(true)
    try {
      const result = await deleteContent(deleting.id, true)
      setItems((current) => current.filter((item) => item.id !== deleting.id))
      setTotal((current) => Math.max(0, current - 1))
      if (items.length === 1 && page > 1) setPage(page - 1)
      setNotice(result.publish_started ? "内容已删除，正在生成网站。" : "内容已删除并加入发布队列。")
      setDeleting(null)
    } catch (deleteError) {
      setError(deleteError instanceof Error ? deleteError.message : "删除失败")
    } finally {
      setDeleteSaving(false)
    }
  }

  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  return (
    <div className="space-y-4 lg:flex lg:h-full lg:min-h-0 lg:flex-col lg:space-y-0 lg:gap-4">
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-center">
        <div className="flex w-full flex-wrap gap-2 sm:flex-nowrap">
          <form className="flex w-full max-w-md gap-2" onSubmit={submitSearch}>
            <SearchField value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索标题、编号或关键词" />
            <Button type="submit" variant="outline"><Search />搜索</Button>
          </form>
          <Select
            value={String(categoryID)}
            onValueChange={(value) => {
              setLoading(true)
              setPage(1)
              setCategoryID(Number(value ?? 0))
            }}
          >
            <SelectTrigger className="w-full sm:w-56" aria-label="按分类筛选">
              <SelectValue>
                {(value) => {
                  const selectedID = Number(value ?? 0)
                  return selectedID > 0
                    ? categoryOptions.find((category) => category.id === selectedID)?.name ?? "全部分类"
                    : "全部分类"
                }}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="0">全部分类</SelectItem>
              {categoryOptions.map((category) => (
                <SelectItem key={category.id} value={String(category.id)}>
                  <span className="whitespace-pre">{"  ".repeat(category.depth)}{category.name}</span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {languages.length > 1 && (
            <Select
              value={activeLang}
              onValueChange={(val) => {
                if (val) setActiveLang(val)
              }}
            >
              <SelectTrigger className="w-full sm:w-36" aria-label="选择语言">
                <Languages className="size-3.5 mr-1 shrink-0 text-muted-foreground" />
                <SelectValue>
                  {currentLanguage?.name || activeLang}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {languages.map((lang) => (
                  <SelectItem key={lang.code} value={lang.code}>
                    <div className="flex items-center gap-1.5">
                      <span>{lang.name}</span>
                      {lang.is_default === 1 && <span className="text-[10px] text-muted-foreground">(主站)</span>}
                      {lang.is_fallback === 1 && <span className="text-[10px] text-muted-foreground">(兜底)</span>}
                    </div>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        </div>
        <Button onClick={openNew}><Plus />新增内容</Button>
      </div>
      {notice && <p role="status" className="text-sm text-muted-foreground">{notice}</p>}
      {error ? <InlineAlert>{error}</InlineAlert> : null}
      <div className="border-y lg:flex lg:min-h-0 lg:flex-1 lg:flex-col lg:overflow-hidden lg:border">
        <ScrollArea className="lg:h-0 lg:min-h-0 lg:flex-1">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-4">标题</TableHead>
                <TableHead>分类</TableHead>
                <TableHead>发布日期</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className="pr-4 text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading ? (
                <TableRow><TableCell colSpan={5} className="h-28 text-center text-muted-foreground"><LoaderCircle className="mx-auto size-5 animate-spin" /></TableCell></TableRow>
              ) : items.length === 0 ? (
                <TableRow><TableCell colSpan={5} className="h-28 text-center text-muted-foreground">没有匹配的内容</TableCell></TableRow>
              ) : items.map((item) => (
                <TableRow key={item.id}>
                  <TableCell className="max-w-[520px] pl-4">
                    <div className="flex min-w-0 items-center gap-3">
                      <div className="flex size-9 shrink-0 items-center justify-center overflow-hidden rounded-lg bg-muted text-muted-foreground">
                        {item.cover_image ? <img src={item.cover_image} alt="" className="size-full object-contain" /> : <FileText className="size-4" />}
                      </div>
                      <div className="min-w-0">
                        <p className="truncate font-medium">{item.title}</p>
                        <p className="truncate text-xs text-muted-foreground">{item.code || "无编号"} · #{item.id}</p>
                      </div>
                    </div>
                  </TableCell>
                  <TableCell className="text-muted-foreground">{categoryName(categories, item.category_id)}</TableCell>
                  <TableCell className="text-muted-foreground">{formatDate(item.published_at)}</TableCell>
                  <TableCell>
                    <div className="flex gap-1.5 text-xs">
                      <span>{item.visible ? "公开" : "隐藏"}</span>
                      {item.featured ? <span className="text-muted-foreground">推荐</span> : null}
                    </div>
                  </TableCell>
                  <TableCell className="pr-4 text-right">
                    <div className="flex justify-end gap-1">
                      <IconButton variant="ghost" size="icon-sm" onClick={() => openEdit(item)} label={`编辑${item.title}`}><Pencil /></IconButton>
                      <IconButton variant="ghost" size="icon-sm" className="text-destructive hover:text-destructive" onClick={() => setDeleting(item)} label={`删除${item.title}`}><Trash2 /></IconButton>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </ScrollArea>
        <TablePagination page={page} totalPages={totalPages} total={total} pageSize={pageSize} loading={loading} onPageChange={(nextPage) => { setLoading(true); setPage(nextPage) }} />
      </div>

      <Dialog open={editorOpen} onOpenChange={closeEditor}>
        <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-hidden sm:max-w-5xl">
          <ScrollArea className="max-h-[calc(100dvh-4rem)]" contentClassName="space-y-4 pr-2">
            {editorLoading ? (
              <><DialogHeader><DialogTitle>编辑内容</DialogTitle><DialogDescription>正在加载内容。</DialogDescription></DialogHeader><div className="flex h-32 items-center justify-center text-muted-foreground"><LoaderCircle className="size-5 animate-spin" /></div></>
            ) : <ContentEditor key={`${editing?.id ?? "new"}-${activeLang}`} content={formContent} categories={categories} models={models} modelFields={modelFields} onSave={save} onCancel={() => closeEditor(false)} saving={saving} />}
          </ScrollArea>
        </DialogContent>
      </Dialog>
      <ConfirmDialog
        open={Boolean(deleting)}
        onOpenChange={(open) => { if (!open) setDeleting(null) }}
        title="删除内容"
        description={deleting ? `确定删除“${deleting.title}”吗？删除后公开详情页和列表链接都会移除。` : "确认删除这条内容吗？"}
        confirmLabel="确认删除"
        pending={deleteSaving}
        onConfirm={confirmDelete}
      />
    </div>
  )
}
