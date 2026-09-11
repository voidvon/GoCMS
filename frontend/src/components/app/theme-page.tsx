import { useEffect, useMemo, useRef, useState } from "react"
import { Check, Copy, Download, FileCode, FileText, Image as ImageIcon, LoaderCircle, Save, Search, Trash2, Upload } from "lucide-react"

import {
  activateTheme,
  deleteThemeFile,
  getThemeFile,
  getThemeFiles,
  importTheme,
  themeExportURL,
  updateThemeAssignment,
  updateThemeFile,
  uploadThemeFile,
  type CustomFileKind,
  type ThemeFile,
  type ThemeFileContent,
  type ThemeFiles,
  type ThemeTemplateGroup,
} from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Textarea } from "@/components/ui/textarea"
import { ConfirmDialog, InlineAlert } from "@/components/app/app-ui"
import { TemplateLabelsPanel } from "@/components/app/template-labels-panel"
import { TemplateVariablesPanel } from "@/components/app/template-variables-panel"
import { cn } from "@/lib/utils"

type TemplateGroupKey = "home" | "cover" | "list" | "content" | "public"
type TemplateSection = TemplateGroupKey | "tags" | "tempvars" | "custom"

const templateGroupDefinitions: { key: TemplateGroupKey; label: string }[] = [
  { key: "home", label: "首页模板" },
  { key: "cover", label: "封面模板" },
  { key: "list", label: "列表模板" },
  { key: "content", label: "内容模板" },
  { key: "public", label: "公共模板" },
]

