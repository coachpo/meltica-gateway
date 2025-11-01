'use client';

import { ChangeEvent, useCallback, useEffect, useMemo, useState } from 'react';
import { apiClient } from '@/lib/api-client';
import type { ContextBackupPayload } from '@/lib/types';
import {
  formatContextBackupPayload,
  getSensitiveKeyFragments,
  sanitizeContextBackupPayload,
} from '@/lib/context-backup';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Separator } from '@/components/ui/separator';
import { Textarea } from '@/components/ui/textarea';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';

export default function ContextBackupPage() {
  const [snapshot, setSnapshot] = useState<ContextBackupPayload | null>(null);
  const [loadingSnapshot, setLoadingSnapshot] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [downloading, setDownloading] = useState(false);
  const [restoring, setRestoring] = useState(false);
  const [importText, setImportText] = useState('');
  const [sanitizedPreview, setSanitizedPreview] = useState('');
  const [validationError, setValidationError] = useState<string | null>(null);

  const sensitivePatterns = useMemo(() => getSensitiveKeyFragments().join(', '), []);

  const loadSnapshot = useCallback(async (showNotice = false, silent = false) => {
    if (!silent) {
      setLoadingSnapshot(true);
    }
    setError(null);
    try {
      const data = await apiClient.getContextBackup();
      setSnapshot(data);
      if (showNotice) {
        setNotice('Context snapshot refreshed');
      }
      return data;
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load context backup snapshot';
      setError(message);
      return null;
    } finally {
      if (!silent) {
        setLoadingSnapshot(false);
      }
    }
  }, []);

  useEffect(() => {
    void loadSnapshot();
  }, [loadSnapshot]);

  const handleRefresh = async () => {
    setNotice(null);
    await loadSnapshot(true);
  };

  const obtainSnapshot = useCallback(async () => {
    const current = snapshot ?? (await loadSnapshot(false, true));
    return current;
  }, [snapshot, loadSnapshot]);

  const handleDownload = async () => {
    setNotice(null);
    setError(null);
    setDownloading(true);
    try {
      const data = await obtainSnapshot();
      if (!data) {
        return;
      }
      const formatted = formatContextBackupPayload(data);
      const blob = new Blob([formatted], { type: 'application/json' });
      const href = URL.createObjectURL(blob);
      const anchor = document.createElement('a');
      anchor.href = href;
      anchor.download = `meltica-context-backup-${new Date().toISOString().replace(/[:.]/g, '-')}.json`;
      document.body.appendChild(anchor);
      anchor.click();
      document.body.removeChild(anchor);
      URL.revokeObjectURL(href);
      setNotice('Context backup downloaded');
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to download context backup';
      setError(message);
    } finally {
      setDownloading(false);
    }
  };

  const handleCopy = async () => {
    setNotice(null);
    setError(null);
    try {
      if (typeof navigator === 'undefined' || !navigator.clipboard) {
        throw new Error('Clipboard API unavailable in this environment');
      }
      const data = await obtainSnapshot();
      if (!data) {
        return;
      }
      await navigator.clipboard.writeText(formatContextBackupPayload(data));
      setNotice('Context backup copied to clipboard');
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to copy context backup';
      setError(message);
    }
  };

  const handleImportChange = (event: ChangeEvent<HTMLTextAreaElement>) => {
    setImportText(event.target.value);
    setValidationError(null);
    setSanitizedPreview('');
  };

  const handleImportFile = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (!file) {
      return;
    }
    try {
      const text = await file.text();
      setImportText(text);
      setValidationError(null);
      setSanitizedPreview('');
      setNotice(`Loaded backup file ${file.name}`);
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Unable to read selected file';
      setError(message);
    } finally {
      event.target.value = '';
    }
  };

  const sanitizeInputPayload = (): ContextBackupPayload | null => {
    if (!importText.trim()) {
      setValidationError('Paste a backup payload or select a file to continue');
      return null;
    }
    try {
      const parsed = JSON.parse(importText);
      const sanitized = sanitizeContextBackupPayload(parsed);
      setSanitizedPreview(formatContextBackupPayload(sanitized));
      setValidationError(null);
      return sanitized;
    } catch (err) {
      const message = err instanceof SyntaxError ? 'Backup payload must be valid JSON' : err instanceof Error ? err.message : 'Backup payload is invalid';
      setValidationError(message);
      setSanitizedPreview('');
      return null;
    }
  };

  const handleValidate = () => {
    setNotice(null);
    setError(null);
    const sanitized = sanitizeInputPayload();
    if (sanitized) {
      setNotice('Payload sanitized. Review the preview below before restoring.');
    }
  };

  const handleRestore = async () => {
    setNotice(null);
    setError(null);
    setRestoring(true);
    const sanitized = sanitizeInputPayload();
    if (!sanitized) {
      setRestoring(false);
      return;
    }
    try {
      await apiClient.restoreContextBackup(sanitized);
      setNotice('Context restored. Providers and lambdas were recreated stopped. Start them manually after validation.');
      await loadSnapshot(false, true);
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to restore context backup';
      setError(message);
    } finally {
      setRestoring(false);
    }
  };

  const providerCount = snapshot?.providers?.length ?? 0;
  const lambdaCount = snapshot?.lambdas?.length ?? 0;

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-2">
        <h1 className="text-3xl font-bold tracking-tight">Context Backup</h1>
        <p className="text-muted-foreground">
          Export runtime providers, lambdas, and risk limits or restore a sanitized backup payload.
        </p>
      </div>

      <Alert>
        <AlertTitle>Restores resume in a stopped state</AlertTitle>
        <AlertDescription>
          Restored providers and lambdas return disabled. Start them manually after confirming their configuration. Sensitive fields matching
          {' '}
          <span className="font-medium text-foreground">{sensitivePatterns}</span>
          {' '}fragments are stripped automatically.
        </AlertDescription>
      </Alert>

      {notice && (
        <Alert>
          <AlertDescription>{notice}</AlertDescription>
        </Alert>
      )}

      {error && (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      <div className="grid gap-6 lg:grid-cols-2">
        <Card className="flex flex-col">
          <CardHeader>
            <CardTitle>Export context backup</CardTitle>
            <CardDescription>
              Download or copy the current runtime-only providers, lambdas, and risk configuration snapshot.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-1 flex-col gap-4">
            <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
              <Badge variant="secondary">{providerCount} providers</Badge>
              <Badge variant="secondary">{lambdaCount} lambdas</Badge>
            </div>
            <Separator />
            <div className="flex flex-wrap gap-2">
              <Button onClick={handleDownload} disabled={downloading || loadingSnapshot}>
                {downloading ? 'Downloading...' : 'Download JSON'}
              </Button>
              <Button variant="outline" onClick={handleCopy} disabled={loadingSnapshot}>
                Copy JSON
              </Button>
              <Button variant="ghost" onClick={handleRefresh} disabled={loadingSnapshot}>
                {loadingSnapshot ? 'Refreshing...' : 'Refresh snapshot'}
              </Button>
            </div>
            {loadingSnapshot && (
              <p className="text-sm text-muted-foreground">Loading context snapshot...</p>
            )}
            {snapshot && (
              <Textarea
                className="h-64 font-mono text-xs"
                value={formatContextBackupPayload(snapshot)}
                readOnly
              />
            )}
          </CardContent>
        </Card>

        <Card className="flex flex-col">
          <CardHeader>
            <CardTitle>Restore context backup</CardTitle>
            <CardDescription>
              Paste or select a sanitized payload to recreate runtime resources. Invalid payloads are rejected before sending.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-1 flex-col gap-4">
            <div className="space-y-2">
              <label htmlFor="context-backup-import" className="text-sm font-medium text-foreground">
                Paste backup JSON
              </label>
              <Textarea
                id="context-backup-import"
                className="h-48 font-mono text-xs"
                placeholder={`{
  "providers": [],
  "lambdas": [],
  "risk": {}
}`}
                value={importText}
                onChange={handleImportChange}
              />
            </div>

            <div className="flex flex-wrap items-center gap-2">
              <Input type="file" accept="application/json" onChange={handleImportFile} className="max-w-xs" />
              <Button variant="outline" onClick={handleValidate} disabled={restoring}>
                Validate payload
              </Button>
              <Button onClick={handleRestore} disabled={restoring}>
                {restoring ? 'Restoring...' : 'Restore backup'}
              </Button>
            </div>

            {validationError && (
              <Alert variant="destructive">
                <AlertDescription>{validationError}</AlertDescription>
              </Alert>
            )}

            {sanitizedPreview && (
              <div className="space-y-2">
                <div className="text-sm font-medium text-foreground">Sanitized payload preview</div>
                <Textarea className="h-48 font-mono text-xs" value={sanitizedPreview} readOnly />
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
