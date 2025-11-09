import * as React from "react"

import { cn } from "@/lib/utils"

function Textarea({ className, ...props }: React.ComponentProps<"textarea">) {
  return (
    <textarea
      data-slot="textarea"
      className={cn(
        "flex field-sizing-content min-h-24 w-full rounded-2xl border border-border/40 bg-card/60 px-4 py-3 text-base text-foreground shadow-[0_18px_40px_-38px_rgba(15,23,42,0.55)] backdrop-blur-xl transition-all duration-300 placeholder:text-muted-foreground outline-none focus-visible:border-primary/60 focus-visible:ring-2 focus-visible:ring-primary/40 focus-visible:ring-offset-2 focus-visible:ring-offset-background aria-invalid:border-destructive/60 aria-invalid:ring-destructive/30 disabled:cursor-not-allowed disabled:opacity-60 md:text-sm",
        className
      )}
      {...props}
    />
  )
}

export { Textarea }