function formatBytes(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

function formatModifiedAt(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString("zh-CN")
}

function emptyTemplateGroup(key: TemplateGroupKey): ThemeTemplateGroup {
  return {
    key,
    label: templateGroupDefinitions.find((item) => item.key === key)?.label ?? "公共模板",
    count: 0,
    files: [],
    assignments: [],
  }
}

function getTemplateGroup(files: ThemeFiles, key: TemplateGroupKey) {
  return (
    files.template_groups.find((group) => group.key === key) ??
    (key === "public" ? files.template_groups.find((group) => group.key === "other") : undefined) ??
    emptyTemplateGroup(key)
  )
}

function customFilesForKind(files: ThemeFiles, kind: CustomFileKind) {
  if (kind === "css") return files.custom_files?.css ?? files.css_files
  if (kind === "js") return files.custom_files?.js ?? files.js_files
  return files.custom_files?.images ?? files.image_files
}

function updateCustomFileCollection(files: ThemeFiles, kind: CustomFileKind, item: ThemeFile) {
  const current = customFilesForKind(files, kind)
  const next = [...current.filter((file) => file.path !== item.path), item].sort((left, right) => left.path.localeCompare(right.path))
  const custom = files.custom_files ?? { css: files.css_files, js: files.js_files, images: files.image_files }
  const nextCustom = {
    ...custom,
    ...(kind === "css" ? { css: next } : {}),
    ...(kind === "js" ? { js: next } : {}),
    ...(kind === "image" ? { images: next } : {}),
  }
  return {
    ...files,
    css_files: kind === "css" ? next : files.css_files,
    js_files: kind === "js" ? next : files.js_files,
    image_files: kind === "image" ? next : files.image_files,
    custom_files: nextCustom,
  }
}

function removeCustomFileCollection(files: ThemeFiles, kind: CustomFileKind, path: string) {
  const current = customFilesForKind(files, kind).filter((file) => file.path !== path)
  const custom = files.custom_files ?? { css: files.css_files, js: files.js_files, images: files.image_files }
  const nextCustom = {
    ...custom,
    ...(kind === "css" ? { css: current } : {}),
    ...(kind === "js" ? { js: current } : {}),
    ...(kind === "image" ? { images: current } : {}),
  }
  return {
    ...files,
    css_files: kind === "css" ? current : files.css_files,
    js_files: kind === "js" ? current : files.js_files,
    image_files: kind === "image" ? current : files.image_files,
    custom_files: nextCustom,
  }
}

function updateTemplateFileCollection(files: ThemeFiles, item: ThemeFile) {
  const update = (items: ThemeFile[]) => [...items.filter((file) => file.path !== item.path), item].sort((left, right) => left.path.localeCompare(right.path))
  return {
    ...files,
    template_files: update(files.template_files),
    template_groups: files.template_groups.map((group) => ({
      ...group,
      files: group.files.some((file) => file.path === item.path) ? update(group.files) : group.files,
    })),
  }
}

function assetURL(filePath: string) {
  return `/${filePath.split("/").map((part) => encodeURIComponent(part)).join("/")}`
}

type FileBrowserProps = {
  files: ThemeFile[]
  selectedPath: string
  search: string
  content: ThemeFileContent | null
  draft: string
  loading: boolean
  error: string
  copied: boolean
  saving: boolean
  onSearchChange: (value: string) => void
  onSelect: (path: string) => void
  onCopy: () => void
  onDraftChange: (value: string) => void
  onSave: () => void
}

function FileBrowser({ files, selectedPath, search, content, draft, loading, error, copied, saving, onSearchChange, onSelect, onCopy, onDraftChange, onSave }: FileBrowserProps) {
  const filteredFiles = useMemo(() => {
    const normalizedSearch = search.trim().toLowerCase()
    if (!normalizedSearch) return files
    return files.filter((file) => file.path.toLowerCase().includes(normalizedSearch))
  }, [files, search])

  return (
    <div className="grid min-h-[28rem] overflow-hidden rounded-lg border lg:h-[calc(100dvh-13rem)] lg:grid-cols-[minmax(15rem,21rem)_minmax(0,1fr)]">
      <section className="flex min-h-0 flex-col border-b bg-muted/20 lg:border-r lg:border-b-0">
        <div className="border-b p-3">
          <div className="relative">
            <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input value={search} onChange={(event) => onSearchChange(event.target.value)} placeholder="筛选文件" aria-label="筛选文件" className="pl-8" />
          </div>
          <p className="mt-2 text-xs text-muted-foreground">{filteredFiles.length} / {files.length} 个文件</p>
        </div>
        <ScrollArea className="min-h-0 flex-1" contentClassName="p-2">
          {filteredFiles.length === 0 ? (
            <p className="p-4 text-center text-sm text-muted-foreground">当前分类还没有模板文件</p>
          ) : (
            <div className="space-y-0.5">
              {filteredFiles.map((file) => (
                <button
                  key={file.path}
                  type="button"
                  className={cn(
                    "flex w-full items-start gap-2 rounded-md px-2.5 py-2 text-left transition-colors hover:bg-muted",
                    selectedPath === file.path && "bg-background shadow-sm ring-1 ring-foreground/10",
                  )}
                  onClick={() => onSelect(file.path)}
                >
                  <FileText className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                  <span className="min-w-0 flex-1 truncate font-mono text-xs leading-5">{file.path}</span>
                  <span className="shrink-0 text-[10px] text-muted-foreground">{formatBytes(file.size)}</span>
                </button>
              ))}
            </div>
          )}
        </ScrollArea>
      </section>

      <section className="flex min-h-0 min-w-0 flex-col bg-background">
        <div className="flex min-h-14 flex-wrap items-center justify-between gap-3 border-b px-4 py-3">
          <div className="min-w-0">
            <p className="truncate font-mono text-xs font-medium">{(content?.path ?? selectedPath) || "选择模板"}</p>
            {content && <p className="mt-1 text-[11px] text-muted-foreground">{formatBytes(content.size)} · 更新于 {formatModifiedAt(content.modified_at)}</p>}
          </div>
          <div className="flex items-center gap-1">
            <Button variant="outline" size="sm" onClick={onSave} disabled={!content || loading || saving}><Save />{saving ? "保存中" : "保存"}</Button>
            <Button variant="ghost" size="icon-sm" onClick={onCopy} disabled={!content || loading} aria-label="复制模板内容" title="复制模板内容">
              {copied ? <Check /> : <Copy />}
            </Button>
          </div>
        </div>
        {error ? (
          <div className="p-4"><InlineAlert>{error}</InlineAlert></div>
        ) : loading ? (
          <div className="flex min-h-96 items-center justify-center text-muted-foreground"><LoaderCircle className="size-5 animate-spin" /></div>
        ) : content ? (
          <Textarea value={draft} onChange={(event) => onDraftChange(event.target.value)} className="min-h-96 flex-1 resize-none rounded-none border-0 bg-muted/20 p-4 font-mono text-xs leading-6 shadow-none focus-visible:ring-0" spellCheck={false} aria-label={`${content.path} 模板内容`} />
        ) : (
          <div className="flex min-h-96 items-center justify-center text-sm text-muted-foreground">选择一个模板查看内容</div>
        )}
      </section>
    </div>
  )
}

type SingleTemplateEditorProps = {
  filePath: string
  content: ThemeFileContent | null
  draft: string
  loading: boolean
  error: string
  copied: boolean
  saving: boolean
  onCopy: () => void
  onDraftChange: (value: string) => void
  onSave: () => void
}

function SingleTemplateEditor({
  filePath,
  content,
  draft,
  loading,
  error,
  copied,
  saving,
  onCopy,
  onDraftChange,
  onSave,
}: SingleTemplateEditorProps) {
  return (
    <div className="flex min-h-[28rem] flex-col overflow-hidden rounded-lg border bg-background lg:h-[calc(100dvh-13rem)]">
      <div className="flex min-h-14 flex-wrap items-center justify-between gap-3 border-b px-4 py-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <FileText className="size-4 shrink-0 text-muted-foreground" />
            <p className="truncate font-mono text-xs font-medium">{(content?.path ?? filePath) || "index.html"}</p>
          </div>
          {content && (
            <p className="mt-1 text-[11px] text-muted-foreground">
              {formatBytes(content.size)} · 更新于 {formatModifiedAt(content.modified_at)}
            </p>
          )}
        </div>
        <div className="flex items-center gap-1">
          <Button variant="outline" size="sm" onClick={onSave} disabled={!content || loading || saving}>
            <Save />{saving ? "保存中" : "保存"}
          </Button>
          <Button variant="ghost" size="icon-sm" onClick={onCopy} disabled={!content || loading} aria-label="复制模板内容" title="复制模板内容">
            {copied ? <Check /> : <Copy />}
          </Button>
        </div>
      </div>
      {error ? (
        <div className="p-4"><InlineAlert>{error}</InlineAlert></div>
      ) : loading ? (
        <div className="flex min-h-96 items-center justify-center text-muted-foreground"><LoaderCircle className="size-5 animate-spin" /></div>
      ) : content ? (
        <Textarea
          value={draft}
          onChange={(event) => onDraftChange(event.target.value)}
          className="min-h-96 flex-1 resize-none rounded-none border-0 bg-muted/20 p-4 font-mono text-xs leading-6 shadow-none focus-visible:ring-0"
          spellCheck={false}
          aria-label={`${content.path} 模板内容`}
        />
      ) : (
        <div className="flex min-h-96 items-center justify-center text-sm text-muted-foreground">
          当前模板组缺少首页模板文件（{filePath || "index.html"}）
        </div>
      )}
    </div>
  )
}

type TemplateAssignmentPanelProps = {
  group: ThemeTemplateGroup
  files: ThemeFile[]
  savingKey: string
  onAssignmentChange: (key: string, value: string | null) => void
}

function TemplateAssignmentPanel({ group, files, savingKey, onAssignmentChange }: TemplateAssignmentPanelProps) {
  if (group.assignments.length === 0) return null
  return (
    <div className="mb-4 space-y-3 rounded-lg border p-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <p className="text-sm font-medium">模板绑定</p>
          <p className="text-xs text-muted-foreground">全局页面使用这里的模板；栏目列表、封面和内容模板在分类中绑定。</p>
        </div>
        <span className="text-xs text-muted-foreground">{group.assignments.length} 个场景</span>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        {group.assignments.map((assignment) => {
          const hasSelectedFile = files.some((file) => file.path === assignment.template_path)
          return (
            <div key={assignment.key} className="min-w-0 space-y-1.5">
              <label className="text-xs font-medium" htmlFor={`template-${assignment.key}`}>{assignment.label}</label>
              <Select value={assignment.template_path} onValueChange={(value) => onAssignmentChange(assignment.key, value)} disabled={savingKey === assignment.key}>
                <SelectTrigger id={`template-${assignment.key}`} className="w-full"><SelectValue placeholder="选择模板文件" /></SelectTrigger>
                <SelectContent>
                  {!hasSelectedFile && <SelectItem value={assignment.template_path}>{assignment.template_path}（文件缺失）</SelectItem>}
                  {files.map((file) => <SelectItem key={file.path} value={file.path}>{file.path}</SelectItem>)}
                </SelectContent>
              </Select>
              {!assignment.available && <p className="text-[11px] text-destructive">当前文件不存在，发布前请重新选择。</p>}
            </div>
          )
        })}
      </div>
    </div>
  )
}

type CustomFilesPanelProps = {
  files: ThemeFiles
  kind: CustomFileKind
  onFilesChange: (files: ThemeFiles) => void
  onNotice: (notice: string) => void
  onError: (error: string) => void
}

function CustomFilesPanel({ files, kind, onFilesChange, onNotice, onError }: CustomFilesPanelProps) {
  const fileInputRef = useRef<HTMLInputElement>(null)
  const [activeKind, setActiveKind] = useState<CustomFileKind>(kind)
  const [selectedPath, setSelectedPath] = useState("")
  const [search, setSearch] = useState("")
  const [uploadPath, setUploadPath] = useState("")
  const [draft, setDraft] = useState("")
  const [fileState, setFileState] = useState<{ key: string; content: ThemeFileContent | null; error: string }>({ key: "", content: null, error: "" })
  const [busy, setBusy] = useState<"upload" | "save" | "delete" | "">("")
  const [deleteTarget, setDeleteTarget] = useState<ThemeFile | null>(null)

  const customFiles = customFilesForKind(files, activeKind)
  const filteredFiles = useMemo(() => {
    const normalizedSearch = search.trim().toLowerCase()
    if (!normalizedSearch) return customFiles
    return customFiles.filter((file) => file.path.toLowerCase().includes(normalizedSearch))
  }, [customFiles, search])
  const activePath = customFiles.some((file) => file.path === selectedPath) ? selectedPath : customFiles[0]?.path ?? ""
  const activeFile = customFiles.find((file) => file.path === activePath) ?? null
  const contentKey = activeKind === "image" ? "" : `${activeKind}:${activePath}`
  const content = fileState.key === contentKey ? fileState.content : null
  const loading = Boolean(contentKey) && fileState.key !== contentKey
  const title = activeKind === "css" ? "CSS 文件" : activeKind === "js" ? "JS 文件" : "图片文件"
  const accept = activeKind === "css" ? ".css,text/css" : activeKind === "js" ? ".js,text/javascript" : "image/png,image/jpeg,image/gif,image/webp,image/x-icon"

  useEffect(() => {
    if (activeKind === "image" || !activePath) return
    let active = true
    getThemeFile(activeKind, activePath)
      .then((response) => {
        if (!active) return
        setFileState({ key: contentKey, content: response, error: "" })
        setDraft(response.content)
      })
      .catch((loadError) => {
        if (active) setFileState({ key: contentKey, content: null, error: loadError instanceof Error ? loadError.message : "文件内容加载失败" })
      })
    return () => {
      active = false
    }
  }, [activeKind, activePath, contentKey])

  function selectKind(nextKind: CustomFileKind) {
    setActiveKind(nextKind)
    setSearch("")
    setSelectedPath(customFilesForKind(files, nextKind)[0]?.path ?? "")
  }

  async function handleUpload(file: File) {
    setBusy("upload")
    onError("")
    try {
      const result = await uploadThemeFile(activeKind, file, uploadPath.trim() || undefined)
      onFilesChange(updateCustomFileCollection(files, activeKind, result.file))
      setSelectedPath(result.file.path)
      setUploadPath("")
      onNotice(`${title}已上传。`)
    } catch (uploadError) {
      onError(uploadError instanceof Error ? uploadError.message : `${title}上传失败`)
    } finally {
      setBusy("")
    }
  }

  async function saveFile() {
    if (!activePath || activeKind === "image") return
    setBusy("save")
    onError("")
    try {
      const result = await updateThemeFile(activeKind, activePath, draft)
      onFilesChange(updateCustomFileCollection(files, activeKind, result.file))
      setFileState((current) => current.content ? { ...current, content: { ...current.content, ...result.file, content: draft } } : current)
      onNotice(`${title}已保存。`)
    } catch (saveError) {
      onError(saveError instanceof Error ? saveError.message : `${title}保存失败`)
    } finally {
      setBusy("")
    }
  }

  async function deleteFile() {
    if (!deleteTarget) return
    setBusy("delete")
    onError("")
    try {
      await deleteThemeFile(activeKind, deleteTarget.path)
      const nextFiles = removeCustomFileCollection(files, activeKind, deleteTarget.path)
      onFilesChange(nextFiles)
      setSelectedPath(customFilesForKind(nextFiles, activeKind)[0]?.path ?? "")
      setDeleteTarget(null)
      onNotice(`${title}已删除。`)
    } catch (deleteError) {
      onError(deleteError instanceof Error ? deleteError.message : `${title}删除失败`)
    } finally {
      setBusy("")
    }
  }

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="text-sm font-medium">自定义文件</p>
          <p className="text-xs text-muted-foreground">管理模板引用的 CSS、JS 和图片，文件会保存到当前模板组的公开资源目录。</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Input value={uploadPath} onChange={(event) => setUploadPath(event.target.value)} placeholder="可选保存路径" aria-label="可选保存路径" className="w-44" />
          <input
            ref={fileInputRef}
            type="file"
            accept={accept}
            className="hidden"
            onChange={(event) => {
              const file = event.target.files?.[0]
              if (file) void handleUpload(file)
              event.currentTarget.value = ""
            }}
          />
          <Button variant="outline" size="sm" onClick={() => fileInputRef.current?.click()} disabled={busy !== ""}>
            {busy === "upload" ? <LoaderCircle className="animate-spin" /> : <Upload />}
            上传{activeKind === "image" ? "图片" : "文件"}
          </Button>
        </div>
      </div>

      <Tabs value={activeKind} onValueChange={(value) => selectKind(value as CustomFileKind)}>
        <TabsList>
          <TabsTrigger value="css"><FileCode /> CSS ({customFilesForKind(files, "css").length})</TabsTrigger>
          <TabsTrigger value="js"><FileCode /> JS ({customFilesForKind(files, "js").length})</TabsTrigger>
          <TabsTrigger value="image"><ImageIcon /> 图片 ({customFilesForKind(files, "image").length})</TabsTrigger>
        </TabsList>
      </Tabs>

      <div className="grid min-h-[28rem] overflow-hidden rounded-lg border lg:h-[calc(100dvh-16rem)] lg:grid-cols-[minmax(15rem,21rem)_minmax(0,1fr)]">
        <section className="flex min-h-0 flex-col border-b bg-muted/20 lg:border-r lg:border-b-0">
          <div className="border-b p-3">
            <div className="relative">
              <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="筛选文件" aria-label="筛选自定义文件" className="pl-8" />
            </div>
            <p className="mt-2 text-xs text-muted-foreground">{filteredFiles.length} / {customFiles.length} 个文件</p>
          </div>
          <ScrollArea className="min-h-0 flex-1" contentClassName="p-2">
            {filteredFiles.length === 0 ? (
              <p className="p-4 text-center text-sm text-muted-foreground">当前分类还没有文件</p>
            ) : (
              <div className="space-y-0.5">
                {filteredFiles.map((file) => (
                  <button
                    key={file.path}
                    type="button"
                    className={cn(
                      "flex w-full items-start gap-2 rounded-md px-2.5 py-2 text-left transition-colors hover:bg-muted",
                      activePath === file.path && "bg-background shadow-sm ring-1 ring-foreground/10",
                    )}
                    onClick={() => setSelectedPath(file.path)}
                  >
                    {activeKind === "image" ? <ImageIcon className="mt-0.5 size-4 shrink-0 text-muted-foreground" /> : <FileCode className="mt-0.5 size-4 shrink-0 text-muted-foreground" />}
                    <span className="min-w-0 flex-1 truncate font-mono text-xs leading-5">{file.path}</span>
                    <span className="shrink-0 text-[10px] text-muted-foreground">{formatBytes(file.size)}</span>
                  </button>
                ))}
              </div>
            )}
          </ScrollArea>
        </section>

        <section className="flex min-h-0 min-w-0 flex-col bg-background">
          <div className="flex min-h-14 flex-wrap items-center justify-between gap-3 border-b px-4 py-3">
            <div className="min-w-0">
              <p className="truncate font-mono text-xs font-medium">{activePath || "选择文件"}</p>
              {activeFile && <p className="mt-1 text-[11px] text-muted-foreground">{formatBytes(activeFile.size)} · 更新于 {formatModifiedAt(activeFile.modified_at)}</p>}
            </div>
            <div className="flex items-center gap-1">
              {activeKind !== "image" && <Button variant="outline" size="sm" onClick={() => void saveFile()} disabled={!content || busy !== "" || loading}><Save />{busy === "save" ? "保存中" : "保存"}</Button>}
              {activeFile && <Button variant="ghost" size="icon-sm" className="text-destructive hover:text-destructive" onClick={() => setDeleteTarget(activeFile)} disabled={busy !== ""} aria-label={`删除${activeFile.path}`} title={`删除${activeFile.path}`}><Trash2 /></Button>}
            </div>
          </div>
          {loading ? (
            <div className="flex min-h-96 items-center justify-center text-muted-foreground"><LoaderCircle className="size-5 animate-spin" /></div>
          ) : fileState.error ? (
            <div className="p-4"><InlineAlert>{fileState.error}</InlineAlert></div>
          ) : activeKind === "image" && activeFile ? (
            <ScrollArea className="min-h-96 flex-1 bg-muted/20" contentClassName="flex min-h-full items-center justify-center p-6">
              <img src={assetURL(activeFile.path)} alt={activeFile.path} className="max-h-[min(52vh,32rem)] max-w-full object-contain" />
            </ScrollArea>
          ) : content ? (
            <Textarea value={draft} onChange={(event) => setDraft(event.target.value)} className="min-h-96 flex-1 resize-none rounded-none border-0 bg-muted/20 p-4 font-mono text-xs leading-6 shadow-none focus-visible:ring-0" spellCheck={false} aria-label={`${activePath} 文件内容`} />
          ) : (
            <div className="flex min-h-96 items-center justify-center text-sm text-muted-foreground">选择一个文件进行编辑</div>
          )}
        </section>
      </div>

      <ConfirmDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => { if (!open && busy !== "delete") setDeleteTarget(null) }}
        title={`删除${title}`}
        description={deleteTarget ? `确定删除“${deleteTarget.path}”吗？此操作会立即从当前模板组资源目录中移除文件。` : "确认删除这个文件吗？"}
        confirmLabel="删除"
        pending={busy === "delete"}
        onConfirm={() => void deleteFile()}
      />
    </div>
  )
}

