'use client';

import { useCallback, useEffect, useState } from 'react';
import { apiClient } from '@/lib/api-client';
import type { RuntimeConfig } from '@/lib/types';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Textarea } from '@/components/ui/textarea';
import { Button } from '@/components/ui/button';
import { Alert, AlertDescription } from '@/components/ui/alert';

const formatConfig = (value: RuntimeConfig): string => JSON.stringify(value, null, 2);

export default function RuntimeConfigPage() {
  const [exportText, setExportText] = useState('');
  const [importText, setImportText] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const fetchConfig = useCallback(async () => {
    setLoading(true);
    setError(null);
    setNotice(null);
    try {
      const config = await apiClient.getRuntimeConfig();
      const formatted = formatConfig(config);
      setExportText(formatted);
      setImportText(formatted);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load runtime configuration');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void fetchConfig();
  }, [fetchConfig]);

  const handleCopy = async () => {
    if (typeof navigator === 'undefined' || !navigator.clipboard) {
      setError('Clipboard API unavailable in this environment');
      return;
    }
    try {
      await navigator.clipboard.writeText(exportText);
      setNotice('Runtime configuration copied to clipboard');
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to copy configuration');
    }
  };

  const handleFormatImport = () => {
    try {
      const parsed = JSON.parse(importText) as RuntimeConfig;
      setImportText(formatConfig(parsed));
      setError(null);
      setNotice('Import payload formatted');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Import payload must be valid JSON');
    }
  };

  const handleApply = async () => {
    setSaving(true);
    setError(null);
    setNotice(null);
    try {
      const parsed = JSON.parse(importText) as RuntimeConfig;
      const updated = await apiClient.updateRuntimeConfig(parsed);
      const formatted = formatConfig(updated);
      setExportText(formatted);
      setImportText(formatted);
      setNotice('Runtime configuration updated successfully');
    } catch (err) {
      if (err instanceof SyntaxError) {
        setError('Import payload must be valid JSON');
      } else {
        setError(err instanceof Error ? err.message : 'Failed to update runtime configuration');
      }
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-6">
      <div className="space-y-1">
        <h1 className="text-3xl font-bold tracking-tight">Runtime configuration</h1>
        <p className="text-muted-foreground">
          Export the current gateway snapshot or import updates to apply new runtime settings.
        </p>
      </div>

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

      <Card>
        <CardHeader>
          <CardTitle>Export snapshot</CardTitle>
          <CardDescription>
            View the live runtime configuration. Copy or refresh to ensure you have the latest state.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <Textarea
            value={exportText}
            readOnly
            className="font-mono text-sm"
            rows={16}
            aria-label="Runtime configuration snapshot"
          />
          <div className="flex flex-wrap gap-3">
            <Button onClick={handleCopy} disabled={loading || !exportText}>
              Copy to clipboard
            </Button>
            <Button variant="outline" onClick={() => void fetchConfig()} disabled={loading}>
              {loading ? 'Refreshing…' : 'Refresh snapshot'}
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Import configuration</CardTitle>
          <CardDescription>
            Paste a JSON payload to update runtime settings. Formatting and validation are performed client-side before import.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <Textarea
            value={importText}
            onChange={(event) => setImportText(event.target.value)}
            className="font-mono text-sm"
            rows={18}
            aria-label="Runtime configuration import payload"
            disabled={saving}
          />
          <div className="flex flex-wrap gap-3">
            <Button type="button" variant="outline" onClick={handleFormatImport} disabled={saving}>
              Format JSON
            </Button>
            <Button type="button" onClick={handleApply} disabled={saving}>
              {saving ? 'Applying…' : 'Apply configuration'}
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
