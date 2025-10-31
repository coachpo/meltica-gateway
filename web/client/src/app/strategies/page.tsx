'use client';

import { useEffect, useState } from 'react';
import { apiClient } from '@/lib/api-client';
import { Strategy } from '@/lib/types';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Alert, AlertDescription } from '@/components/ui/alert';

export default function StrategiesPage() {
  const [strategies, setStrategies] = useState<Strategy[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const fetchStrategies = async () => {
      try {
        const response = await apiClient.getStrategies();
        setStrategies(response.strategies);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to fetch strategies');
      } finally {
        setLoading(false);
      }
    };

    fetchStrategies();
  }, []);

  if (loading) {
    return <div>Loading strategies...</div>;
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
        <h1 className="text-3xl font-bold tracking-tight">Strategies</h1>
        <p className="text-muted-foreground">
          Browse available trading strategy definitions and their configuration options
        </p>
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {strategies.map((strategy) => (
          <Card key={strategy.name}>
            <CardHeader>
              <CardTitle>{strategy.displayName}</CardTitle>
              <CardDescription>{strategy.description}</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div>
                <h4 className="text-sm font-semibold mb-2">Events</h4>
                <div className="flex flex-wrap gap-1">
                  {strategy.events.map((event) => (
                    <Badge key={event} variant="secondary">
                      {event}
                    </Badge>
                  ))}
                </div>
              </div>
              {strategy.config.length > 0 && (
                <div>
                  <h4 className="text-sm font-semibold mb-2">Configuration</h4>
                  <ul className="text-sm space-y-1">
                    {strategy.config.map((cfg) => (
                      <li key={cfg.name} className="text-muted-foreground">
                        <span className="font-medium">{cfg.name}</span>
                        {cfg.required && <span className="text-red-500">*</span>}
                        {' '}({cfg.type})
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