export function ThemePage() {
  const [files, setFiles] = useState<ThemeFiles | null>(null)
  const [section, setSection] = useState<TemplateSection>("home")
  const [selectedPath, setSelectedPath] = useState("")
  const [search, setSearch] = useState("")
  const [fileState, setFileState] = useState<{ key: string; content: ThemeFileContent | null; error: string }>({ key: "", content: null, error: "" })
  const [templateDraft, setTemplateDraft] = useState("")
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const [copiedKey, setCopiedKey] = useState("")
  const [savingTemplate, setSavingTemplate] = useState(false)
  const [savingAssignmentKey, setSavingAssignmentKey] = useState("")
  const [templateGroupActionID, setTemplateGroupActionID] = useState("")
  const [importing, setImporting] = useState(false)
  const templateGroupInputRef = useRef<HTMLInputElement>(null)

  async function loadFiles() {
    setLoading(true)
    setError("")
    try {
      const response = await getThemeFiles()
      setFiles(response)
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : "模板组加载失败")
    } finally {
      setLoading(false)
    }
  }

  async function handleActivate(id: string) {
    setTemplateGroupActionID(id)
    setError("")
    setNotice("")
    try {
      const result = await activateTheme(id)
      setNotice(result.publish_started ? `已切换到模板组${result.theme.name}，网站正在重新生成。` : `已切换到模板组${result.theme.name}。`)
      await loadFiles()
    } catch (actionError) {
      setError(actionError instanceof Error ? actionError.message : "模板组切换失败")
    } finally {
      setTemplateGroupActionID("")
    }
  }

  async function handleImport(file: File) {
    setImporting(true)
    setError("")
    setNotice("")
    try {
      const result = await importTheme(file)
      setNotice(`已导入模板组${result.theme.name}。`)
      await loadFiles()
    } catch (importError) {
      setError(importError instanceof Error ? importError.message : "模板组导入失败")
    } finally {
      setImporting(false)
    }
  }

  useEffect(() => {
    let active = true
    getThemeFiles()
      .then((response) => {
        if (active) setFiles(response)
      })
      .catch((loadError) => {
        if (active) setError(loadError instanceof Error ? loadError.message : "模板组加载失败")
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [])

  const activeGroup = files && section !== "tags" && section !== "tempvars" && section !== "custom" ? getTemplateGroup(files, section) : null
  const defaultPath = section === "home" ? (activeGroup?.files[0]?.path ?? "index.html") : (activeGroup?.files[0]?.path ?? "")
  const activePath = activeGroup?.files.some((file) => file.path === selectedPath) ? selectedPath : defaultPath
  const activeKey = activePath ? `template:${activePath}` : ""
  const content = fileState.key === activeKey ? fileState.content : null
  const fileError = fileState.key === activeKey ? fileState.error : ""
  const fileLoading = Boolean(activeKey) && fileState.key !== activeKey
  const copied = copiedKey === activeKey
  const labelGroup = files?.template_groups.find((group) => group.key === "label")
  const templateGroupsCatalog = files?.template_groups_catalog?.length ? files.template_groups_catalog : files?.themes ?? []

  useEffect(() => {
    if (!activeKey || !activePath) return
    let active = true
    getThemeFile("template", activePath)
      .then((response) => {
        if (active) {
          setFileState({ key: activeKey, content: response, error: "" })
          setTemplateDraft(response.content)
        }
      })
      .catch((loadError) => {
        if (active) setFileState({ key: activeKey, content: null, error: loadError instanceof Error ? loadError.message : "模板内容加载失败" })
      })
    return () => {
      active = false
    }
  }, [activeKey, activePath])

  function selectSection(value: string | null) {
    if (!value) return
    setSection(value as TemplateSection)
    setSearch("")
    setSelectedPath("")
  }

  async function saveTemplate() {
    if (!activePath || !content) return
    setSavingTemplate(true)
    setError("")
    try {
      const result = await updateThemeFile("template", activePath, templateDraft)
      setFiles((current) => current ? updateTemplateFileCollection(current, result.file) : current)
      setFileState((current) => current.content ? { ...current, content: { ...current.content, ...result.file, content: templateDraft } } : current)
      setNotice("模板已保存。")
    } catch (saveError) {
      setError(saveError instanceof Error ? saveError.message : "模板保存失败")
    } finally {
      setSavingTemplate(false)
    }
  }

  async function changeAssignment(key: string, value: string | null) {
    if (!value) return
    setSavingAssignmentKey(key)
    setError("")
    try {
      const result = await updateThemeAssignment(key, value)
      setFiles((current) => current ? {
        ...current,
        template_groups: current.template_groups.map((group) => ({
          ...group,
          assignments: group.assignments.map((assignment) => assignment.key === key
            ? { ...assignment, template_path: result.template_path, available: true }
            : assignment),
        })),
      } : current)
      setSelectedPath(result.template_path)
      setNotice("模板绑定已保存。")
    } catch (saveError) {
      setError(saveError instanceof Error ? saveError.message : "模板绑定保存失败")
    } finally {
      setSavingAssignmentKey("")
    }
  }

  async function copyContent() {
    if (!content) return
    try {
      await navigator.clipboard.writeText(content.content)
      setCopiedKey(activeKey)
      window.setTimeout(() => setCopiedKey(""), 1600)
    } catch {
      setError("复制模板内容失败")
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 className="text-base font-semibold">模板组</h2>
          <p className="mt-1 text-sm text-muted-foreground">对齐帝国 CMS 模板体系，按首页、封面、列表、内容、标签模板、公共模板变量和公共模板统一管理。</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Select value={files?.active_theme ?? ""} onValueChange={(value) => { if (value) void handleActivate(value) }} disabled={templateGroupActionID !== "" || importing || templateGroupsCatalog.length === 0}>
            <SelectTrigger aria-label="当前模板组" className="w-full sm:w-48"><SelectValue placeholder="选择模板组" /></SelectTrigger>
            <SelectContent>
              {templateGroupsCatalog.map((group) => <SelectItem key={group.id} value={group.id}>{group.name}</SelectItem>)}
            </SelectContent>
          </Select>
          <Button variant="outline" size="sm" render={<a href={files?.active_theme ? themeExportURL(files.active_theme) : undefined} download={files?.active_theme ? `template-group-${files.active_theme}.zip` : undefined} />} disabled={!files?.active_theme || templateGroupActionID !== "" || importing}>
            <Download />
            导出模板组
          </Button>
          <input
            ref={templateGroupInputRef}
            type="file"
            accept=".zip,application/zip,application/x-zip-compressed"
            className="hidden"
            onChange={(event) => {
              const file = event.target.files?.[0]
              if (file) void handleImport(file)
              event.currentTarget.value = ""
            }}
          />
          <Button variant="outline" size="sm" onClick={() => templateGroupInputRef.current?.click()} disabled={importing || templateGroupActionID !== ""}>
            {importing ? <LoaderCircle className="animate-spin" /> : <Upload />}
            导入模板组
          </Button>
        </div>
      </div>

      {notice && <div className="rounded-lg border border-emerald-500/30 bg-emerald-500/10 px-3 py-2 text-sm text-emerald-700 dark:text-emerald-300">{notice}</div>}
      {error && <InlineAlert>{error}</InlineAlert>}

      {loading && !files ? (
        <div className="flex min-h-64 items-center justify-center text-muted-foreground"><LoaderCircle className="size-5 animate-spin" /></div>
      ) : files ? (
        <Tabs value={section} onValueChange={selectSection} className="gap-4">
          <ScrollArea className="max-w-full" orientation="horizontal">
            <TabsList className="w-max">
              <TabsTrigger value="home">首页模板 ({getTemplateGroup(files, "home").count})</TabsTrigger>
              <TabsTrigger value="cover">封面模板 ({getTemplateGroup(files, "cover").count})</TabsTrigger>
              <TabsTrigger value="list">列表模板 ({getTemplateGroup(files, "list").count})</TabsTrigger>
              <TabsTrigger value="content">内容模板 ({getTemplateGroup(files, "content").count})</TabsTrigger>
              <TabsTrigger value="tags">标签模板</TabsTrigger>
              <TabsTrigger value="tempvars">公共模板变量 ({labelGroup?.count ?? 0})</TabsTrigger>
              <TabsTrigger value="public">公共模板 ({getTemplateGroup(files, "public").count})</TabsTrigger>
              <TabsTrigger value="custom">自定义文件</TabsTrigger>
            </TabsList>
          </ScrollArea>

          {templateGroupDefinitions.map((definition) => {
            const group = getTemplateGroup(files, definition.key)
            return (
              <TabsContent key={definition.key} value={definition.key} className="mt-0">
                {definition.key === "home" ? (
                  <SingleTemplateEditor
                    filePath={activePath || group.files[0]?.path || "index.html"}
                    content={content}
                    draft={templateDraft}
                    loading={fileLoading}
                    error={fileError}
                    copied={copied}
                    saving={savingTemplate}
                    onCopy={() => void copyContent()}
                    onDraftChange={setTemplateDraft}
                    onSave={() => void saveTemplate()}
                  />
                ) : (
                  <>
                    <TemplateAssignmentPanel
                      group={group}
                      files={files.template_files}
                      savingKey={savingAssignmentKey}
                      onAssignmentChange={(key, value) => void changeAssignment(key, value)}
                    />
                    <FileBrowser
                      files={group.files}
                      selectedPath={activePath}
                      search={search}
                      content={content}
                      draft={templateDraft}
                      loading={fileLoading}
                      error={fileError}
                      copied={copied}
                      saving={savingTemplate}
                      onSearchChange={setSearch}
                      onSelect={setSelectedPath}
                      onCopy={() => void copyContent()}
                      onDraftChange={setTemplateDraft}
                      onSave={() => void saveTemplate()}
                    />
                  </>
                )}
              </TabsContent>
            )
          })}

          <TabsContent value="tags" className="mt-0">
            <TemplateLabelsPanel />
          </TabsContent>

          <TabsContent value="tempvars" className="mt-0">
            <TemplateVariablesPanel />
          </TabsContent>

          <TabsContent value="custom" className="mt-0">
            <CustomFilesPanel files={files} kind="css" onFilesChange={setFiles} onNotice={setNotice} onError={setError} />
          </TabsContent>
        </Tabs>
      ) : null}
    </div>
  )
}
