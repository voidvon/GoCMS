import type { ComponentProps } from "react"
import { ScrollArea as ScrollAreaPrimitive } from "@base-ui/react/scroll-area"
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
    <ScrollAreaPrimitive.Root
      data-slot="scroll-area"
      className={cn("relative grid min-h-0 min-w-0 grid-rows-[minmax(0,1fr)] overflow-hidden", className)}
      {...props}
    >
      <ScrollAreaPrimitive.Viewport
        data-slot="scroll-area-viewport"
        className={cn(
          "min-h-0 min-w-0 max-h-[inherit] overscroll-contain rounded-[inherit] outline-none focus-visible:ring-2 focus-visible:ring-ring/50",
          orientation === "vertical" && "overflow-x-hidden!",
          orientation === "horizontal" && "overflow-y-hidden!",
          viewportClassName
        )}
      >
        <div data-slot="scroll-area-content" className={contentClassName}>
          {children}
        </div>
      </ScrollAreaPrimitive.Viewport>
      {orientation !== "horizontal" && <ScrollBar />}
      {orientation !== "vertical" && <ScrollBar orientation="horizontal" />}
      <ScrollAreaPrimitive.Corner />
    </ScrollAreaPrimitive.Root>
  )
}

function ScrollBar({ orientation = "vertical" }: { orientation?: "vertical" | "horizontal" }) {
  return (
    <ScrollAreaPrimitive.Scrollbar
      data-slot="scroll-area-scrollbar"
      orientation={orientation}
      className={cn(
        "flex touch-none select-none p-px transition-opacity",
        orientation === "vertical" ? "h-full w-2.5 border-l border-l-transparent" : "h-2.5 flex-col border-t border-t-transparent"
      )}
    >
      <ScrollAreaPrimitive.Thumb data-slot="scroll-area-thumb" className="relative flex-1 rounded-full bg-border" />
    </ScrollAreaPrimitive.Scrollbar>
  )
}

export { ScrollArea, ScrollBar }
