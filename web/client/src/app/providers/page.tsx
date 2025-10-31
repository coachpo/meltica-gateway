'use client';

import { useEffect, useState } from 'react';
import { apiClient } from '@/lib/api-client';
import { Provider } from '@/lib/types';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Alert, AlertDescription } from '@/components/ui/alert';

export default function ProvidersPage() {
  const [providers, setProviders] = useState<Provider[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const fetchProviders = async () => {
      try {
        const response = await apiClient.getProviders();
        setProviders(response.providers);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to fetch providers');
      } finally {
        setLoading(false);
      }
    };

    fetchProviders();
  }, []);

  if (loading) {
    return <div>Loading providers...</div>;
  }

  if (error) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{error}</AlertDescription>
      </Alert>
    );
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">Providers</h1>
        <p className="text-muted-foreground">
          Monitor exchange provider connections and instrument catalogs
        </p>
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {providers.map((provider) => (
          <Card key={provider.name}>
            <CardHeader>
              <div className="flex items-center justify-between">
                <CardTitle>{provider.name}</CardTitle>
                <Badge variant="outline">{provider.exchange}</Badge>
              </div>
              <CardDescription>
                {provider.instrumentCount} instruments available
              </CardDescription>
            </CardHeader>
            <CardContent>
              <div className="text-sm space-y-1">
                <div>
                  <span className="font-medium">Identifier:</span>{' '}
                  <span className="text-muted-foreground">{provider.identifier}</span>
                </div>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  );
}
