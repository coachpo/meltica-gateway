import * as React from "react"
import { Slot } from "@radix-ui/react-slot"
import { cva, type VariantProps } from "class-variance-authority"

import { cn } from "@/lib/utils"

const buttonVariants = cva(
  "relative inline-flex items-center justify-center gap-2 overflow-hidden whitespace-nowrap rounded-xl text-sm font-semibold tracking-wide transition-all duration-300 disabled:pointer-events-none disabled:opacity-60 [&_svg]:pointer-events-none [&_svg:not([class*='size-'])]:size-4 shrink-0 [&_svg]:shrink-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/70 focus-visible:ring-offset-2 focus-visible:ring-offset-background before:absolute before:inset-0 before:-z-10 before:rounded-[inherit] before:bg-[radial-gradient(circle_at_top_left,theme(colors.sky.400/.55),transparent_55%)] before:opacity-0 before:transition-opacity before:duration-300 hover:before:opacity-100",
  {
    variants: {
      variant: {
        default:
          "border border-transparent bg-[linear-gradient(135deg,theme(colors.sky.500),theme(colors.violet.500),theme(colors.fuchsia.500))] text-primary-foreground shadow-[0_20px_45px_-25px_rgba(79,70,229,0.85)] hover:shadow-[0_28px_55px_-24px_rgba(79,70,229,0.9)]",
        destructive:
          "border border-transparent bg-[linear-gradient(135deg,theme(colors.rose.600),theme(colors.orange.500))] text-destructive-foreground shadow-[0_16px_40px_-22px_rgba(225,29,72,0.75)] focus-visible:ring-destructive/40",
        outline:
          "border border-border/60 bg-transparent text-foreground hover:border-primary/70 hover:text-primary",
        secondary:
          "border border-transparent bg-[linear-gradient(135deg,theme(colors.cyan.400),theme(colors.sky.500)/70)] text-secondary-foreground",
        ghost:
          "border border-transparent bg-transparent text-foreground hover:bg-primary/10 hover:text-primary",
        link: "border border-transparent bg-transparent text-primary underline-offset-4 hover:underline",
      },
      size: {
        default: "h-10 px-5 py-2",
        sm: "h-9 rounded-lg gap-1.5 px-4 py-1.5 text-xs",
        lg: "h-11 rounded-2xl px-7 py-2.5 text-base",
        icon: "size-10",
        "icon-sm": "size-9",
        "icon-lg": "size-12",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  }
)

function Button({
  className,
  variant,
  size,
  asChild = false,
  ...props
}: React.ComponentProps<"button"> &
  VariantProps<typeof buttonVariants> & {
    asChild?: boolean
  }) {
  const Comp = asChild ? Slot : "button"

  return (
    <Comp
      data-slot="button"
      className={cn(buttonVariants({ variant, size, className }))}
      {...props}
    />
  )
}

export { Button, buttonVariants }
