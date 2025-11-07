'use client';

import { useState } from 'react';
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
import { Button } from '@/components/ui/button';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { ScrollArea } from '@/components/ui/scroll-area';
import { Separator } from '@/components/ui/separator';
import { useAdaptersQuery } from '@/lib/hooks';

function formatDefault(value: unknown): string {
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
      return serialized.length > 40 ? `${serialized.slice(0, 37)}…` : serialized;
    } catch {
      return '[unserializable]';
    }
  }
  const text = String(value);
  return text.length > 40 ? `${text.slice(0, 37)}…` : text;
}

export default function AdaptersPage() {
  const { data, isLoading, isError, error } = useAdaptersQuery();
  const [dialogOpen, setDialogOpen] = useState(false);
  const [selectedIdentifier, setSelectedIdentifier] = useState<string | null>(null);
  const adapters = data ?? [];
  const selectedAdapter = adapters.find((adapter) => adapter.identifier === selectedIdentifier);

  if (isLoading) {
    return <div>Loading adapters...</div>;
  }

  if (isError) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{error instanceof Error ? error.message : 'Failed to load adapters'}</AlertDescription>
      </Alert>
    );
  }

  const handleOpenSchema = (identifier: string) => {
    setSelectedIdentifier(identifier);
    setDialogOpen(true);
  };

  const closeDialog = (open: boolean) => {
    setDialogOpen(open);
    if (!open) {
      setSelectedIdentifier(null);
    }
  };

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
              <div className="flex items-center justify-between">
                <div>
                  <p className="text-xs text-muted-foreground">
                    {adapter.settingsSchema.length}{' '}
                    field{adapter.settingsSchema.length === 1 ? '' : 's'} in schema
                  </p>
                </div>
                <Button variant="outline" size="sm" onClick={() => handleOpenSchema(adapter.identifier)}>
                  View schema
                </Button>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>

      <Dialog open={dialogOpen} onOpenChange={closeDialog}>
        <DialogContent className="max-w-3xl sm:max-h-[85vh] flex flex-col">
          <DialogHeader>
            <DialogTitle>{selectedAdapter ? selectedAdapter.displayName : 'Adapter schema'}</DialogTitle>
            <DialogDescription>
              Review field requirements and capabilities for the selected adapter.
            </DialogDescription>
          </DialogHeader>

          {selectedAdapter ? (
            <ScrollArea className="flex-1" type="auto">
              <div className="space-y-4 pr-1 text-sm">
                <section>
                  <p className="text-muted-foreground">
                    Identifier: <span className="font-mono">{selectedAdapter.identifier}</span>
                  </p>
                  <p className="text-muted-foreground">
                    Venue: <span className="font-semibold">{selectedAdapter.venue}</span>
                  </p>
                  {selectedAdapter.description && (
                    <p className="mt-2 text-muted-foreground">{selectedAdapter.description}</p>
                  )}
                </section>

                <Separator />

                <section className="space-y-2">
                  <h3 className="text-sm font-semibold">Capabilities</h3>
                  {selectedAdapter.capabilities.length === 0 ? (
                    <p className="text-muted-foreground">No capabilities listed.</p>
                  ) : (
                    <div className="flex flex-wrap gap-1">
                      {selectedAdapter.capabilities.map((capability) => (
                        <Badge key={capability} variant="secondary">
                          {capability}
                        </Badge>
                      ))}
                    </div>
                  )}
                </section>

                <Separator />

                <section className="space-y-3">
                  <div className="flex items-center justify-between">
                    <h3 className="text-sm font-semibold">Settings schema</h3>
                    <p className="text-xs text-muted-foreground">
                      {selectedAdapter.settingsSchema.length}{' '}
                      field{selectedAdapter.settingsSchema.length === 1 ? '' : 's'}
                    </p>
                  </div>
                  {selectedAdapter.settingsSchema.length === 0 ? (
                    <p className="text-muted-foreground">No configuration fields defined.</p>
                  ) : (
                    <div className="rounded-md border">
                      <Table>
                        <TableHeader>
                          <TableRow>
                            <TableHead className="w-[25%]">Field</TableHead>
                            <TableHead>Type</TableHead>
                            <TableHead>Default</TableHead>
                            <TableHead>Description</TableHead>
                          </TableRow>
                        </TableHeader>
                        <TableBody>
                          {selectedAdapter.settingsSchema.map((field) => (
                            <TableRow key={field.name}>
                              <TableCell className="font-medium">
                                {field.name}
                                {field.required && <span className="text-red-500">*</span>}
                              </TableCell>
                              <TableCell>{field.type}</TableCell>
                              <TableCell className="text-muted-foreground">
                                {formatDefault(field.default)}
                              </TableCell>
                              <TableCell className="text-muted-foreground">
                                {field.description || '—'}
                              </TableCell>
                            </TableRow>
                          ))}
                        </TableBody>
                      </Table>
                    </div>
                  )}
                </section>
              </div>
            </ScrollArea>
          ) : (
            <div className="text-sm text-muted-foreground">Select an adapter to inspect its schema.</div>
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}
