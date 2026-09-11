import { useEffect, useRef, useState } from "react"
import {
  AlignCenter,
  AlignJustify,
  AlignLeft,
  AlignRight,
  Bold,
  Code2,
  Heading1,
  Heading2,
  Heading3,
  ImagePlus,
  Italic,
  Link2,
  List,
  ListOrdered,
  Library,
  Minus,
  Quote,
  Redo2,
  RemoveFormatting,
  Table2,
  Trash2,
  Underline,
  Undo2,
  Unlink2,
} from "lucide-react"
import { TableKit } from "@tiptap/extension-table"
import TextAlign from "@tiptap/extension-text-align"
import Image from "@tiptap/extension-image"
import StarterKit from "@tiptap/starter-kit"
import { EditorContent, useEditor, useEditorState } from "@tiptap/react"

import { IconButton } from "@/components/app/app-ui"
import { MediaPickerDialog } from "@/components/app/media-picker-dialog"
import { uploadMedia, type MediaAsset } from "@/lib/api"
import { cn } from "@/lib/utils"

type RichTextEditorProps = {
  id?: string
  value: string
  onChange: (value: string) => void
  onUploadingChange?: (uploading: boolean) => void
  className?: string
}

type EditorState = {
  bold: boolean
  italic: boolean
  underline: boolean
  bulletList: boolean
  orderedList: boolean
  blockquote: boolean
  codeBlock: boolean
  link: boolean
  table: boolean
  align: string | null
  canUndo: boolean
  canRedo: boolean
}

const emptyEditorState: EditorState = {
  bold: false,
  italic: false,
  underline: false,
  bulletList: false,
  orderedList: false,
  blockquote: false,
  codeBlock: false,
  link: false,
  table: false,
  align: null,
  canUndo: false,
  canRedo: false,
}

function Divider() {
  return <span className="mx-0.5 h-5 w-px bg-border" aria-hidden="true" />
}

