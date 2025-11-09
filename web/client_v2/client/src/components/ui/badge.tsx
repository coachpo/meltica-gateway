import * as React from "react"
import { Slot } from "@radix-ui/react-slot"
import { cva, type VariantProps } from "class-variance-authority"

import { cn } from "@/lib/utils"

const badgeVariants = cva(
  "inline-flex w-fit shrink-0 items-center justify-center gap-1 overflow-hidden rounded-full px-2.5 py-1 text-[11px] font-semibold uppercase tracking-[0.08em] transition-all duration-300 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/60 focus-visible:ring-offset-2 focus-visible:ring-offset-background [&>svg]:size-3 [&>svg]:pointer-events-none",
  {
    variants: {
      variant: {
        default:
          "border border-transparent bg-[linear-gradient(120deg,theme(colors.sky.400/.8),theme(colors.violet.500/.85),theme(colors.fuchsia.500/.8))] text-primary-foreground shadow-[0_12px_30px_-20px_rgba(79,70,229,0.85)]",
        secondary:
          "border border-secondary/40 bg-secondary/60 text-secondary-foreground",
        destructive:
          "border border-destructive/50 bg-destructive/20 text-destructive-foreground focus-visible:ring-destructive/40",
        outline:
          "border border-border/60 bg-transparent text-muted-foreground hover:border-primary/60 hover:text-primary",
        muted: "border border-muted/40 bg-muted/50 text-muted-foreground",
        success:
          "border border-success/40 bg-success/15 text-success",
        warning:
          "border border-warning/50 bg-warning/15 text-warning-foreground",
        info: "border border-info/50 bg-info/15 text-info",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
)

function Badge({
  className,
  variant,
  asChild = false,
  ...props
}: React.ComponentProps<"span"> &
  VariantProps<typeof badgeVariants> & { asChild?: boolean }) {
  const Comp = asChild ? Slot : "span"

  return (
    <Comp
      data-slot="badge"
      className={cn(badgeVariants({ variant }), className)}
      {...props}
    />
  )
}

export { Badge, badgeVariants }
