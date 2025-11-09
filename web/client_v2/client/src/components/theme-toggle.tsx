'use client';

import { Moon, Sun } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { useTheme } from '@/components/ui/theme-provider';
import { cn } from '@/lib/utils';

export function ThemeToggle() {
  const { theme, toggleTheme } = useTheme();
  const isDark = theme === 'dark';

  return (
    <Button
      variant="ghost"
      size="icon"
      onClick={toggleTheme}
      aria-label="Toggle theme"
      className={cn(
        'group relative h-10 w-10 overflow-hidden rounded-2xl border backdrop-blur-md transition-all',
        'hover:shadow-[0_15px_35px_-20px_rgba(79,70,229,0.85)]',
        isDark
          ? 'border-white/20 bg-white/10 hover:border-white/50'
          : 'border-primary/30 bg-background/70 hover:border-primary/70'
      )}
    >
      <span
        className={cn(
          'absolute inset-px rounded-[inherit] transition-opacity duration-300',
          isDark
            ? 'bg-[radial-gradient(circle_at_top,rgba(148,163,255,0.45),transparent_65%)] opacity-75 group-hover:opacity-100'
            : 'bg-[radial-gradient(circle_at_top,theme(colors.sky.400/.28),transparent_65%)] opacity-0 group-hover:opacity-100'
        )}
      />
      <span className="relative flex items-center justify-center">
        {isDark ? (
          <Sun className="h-5 w-5 text-sky-200" />
        ) : (
          <Moon className="h-5 w-5 text-primary" />
        )}
      </span>
    </Button>
  );
}
