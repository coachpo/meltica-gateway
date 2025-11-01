'use client';

import { useCallback, useEffect, useMemo, useState, type ChangeEvent } from 'react';
import { apiClient } from '@/lib/api-client';
import type { ConfigBackup, RuntimeConfig, RuntimeConfigSnapshot } from '@/lib/types';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Label } from '@/components/ui/label';
import { Input } from '@/components/ui/input';
import { Textarea } from '@/components/ui/textarea';
import { Separator } from '@/components/ui/separator';

const DEFAULT_RUNTIME_CONFIG: RuntimeConfig = {
  eventbus: {
    bufferSize: 8192,
    fanoutWorkers: 8,
  },
  pools: {
    event: {
      size: 8192,
      waitQueueSize: 8192,
    },
    orderRequest: {
      size: 4096,
      waitQueueSize: 4096,
    },
  },
  risk: {
    maxPositionSize: '250',
    maxNotionalValue: '50000',
    notionalCurrency: 'USDT',
    orderThrottle: 5,
    orderBurst: 3,
    maxConcurrentOrders: 6,
    priceBandPercent: 1,
    allowedOrderTypes: ['Limit', 'Market'],
    killSwitchEnabled: true,
    maxRiskBreaches: 3,
    circuitBreaker: {
      enabled: true,
      threshold: 4,
      cooldown: '90s',
    },
  },
  apiServer: {
    addr: ':8880',
  },
  telemetry: {
    otlpEndpoint: '',
    serviceName: 'meltica-gateway',
    otlpInsecure: true,
    enableMetrics: true,
  },
};

const cloneRuntimeConfig = (config: RuntimeConfig | null): RuntimeConfig | null => {
  if (!config) {
    return null;
  }
  return JSON.parse(JSON.stringify(config)) as RuntimeConfig;
};

const formatJson = (value: unknown) => JSON.stringify(value, null, 2);

const parseAllowedOrderTypes = (value: string): string[] =>
  value
    .split(',')
    .map((entry) => entry.trim())
    .filter((entry) => entry.length > 0);

const deepEqualRuntime = (a: RuntimeConfig | null, b: RuntimeConfig | null): boolean => {
  if (!a && !b) {
    return true;
  }
  if (!a || !b) {
    return false;
  }
  return formatJson(a) === formatJson(b);
};

const hasListEntries = (record: Record<string, unknown> | null | undefined): boolean =>
  Boolean(record && Object.keys(record).length > 0);

