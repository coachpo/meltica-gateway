'use client';

import { useEffect, useState } from 'react';
import { apiClient } from '@/lib/api-client';
import { AdapterMetadata } from '@/lib/types';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Alert, AlertDescription } from '@/components/ui/alert';

export default function AdaptersPage() {
  const [adapters, setAdapters] = useState<AdapterMetadata[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const fetchAdapters = async () => {
      try {
        const response = await apiClient.getAdapters();
        setAdapters(response.adapters);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to fetch adapters');
      } finally {
        setLoading(false);
      }
    };

    fetchAdapters();
  }, []);

  if (loading) {
    return <div>Loading adapters...</div>;
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
        <h1 className="text-3xl font-bold tracking-tight">Adapters</h1>
        <p className="text-muted-foreground">
          View exchange adapter definitions and configuration schemas
        </p>
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {adapters.map((adapter) => (
          <Card key={adapter.identifier}>
            <CardHeader>
              <div className="flex items-center justify-between">
                <CardTitle>{adapter.displayName}</CardTitle>
                <Badge variant="outline">{adapter.venue}</Badge>
              </div>
              {adapter.description && (
                <CardDescription className="mt-2">
                  {adapter.description}
                </CardDescription>
              )}
              <p className="text-xs text-muted-foreground">
                Identifier: {adapter.identifier}
              </p>
            </CardHeader>
            <CardContent className="space-y-4">
              <div>
                <h4 className="text-sm font-semibold mb-2">Capabilities</h4>
                <div className="flex flex-wrap gap-1">
                  {adapter.capabilities.map((cap) => (
                    <Badge key={cap} variant="secondary">
                      {cap}
                    </Badge>
                  ))}
                </div>
              </div>
              {adapter.settingsSchema.length > 0 && (
                <div>
                  <h4 className="text-sm font-semibold mb-2">Settings Schema</h4>
                  <ul className="text-sm space-y-1">
                    {adapter.settingsSchema.map((setting) => (
                      <li key={setting.name} className="text-muted-foreground">
                        <span className="font-medium">{setting.name}</span>
                        {setting.required && <span className="text-red-500">*</span>}
                        {' '}({setting.type})
                      </li>
                    ))}
                  </ul>
                </div>
              )}
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  );
}
