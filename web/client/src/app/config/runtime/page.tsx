'use client';

import { useEffect, useState } from 'react';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import { useRuntimeConfigQuery, useUpdateRuntimeConfigMutation, useRevertRuntimeConfigMutation } from '@/lib/hooks';
import type { RuntimeConfig } from '@/lib/types';

export default function RuntimeConfigPage() {
  const { data, isLoading, isError, error } = useRuntimeConfigQuery();
  const updateMutation = useUpdateRuntimeConfigMutation();
  const revertMutation = useRevertRuntimeConfigMutation();
  const [draft, setDraft] = useState('');
  const [parseError, setParseError] = useState<string | null>(null);

  /* eslint-disable react-hooks/set-state-in-effect */
  useEffect(() => {
    if (data?.config) {
      setDraft(JSON.stringify(data.config, null, 2));
    }
  }, [data]);
  /* eslint-enable react-hooks/set-state-in-effect */

  const handleSave = async () => {
    setParseError(null);
    try {
      const parsed = JSON.parse(draft) as RuntimeConfig;
      await updateMutation.mutateAsync(parsed);
    } catch (err) {
      setParseError(err instanceof Error ? err.message : 'Invalid runtime configuration JSON');
    }
  };

  const handleRevert = async () => {
    await revertMutation.mutateAsync();
  };

  if (isLoading) {
    return <div>Loading runtime configuration…</div>;
  }

  if (isError) {
    return (
      <Alert variant="destructive">
        <AlertDescription>
          {error instanceof Error ? error.message : 'Failed to load runtime configuration.'}
        </AlertDescription>
      </Alert>
    );
  }

  const metadata = data?.metadata ?? {};
  const persistedAt = data?.persistedAt ? new Date(data.persistedAt).toLocaleString() : null;
  const filePath =
    typeof data?.filePath === 'string'
      ? data.filePath
      : typeof metadata?.path === 'string'
        ? (metadata.path as string)
        : null;

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-2 md:flex-row md:items-center md:justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Runtime Config</h1>
          <p className="text-muted-foreground">
            Review and edit the gateway&apos;s runtime configuration snapshot.
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" onClick={handleRevert} disabled={revertMutation.isPending}>
            {revertMutation.isPending ? 'Reverting…' : 'Revert'}
          </Button>
          <Button onClick={handleSave} disabled={updateMutation.isPending}>
            {updateMutation.isPending ? 'Saving…' : 'Save'}
          </Button>
        </div>
      </div>

      {parseError && (
        <Alert variant="destructive">
          <AlertDescription>{parseError}</AlertDescription>
        </Alert>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Snapshot</CardTitle>
          <CardDescription>
            Source: {data?.source ?? 'runtime'}
            {persistedAt ? ` · persisted ${persistedAt}` : ''}
            {filePath ? ` · ${filePath}` : ''}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="runtime-json">Runtime JSON</Label>
            <Textarea
              id="runtime-json"
              value={draft}
              onChange={(event) => setDraft(event.target.value)}
              spellCheck={false}
              rows={24}
              className="font-mono text-sm"
            />
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
