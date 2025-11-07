'use client';

import { useState } from 'react';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import { useConfigBackupQuery, useRestoreConfigBackupMutation } from '@/lib/hooks';

export default function ConfigBackupPage() {
  const { data, isLoading, isError, error, refetch, isFetching } = useConfigBackupQuery(true);
  const restoreMutation = useRestoreConfigBackupMutation();
  const [draft, setDraft] = useState('');
  const [validationError, setValidationError] = useState<string | null>(null);

  const backupJson = data ? JSON.stringify(data, null, 2) : '';

  const handleDownload = () => {
    if (!data) {
      return;
    }
    const blob = new Blob([backupJson], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement('a');
    anchor.href = url;
    anchor.download = `config-backup-${new Date().toISOString().replace(/[:.]/g, '-')}.json`;
    document.body.appendChild(anchor);
    anchor.click();
    document.body.removeChild(anchor);
    URL.revokeObjectURL(url);
  };

  const handleRestore = async () => {
    setValidationError(null);
    try {
      const parsed = JSON.parse(draft);
      await restoreMutation.mutateAsync(parsed);
      setDraft('');
      await refetch();
    } catch (err) {
      setValidationError(err instanceof Error ? err.message : 'Invalid backup payload');
    }
  };

  if (isLoading) {
    return <div>Loading configuration backup…</div>;
  }

  if (isError) {
    return (
      <Alert variant="destructive">
        <AlertDescription>
          {error instanceof Error ? error.message : 'Failed to load configuration backup.'}
        </AlertDescription>
      </Alert>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-2 md:flex-row md:items-center md:justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Config Backup</h1>
          <p className="text-muted-foreground">
            Download full gateway configuration snapshots or restore published backups.
          </p>
        </div>
        <Button variant="outline" onClick={() => refetch()} disabled={isFetching}>
          {isFetching ? 'Refreshing…' : 'Refresh'}
        </Button>
      </div>

      {validationError && (
        <Alert variant="destructive">
          <AlertDescription>{validationError}</AlertDescription>
        </Alert>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Current backup</CardTitle>
          <CardDescription>
            Generated {data?.generatedAt ? new Date(data.generatedAt).toLocaleString() : '—'} ·{' '}
            Environment {data?.environment}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex flex-wrap gap-2 text-xs text-muted-foreground">
            {data?.meta?.name && <Badge variant="secondary">{data.meta.name}</Badge>}
            {data?.version && <Badge variant="outline">Version {data.version}</Badge>}
          </div>
          <pre className="max-h-[24rem] overflow-auto rounded-md border bg-muted/40 p-4 text-xs">
            {backupJson}
          </pre>
          <Button onClick={handleDownload} disabled={!data}>
            Download JSON
          </Button>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Restore backup</CardTitle>
          <CardDescription>Paste a validated JSON payload to restore configuration.</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="config-restore">Backup JSON</Label>
            <Textarea
              id="config-restore"
              rows={14}
              value={draft}
              onChange={(event) => setDraft(event.target.value)}
              placeholder="{}"
              spellCheck={false}
              className="font-mono text-sm"
            />
          </div>
          <div className="flex gap-2">
            <Button onClick={handleRestore} disabled={restoreMutation.isPending || !draft.trim()}>
              {restoreMutation.isPending ? 'Restoring…' : 'Restore'}
            </Button>
            <Button variant="outline" onClick={() => setDraft('')} disabled={!draft}>
              Clear
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
