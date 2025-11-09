import * as React from "react"

import { cn } from "@/lib/utils"

function Card({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card"
      className={cn(
        "group relative flex flex-col gap-6 overflow-hidden rounded-2xl border border-border/50 py-6 text-card-foreground shadow-[0_35px_65px_-45px_rgba(15,23,42,0.55)] backdrop-blur-xl transition-all duration-500 before:pointer-events-none before:absolute before:inset-[-2px] before:-z-20 before:rounded-[inherit] before:bg-[conic-gradient(at_top_left,theme(colors.sky.400),theme(colors.violet.500),theme(colors.fuchsia.500),theme(colors.sky.400))] before:opacity-0 before:blur-2xl before:transition-opacity before:duration-500 after:pointer-events-none after:absolute after:inset-[1px] after:-z-10 after:rounded-[inherit] after:border after:border-white/10 after:bg-card/85 after:backdrop-blur-2xl hover:border-transparent hover:before:opacity-80 dark:border-white/10",
        className
      )}
      {...props}
    />
  )
}

function CardHeader({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card-header"
      className={cn(
        "@container/card-header relative grid auto-rows-min grid-rows-[auto_auto] items-start gap-2 px-7 text-foreground has-data-[slot=card-action]:grid-cols-[1fr_auto] [.border-b]:pb-6",
        className
      )}
      {...props}
    />
  )
}

function CardTitle({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card-title"
      className={cn("text-lg font-semibold tracking-wide", className)}
      {...props}
    />
  )
}

function CardDescription({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card-description"
      className={cn("text-sm text-muted-foreground/85", className)}
      {...props}
    />
  )
}

function CardAction({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card-action"
      className={cn(
        "col-start-2 row-span-2 row-start-1 self-start justify-self-end",
        className
      )}
      {...props}
    />
  )
}

function CardContent({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card-content"
      className={cn("px-7", className)}
      {...props}
    />
  )
}

function CardFooter({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card-footer"
      className={cn("flex items-center px-7 [.border-t]:pt-6", className)}
      {...props}
    />
  )
}

export {
  Card,
  CardHeader,
  CardFooter,
  CardTitle,
  CardAction,
  CardDescription,
  CardContent,
}
