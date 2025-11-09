"use client"

import * as React from "react"
import * as TabsPrimitive from "@radix-ui/react-tabs"

import { cn } from "@/lib/utils"

function Tabs({
  className,
  ...props
}: React.ComponentProps<typeof TabsPrimitive.Root>) {
  return (
    <TabsPrimitive.Root
      data-slot="tabs"
      className={cn("flex flex-col gap-2", className)}
      {...props}
    />
  )
}

function TabsList({
  className,
  ...props
}: React.ComponentProps<typeof TabsPrimitive.List>) {
  return (
    <TabsPrimitive.List
      data-slot="tabs-list"
      className={cn(
        "inline-flex h-11 w-fit items-center justify-center gap-1 rounded-full border border-border/40 bg-card/70 p-1 shadow-[0_18px_40px_-30px_rgba(15,23,42,0.6)] backdrop-blur-xl",
        className
      )}
      {...props}
    />
  )
}

function TabsTrigger({
  className,
  ...props
}: React.ComponentProps<typeof TabsPrimitive.Trigger>) {
  return (
    <TabsPrimitive.Trigger
      data-slot="tabs-trigger"
      className={cn(
        "relative inline-flex min-w-[110px] flex-1 items-center justify-center gap-1.5 rounded-full border border-transparent px-4 py-1.5 text-sm font-semibold uppercase tracking-[0.1em] text-muted-foreground transition-all duration-300 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/60 focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:pointer-events-none disabled:opacity-40 before:absolute before:inset-0 before:-z-10 before:rounded-[inherit] before:opacity-0 before:transition-opacity before:duration-300 data-[state=active]:text-primary-foreground data-[state=active]:shadow-[0_18px_40px_-28px_rgba(79,70,229,0.8)] data-[state=active]:before:opacity-100 data-[state=active]:before:bg-[linear-gradient(135deg,theme(colors.sky.500),theme(colors.violet.500),theme(colors.fuchsia.500))]",
        className
      )}
      {...props}
    />
  )
}

function TabsContent({
  className,
  ...props
}: React.ComponentProps<typeof TabsPrimitive.Content>) {
  return (
    <TabsPrimitive.Content
      data-slot="tabs-content"
      className={cn(
        "flex-1 rounded-2xl border border-border/35 bg-card/80 p-5 shadow-[0_25px_50px_-40px_rgba(15,23,42,0.6)] backdrop-blur-xl outline-none",
        className
      )}
      {...props}
    />
  )
}

export { Tabs, TabsList, TabsTrigger, TabsContent }
