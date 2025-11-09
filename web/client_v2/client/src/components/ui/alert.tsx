import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"

import { cn } from "@/lib/utils"

const alertVariants = cva(
  "group/alert relative grid w-full grid-cols-[0_1fr] items-start gap-y-1 rounded-2xl border px-5 py-4 text-sm shadow-[0_22px_45px_-38px_rgba(15,23,42,0.65)] backdrop-blur-xl transition-all has-[>svg]:grid-cols-[calc(var(--spacing)*4)_1fr] has-[>svg]:gap-x-3 [&>svg]:size-4 [&>svg]:translate-y-0.5 [&>svg]:text-current",
  {
    variants: {
      variant: {
        default: "border-border/60 bg-card/75 text-card-foreground",
        destructive:
          "border-destructive/60 bg-destructive/15 text-destructive-foreground [&>svg]:text-destructive",
        success:
          "border-success/60 bg-success/15 text-success-foreground [&>svg]:text-success",
        warning:
          "border-warning/60 bg-warning/15 text-warning-foreground [&>svg]:text-warning",
        info:
          "border-info/60 bg-info/15 text-info-foreground [&>svg]:text-info",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
)

function Alert({
  className,
  variant,
  ...props
}: React.ComponentProps<"div"> & VariantProps<typeof alertVariants>) {
  return (
    <div
      data-slot="alert"
      role="alert"
      className={cn(alertVariants({ variant }), className)}
      {...props}
    />
  )
}

function AlertTitle({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="alert-title"
      className={cn(
        "col-start-2 line-clamp-1 min-h-4 font-medium tracking-tight",
        className
      )}
      {...props}
    />
  )
}

function AlertDescription({
  className,
  ...props
}: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="alert-description"
      className={cn(
        "text-muted-foreground col-start-2 grid justify-items-start gap-1 text-sm [&_p]:leading-relaxed",
        className
      )}
      {...props}
    />
  )
}

export { Alert, AlertTitle, AlertDescription }
