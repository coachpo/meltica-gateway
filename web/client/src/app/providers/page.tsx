'use client';

import { useEffect, useMemo, useState } from 'react';
import { apiClient } from '@/lib/api-client';
import type {
  AdapterMetadata,
  Provider,
  ProviderDetail,
  ProviderRequest,
  SettingsSchema,
} from '@/lib/types';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Separator } from '@/components/ui/separator';

type FormMode = 'create' | 'edit';

type FormState = {
  name: string;
  adapter: string;
  configValues: Record<string, string>;
  enabled: boolean;
};

const defaultFormState: FormState = {
  name: '',
  adapter: '',
  configValues: {},
  enabled: true,
};

function valueToString(value: unknown): string {
  if (value === null || value === undefined) {
    return '';
  }
  if (typeof value === 'object') {
    return JSON.stringify(value);
  }
  return String(value);
}

function buildConfigValues(
  metadata: AdapterMetadata | undefined,
  existing?: Record<string, unknown>,
): Record<string, string> {
  if (!metadata) {
    return {};
  }
  const values: Record<string, string> = {};
  metadata.settingsSchema.forEach((setting) => {
    if (existing && Object.prototype.hasOwnProperty.call(existing, setting.name)) {
      values[setting.name] = valueToString(existing[setting.name]);
    } else if (setting.default !== undefined && setting.default !== null) {
      values[setting.name] = valueToString(setting.default);
    } else {
      values[setting.name] = '';
    }
  });
  return values;
}

function parseConfigValue(setting: SettingsSchema, raw: string): { value?: unknown; error?: string } {
  const kind = setting.type.toLowerCase();
  const trimmed = raw.trim();

  switch (kind) {
    case 'int':
    case 'integer': {
      const parsed = Number.parseInt(trimmed, 10);
      if (Number.isNaN(parsed)) {
        return { error: `${setting.name} must be an integer` };
      }
      return { value: parsed };
    }
    case 'float':
    case 'double':
    case 'number': {
      const parsed = Number.parseFloat(trimmed);
      if (Number.isNaN(parsed)) {
        return { error: `${setting.name} must be a number` };
      }
      return { value: parsed };
    }
    case 'bool':
    case 'boolean': {
      const normalized = trimmed.toLowerCase();
      if (['true', '1', 'yes', 'on'].includes(normalized)) {
        return { value: true };
      }
      if (['false', '0', 'no', 'off'].includes(normalized)) {
        return { value: false };
      }
      return { error: `${setting.name} must be a boolean` };
    }
    default:
      return { value: raw };
  }
}

function collectConfigPayload(
  metadata: AdapterMetadata,
  configValues: Record<string, string>,
): { config: Record<string, unknown>; error?: string } {
  const config: Record<string, unknown> = {};
  for (const setting of metadata.settingsSchema) {
    const rawValue = configValues[setting.name] ?? '';
    if (rawValue.trim() === '') {
      if (setting.required) {
        return { config: {}, error: `${setting.name} is required` };
      }
      continue;
    }
    const result = parseConfigValue(setting, rawValue);
    if (result.error) {
      return { config: {}, error: result.error };
    }
    config[setting.name] = result.value;
  }
  return { config };
}

