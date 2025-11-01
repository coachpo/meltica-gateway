'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { cn } from '@/lib/utils';
import { ThemeToggle } from '@/components/theme-toggle';

const navItems = [
  { href: '/', label: 'Dashboard' },
  { href: '/instances', label: 'Instances' },
  { href: '/strategies', label: 'Strategies' },
  { href: '/providers', label: 'Providers' },
  { href: '/adapters', label: 'Adapters' },
  { href: '/risk', label: 'Risk Limits' },
  { href: '/context/backup', label: 'Context Backup' },
];

export function Nav() {
  const pathname = usePathname();

  return (
    <nav className="border-b">
      <div className="flex h-16 items-center px-6">
        <div className="mr-8 text-xl font-semibold">Meltica Control</div>
        <div className="flex flex-1 items-center justify-between gap-6">
          <div className="flex flex-wrap gap-6">
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
          <ThemeToggle />
        </div>
      </div>
    </nav>
  );
}
