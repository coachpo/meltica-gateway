import Link from 'next/link';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';

export default function Home() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">Dashboard</h1>
        <p className="text-muted-foreground">
          Manage your trading strategies and monitor system resources.
        </p>
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        <Link href="/instances">
          <Card className="hover:border-primary cursor-pointer transition-colors">
            <CardHeader>
              <CardTitle>Strategy Instances</CardTitle>
              <CardDescription>
                Manage running strategy instances
              </CardDescription>
            </CardHeader>
            <CardContent>
              <p className="text-sm text-muted-foreground">
                Create, start, stop, and configure strategy instances
              </p>
            </CardContent>
          </Card>
        </Link>

        <Link href="/strategies">
          <Card className="hover:border-primary cursor-pointer transition-colors">
            <CardHeader>
              <CardTitle>Strategies</CardTitle>
              <CardDescription>
                Browse available trading strategies
              </CardDescription>
            </CardHeader>
            <CardContent>
              <p className="text-sm text-muted-foreground">
                View strategy definitions and configuration options
              </p>
            </CardContent>
          </Card>
        </Link>

        <Link href="/strategies/modules">
          <Card className="hover:border-primary cursor-pointer transition-colors">
            <CardHeader>
              <CardTitle>Strategy Modules</CardTitle>
              <CardDescription>
                Manage JavaScript strategy source files
              </CardDescription>
            </CardHeader>
            <CardContent>
              <p className="text-sm text-muted-foreground">
                Upload, edit, and refresh runtime strategy modules
              </p>
            </CardContent>
          </Card>
        </Link>

        <Link href="/providers">
          <Card className="hover:border-primary cursor-pointer transition-colors">
            <CardHeader>
              <CardTitle>Providers</CardTitle>
              <CardDescription>
                Monitor exchange providers
              </CardDescription>
            </CardHeader>
            <CardContent>
              <p className="text-sm text-muted-foreground">
                View provider metadata and instrument catalogs
              </p>
            </CardContent>
          </Card>
        </Link>

        <Link href="/adapters">
          <Card className="hover:border-primary cursor-pointer transition-colors">
            <CardHeader>
              <CardTitle>Adapters</CardTitle>
              <CardDescription>
                View exchange adapter definitions
              </CardDescription>
            </CardHeader>
            <CardContent>
              <p className="text-sm text-muted-foreground">
                Explore adapter capabilities and configuration schemas
              </p>
            </CardContent>
          </Card>
        </Link>

        <Link href="/risk">
          <Card className="hover:border-primary cursor-pointer transition-colors">
            <CardHeader>
              <CardTitle>Risk Limits</CardTitle>
              <CardDescription>
                Configure risk management settings
              </CardDescription>
            </CardHeader>
            <CardContent>
              <p className="text-sm text-muted-foreground">
                Adjust position limits, order throttling, and circuit breakers
              </p>
            </CardContent>
          </Card>
        </Link>
      </div>
    </div>
  );
}
