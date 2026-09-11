import type { ComponentProps } from "react"
import { cn } from "cn"

type ScrollAreaProps = ComponentProps<"div"> & {
  contentClassName?: string
  orientation?: "vertical" | "horizontal" | "both"
  viewportClassName?: string
}

function ScrollArea({
  className,
  viewportClassName,
  contentClassName,
  orientation = "vertical",
  children,
  ...props
}: ScrollAreaProps) {
  return (
    <div
      data-slot="scroll-area"
      className={cn(
        "relative min-h-0 min-w-0 overscroll-contain",
        "[scrollbar-color:color-mix(in_oklab,var(--muted-foreground)_38%,transparent)_transparent] [scrollbar-width:thin]",
        "[&::-webkit-scrollbar]:size-2 [&::-webkit-scrollbar-thumb]:rounded-full [&::-webkit-scrollbar-thumb]:bg-muted-foreground/35 [&::-webkit-scrollbar-thumb:hover]:bg-muted-foreground/55",
        orientation === "vertical" && "overflow-x-hidden overflow-y-auto",
        orientation === "horizontal" && "overflow-x-auto overflow-y-hidden",
        orientation === "both" && "overflow-auto",
        className,
        viewportClassName
      )}
      {...props}
    >
      <div data-slot="scroll-area-content" className={contentClassName}>
        {children}
      </div>
    </div>
  )
}

export { ScrollArea }
