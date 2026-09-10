import { useEffect, useMemo, useRef, useState } from "react"
import { Check, Copy, Download, FileCode, FileText, LoaderCircle, Search, Upload } from "lucide-react"

import {
	activateTheme,
	getThemeFile,
	getThemeFiles,
	importTheme,
	themeExportURL,
	updateThemeAssignment,
	type ThemeFile,
	type ThemeFileContent,
	type ThemeFileKind,
	type ThemeFiles,
	type ThemeTemplateGroup,
} from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { InlineAlert } from "@/components/app/app-ui"
import { cn } from "@/lib/utils"

function formatBytes(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

function formatModifiedAt(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString("zh-CN")
}

type FileBrowserProps = {
  kind: ThemeFileKind
  files: ThemeFile[]
  selectedPath: string
  search: string
  content: ThemeFileContent | null
  loading: boolean
  error: string
  copied: boolean
  onSearchChange: (value: string) => void
  onSelect: (path: string) => void
  onCopy: () => void
}

function FileBrowser({
  kind,
  files,
  selectedPath,
  search,
  content,
  loading,
  error,
  copied,
  onSearchChange,
  onSelect,
  onCopy,
}: FileBrowserProps) {
  const filteredFiles = useMemo(() => {
    const normalizedSearch = search.trim().toLowerCase()
    if (!normalizedSearch) return files
    return files.filter((file) => file.path.toLowerCase().includes(normalizedSearch))
  }, [files, search])
  const FileIcon = kind === "css" ? FileCode : FileText

  return (
    <div className="grid min-h-[32rem] overflow-hidden rounded-lg border lg:grid-cols-[minmax(15rem,21rem)_minmax(0,1fr)]">
      <section className="flex min-h-0 flex-col border-b bg-muted/20 lg:border-r lg:border-b-0">
        <div className="border-b p-3">
          <div className="relative">
            <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={search}
              onChange={(event) => onSearchChange(event.target.value)}
              placeholder="筛选文件"
              aria-label="筛选文件"
              className="pl-8"
            />
          </div>
          <p className="mt-2 text-xs text-muted-foreground">{filteredFiles.length} / {files.length} 个文件</p>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto p-2">
          {filteredFiles.length === 0 ? (
            <p className="p-4 text-center text-sm text-muted-foreground">没有匹配的文件</p>
          ) : (
            <div className="space-y-0.5">
              {filteredFiles.map((file) => (
                <button
                  key={file.path}
                  type="button"
                  className={cn(
                    "flex w-full items-start gap-2 rounded-md px-2.5 py-2 text-left transition-colors hover:bg-muted",
                    selectedPath === file.path && "bg-background shadow-sm ring-1 ring-foreground/10"
                  )}
                  onClick={() => onSelect(file.path)}
                >
                  <FileIcon className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                  <span className="min-w-0 flex-1 truncate font-mono text-xs leading-5">{file.path}</span>
                  <span className="shrink-0 text-[10px] text-muted-foreground">{formatBytes(file.size)}</span>
                </button>
              ))}
            </div>
          )}
        </div>
      </section>

      <section className="flex min-h-0 min-w-0 flex-col bg-background">
        <div className="flex min-h-14 flex-wrap items-center justify-between gap-3 border-b px-4 py-3">
          <div className="min-w-0">
            <p className="truncate font-mono text-xs font-medium">{(content?.path ?? selectedPath) || "选择文件"}</p>
            {content && <p className="mt-1 text-[11px] text-muted-foreground">{formatBytes(content.size)} · 更新于 {formatModifiedAt(content.modified_at)}</p>}
          </div>
          <Button variant="outline" size="sm" onClick={onCopy} disabled={!content || loading}>
            {copied ? <Check /> : <Copy />}
            {copied ? "已复制" : "复制内容"}
          </Button>
        </div>
        {error ? (
          <div className="p-4"><InlineAlert>{error}</InlineAlert></div>
        ) : loading ? (
          <div className="flex min-h-96 items-center justify-center text-muted-foreground"><LoaderCircle className="size-5 animate-spin" /></div>
        ) : content ? (
          <pre className="min-h-96 flex-1 overflow-auto bg-muted/20 p-4 font-mono text-xs leading-6 whitespace-pre-wrap break-words"><code>{content.content}</code></pre>
        ) : (
          <div className="flex min-h-96 items-center justify-center text-sm text-muted-foreground">选择一个文件查看内容</div>
        )}
      </section>
    </div>
  )
}

function filesForKind(files: ThemeFiles | null, kind: ThemeFileKind) {
  if (!files) return []
  return kind === "css" ? files.css_files : files.template_files
}

type TemplateAssignmentPanelProps = {
  group: ThemeTemplateGroup
  files: ThemeFile[]
  savingKey: string
  onAssignmentChange: (key: string, value: string | null) => void
}

function TemplateAssignmentPanel({
  group,
  files,
  savingKey,
  onAssignmentChange,
}: TemplateAssignmentPanelProps) {
  if (group.assignments.length === 0) return null
  return (
    <div className="mb-4 space-y-3 rounded-lg border p-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <p className="text-sm font-medium">站点模板绑定</p>
          <p className="text-xs text-muted-foreground">首页、招聘、公司介绍、留言和联系等站点页面使用这里的模板；内容栏目在分类中绑定。</p>
        </div>
        <span className="text-xs text-muted-foreground">{group.assignments.length} 个场景</span>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        {group.assignments.map((assignment) => {
          const hasSelectedFile = files.some((file) => file.path === assignment.template_path)
          return (
            <div key={assignment.key} className="min-w-0 space-y-1.5">
              <label className="text-xs font-medium" htmlFor={`template-${assignment.key}`}>{assignment.label}</label>
              <Select
                value={assignment.template_path}
                onValueChange={(value) => onAssignmentChange(assignment.key, value)}
                disabled={savingKey === assignment.key}
              >
                <SelectTrigger id={`template-${assignment.key}`} className="w-full">
                  <SelectValue placeholder="选择模板文件" />
                </SelectTrigger>
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

export function ThemePage() {
  const [files, setFiles] = useState<ThemeFiles | null>(null)
  const [kind, setKind] = useState<ThemeFileKind>("css")
  const [templateGroup, setTemplateGroup] = useState("home")
  const [selectedPath, setSelectedPath] = useState("")
  const [search, setSearch] = useState("")
  const [fileState, setFileState] = useState<{ key: string; content: ThemeFileContent | null; error: string }>({ key: "", content: null, error: "" })
  const [loading, setLoading] = useState(true)
	const [error, setError] = useState("")
	const [notice, setNotice] = useState("")
	const [copiedKey, setCopiedKey] = useState("")
	const [savingAssignmentKey, setSavingAssignmentKey] = useState("")
	const [themeActionID, setThemeActionID] = useState("")
	const [importing, setImporting] = useState(false)
	const themeInputRef = useRef<HTMLInputElement>(null)

	async function loadFiles() {
    setLoading(true)
    setError("")
	try {
		const response = await getThemeFiles()
		setFiles(response)
	} catch (loadError) {
		setError(loadError instanceof Error ? loadError.message : "主题文件加载失败")
	} finally {
		setLoading(false)
	}
	}

	async function handleActivate(id: string) {
		setThemeActionID(id)
		setError("")
		setNotice("")
		try {
			const result = await activateTheme(id)
			setNotice(result.publish_started ? `已切换到${result.theme.name}，网站正在重新生成。` : `已切换到${result.theme.name}。`)
			await loadFiles()
		} catch (actionError) {
			setError(actionError instanceof Error ? actionError.message : "主题切换失败")
		} finally {
			setThemeActionID("")
		}
	}

	async function handleImport(file: File) {
		setImporting(true)
		setError("")
		setNotice("")
		try {
			const result = await importTheme(file)
			setNotice(`已导入主题${result.theme.name}。`)
			await loadFiles()
		} catch (importError) {
			setError(importError instanceof Error ? importError.message : "主题导入失败")
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
        if (active) setError(loadError instanceof Error ? loadError.message : "主题文件加载失败")
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [])

  const templateGroups = files?.template_groups ?? []
  const activeTemplateGroup = templateGroups.find((group) => group.key === templateGroup) ?? templateGroups[0]
  const currentFiles = kind === "template" && activeTemplateGroup
    ? activeTemplateGroup.files
    : filesForKind(files, kind)
  const activePath = currentFiles.some((file) => file.path === selectedPath) ? selectedPath : currentFiles[0]?.path ?? ""
  const activeKey = activePath ? `${kind}:${activePath}` : ""
  const content = fileState.key === activeKey ? fileState.content : null
  const fileError = fileState.key === activeKey ? fileState.error : ""
  const fileLoading = Boolean(activeKey) && fileState.key !== activeKey
  const copied = copiedKey === activeKey

  useEffect(() => {
    if (!activeKey) return
    let active = true
    getThemeFile(kind, activePath)
      .then((response) => {
        if (active) setFileState({ key: activeKey, content: response, error: "" })
      })
      .catch((loadError) => {
        if (active) {
          setFileState({ key: activeKey, content: null, error: loadError instanceof Error ? loadError.message : "文件内容加载失败" })
        }
      })
    return () => {
      active = false
    }
  }, [activeKey, activePath, kind])

  function selectKind(value: string | null) {
    if (value !== "css" && value !== "template") return
    setKind(value)
    setSearch("")
    const nextFiles = filesForKind(files, value)
    setSelectedPath(nextFiles[0]?.path ?? "")
  }

  function selectTemplateGroup(value: string | null) {
    if (!value) return
    setTemplateGroup(value)
    setSearch("")
    const nextGroup = templateGroups.find((group) => group.key === value)
    setSelectedPath(nextGroup?.files[0]?.path ?? "")
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
    } catch (saveError) {
      setError(saveError instanceof Error ? saveError.message : "模板绑定保存失败")
    } finally {
      setSavingAssignmentKey("")
    }
  }

  async function copyContent() {
    if (!content) return
    await navigator.clipboard.writeText(content.content)
    setCopiedKey(activeKey)
    window.setTimeout(() => setCopiedKey(""), 1600)
  }

	return (
		<div className="space-y-4">
			{notice && <div className="rounded-lg border border-emerald-500/30 bg-emerald-500/10 px-3 py-2 text-sm text-emerald-700 dark:text-emerald-300">{notice}</div>}

      {error && <InlineAlert>{error}</InlineAlert>}

		<Card>
        <CardContent className="pt-4">
          {loading && !files ? (
            <div className="flex min-h-64 items-center justify-center text-muted-foreground"><LoaderCircle className="size-5 animate-spin" /></div>
          ) : files ? (
            <Tabs value={kind} onValueChange={selectKind} className="gap-4">
              <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
                <TabsList>
                  <TabsTrigger value="css">CSS 样式 ({files.css_files.length})</TabsTrigger>
                  <TabsTrigger value="template">HTML 模板 ({files.template_files.length})</TabsTrigger>
                </TabsList>
                <div className="flex flex-wrap items-center gap-2">
                  <Select
                    value={files.active_theme}
                    onValueChange={(value) => {
                      if (value) void handleActivate(value)
                    }}
                    disabled={themeActionID !== "" || importing || files.themes.length === 0}
                  >
                    <SelectTrigger aria-label="当前主题" className="w-full sm:w-44">
                      <SelectValue placeholder="选择主题" />
                    </SelectTrigger>
                    <SelectContent>
                      {files.themes.map((theme) => (
                        <SelectItem key={theme.id} value={theme.id}>{theme.name}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <Button
                    variant="outline"
                    size="sm"
                    render={<a href={files.active_theme ? themeExportURL(files.active_theme) : undefined} download={files.active_theme ? `theme-${files.active_theme}.zip` : undefined} />}
                    disabled={!files.active_theme || themeActionID !== "" || importing}
                  >
                    <Download />
                    导出
                  </Button>
                  <input
                    ref={themeInputRef}
                    type="file"
                    accept=".zip,application/zip,application/x-zip-compressed"
                    className="hidden"
                    onChange={(event) => {
                      const file = event.target.files?.[0]
                      if (file) void handleImport(file)
                      event.currentTarget.value = ""
                    }}
                  />
                  <Button variant="outline" size="sm" onClick={() => themeInputRef.current?.click()} disabled={importing || themeActionID !== ""}>
                    {importing ? <LoaderCircle className="animate-spin" /> : <Upload />}
                    导入
                  </Button>
                </div>
              </div>
              <TabsContent value="css" className="mt-0">
                <FileBrowser kind="css" files={files.css_files} selectedPath={activePath} search={search} content={content} loading={fileLoading} error={fileError} copied={copied} onSearchChange={setSearch} onSelect={setSelectedPath} onCopy={() => void copyContent()} />
              </TabsContent>
              <TabsContent value="template" className="mt-0">
                {templateGroups.length > 0 && (
                  <Tabs value={activeTemplateGroup?.key ?? templateGroups[0].key} onValueChange={selectTemplateGroup} className="mb-4">
                    <TabsList className="max-w-full overflow-x-auto">
                      {templateGroups.map((group) => <TabsTrigger key={group.key} value={group.key}>{group.label} ({group.files.length})</TabsTrigger>)}
                    </TabsList>
                  </Tabs>
                )}
                {activeTemplateGroup && (
                  <TemplateAssignmentPanel
                    group={activeTemplateGroup}
                    files={files.template_files}
                    savingKey={savingAssignmentKey}
                    onAssignmentChange={(key, value) => void changeAssignment(key, value)}
                  />
                )}
                <FileBrowser kind="template" files={currentFiles} selectedPath={activePath} search={search} content={content} loading={fileLoading} error={fileError} copied={copied} onSearchChange={setSearch} onSelect={setSelectedPath} onCopy={() => void copyContent()} />
              </TabsContent>
            </Tabs>
          ) : null}
        </CardContent>
      </Card>
    </div>
  )
}
