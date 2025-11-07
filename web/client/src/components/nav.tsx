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
    <nav className="border-b bg-background">
      <div className="flex flex-col gap-4 px-4 py-4 md:flex-row md:items-center md:justify-between md:px-6">
        <div className="flex items-center justify-between gap-4">
          <div className="text-xl font-semibold">Meltica Control</div>
          <div className="md:hidden">
            <ThemeToggle />
          </div>
        </div>
        <div className="flex flex-col gap-3 md:flex-row md:flex-1 md:items-center md:justify-between">
          <div className="flex flex-wrap gap-x-4 gap-y-2">
            {navItems.map((item) => (
              <Link
                key={item.href}
                href={item.href}
                className={cn(
                  'text-sm font-medium transition-colors hover:text-primary',
                  pathname === item.href
                    ? 'text-foreground'
                    : 'text-muted-foreground'
                )}
              >
                {item.label}
              </Link>
            ))}
          </div>
          <div className="hidden md:block">
            <ThemeToggle />
          </div>
        </div>
      </div>
    </nav>
  );
}
