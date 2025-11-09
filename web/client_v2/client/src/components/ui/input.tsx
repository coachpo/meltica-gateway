import * as React from "react"

import { cn } from "@/lib/utils"

function Input({ className, type, ...props }: React.ComponentProps<"input">) {
  return (
    <input
      type={type}
      data-slot="input"
      className={cn(
        "file:text-foreground placeholder:text-muted-foreground selection:bg-primary selection:text-primary-foreground h-10 w-full min-w-0 rounded-xl border border-border/40 bg-card/60 px-4 py-2 text-base shadow-[0_18px_40px_-38px_rgba(15,23,42,0.55)] backdrop-blur-xl transition-all duration-300 outline-none file:inline-flex file:h-7 file:rounded-lg file:border-0 file:bg-primary/10 file:px-3 file:text-xs file:font-semibold disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-60 md:text-sm",
        "focus-visible:border-primary/60 focus-visible:ring-2 focus-visible:ring-primary/40 focus-visible:ring-offset-2 focus-visible:ring-offset-background",
        "aria-invalid:border-destructive/60 aria-invalid:ring-destructive/30",
        className
      )}
      {...props}
    />
  )
}

export { Input }
