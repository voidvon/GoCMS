import { useEffect, useRef } from "react"
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
import { cn } from "@/lib/utils"

type RichTextEditorProps = {
  id?: string
  value: string
  onChange: (value: string) => void
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

function RichTextToolbar({ editor, state }: { editor: NonNullable<ReturnType<typeof useEditor>>; state: EditorState }) {
  function setLink() {
    const currentHref = editor.getAttributes("link").href as string | undefined
    const href = window.prompt("链接地址", currentHref ?? "https://")?.trim()
    if (!href) return
    editor.chain().focus().extendMarkRange("link").setLink({ href }).run()
  }

  function insertImage() {
    const src = window.prompt("图片地址", "/images/")?.trim()
    if (!src) return
    editor.chain().focus().setImage({ src }).run()
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
      <IconButton label="插入图片" variant="ghost" size="icon-sm" onClick={insertImage}>
        <ImagePlus />
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

export function RichTextEditor({ id, value, onChange, className }: RichTextEditorProps) {
  const onChangeRef = useRef(onChange)

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

  return (
    <div id={id} className={cn("rich-text-editor overflow-hidden rounded-lg border border-input bg-background", className)}>
      {editor ? <RichTextToolbar editor={editor} state={state ?? emptyEditorState} /> : null}
      <EditorContent editor={editor} />
    </div>
  )
}