function RichTextToolbar({
  editor,
  state,
  imageUploading,
  onUploadImage,
  onSelectMedia,
}: {
  editor: NonNullable<ReturnType<typeof useEditor>>
  state: EditorState
  imageUploading: boolean
  onUploadImage: () => void
  onSelectMedia: () => void
}) {
  function setLink() {
    const currentHref = editor.getAttributes("link").href as string | undefined
    const href = window.prompt("链接地址", currentHref ?? "https://")?.trim()
    if (!href) return
    editor.chain().focus().extendMarkRange("link").setLink({ href }).run()
  }

  return (
    <div
      className="flex flex-wrap items-center gap-0.5 border-b bg-muted/30 p-1"
      role="toolbar"
      aria-label="内容详情编辑工具"
      onMouseDown={(event) => event.preventDefault()}
    >
      <IconButton
        label="撤销"
        variant="ghost"
        size="icon-sm"
        disabled={!state.canUndo}
        onClick={() => editor.chain().focus().undo().run()}
      >
        <Undo2 />
      </IconButton>
      <IconButton
        label="重做"
        variant="ghost"
        size="icon-sm"
        disabled={!state.canRedo}
        onClick={() => editor.chain().focus().redo().run()}
      >
        <Redo2 />
      </IconButton>
      <Divider />
      <IconButton
        label="标题 1"
        variant={editor.isActive("heading", { level: 1 }) ? "secondary" : "ghost"}
        size="icon-sm"
        onClick={() => editor.chain().focus().toggleHeading({ level: 1 }).run()}
      >
        <Heading1 />
      </IconButton>
      <IconButton
        label="标题 2"
        variant={editor.isActive("heading", { level: 2 }) ? "secondary" : "ghost"}
        size="icon-sm"
        onClick={() => editor.chain().focus().toggleHeading({ level: 2 }).run()}
      >
        <Heading2 />
      </IconButton>
      <IconButton
        label="标题 3"
        variant={editor.isActive("heading", { level: 3 }) ? "secondary" : "ghost"}
        size="icon-sm"
        onClick={() => editor.chain().focus().toggleHeading({ level: 3 }).run()}
      >
        <Heading3 />
      </IconButton>
      <Divider />
      <IconButton
        label="粗体"
        variant={state.bold ? "secondary" : "ghost"}
        size="icon-sm"
        onClick={() => editor.chain().focus().toggleBold().run()}
      >
        <Bold />
      </IconButton>
      <IconButton
        label="斜体"
        variant={state.italic ? "secondary" : "ghost"}
        size="icon-sm"
        onClick={() => editor.chain().focus().toggleItalic().run()}
      >
        <Italic />
      </IconButton>
      <IconButton
        label="下划线"
        variant={state.underline ? "secondary" : "ghost"}
        size="icon-sm"
        onClick={() => editor.chain().focus().toggleUnderline().run()}
      >
        <Underline />
      </IconButton>
      <IconButton
        label="清除格式"
        variant="ghost"
        size="icon-sm"
        onClick={() => editor.chain().focus().clearNodes().unsetAllMarks().run()}
      >
        <RemoveFormatting />
      </IconButton>
      <Divider />
      <IconButton
        label="无序列表"
        variant={state.bulletList ? "secondary" : "ghost"}
        size="icon-sm"
        onClick={() => editor.chain().focus().toggleBulletList().run()}
      >
        <List />
      </IconButton>
      <IconButton
        label="有序列表"
        variant={state.orderedList ? "secondary" : "ghost"}
        size="icon-sm"
        onClick={() => editor.chain().focus().toggleOrderedList().run()}
      >
        <ListOrdered />
      </IconButton>
      <IconButton
        label="引用"
        variant={state.blockquote ? "secondary" : "ghost"}
        size="icon-sm"
        onClick={() => editor.chain().focus().toggleBlockquote().run()}
      >
        <Quote />
      </IconButton>
      <IconButton
        label="代码块"
        variant={state.codeBlock ? "secondary" : "ghost"}
        size="icon-sm"
        onClick={() => editor.chain().focus().toggleCodeBlock().run()}
      >
        <Code2 />
      </IconButton>
      <IconButton
        label="分隔线"
        variant="ghost"
        size="icon-sm"
        onClick={() => editor.chain().focus().setHorizontalRule().run()}
      >
        <Minus />
      </IconButton>
      <Divider />
      <IconButton
        label="左对齐"
        variant={state.align === "left" ? "secondary" : "ghost"}
        size="icon-sm"
        onClick={() => editor.chain().focus().setTextAlign("left").run()}
      >
        <AlignLeft />
      </IconButton>
      <IconButton
        label="居中对齐"
        variant={state.align === "center" ? "secondary" : "ghost"}
        size="icon-sm"
        onClick={() => editor.chain().focus().setTextAlign("center").run()}
      >
        <AlignCenter />
      </IconButton>
      <IconButton
        label="右对齐"
        variant={state.align === "right" ? "secondary" : "ghost"}
        size="icon-sm"
        onClick={() => editor.chain().focus().setTextAlign("right").run()}
      >
        <AlignRight />
      </IconButton>
      <IconButton
        label="两端对齐"
        variant={state.align === "justify" ? "secondary" : "ghost"}
        size="icon-sm"
        onClick={() => editor.chain().focus().setTextAlign("justify").run()}
      >
        <AlignJustify />
      </IconButton>
      <Divider />
      <IconButton label="插入链接" variant={state.link ? "secondary" : "ghost"} size="icon-sm" onClick={setLink}>
        <Link2 />
      </IconButton>
      <IconButton
        label="取消链接"
        variant="ghost"
        size="icon-sm"
        disabled={!state.link}
        onClick={() => editor.chain().focus().unsetLink().run()}
      >
        <Unlink2 />
      </IconButton>
      <IconButton label="上传图片" variant="ghost" size="icon-sm" disabled={imageUploading} onClick={onUploadImage}>
        <ImagePlus />
      </IconButton>
      <IconButton label="选择图片素材" variant="ghost" size="icon-sm" disabled={imageUploading} onClick={onSelectMedia}>
        <Library />
      </IconButton>
      <IconButton
        label="插入表格"
        variant="ghost"
        size="icon-sm"
        disabled={state.table}
        onClick={() => editor.chain().focus().insertTable({ rows: 3, cols: 3, withHeaderRow: true }).run()}
      >
        <Table2 />
      </IconButton>
      <IconButton
        label="删除表格"
        variant="ghost"
        size="icon-sm"
        disabled={!state.table}
        onClick={() => editor.chain().focus().deleteTable().run()}
      >
        <Trash2 />
      </IconButton>
    </div>
  )
}