export default function ProvidersPage() {
  const [providers, setProviders] = useState<Provider[]>([]);
  const [adapters, setAdapters] = useState<AdapterMetadata[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  const [formOpen, setFormOpen] = useState(false);
  const [formMode, setFormMode] = useState<FormMode>('create');
  const [formState, setFormState] = useState<FormState>(defaultFormState);
  const [formError, setFormError] = useState<string | null>(null);
  const [formLoading, setFormLoading] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  const [detailOpen, setDetailOpen] = useState(false);
  const [detail, setDetail] = useState<ProviderDetail | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState<string | null>(null);

  const [pendingAction, setPendingAction] = useState<string | null>(null);

  const selectedAdapter = useMemo(
    () => adapters.find((adapter) => adapter.identifier === formState.adapter),
    [adapters, formState.adapter],
  );

  useEffect(() => {
    const fetchInitial = async () => {
      setLoading(true);
      setError(null);
      try {
        const [providersResponse, adaptersResponse] = await Promise.all([
          apiClient.getProviders(),
          apiClient.getAdapters(),
        ]);
        setProviders(providersResponse.providers);
        setAdapters(adaptersResponse.adapters);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load providers');
      } finally {
        setLoading(false);
      }
    };

    fetchInitial();
  }, []);

  const refreshProviders = async () => {
    try {
      const response = await apiClient.getProviders();
      setProviders(response.providers);
    } catch (err) {
      setActionError(err instanceof Error ? err.message : 'Failed to refresh providers');
    }
  };

  const resetForm = () => {
    setFormState(defaultFormState);
    setFormError(null);
    setFormLoading(false);
  };

  const handleFormOpenChange = (open: boolean) => {
    setFormOpen(open);
    if (!open) {
      resetForm();
      setFormMode('create');
    }
  };

  const handleCreateClick = () => {
    setFormMode('create');
    resetForm();
    setFormOpen(true);
  };

  const handleAdapterChange = (identifier: string) => {
    const metadata = adapters.find((adapter) => adapter.identifier === identifier);
    setFormState((prev) => ({
      ...prev,
      adapter: identifier,
      configValues: buildConfigValues(metadata),
    }));
  };

  const handleConfigChange = (key: string, value: string) => {
    setFormState((prev) => ({
      ...prev,
      configValues: {
        ...prev.configValues,
        [key]: value,
      },
    }));
  };

  const handleEdit = async (name: string) => {
    setFormMode('edit');
    setFormOpen(true);
    setFormLoading(true);
    setFormError(null);
    try {
      const detailResponse = await apiClient.getProvider(name);
      setFormState({
        name: detailResponse.name,
        adapter: detailResponse.adapter.identifier,
        configValues: buildConfigValues(detailResponse.adapter, detailResponse.settings),
        enabled: detailResponse.running,
      });
    } catch (err) {
      setFormError(err instanceof Error ? err.message : 'Failed to load provider');
    } finally {
      setFormLoading(false);
    }
  };

  const handleDetail = async (name: string) => {
    setDetailOpen(true);
    setDetailLoading(true);
    setDetailError(null);
    setDetail(null);
    try {
      const detailResponse = await apiClient.getProvider(name);
      setDetail(detailResponse);
    } catch (err) {
      setDetailError(err instanceof Error ? err.message : 'Failed to load provider details');
    } finally {
      setDetailLoading(false);
    }
  };

  const handleFormSubmit = async () => {
    setFormError(null);
    const trimmedName = formState.name.trim();
    if (!trimmedName) {
      setFormError('Provider name is required');
      return;
    }
    if (!selectedAdapter) {
      setFormError('Adapter selection is required');
      return;
    }

    const { config, error: configError } = collectConfigPayload(selectedAdapter, formState.configValues);
    if (configError) {
      setFormError(configError);
      return;
    }

    const payload: ProviderRequest = {
      name: trimmedName,
      adapter: {
        identifier: selectedAdapter.identifier,
        config,
      },
      enabled: formState.enabled,
    };

    setSubmitting(true);
    try {
      if (formMode === 'create') {
        await apiClient.createProvider(payload);
      } else {
        await apiClient.updateProvider(trimmedName, payload);
      }
      await refreshProviders();
      handleFormOpenChange(false);
    } catch (err) {
      setFormError(err instanceof Error ? err.message : 'Failed to save provider');
    } finally {
      setSubmitting(false);
    }
  };

  const handleStart = async (name: string) => {
    setActionError(null);
    setPendingAction(name);
    try {
      await apiClient.startProvider(name);
      await refreshProviders();
    } catch (err) {
      setActionError(err instanceof Error ? err.message : `Failed to start ${name}`);
    } finally {
      setPendingAction(null);
    }
  };

  const handleStop = async (name: string) => {
    setActionError(null);
    setPendingAction(name);
    try {
      await apiClient.stopProvider(name);
      await refreshProviders();
    } catch (err) {
      setActionError(err instanceof Error ? err.message : `Failed to stop ${name}`);
    } finally {
      setPendingAction(null);
    }
  };

  const handleDelete = async (name: string) => {
    if (typeof window !== 'undefined') {
      const confirmed = window.confirm(`Delete provider ${name}?`);
      if (!confirmed) {
        return;
      }
    }
    setActionError(null);
    setPendingAction(name);
    try {
      await apiClient.deleteProvider(name);
      await refreshProviders();
    } catch (err) {
      setActionError(err instanceof Error ? err.message : `Failed to delete ${name}`);
    } finally {
      setPendingAction(null);
    }
  };

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
      <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Providers</h1>
          <p className="text-muted-foreground">
            Manage exchange provider lifecycles and configuration
          </p>
        </div>
        <Button onClick={handleCreateClick}>Create provider</Button>
      </div>

      {actionError && (
        <Alert variant="destructive">
          <AlertDescription>{actionError}</AlertDescription>
        </Alert>
      )}

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        {providers.map((provider) => {
          const isPending = pendingAction === provider.name;
          return (
            <Card key={provider.name}>
              <CardHeader>
                <div className="flex items-start justify-between gap-4">
                  <div>
                    <CardTitle>{provider.name}</CardTitle>
                    <CardDescription>{provider.adapter}</CardDescription>
                  </div>
                  <Badge variant={provider.running ? 'default' : 'secondary'}>
                    {provider.running ? 'Running' : 'Stopped'}
                  </Badge>
                </div>
              </CardHeader>
              <CardContent className="space-y-4 text-sm">
                <div className="space-y-1 text-muted-foreground">
                  <div>
                    <span className="font-medium text-foreground">Identifier:</span>{' '}
                    {provider.identifier}
                  </div>
                  <div>
                    <span className="font-medium text-foreground">Instruments:</span>{' '}
                    {provider.instrumentCount}
                  </div>
                </div>
                <div className="flex flex-wrap gap-2">
                  <Button variant="outline" size="sm" onClick={() => handleDetail(provider.name)}>
                    Details
                  </Button>
                  <Button variant="default" size="sm" onClick={() => handleEdit(provider.name)}>
                    Edit
                  </Button>
                  {provider.running ? (
                    <Button
                      variant="secondary"
                      size="sm"
                      disabled={isPending}
                      onClick={() => handleStop(provider.name)}
                    >
                      {isPending ? 'Stopping…' : 'Stop'}
                    </Button>
                  ) : (
                    <Button
                      variant="secondary"
                      size="sm"
                      disabled={isPending}
                      onClick={() => handleStart(provider.name)}
                    >
                      {isPending ? 'Starting…' : 'Start'}
                    </Button>
                  )}
                  <Button
                    variant="destructive"
                    size="sm"
                    disabled={isPending}
                    onClick={() => handleDelete(provider.name)}
                  >
                    {isPending ? 'Removing…' : 'Delete'}
                  </Button>
                </div>
              </CardContent>
            </Card>
          );
        })}
        {providers.length === 0 && (
          <div className="col-span-full text-muted-foreground">
            No providers configured yet. Create one to begin streaming market data.
          </div>
        )}
      </div>

      <Dialog open={formOpen} onOpenChange={handleFormOpenChange}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {formMode === 'create' ? 'Create provider' : `Edit provider ${formState.name}`}
            </DialogTitle>
            <DialogDescription>
              Configure adapter credentials and settings for this provider instance.
            </DialogDescription>
          </DialogHeader>

          {formError && (
            <Alert variant="destructive">
              <AlertDescription>{formError}</AlertDescription>
            </Alert>
          )}

          {formLoading ? (
            <div>Loading provider…</div>
          ) : (
            <div className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="provider-name">Name</Label>
                <Input
                  id="provider-name"
                  value={formState.name}
                  onChange={(event) =>
                    setFormState((prev) => ({ ...prev, name: event.target.value }))
                  }
                  placeholder="binance-spot"
                  disabled={formMode === 'edit'}
                />
              </div>

              <div className="space-y-2">
                <Label>Adapter</Label>
                <Select
                  value={formState.adapter}
                  onValueChange={handleAdapterChange}
                  disabled={adapters.length === 0}
                >
                  <SelectTrigger>
                    <SelectValue placeholder="Select adapter" />
                  </SelectTrigger>
                  <SelectContent>
                    {adapters.map((adapter) => (
                      <SelectItem key={adapter.identifier} value={adapter.identifier}>
                        {adapter.displayName}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              {selectedAdapter ? (
                <div className="space-y-4">
                  <div className="space-y-1 text-sm text-muted-foreground">
                    Provide adapter-specific settings. Leave optional fields blank to use defaults.
                  </div>
                  {selectedAdapter.settingsSchema.map((setting) => (
                    <div key={setting.name} className="space-y-2">
                      <Label htmlFor={`setting-${setting.name}`}>
                        {setting.name}
                        {setting.required && <span className="text-red-500">*</span>}
                      </Label>
                      <Input
                        id={`setting-${setting.name}`}
                        type={['int', 'integer', 'float', 'double', 'number'].includes(setting.type.toLowerCase()) ? 'number' : 'text'}
                        value={formState.configValues[setting.name] ?? ''}
                        onChange={(event) => handleConfigChange(setting.name, event.target.value)}
                        placeholder=
                          {setting.default !== undefined && setting.default !== null
                            ? `Default: ${valueToString(setting.default)}`
                            : undefined}
                      />
                      <p className="text-xs text-muted-foreground">Type: {setting.type}</p>
                    </div>
                  ))}
                </div>
              ) : (
                <div className="text-sm text-muted-foreground">
                  Select an adapter to configure its settings.
                </div>
              )}

              <div className="space-y-2">
                <Label>Start provider after saving</Label>
                <Select
                  value={formState.enabled ? 'true' : 'false'}
                  onValueChange={(value) =>
                    setFormState((prev) => ({ ...prev, enabled: value === 'true' }))
                  }
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="true">Enabled</SelectItem>
                    <SelectItem value="false">Disabled</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>
          )}

          <DialogFooter>
            <Button variant="outline" onClick={() => handleFormOpenChange(false)} disabled={submitting}>
              Cancel
            </Button>
            <Button onClick={handleFormSubmit} disabled={submitting || formLoading}>
              {submitting ? 'Saving…' : formMode === 'create' ? 'Create provider' : 'Save changes'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={detailOpen} onOpenChange={setDetailOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Provider details</DialogTitle>
            <DialogDescription>Inspect adapter configuration and subscribed instruments.</DialogDescription>
          </DialogHeader>

          {detailError && (
            <Alert variant="destructive">
              <AlertDescription>{detailError}</AlertDescription>
            </Alert>
          )}

          {detailLoading ? (
            <div>Loading provider…</div>
          ) : detail ? (
            <div className="space-y-4 text-sm">
              <div>
                <p className="font-medium text-foreground">Adapter</p>
                <p className="text-muted-foreground">
                  {detail.adapter.displayName} ({detail.adapter.identifier})
                </p>
              </div>

              <Separator />

              <div>
                <p className="font-medium text-foreground">Settings</p>
                {Object.keys(detail.settings).length === 0 ? (
                  <p className="text-muted-foreground">No adapter settings configured.</p>
                ) : (
                  <div className="space-y-1 text-muted-foreground">
                    {Object.entries(detail.settings).map(([key, value]) => (
                      <div key={key}>
                        <span className="font-medium text-foreground">{key}:</span>{' '}
                        {valueToString(value)}
                      </div>
                    ))}
                  </div>
                )}
              </div>

              <Separator />

              <div>
                <p className="font-medium text-foreground">
                  Instruments ({detail.instruments.length})
                </p>
                {detail.instruments.length === 0 ? (
                  <p className="text-muted-foreground">No instruments registered.</p>
                ) : (
                  <div className="max-h-48 overflow-y-auto space-y-1 text-muted-foreground">
                    {detail.instruments.map((instrument) => (
                      <div key={instrument.symbol}>
                        <span className="font-medium text-foreground">{instrument.symbol}</span>{' '}
                        ({instrument.baseAsset}/{instrument.quoteAsset})
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </div>
          ) : (
            <div className="text-muted-foreground">Select a provider to view details.</div>
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}
