'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { cn } from '@/lib/utils';
import { ThemeToggle } from '@/components/theme-toggle';

const navItems = [
  { href: '/', label: 'Dashboard' },
  { href: '/instances', label: 'Instances' },
  { href: '/strategies', label: 'Strategies' },
  { href: '/strategies/modules', label: 'Strategy Modules' },
  { href: '/providers', label: 'Providers' },
  { href: '/adapters', label: 'Adapters' },
  { href: '/risk', label: 'Risk Limits' },
  { href: '/context/backup', label: 'Context Backup' },
  { href: '/outbox', label: 'Outbox' },
];

export function Nav() {
  const pathname = usePathname();

  return (
    <nav className="relative z-40 border-b border-border/40 bg-background/70 backdrop-blur-xl shadow-[0_20px_45px_-35px_rgba(15,23,42,0.65)]">
      <div className="flex flex-col gap-4 px-5 py-4 md:flex-row md:items-center md:justify-between md:px-8">
        <div className="flex items-center justify-between gap-4">
          <div className="text-xl font-semibold tracking-wide text-foreground">
            Meltica Control
          </div>
          <div className="md:hidden">
            <ThemeToggle />
          </div>
        </div>
        <div className="flex flex-col gap-3 md:flex-row md:flex-1 md:items-center md:justify-between">
          <div className="flex flex-wrap items-center gap-2">
            {navItems.map((item) => {
              const isActive = pathname === item.href;
              return (
                <Link
                  key={item.href}
                  href={item.href}
                  className={cn(
                    'relative inline-flex items-center rounded-full px-4 py-1.5 text-xs font-semibold uppercase tracking-[0.18em] transition-all duration-300 hover:text-primary-foreground',
                    isActive
                      ? 'text-primary-foreground shadow-[0_15px_35px_-25px_rgba(79,70,229,0.85)] before:absolute before:inset-0 before:-z-10 before:rounded-full before:bg-[linear-gradient(135deg,theme(colors.sky.500),theme(colors.violet.500),theme(colors.fuchsia.500))]'
                      : 'text-muted-foreground hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/50 focus-visible:ring-offset-2 focus-visible:ring-offset-background'
                  )}
                >
                  {item.label}
                </Link>
              );
            })}
          </div>
          <div className="hidden md:flex md:items-center md:justify-end">
            <ThemeToggle />
          </div>
        </div>
      </div>
    </nav>
  );
}
