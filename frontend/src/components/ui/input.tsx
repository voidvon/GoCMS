import * as React from "react"
import { Input as InputPrimitive } from "@base-ui/react/input"
import { cva, type VariantProps } from "class-variance-authority"
import { cn } from "cn"

const inputVariants = cva(
  "w-full min-w-0 rounded-lg border border-input bg-transparent text-base transition-colors outline-none file:inline-flex file:h-6 file:border-0 file:bg-transparent file:text-sm file:font-medium file:text-foreground placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:pointer-events-none disabled:cursor-not-allowed disabled:bg-input/50 disabled:opacity-50 aria-invalid:border-destructive aria-invalid:ring-3 aria-invalid:ring-destructive/20 md:text-sm dark:bg-input/30 dark:disabled:bg-input/80 dark:aria-invalid:border-destructive/50 dark:aria-invalid:ring-destructive/40",
  {
    variants: {
      size: {
        default: "h-8 px-2.5 py-1 text-sm md:text-sm",
        sm: "h-7 px-2.5 py-0 text-sm md:text-sm rounded-[min(var(--radius-md),10px)]",
      },
    },
    defaultVariants: {
      size: "default",
    },
  }
)

type InputProps = Omit<React.ComponentProps<"input">, "size"> &
  Omit<VariantProps<typeof inputVariants>, "size"> & {
    size?: "default" | "sm" | number
  }

function Input({
  className,
  type,
  size = "default",
  ...props
}: InputProps) {
  const isNamedSize = size === "default" || size === "sm"
  const namedSize = isNamedSize ? size : "default"
  const htmlSize = typeof size === "number" ? size : undefined

  return (
    <InputPrimitive
      type={type}
      data-slot="input"
      data-size={isNamedSize ? size : undefined}
      size={htmlSize}
      className={cn(inputVariants({ size: namedSize }), className)}
      {...props}
    />
  )
}

export { Input, inputVariants }
