import Link from 'next/link';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';

export default function Home() {
  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <h1 className="bg-gradient-to-r from-sky-500 via-violet-500 to-fuchsia-500 bg-clip-text text-3xl font-extrabold tracking-tight text-transparent">
          Dashboard
        </h1>
        <p className="max-w-2xl text-sm text-muted-foreground/80">
          Manage your trading strategies and monitor system resources with the new Galaxy surface.
        </p>
      </div>

      <div className="grid gap-5 md:grid-cols-2 lg:grid-cols-3">
        <Link href="/instances" className="group block">
          <Card className="cursor-pointer transition-transform duration-300 group-hover:-translate-y-1">
            <CardHeader>
              <CardTitle>Strategy Instances</CardTitle>
              <CardDescription>
                Manage running strategy instances
              </CardDescription>
            </CardHeader>
            <CardContent>
              <p className="text-sm text-muted-foreground/85">
                Create, start, stop, and configure strategy instances
              </p>
            </CardContent>
          </Card>
        </Link>

        <Link href="/strategies" className="group block">
          <Card className="cursor-pointer transition-transform duration-300 group-hover:-translate-y-1">
            <CardHeader>
              <CardTitle>Strategies</CardTitle>
              <CardDescription>
                Browse available trading strategies
              </CardDescription>
            </CardHeader>
            <CardContent>
              <p className="text-sm text-muted-foreground/85">
                View strategy definitions and configuration options
              </p>
            </CardContent>
          </Card>
        </Link>

        <Link href="/strategies/modules" className="group block">
          <Card className="cursor-pointer transition-transform duration-300 group-hover:-translate-y-1">
            <CardHeader>
              <CardTitle>Strategy Modules</CardTitle>
              <CardDescription>
                Manage JavaScript strategy source files
              </CardDescription>
            </CardHeader>
            <CardContent>
              <p className="text-sm text-muted-foreground/85">
                Upload, edit, and refresh runtime strategy modules
              </p>
            </CardContent>
          </Card>
        </Link>

        <Link href="/providers" className="group block">
          <Card className="cursor-pointer transition-transform duration-300 group-hover:-translate-y-1">
            <CardHeader>
              <CardTitle>Providers</CardTitle>
              <CardDescription>
                Monitor exchange providers
              </CardDescription>
            </CardHeader>
            <CardContent>
              <p className="text-sm text-muted-foreground/85">
                View provider metadata and instrument catalogs
              </p>
            </CardContent>
          </Card>
        </Link>

        <Link href="/adapters" className="group block">
          <Card className="cursor-pointer transition-transform duration-300 group-hover:-translate-y-1">
            <CardHeader>
              <CardTitle>Adapters</CardTitle>
              <CardDescription>
                View exchange adapter definitions
              </CardDescription>
            </CardHeader>
            <CardContent>
              <p className="text-sm text-muted-foreground/85">
                Explore adapter capabilities and configuration schemas
              </p>
            </CardContent>
          </Card>
        </Link>

        <Link href="/risk" className="group block">
          <Card className="cursor-pointer transition-transform duration-300 group-hover:-translate-y-1">
            <CardHeader>
              <CardTitle>Risk Limits</CardTitle>
              <CardDescription>
                Configure risk management settings
              </CardDescription>
            </CardHeader>
            <CardContent>
              <p className="text-sm text-muted-foreground/85">
                Adjust position limits, order throttling, and circuit breakers
              </p>
            </CardContent>
          </Card>
        </Link>
      </div>
    </div>
  );
}
