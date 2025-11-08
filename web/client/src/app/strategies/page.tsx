'use client';

import { useMemo, useState, useCallback } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { Loader2 } from 'lucide-react';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Alert, AlertDescription } from '@/components/ui/alert';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { ScrollArea } from '@/components/ui/scroll-area';
import { Separator } from '@/components/ui/separator';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import {
  useStrategiesQuery,
  useStrategyModulesQuery,
  useStrategyQuery,
} from '@/lib/hooks';
import { fetchStrategy } from '@/lib/api/strategies';
import { queryKeys } from '@/lib/hooks/query-keys';

function formatDefaultValue(value: unknown): string {
  if (value === undefined) {
    return '—';
  }
  if (value === null) {
    return 'null';
  }
  if (typeof value === 'boolean') {
    return value ? 'true' : 'false';
  }
  if (typeof value === 'object') {
    try {
      const serialized = JSON.stringify(value);
      return serialized.length > 48 ? `${serialized.slice(0, 45)}…` : serialized;
    } catch {
      return '[unserializable]';
    }
  }
  const text = String(value);
  return text.length > 48 ? `${text.slice(0, 45)}…` : text;
}

export default function StrategiesPage() {
  const queryClient = useQueryClient();
  const { data, isLoading, isError, error } = useStrategiesQuery();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [selectedStrategyName, setSelectedStrategyName] = useState<string | null>(null);
  const strategyDetailQuery = useStrategyQuery(
    selectedStrategyName ?? undefined,
    Boolean(drawerOpen && selectedStrategyName),
  );
  const modulesQuery = useStrategyModulesQuery(
    selectedStrategyName ? { strategy: selectedStrategyName } : undefined,
    Boolean(drawerOpen && selectedStrategyName),
  );

  const handleCardClick = useCallback((name: string) => {
    setSelectedStrategyName(name);
    setDrawerOpen(true);
  }, []);

  const prefetchStrategy = useCallback(
    (name: string) => {
      void queryClient.prefetchQuery({
        queryKey: queryKeys.strategy(name),
        queryFn: () => fetchStrategy(name),
        staleTime: 60_000,
      });
    },
    [queryClient],
  );

  const activeStrategy = useMemo(() => {
    if (strategyDetailQuery.data) {
      return strategyDetailQuery.data;
    }
    if (!selectedStrategyName || !data) {
      return null;
    }
    return data.find((strategy) => strategy.name === selectedStrategyName) ?? null;
  }, [strategyDetailQuery.data, selectedStrategyName, data]);

  const drawerLoading = Boolean(drawerOpen && selectedStrategyName && strategyDetailQuery.isLoading);
  const drawerError =
    drawerOpen && strategyDetailQuery.error
      ? strategyDetailQuery.error instanceof Error
        ? strategyDetailQuery.error.message
        : 'Failed to load strategy details'
      : null;

  const drawerModules = modulesQuery.data?.modules ?? [];
  const drawerModulesLoading = Boolean(drawerOpen && modulesQuery.isLoading);
  const drawerModulesError =
    drawerOpen && modulesQuery.error
      ? modulesQuery.error instanceof Error
        ? modulesQuery.error.message
        : 'Failed to load modules'
      : null;

  const handleDrawerChange = (open: boolean) => {
    setDrawerOpen(open);
    if (!open) {
      setSelectedStrategyName(null);
    }
  };

  if (isLoading) {
    return <div>Loading strategies...</div>;
  }

  if (isError) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{error instanceof Error ? error.message : 'Failed to load strategies'}</AlertDescription>
      </Alert>
    );
  }

  const strategies = data ?? [];

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
          <button
            type="button"
            key={strategy.name}
            onClick={() => handleCardClick(strategy.name)}
            onMouseEnter={() => prefetchStrategy(strategy.name)}
            onFocus={() => prefetchStrategy(strategy.name)}
            className="text-left"
          >
            <Card className="h-full transition hover:border-primary">
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
                      {strategy.config.slice(0, 4).map((cfg) => (
                        <li key={cfg.name} className="text-muted-foreground">
                          <span className="font-medium">{cfg.name}</span>
                          {cfg.required && <span className="text-destructive">*</span>}
                          {' '}
                          ({cfg.type})
                        </li>
                      ))}
                      {strategy.config.length > 4 && (
                        <li className="text-xs text-muted-foreground">
                          +{strategy.config.length - 4} more fields
                        </li>
                      )}
                    </ul>
                  </div>
                )}
              </CardContent>
            </Card>
          </button>
        ))}
      </div>

      <Dialog open={drawerOpen} onOpenChange={handleDrawerChange}>
        <DialogContent className="max-w-4xl sm:max-h-[85vh] flex flex-col">
          <DialogHeader>
            <DialogTitle>Strategy details</DialogTitle>
            <DialogDescription>
              Inspect schema, events, and module usage for the selected strategy.
            </DialogDescription>
          </DialogHeader>

          <ScrollArea className="flex-1" type="auto">
            {drawerError && (
              <Alert variant="destructive" className="mb-4">
                <AlertDescription>{drawerError}</AlertDescription>
              </Alert>
            )}

            {drawerLoading ? (
              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                <Loader2 className="h-4 w-4 animate-spin" />
                Loading strategy…
              </div>
            ) : activeStrategy ? (
              <div className="space-y-6 pr-1">
                <section className="space-y-2">
                  <h2 className="text-lg font-semibold text-foreground">
                    {activeStrategy.displayName}
                  </h2>
                  <div className="flex flex-wrap items-center gap-3">
                    <p className="text-sm text-muted-foreground">Identifier</p>
                    <Badge variant="outline">{activeStrategy.name}</Badge>
                    {activeStrategy.version && (
                      <Badge variant="secondary">v{activeStrategy.version}</Badge>
                    )}
                  </div>
                  <p className="text-sm text-muted-foreground">{activeStrategy.description}</p>
                </section>

                <section className="space-y-3">
                  <div>
                    <h3 className="text-sm font-semibold">Emitted events</h3>
                    {activeStrategy.events.length === 0 ? (
                      <p className="text-sm text-muted-foreground">No events declared.</p>
                    ) : (
                      <div className="flex flex-wrap gap-1 pt-2">
                        {activeStrategy.events.map((event) => (
                          <Badge key={event} variant="secondary">
                            {event}
                          </Badge>
                        ))}
                      </div>
                    )}
                  </div>
                </section>

                <section className="space-y-3">
                  <div className="flex items-center justify-between">
                    <h3 className="text-sm font-semibold">Configuration schema</h3>
                    <p className="text-xs text-muted-foreground">
                      {activeStrategy.config.length}{' '}
                      field{activeStrategy.config.length === 1 ? '' : 's'}
                    </p>
                  </div>
                  {activeStrategy.config.length === 0 ? (
                    <p className="text-sm text-muted-foreground">No configurable fields.</p>
                  ) : (
                    <div className="rounded-md border">
                      <Table>
                        <TableHeader>
                          <TableRow>
                            <TableHead className="w-[30%]">Field</TableHead>
                            <TableHead>Type</TableHead>
                            <TableHead>Default</TableHead>
                            <TableHead>Description</TableHead>
                          </TableRow>
                        </TableHeader>
                        <TableBody>
                          {activeStrategy.config.map((cfg) => (
                            <TableRow key={cfg.name}>
                              <TableCell className="font-medium">
                                {cfg.name}
                                {cfg.required && <span className="text-destructive">*</span>}
                              </TableCell>
                              <TableCell>{cfg.type}</TableCell>
                              <TableCell className="text-muted-foreground">
                                {formatDefaultValue(cfg.default)}
                              </TableCell>
                              <TableCell className="text-muted-foreground">
                                {cfg.description || '—'}
                              </TableCell>
                            </TableRow>
                          ))}
                        </TableBody>
                      </Table>
                    </div>
                  )}
                </section>

                <section className="space-y-3">
                  <div className="flex items-center justify-between">
                    <h3 className="text-sm font-semibold">Modules & usage</h3>
                    <p className="text-xs text-muted-foreground">
                      {drawerModules.length}{' '}
                      module{drawerModules.length === 1 ? '' : 's'}
                    </p>
                  </div>
                  {drawerModulesError && (
                    <Alert variant="destructive">
                      <AlertDescription>{drawerModulesError}</AlertDescription>
                    </Alert>
                  )}
                  {drawerModulesLoading ? (
                    <div className="flex items-center gap-2 text-sm text-muted-foreground">
                      <Loader2 className="h-4 w-4 animate-spin" /> Loading modules…
                    </div>
                  ) : drawerModules.length === 0 ? (
                    <p className="text-sm text-muted-foreground">No modules registered for this strategy.</p>
                  ) : (
                    <div className="space-y-3">
                      {drawerModules.map((module) => (
                        <div
                          key={`${module.name}-${module.hash}`}
                          className="rounded-md border p-3 text-sm"
                        >
                          <div className="flex flex-wrap items-center gap-2">
                            <p className="font-medium text-foreground">{module.name}</p>
                            {module.version && <Badge variant="secondary">v{module.version}</Badge>}
                            {module.tags.length > 0 && (
                              <Badge variant="outline">{module.tags.join(', ')}</Badge>
                            )}
                          </div>
                          <div className="text-xs text-muted-foreground mt-1">
                            Hash {module.hash} · Size {Math.round(module.size / 1024)} KB
                          </div>
                          {module.running && module.running.length > 0 && (
                            <div className="text-xs text-muted-foreground mt-2">
                              {module.running.reduce((sum, entry) => sum + entry.count, 0)} active instance
                              {module.running.reduce((sum, entry) => sum + entry.count, 0) === 1 ? '' : 's'}
                            </div>
                          )}
                        </div>
                      ))}
                    </div>
                  )}
                </section>

                <Separator />
                <p className="text-xs text-muted-foreground">
                  Strategies and modules refresh automatically when the registry is updated.
                </p>
              </div>
            ) : (
              <div className="text-sm text-muted-foreground">Select a strategy to view details.</div>
            )}
          </ScrollArea>
        </DialogContent>
      </Dialog>
    </div>
  );
}
