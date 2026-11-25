import type { ComponentProps, ReactNode } from "react"
import { useState } from "react"
import { AlertCircle, ChevronLeft, ChevronRight, Search, X } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"

type IconButtonProps = ComponentProps<typeof Button> & {
  label: string
}

export function IconButton({
  label,
  children,
  className,
  ...props
}: IconButtonProps) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            {...props}
            type={props.type ?? "button"}
            aria-label={props["aria-label"] ?? label}
            className={cn("shrink-0", className)}
          />
        }
      >
        {children}
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}

type InlineAlertProps = ComponentProps<"div"> & {
  children: ReactNode
}

export function InlineAlert({
  children,
  className,
  ...props
}: InlineAlertProps) {
  return (
    <div
      role="alert"
      className={cn(
        "flex items-start gap-2 rounded-lg border border-destructive/20 bg-destructive/5 px-3 py-2 text-sm text-destructive",
        className
      )}
      {...props}
    >
      <AlertCircle className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  )
}

export function SearchField({
  className,
  size = "default",
  onClear,
  ...props
}: ComponentProps<typeof Input> & { onClear?: () => void }) {
  const showClear = Boolean(onClear && String(props.value ?? ""))
  return (
    <div className="relative min-w-0 flex-1">
      <Search
        className={cn(
          "pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground",
          size === "sm" && "left-2 size-3.5"
        )}
        aria-hidden="true"
      />
      <Input
        size={size}
        {...props}
        className={cn(size === "sm" ? "pl-[26px]" : "pl-8", showClear && "pr-8", className)}
      />
      {showClear && (
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          className="absolute top-1/2 right-0.5 -translate-y-1/2 text-muted-foreground"
          aria-label="清空搜索"
          title="清空搜索"
          disabled={props.disabled}
          onClick={onClear}
        >
          <X aria-hidden="true" />
        </Button>
      )}
    </div>
  )
}

type TablePaginationProps = {
  className?: string
  page: number
  totalPages: number
  total: number
  pageSize: number
  loading?: boolean
  onPageChange: (page: number) => void
}

export function TablePagination({
  className,
  page,
  totalPages,
  total,
  pageSize,
  loading = false,
  onPageChange,
}: TablePaginationProps) {
  const [jumpInput, setJumpInput] = useState("")
  const trimmedInput = jumpInput.trim()
  const targetPage = Number(trimmedInput)
  const validTarget = /^\d+$/.test(trimmedInput) &&
    Number.isSafeInteger(targetPage) && targetPage >= 1 && targetPage <= totalPages
  const canJump = validTarget && targetPage !== page && !loading
  const pageRangeLabel = `跳转页码（1–${totalPages}）`

  function changePage(nextPage: number) {
    setJumpInput("")
    onPageChange(nextPage)
  }

  function jumpToPage() {
    if (canJump) changePage(targetPage)
  }

  return (
    <div className={cn("flex flex-col gap-3 border-t px-4 py-3 sm:flex-row sm:items-center sm:justify-between", className)}>
      <p className="text-xs text-muted-foreground">
        {total.toLocaleString("zh-CN")} 条记录 · 每页 {pageSize} 条
      </p>
      <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2 sm:justify-end">
        <span className="text-xs whitespace-nowrap tabular-nums text-muted-foreground">
          第 {page} / {totalPages} 页
        </span>
        <div className="flex gap-1">
          <IconButton
            variant="outline"
            size="icon-sm"
            label="上一页"
            disabled={page <= 1 || loading}
            onClick={() => changePage(page - 1)}
          >
            <ChevronLeft />
          </IconButton>
          <IconButton
            variant="outline"
            size="icon-sm"
            label="下一页"
            disabled={page >= totalPages || loading}
            onClick={() => changePage(page + 1)}
          >
            <ChevronRight />
          </IconButton>
        </div>
        {totalPages > 1 && (
          <div className="flex items-center gap-1">
            <Input
              size="sm"
              className="w-16 px-1.5 text-center tabular-nums"
              inputMode="numeric"
              autoComplete="off"
              placeholder="页码"
              aria-label={pageRangeLabel}
              title={pageRangeLabel}
              aria-invalid={trimmedInput !== "" && !validTarget ? true : undefined}
              value={jumpInput}
              disabled={loading}
              onChange={event => setJumpInput(event.target.value)}
              onKeyDown={event => {
                if (event.key !== "Enter" || event.nativeEvent.isComposing) return
                event.preventDefault()
                jumpToPage()
              }}
            />
            <Button type="button" variant="outline" size="sm" disabled={!canJump} onClick={jumpToPage}>
              跳转
            </Button>
          </div>
        )}
      </div>
    </div>
  )
}

type ConfirmDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onOpenChangeComplete?: (open: boolean) => void
  title: string
  description: ReactNode
  confirmLabel: string
  confirmVariant?: "default" | "destructive"
  pending?: boolean
  onConfirm: () => void
}

export function ConfirmDialog({
  open,
  onOpenChange,
  onOpenChangeComplete,
  title,
  description,
  confirmLabel,
  confirmVariant = "destructive",
  pending = false,
  onConfirm,
}: ConfirmDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange} onOpenChangeComplete={onOpenChangeComplete}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={pending}
          >
            取消
          </Button>
          <Button
            variant={confirmVariant}
            onClick={onConfirm}
            disabled={pending}
          >
            {pending ? "处理中..." : confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
