'use client'

import * as React from "react"

import { cn } from "@/lib/utils"

const COLOR_MAP = {
  success: "var(--color-success)",
  warning: "var(--color-warning)",
  info: "var(--color-info)",
  destructive: "var(--color-destructive)",
  muted: "var(--color-muted)",
} as const

export type ChartSegmentColor = keyof typeof COLOR_MAP

export type ChartSegment = {
  label: string
  value: number
  color: ChartSegmentColor
}

type StackedBarChartProps = {
  segments: ChartSegment[]
  className?: string
}

export function StackedBarChart({
  segments,
  className,
}: StackedBarChartProps) {
  const total = React.useMemo(
    () => segments.reduce((sum, segment) => sum + Math.max(segment.value, 0), 0),
    [segments]
  )

  if (!total) {
    return null
  }

  const visibleSegments = segments.filter((segment) => segment.value > 0)

  if (visibleSegments.length === 0) {
    return null
  }

  return (
    <div
      data-slot="chart-bar"
      className={cn(
        "flex h-2 w-full overflow-hidden rounded-full border border-border/60 bg-muted/60",
        className
      )}
    >
      {visibleSegments.map((segment) => (
        <span
          key={`${segment.label}-${segment.color}`}
          className="h-full"
          style={{
            width: `${(segment.value / total) * 100}%`,
            backgroundColor: COLOR_MAP[segment.color],
          }}
        />
      ))}
    </div>
  )
}

type ChartLegendProps = {
  segments: ChartSegment[]
  className?: string
  showValues?: boolean
}

export function ChartLegend({
  segments,
  className,
  showValues = true,
}: ChartLegendProps) {
  const visibleSegments = segments.filter((segment) => segment.value > 0)

  if (visibleSegments.length === 0) {
    return null
  }

  return (
    <div
      data-slot="chart-legend"
      className={cn(
        "flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground",
        className
      )}
    >
      {visibleSegments.map((segment) => (
        <span
          key={`${segment.label}-${segment.color}`}
          className="inline-flex items-center gap-1"
        >
          <span
            className="h-2 w-2 rounded-full"
            style={{ backgroundColor: COLOR_MAP[segment.color] }}
          />
          {showValues ? (
            <>
              <span className="font-medium text-foreground">
                {segment.value}
              </span>
              <span>{segment.label}</span>
            </>
          ) : (
            <span className="text-foreground">{segment.label}</span>
          )}
        </span>
      ))}
    </div>
  )
}