export function RichTextEditor({ id, value, onChange, onUploadingChange, className }: RichTextEditorProps) {
  const onChangeRef = useRef(onChange)
  const imageInputRef = useRef<HTMLInputElement>(null)
  const imageSelectionRef = useRef<{ from: number; to: number } | null>(null)
  const [imageUploading, setImageUploading] = useState(false)
  const [imageError, setImageError] = useState("")
  const [mediaPickerOpen, setMediaPickerOpen] = useState(false)

  useEffect(() => {
    onChangeRef.current = onChange
  }, [onChange])

  const editor = useEditor({
    immediatelyRender: false,
    extensions: [
      StarterKit.configure({
        link: { openOnClick: false },
      }),
      Image.configure({
        allowBase64: false,
        HTMLAttributes: { class: "rich-text-image" },
      }),
      TableKit.configure({
        table: { resizable: false },
      }),
      TextAlign.configure({ types: ["heading", "paragraph"] }),
    ],
    content: value,
    editorProps: {
      attributes: {
        class: "rich-text-prose focus:outline-none",
      },
    },
    onUpdate: ({ editor: currentEditor }) => {
      onChangeRef.current(currentEditor.isEmpty ? "" : currentEditor.getHTML())
    },
  }, [])

  useEffect(() => {
    if (!editor || value === editor.getHTML()) return
    editor.commands.setContent(value || "", { emitUpdate: false })
  }, [editor, value])

  const state = useEditorState({
    editor,
    selector: ({ editor: currentEditor }) => {
      if (!currentEditor) return null
      return {
        bold: currentEditor.isActive("bold"),
        italic: currentEditor.isActive("italic"),
        underline: currentEditor.isActive("underline"),
        bulletList: currentEditor.isActive("bulletList"),
        orderedList: currentEditor.isActive("orderedList"),
        blockquote: currentEditor.isActive("blockquote"),
        codeBlock: currentEditor.isActive("codeBlock"),
        link: currentEditor.isActive("link"),
        table: currentEditor.isActive("table"),
        align: (currentEditor.getAttributes("paragraph").textAlign as string | undefined) ?? null,
        canUndo: currentEditor.can().undo(),
        canRedo: currentEditor.can().redo(),
      } satisfies EditorState
    },
  })

  function rememberSelection() {
    if (!editor || imageUploading) return
    imageSelectionRef.current = {
      from: editor.state.selection.from,
      to: editor.state.selection.to,
    }
    return true
  }

  function chooseImage() {
    if (!rememberSelection()) return
    imageInputRef.current?.click()
  }

  function chooseMedia() {
    if (!rememberSelection()) return
    setMediaPickerOpen(true)
  }

  function insertImage(asset: Pick<MediaAsset, "url" | "original_name">) {
    if (!editor) return
    const selection = imageSelectionRef.current
    const chain = editor.chain().focus()
    if (selection) chain.setTextSelection(selection)
    chain.setImage({ src: asset.url, alt: asset.original_name }).run()
    imageSelectionRef.current = null
  }

  async function handleImageUpload(file: File, preserveSelection = false) {
    if (!editor || imageUploading) return
    if (!preserveSelection && !rememberSelection()) return
    setImageUploading(true)
    setImageError("")
    onUploadingChange?.(true)
    try {
      const result = await uploadMedia(file)
      insertImage({ url: result.asset.url, original_name: file.name })
    } catch (uploadError) {
      setImageError(uploadError instanceof Error ? uploadError.message : "图片上传失败")
    } finally {
      imageSelectionRef.current = null
      setImageUploading(false)
      onUploadingChange?.(false)
    }
  }

  function uploadDroppedImage(file: File) {
    if (!file.type.startsWith("image/")) return
    void handleImageUpload(file)
  }

  return (
    <div id={id} className={cn("rich-text-editor overflow-hidden rounded-lg border border-input bg-background", className)}>
      <input
        ref={imageInputRef}
        type="file"
        accept="image/jpeg,image/png,image/gif"
        className="hidden"
        onChange={(event) => {
          const file = event.target.files?.[0]
          event.target.value = ""
          if (file) void handleImageUpload(file, true)
        }}
      />
      {editor ? <RichTextToolbar editor={editor} state={state ?? emptyEditorState} imageUploading={imageUploading} onUploadImage={chooseImage} onSelectMedia={chooseMedia} /> : null}
      <div
        onDragOver={(event) => {
          if ([...event.dataTransfer.types].includes("Files")) event.preventDefault()
        }}
        onDrop={(event) => {
          const file = event.dataTransfer.files[0]
          if (!file || !file.type.startsWith("image/")) return
          event.preventDefault()
          uploadDroppedImage(file)
        }}
        onPaste={(event) => {
          const file = [...event.clipboardData.files].find((candidate) => candidate.type.startsWith("image/"))
          if (!file) return
          event.preventDefault()
          uploadDroppedImage(file)
        }}
      >
        <EditorContent editor={editor} />
      </div>
      {imageError ? <p role="alert" className="border-t px-3 py-2 text-xs text-destructive">{imageError}</p> : null}
      <MediaPickerDialog
        open={mediaPickerOpen}
        onOpenChange={setMediaPickerOpen}
        onSelect={insertImage}
        title="插入图片素材"
        description="从已上传的图片中插入到当前光标位置。"
      />
    </div>
  )
}
