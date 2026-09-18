import type { ReactNode } from "react"

import { TablePagination } from "@/components/app/app-ui"
import { cn } from "@/lib/utils"

type PaginatedTableProps = {
  children: ReactNode
  page: number
  totalPages: number
  total: number
  pageSize: number
  loading?: boolean
  onPageChange: (page: number) => void
  className?: string
  scrollAreaClassName?: string
  paginationClassName?: string
}

export function PaginatedTable({
  children,
  page,
  totalPages,
  total,
  pageSize,
  loading,
  onPageChange,
  className,
  scrollAreaClassName,
  paginationClassName,
}: PaginatedTableProps) {
  return (
    <div className={cn("flex min-h-0 flex-1 flex-col rounded-md border bg-background lg:overflow-hidden", className)}>
      <div
        className={cn(
          "relative min-h-0 flex-1 overflow-auto",
          "[scrollbar-color:color-mix(in_oklab,var(--muted-foreground)_38%,transparent)_transparent] [scrollbar-width:thin]",
          "[&::-webkit-scrollbar]:size-2 [&::-webkit-scrollbar-thumb]:rounded-full [&::-webkit-scrollbar-thumb]:bg-muted-foreground/35 [&::-webkit-scrollbar-thumb:hover]:bg-muted-foreground/55",
          "[&>[data-slot=table-container]]:overflow-visible [&>[data-slot=table-container]]:static",
          scrollAreaClassName
        )}
      >
        {children}
      </div>
      <TablePagination
        className={cn("shrink-0 border-t px-4 py-3", paginationClassName)}
        page={page}
        totalPages={totalPages}
        total={total}
        pageSize={pageSize}
        loading={loading}
        onPageChange={onPageChange}
      />
    </div>
  )
}