export default function RuntimeConfigPage() {
  const [runtimeSnapshot, setRuntimeSnapshot] = useState<RuntimeConfigSnapshot | null>(null);
  const [runtimeDraft, setRuntimeDraft] = useState<RuntimeConfig | null>(null);
  const [allowedOrderTypesInput, setAllowedOrderTypesInput] = useState('');
  const [backup, setBackup] = useState<ConfigBackup | null>(null);
  const [backupText, setBackupText] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [downloading, setDownloading] = useState(false);
  const [restoring, setRestoring] = useState(false);
  const [reverting, setReverting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const runtime = runtimeSnapshot?.config ?? null;
  const runtimeSource = runtimeSnapshot?.source ?? 'runtime';
  const runtimeSourceLabel = runtimeSource === 'file'
    ? 'Configuration file'
    : runtimeSource === 'bootstrap'
      ? 'Bootstrap defaults'
      : 'Runtime overrides';
  const runtimePersistedAt = runtimeSnapshot?.persistedAt ?? null;
  const runtimeFilePath = runtimeSnapshot?.filePath ?? null;

  const syncAllowedOrderTypes = useCallback((source: RuntimeConfig | null) => {
    if (!source) {
      return;
    }
    setAllowedOrderTypesInput(source.risk.allowedOrderTypes.join(', '));
  }, []);

  const fetchData = useCallback(async () => {
    setLoading(true);
    setError(null);
    setNotice(null);
    try {
      const [runtimeConfigSnapshot, fullBackup] = await Promise.all([
        apiClient.getRuntimeConfig(),
        apiClient.getConfigBackup(),
      ]);
      setRuntimeSnapshot(runtimeConfigSnapshot);
      const draft = cloneRuntimeConfig(runtimeConfigSnapshot.config);
      setRuntimeDraft(draft);
      syncAllowedOrderTypes(draft);
      setBackup(fullBackup);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load runtime configuration');
    } finally {
      setLoading(false);
    }
  }, [syncAllowedOrderTypes]);

  useEffect(() => {
    void fetchData();
  }, [fetchData]);

  const handleRefresh = async () => {
    await fetchData();
    setNotice('Configuration snapshot refreshed');
  };

  const handleCopyRuntime = async () => {
    if (!runtime) {
      return;
    }
    if (typeof navigator === 'undefined' || !navigator.clipboard) {
      setError('Clipboard API unavailable in this environment');
      return;
    }
    try {
      await navigator.clipboard.writeText(formatJson(runtime));
      setNotice('Runtime configuration copied to clipboard');
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to copy configuration');
    }
  };

  const handleDownloadBackup = async () => {
    setDownloading(true);
    setError(null);
    setNotice(null);
    try {
      const snapshot = await apiClient.getConfigBackup();
      setBackup(snapshot);
      const blob = new Blob([formatJson(snapshot)], {
        type: 'application/json',
      });
      const href = URL.createObjectURL(blob);
      const anchor = document.createElement('a');
      anchor.href = href;
      anchor.download = `meltica-config-backup-${new Date().toISOString().replace(/[:.]/g, '-')}.json`;
      document.body.appendChild(anchor);
      anchor.click();
      document.body.removeChild(anchor);
      URL.revokeObjectURL(href);
      setNotice('Full configuration backup downloaded');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to download configuration backup');
    } finally {
      setDownloading(false);
    }
  };

  const updateRuntimeDraft = useCallback(<K extends keyof RuntimeConfig>(key: K, value: RuntimeConfig[K]) => {
    setRuntimeDraft((previous) => {
      if (!previous) {
        return previous;
      }
      return {
        ...previous,
        [key]: value,
      };
    });
  }, []);

  const handleEventbusChange = (field: 'bufferSize' | 'fanoutWorkers', raw: string) => {
    setRuntimeDraft((previous) => {
      if (!previous) {
        return previous;
      }
      let value: number | string = raw;
      if (field === 'bufferSize') {
        const numeric = Number(raw);
        value = Number.isNaN(numeric) ? 0 : numeric;
      } else {
        const numeric = Number(raw);
        value = raw.trim().length === 0 ? 'default' : Number.isNaN(numeric) ? raw.trim() : numeric;
      }
      return {
        ...previous,
        eventbus: {
          ...previous.eventbus,
          [field]: value,
        },
      };
    });
  };

  const handlePoolChange = (
    pool: 'event' | 'orderRequest',
    field: 'size' | 'waitQueueSize',
    raw: string,
  ) => {
    const numeric = Number(raw);
    setRuntimeDraft((previous) => {
      if (!previous) {
        return previous;
      }
      return {
        ...previous,
        pools: {
          ...previous.pools,
          [pool]: {
            ...previous.pools[pool],
            [field]: Number.isNaN(numeric) ? 0 : numeric,
          },
        },
      };
    });
  };

  const handleRiskChange = (field: keyof RuntimeConfig['risk'], raw: string | boolean | number) => {
    setRuntimeDraft((previous) => {
      if (!previous) {
        return previous;
      }
      const nextRisk = { ...previous.risk };
      if (field === 'orderThrottle' || field === 'orderBurst' || field === 'maxConcurrentOrders' || field === 'priceBandPercent' || field === 'maxRiskBreaches') {
        const numeric = typeof raw === 'string' ? Number(raw) : Number(raw);
        nextRisk[field] = Number.isNaN(numeric) ? 0 : numeric;
      } else if (field === 'allowedOrderTypes') {
        nextRisk.allowedOrderTypes = Array.isArray(raw)
          ? (raw as string[])
          : parseAllowedOrderTypes(String(raw));
      } else if (field === 'killSwitchEnabled') {
        nextRisk.killSwitchEnabled = Boolean(raw);
      } else if (field === 'circuitBreaker') {
        nextRisk.circuitBreaker = raw as RuntimeConfig['risk']['circuitBreaker'];
      } else {
        nextRisk[field] = String(raw);
      }
      return {
        ...previous,
        risk: nextRisk,
      };
    });
  };

  const handleCircuitBreakerChange = (field: 'enabled' | 'threshold' | 'cooldown', raw: string | boolean) => {
    setRuntimeDraft((previous) => {
      if (!previous) {
        return previous;
      }
      const nextBreaker = { ...previous.risk.circuitBreaker };
      if (field === 'enabled') {
        nextBreaker.enabled = Boolean(raw);
      } else if (field === 'threshold') {
        const numeric = Number(raw);
        nextBreaker.threshold = Number.isNaN(numeric) ? 0 : numeric;
      } else {
        nextBreaker.cooldown = String(raw);
      }
      return {
        ...previous,
        risk: {
          ...previous.risk,
          circuitBreaker: nextBreaker,
        },
      };
    });
  };

  const handleTelemetryChange = (field: keyof RuntimeConfig['telemetry'], raw: string | boolean) => {
    setRuntimeDraft((previous) => {
      if (!previous) {
        return previous;
      }
      const value = field === 'enableMetrics' || field === 'otlpInsecure' ? Boolean(raw) : String(raw);
      return {
        ...previous,
        telemetry: {
          ...previous.telemetry,
          [field]: value,
        },
      };
    });
  };

  const handleApplyRuntime = async () => {
    if (!runtimeDraft) {
      return;
    }
    setSaving(true);
    setError(null);
    setNotice(null);
    try {
      const updatedSnapshot = await apiClient.updateRuntimeConfig(runtimeDraft);
      setRuntimeSnapshot(updatedSnapshot);
      const draft = cloneRuntimeConfig(updatedSnapshot.config);
      setRuntimeDraft(draft);
      syncAllowedOrderTypes(draft);
      const latestBackup = await apiClient.getConfigBackup();
      setBackup(latestBackup);
      setNotice('Runtime configuration updated and persisted');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update runtime configuration');
    } finally {
      setSaving(false);
    }
  };

  const handleRestoreBackup = async () => {
    setRestoring(true);
    setError(null);
    setNotice(null);
    try {
      const parsed = JSON.parse(backupText) as ConfigBackup;
      await apiClient.restoreConfigBackup(parsed);
      await fetchData();
      setNotice('Backup imported successfully. Providers, lambdas, and runtime settings restored.');
    } catch (err) {
      if (err instanceof SyntaxError) {
        setError('Backup payload must be valid JSON');
      } else {
        setError(err instanceof Error ? err.message : 'Failed to restore configuration backup');
      }
    } finally {
      setRestoring(false);
    }
  };

  const handleRevertRuntime = async () => {
    setReverting(true);
    setError(null);
    setNotice(null);
    try {
      const snapshot = await apiClient.revertRuntimeConfig();
      setRuntimeSnapshot(snapshot);
      const draft = cloneRuntimeConfig(snapshot.config);
      setRuntimeDraft(draft);
      syncAllowedOrderTypes(draft);
      const latestBackup = await apiClient.getConfigBackup();
      setBackup(latestBackup);
      setNotice('Runtime overrides cleared. Gateway now using configuration file values.');
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to revert runtime configuration';
      if (/not allow/i.test(message) || /405/.test(message)) {
        setError('Runtime revert is not supported by the connected gateway version. Update the backend and try again.');
      } else {
        setError(message);
      }
    } finally {
      setReverting(false);
    }
  };

  const handleBackupFile = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (!file) {
      return;
    }
    try {
      const text = await file.text();
      setBackupText(text);
      setNotice(`Loaded backup file ${file.name}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unable to read selected backup file');
    } finally {
      event.target.value = '';
    }
  };

  const isRuntimeDefault = useMemo(() => deepEqualRuntime(runtimeDraft, DEFAULT_RUNTIME_CONFIG), [runtimeDraft]);
  const isRuntimeDirty = useMemo(() => !deepEqualRuntime(runtime, runtimeDraft), [runtime, runtimeDraft]);

  const providerRuntime = backup?.providers?.runtime ?? [];
  const lambdaManifest = backup?.lambdas?.manifest?.lambdas ?? [];
  const lambdaInstances = backup?.lambdas?.instances ?? [];
  const lambdaCount = lambdaManifest.length;

  const runtimeOverridesActive = useMemo(() => {
    if (runtimeSnapshot) {
      if (runtimeSnapshot.source === 'file') {
        return false;
      }
      if (runtimeSnapshot.source === 'bootstrap') {
        return !deepEqualRuntime(runtimeSnapshot.config, DEFAULT_RUNTIME_CONFIG);
      }
      return true;
    }
    if (!runtime) {
      return false;
    }
    return !deepEqualRuntime(runtime, DEFAULT_RUNTIME_CONFIG);
  }, [runtimeSnapshot, runtime]);

  const hasCustomConfig = useMemo(() => {
    const providerConfigDefined = hasListEntries((backup?.providers?.config as Record<string, unknown>) ?? null);
    return providerConfigDefined || lambdaCount > 0 || runtimeOverridesActive;
  }, [backup, lambdaCount, runtimeOverridesActive]);

  const runningProviders = providerRuntime.filter((provider) => provider.running).length;
  const definedProviders = providerRuntime.length;
  const lambdaDefinitions = lambdaCount;
  const runningLambdas = lambdaInstances.filter((instance) => instance.running).length;

  return (
    <div className="space-y-8">
      <div className="space-y-2">
        <h1 className="text-3xl font-bold tracking-tight">Configuration control</h1>
        <p className="text-muted-foreground">
          Meltica boots from safe defaults, generates a configuration file automatically when you customise settings,
          and can export or restore the full application snapshot on demand.
        </p>
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        <Card>
          <CardHeader className="flex flex-row items-start justify-between gap-4">
            <div>
              <CardTitle>Runtime overview</CardTitle>
              <CardDescription>Inspect the live configuration and persistence state.</CardDescription>
            </div>
            <Button variant="outline" onClick={handleRefresh} disabled={loading}>
              {loading ? 'Refreshing…' : 'Refresh'}
            </Button>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex flex-wrap gap-3">
              <Badge
                variant={runtimeSource === 'runtime' ? 'default' : runtimeSource === 'file' ? 'secondary' : 'outline'}
              >
                Source: {runtimeSourceLabel}
              </Badge>
              <Badge variant={hasCustomConfig ? 'default' : 'secondary'}>
                {hasCustomConfig ? 'Custom configuration active' : 'Running on defaults'}
              </Badge>
              <Badge variant={isRuntimeDefault ? 'secondary' : 'default'}>
                {isRuntimeDefault ? 'Runtime equals defaults' : 'Runtime contains overrides'}
              </Badge>
              {backup?.environment && (
                <Badge variant="outline">Environment: {backup.environment}</Badge>
              )}
            </div>
            <ul className="grid gap-2 text-sm text-muted-foreground md:grid-cols-2">
              <li>
                <span className="font-medium text-foreground">Telemetry service:</span>{' '}
                {runtime?.telemetry.serviceName ?? '—'}
              </li>
              <li>
                <span className="font-medium text-foreground">API server:</span>{' '}
                {runtime?.apiServer.addr ?? '—'}
              </li>
              {runtimePersistedAt && (
                <li>
                  <span className="font-medium text-foreground">Persisted at:</span>{' '}
                  {new Date(runtimePersistedAt).toLocaleString()}
                </li>
              )}
              {runtimeFilePath && (
                <li>
                  <span className="font-medium text-foreground">Config file:</span>{' '}
                  {runtimeFilePath}
                </li>
              )}
              <li>
                <span className="font-medium text-foreground">Providers running:</span>{' '}
                {runningProviders}/{definedProviders}
              </li>
              <li>
                <span className="font-medium text-foreground">Lambdas running:</span>{' '}
                {runningLambdas}/{lambdaDefinitions}
              </li>
            </ul>
            <p className="text-sm text-muted-foreground">
              Meltica persists any edits made below to <code>config/app.yaml</code> automatically. Use revert to drop
              overrides and follow the configuration file on restart.
            </p>
            <div className="flex flex-wrap gap-3">
              <Button variant="secondary" onClick={handleCopyRuntime} disabled={!runtime || loading}>
                Copy runtime JSON
              </Button>
              <Button
                variant="destructive"
                onClick={handleRevertRuntime}
                disabled={reverting || runtimeSource === 'file' || runtimeSource === 'bootstrap' || !runtimeSnapshot}
              >
                {reverting ? 'Reverting…' : 'Revert to config file'}
              </Button>
              <Button
                variant="ghost"
                onClick={() => {
                  const defaults = JSON.parse(JSON.stringify(DEFAULT_RUNTIME_CONFIG)) as RuntimeConfig;
                  setRuntimeDraft(defaults);
                  syncAllowedOrderTypes(defaults);
                  setNotice('Runtime defaults loaded');
                }}
                disabled={saving}
              >
                Load defaults
              </Button>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Backup &amp; restore</CardTitle>
            <CardDescription>
              Export the complete application context or import a previously saved snapshot.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="space-y-1 text-sm text-muted-foreground">
              <div>
                <span className="font-medium text-foreground">Last exported:</span>{' '}
                {backup?.generatedAt ? new Date(backup.generatedAt).toLocaleString() : 'Pending export'}
              </div>
              {hasListEntries(backup?.providers.config ?? null) ? (
                <div>Provider configuration will be included in exports.</div>
              ) : (
                <div>Providers are using runtime defaults; custom settings will be exported once created.</div>
              )}
            </div>
            <div className="flex flex-wrap gap-3">
              <Button variant="secondary" onClick={handleDownloadBackup} disabled={downloading || loading}>
                {downloading ? 'Preparing…' : 'Download full backup'}
              </Button>
              <div>
                <Label className="sr-only" htmlFor="backup-file-input">
                  Upload backup file
                </Label>
                <Input id="backup-file-input" type="file" accept="application/json" onChange={handleBackupFile} />
              </div>
            </div>
            <Textarea
              value={backupText}
              onChange={(event) => setBackupText(event.target.value)}
              placeholder="Paste a full configuration backup to restore providers, lambdas, and runtime settings"
              className="font-mono text-xs"
              rows={8}
            />
            <Button onClick={handleRestoreBackup} disabled={restoring || backupText.trim().length === 0}>
              {restoring ? 'Restoring…' : 'Restore backup'}
            </Button>
          </CardContent>
        </Card>
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
          <CardTitle>Runtime settings</CardTitle>
          <CardDescription>
            Adjust live parameters. Submitted changes persist immediately and survive restarts.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-6">
          <section className="space-y-4">
            <div>
              <h2 className="text-lg font-semibold">Event bus</h2>
              <p className="text-sm text-muted-foreground">
                Control in-memory routing throughput and worker scheduling.
              </p>
            </div>
            <div className="grid gap-4 md:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="eventbus-bufferSize">Buffer size</Label>
                <Input
                  id="eventbus-bufferSize"
                  type="number"
                  value={runtimeDraft?.eventbus.bufferSize ?? ''}
                  onChange={(event) => handleEventbusChange('bufferSize', event.target.value)}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="eventbus-fanout">Fanout workers</Label>
                <Input
                  id="eventbus-fanout"
                  value={runtimeDraft?.eventbus.fanoutWorkers ?? ''}
                  onChange={(event) => handleEventbusChange('fanoutWorkers', event.target.value)}
                  placeholder="8 | auto | default"
                />
              </div>
            </div>
          </section>

          <Separator />

          <section className="space-y-4">
            <div>
              <h2 className="text-lg font-semibold">Object pools</h2>
              <p className="text-sm text-muted-foreground">
                Tune pooled event and order capacities to match exchange throughput.
              </p>
            </div>
            <div className="grid gap-4 md:grid-cols-2">
              {(['event', 'orderRequest'] as const).map((pool) => (
                <div key={pool} className="space-y-3 rounded-md border p-4">
                  <h3 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">{pool} pool</h3>
                  <div className="space-y-2">
                    <Label htmlFor={`${pool}-size`}>Pool size</Label>
                    <Input
                      id={`${pool}-size`}
                      type="number"
                      value={runtimeDraft?.pools[pool].size ?? ''}
                      onChange={(event) => handlePoolChange(pool, 'size', event.target.value)}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor={`${pool}-queue`}>Wait queue size</Label>
                    <Input
                      id={`${pool}-queue`}
                      type="number"
                      value={runtimeDraft?.pools[pool].waitQueueSize ?? ''}
                      onChange={(event) => handlePoolChange(pool, 'waitQueueSize', event.target.value)}
                    />
                  </div>
                </div>
              ))}
            </div>
          </section>

          <Separator />

          <section className="space-y-4">
            <div>
              <h2 className="text-lg font-semibold">Risk controls</h2>
              <p className="text-sm text-muted-foreground">
                Update guard rails for live strategy execution.
              </p>
            </div>
            <div className="grid gap-4 md:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="risk-maxPositionSize">Max position size</Label>
                <Input
                  id="risk-maxPositionSize"
                  value={runtimeDraft?.risk.maxPositionSize ?? ''}
                  onChange={(event) => handleRiskChange('maxPositionSize', event.target.value)}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="risk-maxNotionalValue">Max notional value</Label>
                <Input
                  id="risk-maxNotionalValue"
                  value={runtimeDraft?.risk.maxNotionalValue ?? ''}
                  onChange={(event) => handleRiskChange('maxNotionalValue', event.target.value)}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="risk-notionalCurrency">Notional currency</Label>
                <Input
                  id="risk-notionalCurrency"
                  value={runtimeDraft?.risk.notionalCurrency ?? ''}
                  onChange={(event) => handleRiskChange('notionalCurrency', event.target.value)}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="risk-orderThrottle">Order throttle (ops/s)</Label>
                <Input
                  id="risk-orderThrottle"
                  type="number"
                  value={runtimeDraft?.risk.orderThrottle ?? ''}
                  onChange={(event) => handleRiskChange('orderThrottle', event.target.value)}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="risk-orderBurst">Order burst</Label>
                <Input
                  id="risk-orderBurst"
                  type="number"
                  value={runtimeDraft?.risk.orderBurst ?? ''}
                  onChange={(event) => handleRiskChange('orderBurst', event.target.value)}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="risk-maxConcurrentOrders">Max concurrent orders</Label>
                <Input
                  id="risk-maxConcurrentOrders"
                  type="number"
                  value={runtimeDraft?.risk.maxConcurrentOrders ?? ''}
                  onChange={(event) => handleRiskChange('maxConcurrentOrders', event.target.value)}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="risk-priceBandPercent">Price band %</Label>
                <Input
                  id="risk-priceBandPercent"
                  type="number"
                  step="0.1"
                  value={runtimeDraft?.risk.priceBandPercent ?? ''}
                  onChange={(event) => handleRiskChange('priceBandPercent', event.target.value)}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="risk-allowedOrderTypes">Allowed order types</Label>
                <Input
                  id="risk-allowedOrderTypes"
                  value={allowedOrderTypesInput}
                  onChange={(event) => {
                    setAllowedOrderTypesInput(event.target.value);
                    handleRiskChange('allowedOrderTypes', event.target.value);
                  }}
                  placeholder="Limit, Market"
                />
              </div>
              <div className="space-y-2">
                <Label className="flex items-center gap-2" htmlFor="risk-killSwitchEnabled">
                  <input
                    id="risk-killSwitchEnabled"
                    type="checkbox"
                    className="h-4 w-4"
                    checked={runtimeDraft?.risk.killSwitchEnabled ?? false}
                    onChange={(event) => handleRiskChange('killSwitchEnabled', event.target.checked)}
                  />
                  Kill switch enabled
                </Label>
              </div>
              <div className="space-y-2">
                <Label htmlFor="risk-maxRiskBreaches">Max risk breaches</Label>
                <Input
                  id="risk-maxRiskBreaches"
                  type="number"
                  value={runtimeDraft?.risk.maxRiskBreaches ?? ''}
                  onChange={(event) => handleRiskChange('maxRiskBreaches', event.target.value)}
                />
              </div>
              <div className="space-y-2 md:col-span-2">
                <div className="flex items-center gap-2">
                  <input
                    id="risk-cb-enabled"
                    type="checkbox"
                    className="h-4 w-4"
                    checked={runtimeDraft?.risk.circuitBreaker.enabled ?? false}
                    onChange={(event) => handleCircuitBreakerChange('enabled', event.target.checked)}
                  />
                  <Label htmlFor="risk-cb-enabled">Circuit breaker enabled</Label>
                </div>
                <div className="grid gap-4 md:grid-cols-2">
                  <div className="space-y-2">
                    <Label htmlFor="risk-cb-threshold">Breaker threshold</Label>
                    <Input
                      id="risk-cb-threshold"
                      type="number"
                      value={runtimeDraft?.risk.circuitBreaker.threshold ?? ''}
                      onChange={(event) => handleCircuitBreakerChange('threshold', event.target.value)}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="risk-cb-cooldown">Breaker cooldown</Label>
                    <Input
                      id="risk-cb-cooldown"
                      value={runtimeDraft?.risk.circuitBreaker.cooldown ?? ''}
                      onChange={(event) => handleCircuitBreakerChange('cooldown', event.target.value)}
                      placeholder="90s"
                    />
                  </div>
                </div>
              </div>
            </div>
          </section>

          <Separator />

          <section className="grid gap-4 md:grid-cols-2">
            <div className="space-y-2">
              <h2 className="text-lg font-semibold">API server</h2>
              <Input
                value={runtimeDraft?.apiServer.addr ?? ''}
                onChange={(event) =>
                  updateRuntimeDraft('apiServer', {
                    addr: event.target.value,
                  })
                }
                placeholder=":8880"
              />
            </div>
            <div className="space-y-2">
              <h2 className="text-lg font-semibold">Telemetry</h2>
              <div className="space-y-2">
                <Label htmlFor="telemetry-endpoint">OTLP endpoint</Label>
                <Input
                  id="telemetry-endpoint"
                  value={runtimeDraft?.telemetry.otlpEndpoint ?? ''}
                  onChange={(event) => handleTelemetryChange('otlpEndpoint', event.target.value)}
                  placeholder="http://localhost:4318"
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="telemetry-service">Service name</Label>
                <Input
                  id="telemetry-service"
                  value={runtimeDraft?.telemetry.serviceName ?? ''}
                  onChange={(event) => handleTelemetryChange('serviceName', event.target.value)}
                />
              </div>
              <div className="flex items-center gap-2">
                <input
                  id="telemetry-insecure"
                  type="checkbox"
                  className="h-4 w-4"
                  checked={runtimeDraft?.telemetry.otlpInsecure ?? false}
                  onChange={(event) => handleTelemetryChange('otlpInsecure', event.target.checked)}
                />
                <Label htmlFor="telemetry-insecure">Use insecure transport</Label>
              </div>
              <div className="flex items-center gap-2">
                <input
                  id="telemetry-metrics"
                  type="checkbox"
                  className="h-4 w-4"
                  checked={runtimeDraft?.telemetry.enableMetrics ?? false}
                  onChange={(event) => handleTelemetryChange('enableMetrics', event.target.checked)}
                />
                <Label htmlFor="telemetry-metrics">Enable metrics export</Label>
              </div>
            </div>
          </section>

          <div className="flex flex-wrap gap-3">
            <Button onClick={handleApplyRuntime} disabled={saving || !isRuntimeDirty}>
              {saving ? 'Applying…' : 'Save changes'}
            </Button>
            <Button
              type="button"
              variant="outline"
              onClick={() => {
                const snapshot = runtime
                  ? (JSON.parse(JSON.stringify(runtime)) as RuntimeConfig)
                  : null;
                setRuntimeDraft(snapshot);
                syncAllowedOrderTypes(snapshot);
              }}
              disabled={saving || !isRuntimeDirty}
            >
              Discard edits
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
