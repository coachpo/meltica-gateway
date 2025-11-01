'use client';

import { ChangeEvent, useCallback, useEffect, useMemo, useState } from 'react';
import { apiClient } from '@/lib/api-client';
import type {
  AdapterMetadata,
  Instrument,
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
import { cn } from '@/lib/utils';
import { Checkbox } from '@/components/ui/checkbox';
import { useToast } from '@/components/ui/toast-provider';

const INSTRUMENTS_PAGE_SIZE = 120;

function instrumentBaseValue(instrument: Instrument): string {
  return instrument.baseAsset ?? instrument.baseCurrency ?? '';
}

function instrumentQuoteValue(instrument: Instrument): string {
  return instrument.quoteAsset ?? instrument.quoteCurrency ?? '';
}

function formatInstrumentMetric(value: unknown): string {
  if (value === undefined || value === null) {
    return '—';
  }
  if (typeof value === 'string') {
    const trimmed = value.trim();
    return trimmed === '' ? '—' : trimmed;
  }
  return String(value);
}

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
  enabled: false,
};

const MASKED_SECRET_PLACEHOLDER = '••••••';

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
    const normalized = rawValue.trim();
    if (normalized === '' || normalized === MASKED_SECRET_PLACEHOLDER) {
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

const SENSITIVE_SETTING_FRAGMENTS = [
  'secret',
  'passphrase',
  'apikey',
  'wsapikey',
  'wssecret',
  'privatekey',
  'privkey',
  'token',
  'password',
  'clientsecret',
  'accesskey',
  'access_token',
];

const SETTING_NORMALIZER = /[-_\s]/g;

function isSensitiveSettingKey(key: string): boolean {
  const normalized = key.trim().toLowerCase().replace(SETTING_NORMALIZER, '');
  if (!normalized) {
    return false;
  }
  return SENSITIVE_SETTING_FRAGMENTS.some((fragment) =>
    normalized.includes(fragment.replace(SETTING_NORMALIZER, '')),
  );
}

function maskSettingsValue(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map((entry) => maskSettingsValue(entry));
  }
  if (value && typeof value === 'object') {
    return maskProviderSettingsForDisplay(value as Record<string, unknown>);
  }
  return value;
}

function maskProviderSettingsForDisplay(settings: Record<string, unknown> | undefined): Record<string, unknown> {
  if (!settings) {
    return {};
  }
  const masked: Record<string, unknown> = {};
  Object.entries(settings).forEach(([key, value]) => {
    if (isSensitiveSettingKey(key)) {
      masked[key] = '••••••';
      return;
    }
    masked[key] = maskSettingsValue(value);
  });
  return masked;
}

function maskProviderDetail(detail: ProviderDetail): ProviderDetail {
  const instruments = Array.isArray(detail.instruments) ? detail.instruments : [];
  return {
    ...detail,
    instruments,
    settings: maskProviderSettingsForDisplay(detail.settings),
  };
}

export default function ProvidersPage() {
  const [providers, setProviders] = useState<Provider[]>([]);
  const [adapters, setAdapters] = useState<AdapterMetadata[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

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
  const [selectedInstrument, setSelectedInstrument] = useState<Instrument | null>(null);
  const [instrumentQuery, setInstrumentQuery] = useState('');
  const [instrumentPage, setInstrumentPage] = useState(0);

  type ProviderActionType = 'start' | 'stop' | 'delete';
  type ProviderActionState = Record<ProviderActionType, string | null>;
  const [pendingActions, setPendingActions] = useState<ProviderActionState>({
    start: null,
    stop: null,
    delete: null,
  });
  const setPending = useCallback(
    (type: ProviderActionType, name: string | null) => {
      setPendingActions((prev) => {
        if (prev[type] === name) {
          return prev;
        }
        return { ...prev, [type]: name };
      });
    },
    [],
  );
  const { show: showToast } = useToast();

  const selectedAdapter = useMemo(
    () => adapters.find((adapter) => adapter.identifier === formState.adapter),
    [adapters, formState.adapter],
  );

  const adapterByIdentifier = useMemo(() => {
    const map = new Map<string, AdapterMetadata>();
    adapters.forEach((adapter) => {
      map.set(adapter.identifier, adapter);
    });
    return map;
  }, [adapters]);

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

  useEffect(() => {
    if (!detailOpen) {
      setSelectedInstrument(null);
      setInstrumentQuery('');
      setInstrumentPage(0);
    }
  }, [detailOpen]);

  useEffect(() => {
    if (!detail) {
      setSelectedInstrument(null);
      setInstrumentQuery('');
      setInstrumentPage(0);
      return;
    }
    setInstrumentQuery('');
    setInstrumentPage(0);
    setSelectedInstrument(null);
  }, [detail]);

  const filteredInstruments = useMemo(() => {
    if (!detail) {
      return [] as Instrument[];
    }
    const query = instrumentQuery.trim().toLowerCase();
    if (!query) {
      return detail.instruments;
    }
    return detail.instruments.filter((instrument) => {
      const symbol = instrument.symbol?.toLowerCase() ?? '';
      const base = instrumentBaseValue(instrument).toLowerCase();
      const quote = instrumentQuoteValue(instrument).toLowerCase();
      return (
        symbol.includes(query) || base.includes(query) || quote.includes(query)
      );
    });
  }, [detail, instrumentQuery]);

  useEffect(() => {
    setInstrumentPage(0);
  }, [instrumentQuery]);

  useEffect(() => {
    const total = filteredInstruments.length;
    if (total === 0) {
      if (instrumentPage !== 0) {
        setInstrumentPage(0);
      }
      return;
    }
    const maxPage = Math.max(0, Math.ceil(total / INSTRUMENTS_PAGE_SIZE) - 1);
    if (instrumentPage > maxPage) {
      setInstrumentPage(maxPage);
    }
  }, [filteredInstruments.length, instrumentPage]);

  useEffect(() => {
    if (!filteredInstruments.length) {
      if (selectedInstrument !== null) {
        setSelectedInstrument(null);
      }
      return;
    }
    const maxPage = Math.max(0, Math.ceil(filteredInstruments.length / INSTRUMENTS_PAGE_SIZE) - 1);
    const currentPage = Math.min(instrumentPage, maxPage);
    const start = currentPage * INSTRUMENTS_PAGE_SIZE;
    const currentSlice = filteredInstruments.slice(start, start + INSTRUMENTS_PAGE_SIZE);
    if (!currentSlice.length) {
      return;
    }
    if (!selectedInstrument) {
      setSelectedInstrument(currentSlice[0]);
      return;
    }
    const match = currentSlice.find((instrument) => instrument.symbol === selectedInstrument.symbol);
    if (match) {
      if (match !== selectedInstrument) {
        setSelectedInstrument(match);
      }
      return;
    }
    setSelectedInstrument(currentSlice[0]);
  }, [filteredInstruments, instrumentPage, selectedInstrument]);

  const totalInstrumentCount = filteredInstruments.length;
  const totalInstrumentPages = totalInstrumentCount === 0 ? 0 : Math.ceil(totalInstrumentCount / INSTRUMENTS_PAGE_SIZE);
  const effectiveInstrumentPage = totalInstrumentPages === 0 ? 0 : Math.min(instrumentPage, totalInstrumentPages - 1);
  const pageStart = effectiveInstrumentPage * INSTRUMENTS_PAGE_SIZE;
  const currentPageInstruments = filteredInstruments.slice(
    pageStart,
    pageStart + INSTRUMENTS_PAGE_SIZE,
  );
  const pageDisplayStart = totalInstrumentCount === 0 ? 0 : pageStart + 1;
  const pageDisplayEnd = totalInstrumentCount === 0 ? 0 : pageStart + currentPageInstruments.length;

  const selectedInstrumentDisplay = useMemo(() => {
    if (!selectedInstrument) {
      return null;
    }
    return {
      base: instrumentBaseValue(selectedInstrument) || '—',
      quote: instrumentQuoteValue(selectedInstrument) || '—',
      type: selectedInstrument.type ?? '—',
      pricePrecision: formatInstrumentMetric(selectedInstrument.pricePrecision),
      quantityPrecision: formatInstrumentMetric(selectedInstrument.quantityPrecision),
      priceIncrement: formatInstrumentMetric(selectedInstrument.priceIncrement),
      quantityIncrement: formatInstrumentMetric(selectedInstrument.quantityIncrement),
      minQuantity: formatInstrumentMetric(selectedInstrument.minQuantity),
      maxQuantity: formatInstrumentMetric(selectedInstrument.maxQuantity),
      notionalPrecision: formatInstrumentMetric(selectedInstrument.notionalPrecision),
    };
  }, [selectedInstrument]);

  const refreshProviders = async () => {
    try {
      const response = await apiClient.getProviders();
      setProviders(response.providers);
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to refresh providers';
      showToast({
        title: 'Provider refresh failed',
        description: message,
        variant: 'destructive',
      });
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
      setDetail(maskProviderDetail(detailResponse));
    } catch (err) {
      setDetailError(err instanceof Error ? err.message : 'Failed to load provider details');
    } finally {
      setDetailLoading(false);
    }
  };

  const handleFormSubmit = async () => {
    setFormError(null);
    const mode = formMode;
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
      if (mode === 'create') {
        await apiClient.createProvider(payload);
      } else {
        await apiClient.updateProvider(trimmedName, payload);
      }
      await refreshProviders();
      showToast({
        title: mode === 'create' ? 'Provider created' : 'Provider updated',
        description: `Provider ${trimmedName} ${mode === 'create' ? 'created' : 'updated'} successfully.`,
        variant: 'success',
      });
      handleFormOpenChange(false);
    } catch (err) {
      setFormError(err instanceof Error ? err.message : 'Failed to save provider');
    } finally {
      setSubmitting(false);
    }
  };

  const handleStart = async (name: string) => {
    setPending('start', name);
    try {
      await apiClient.startProvider(name);
      await refreshProviders();
      showToast({
        title: 'Provider started',
        description: `${name} is now running.`,
        variant: 'success',
      });
    } catch (err) {
      const message = err instanceof Error ? err.message : `Failed to start ${name}`;
      showToast({
        title: 'Start failed',
        description: message,
        variant: 'destructive',
      });
    } finally {
      setPending('start', null);
    }
  };

  const handleStop = async (name: string) => {
    setPending('stop', name);
    try {
      await apiClient.stopProvider(name);
      await refreshProviders();
      showToast({
        title: 'Provider stopped',
        description: `${name} is now stopped.`,
        variant: 'success',
      });
    } catch (err) {
      const message = err instanceof Error ? err.message : `Failed to stop ${name}`;
      showToast({
        title: 'Stop failed',
        description: message,
        variant: 'destructive',
      });
    } finally {
      setPending('stop', null);
    }
  };

  const handleDelete = async (name: string) => {
    if (typeof window !== 'undefined') {
      const confirmed = window.confirm(`Delete provider ${name}?`);
      if (!confirmed) {
        return;
      }
    }
    setPending('delete', name);
    try {
      await apiClient.deleteProvider(name);
      await refreshProviders();
      showToast({
        title: 'Provider deleted',
        description: `${name} has been removed.`,
        variant: 'success',
      });
    } catch (err) {
      const message = err instanceof Error ? err.message : `Failed to delete ${name}`;
      showToast({
        title: 'Delete failed',
        description: message,
        variant: 'destructive',
      });
    } finally {
      setPending('delete', null);
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

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        {providers.map((provider) => {
          const isStartPending = pendingActions.start === provider.name;
          const isStopPending = pendingActions.stop === provider.name;
          const isDeletePending = pendingActions.delete === provider.name;
          const disableActions = isStartPending || isStopPending || isDeletePending;
          const dependentInstances = provider.dependentInstances ?? [];
          const dependentCount = provider.dependentInstanceCount ?? dependentInstances.length;
          const deleteDisabled =
            dependentCount > 0 || isDeletePending || isStartPending || isStopPending;
          const adapterMeta = adapterByIdentifier.get(provider.adapter);
          return (
            <Card key={provider.name}>
              <CardHeader>
                <div className="flex items-start justify-between gap-4">
                  <div>
                    <CardTitle>{provider.name}</CardTitle>
                    <CardDescription>
                      {adapterMeta
                        ? `${adapterMeta.displayName} (${adapterMeta.identifier})`
                        : provider.adapter}
                    </CardDescription>
                    {adapterMeta?.description && (
                      <p className="mt-1 text-xs text-muted-foreground">
                        {adapterMeta.description}
                      </p>
                    )}
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
                  <div>
                    <span className="font-medium text-foreground">In use by:</span>{' '}
                    {dependentCount === 0 ? (
                      <span className="text-muted-foreground">No instances</span>
                    ) : (
                      <span className="text-muted-foreground">
                        {dependentCount}{' '}
                        {dependentCount === 1 ? 'instance' : 'instances'}
                      </span>
                    )}
                  </div>
                </div>
                {dependentInstances.length > 0 && (
                  <div className="text-xs text-muted-foreground">
                    {dependentCount === 1 ? 'Instance:' : 'Instances:'}{' '}
                    {dependentInstances.join(', ')}
                  </div>
                )}
                <div className="flex flex-wrap gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => handleDetail(provider.name)}
                  >
                    Details
                  </Button>
                  <Button
                    variant="default"
                    size="sm"
                    onClick={() => handleEdit(provider.name)}
                  >
                    Edit
                  </Button>
                  {provider.running ? (
                    <Button
                      variant="secondary"
                      size="sm"
                      disabled={disableActions}
                      onClick={() => handleStop(provider.name)}
                    >
                      {isStopPending ? 'Stopping…' : 'Stop'}
                    </Button>
                  ) : (
                    <Button
                      variant="secondary"
                      size="sm"
                      disabled={disableActions}
                      onClick={() => handleStart(provider.name)}
                    >
                      {isStartPending ? 'Starting…' : 'Start'}
                    </Button>
                  )}
                  <Button
                    variant="destructive"
                    size="sm"
                    disabled={deleteDisabled}
                    onClick={() => handleDelete(provider.name)}
                  >
                    {isDeletePending ? 'Removing…' : 'Delete'}
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
        <DialogContent className="max-w-2xl sm:max-w-3xl sm:max-h-[85vh] flex flex-col">
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

          <div className="flex-1 overflow-y-auto pr-1">
            {formLoading ? (
              <div className="py-10 text-sm text-muted-foreground">Loading provider…</div>
            ) : (
              <div className="space-y-4 pb-4">
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
                          placeholder={
                            setting.default !== undefined && setting.default !== null
                              ? `Default: ${valueToString(setting.default)}`
                              : undefined
                          }
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

                <div className="rounded-md border p-3">
                  <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                    <div>
                      <p className="text-sm font-medium text-foreground">Start provider immediately</p>
                      <p className="text-xs text-muted-foreground">
                        Providers are created stopped by default. Enable this toggle to launch the provider as soon as it is saved.
                      </p>
                    </div>
                    <label className="flex items-center gap-2 text-sm font-medium text-foreground">
                      <Checkbox
                        checked={formState.enabled}
                        onChange={(event: ChangeEvent<HTMLInputElement>) =>
                          setFormState((prev) => ({ ...prev, enabled: event.target.checked }))
                        }
                      />
                      <span>Start immediately</span>
                    </label>
                  </div>
                </div>
              </div>
            )}
          </div>

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
        <DialogContent className="max-w-3xl sm:max-h-[85vh] flex flex-col">
          <DialogHeader>
            <DialogTitle>Provider details</DialogTitle>
            <DialogDescription>Inspect adapter configuration and subscribed instruments.</DialogDescription>
          </DialogHeader>

          {detailError && (
            <Alert variant="destructive">
              <AlertDescription>{detailError}</AlertDescription>
            </Alert>
          )}

          <div className="flex-1 overflow-y-auto pr-1">
          {detailLoading ? (
            <div>Loading provider…</div>
          ) : detail ? (
            <div className="space-y-4 text-sm">
              {(() => {
                const dependentInstances = detail.dependentInstances ?? [];
                const dependentCount = detail.dependentInstanceCount ?? dependentInstances.length;
                return (
                  <>
                    <div>
                      <p className="font-medium text-foreground">Adapter</p>
                      <p className="text-muted-foreground">
                        {detail.adapter.displayName} ({detail.adapter.identifier})
                      </p>
                      {detail.adapter.description && (
                        <p className="mt-1 text-sm text-muted-foreground">
                          {detail.adapter.description}
                        </p>
                      )}
                    </div>

                    <Separator />

                    <div>
                      <p className="font-medium text-foreground">Dependent instances</p>
                      {dependentCount === 0 ? (
                        <p className="text-muted-foreground">No instances depend on this provider.</p>
                      ) : (
                        <div className="space-y-1 text-muted-foreground">
                          <p>
                            {dependentCount}{' '}
                            {dependentCount === 1 ? 'instance' : 'instances'} requiring this provider
                          </p>
                          <ul className="list-disc space-y-1 pl-5">
                            {dependentInstances.map((instance) => (
                              <li key={instance}>{instance}</li>
                            ))}
                          </ul>
                        </div>
                      )}
                    </div>

                    <Separator />
                  </>
                );
              })()}
              <div>
                <p className="font-medium text-foreground">Settings</p>
                <p className="text-xs text-muted-foreground">
                  Sensitive values are masked and must be re-entered when editing.
                </p>
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
                  Instruments (
                  {filteredInstruments.length}
                  {detail.instruments.length !== filteredInstruments.length
                    ? ` of ${detail.instruments.length}`
                    : ''}
                  )
                </p>
                {detail.instruments.length === 0 ? (
                  <p className="text-muted-foreground">No instruments registered.</p>
                ) : (
                  <>
                    <Input
                      value={instrumentQuery}
                      onChange={(event) => setInstrumentQuery(event.target.value)}
                      placeholder="Search symbol or asset"
                      className="mt-2"
                    />
                    {filteredInstruments.length === 0 ? (
                      <p className="mt-2 text-sm text-muted-foreground">
                        No instruments match your filter.
                      </p>
                    ) : (
                      <>
                        <div className="max-h-56 overflow-y-auto space-y-1">
                          {currentPageInstruments.map((instrument) => {
                            const isSelected = selectedInstrument?.symbol === instrument.symbol;
                            const baseLabel = instrumentBaseValue(instrument) || '—';
                            const quoteLabel = instrumentQuoteValue(instrument) || '—';
                            return (
                              <button
                                key={instrument.symbol}
                                type="button"
                                onClick={() => setSelectedInstrument(instrument)}
                                className={cn(
                                  'w-full rounded-md px-2 py-1 text-left text-sm transition-colors',
                                  isSelected
                                    ? 'bg-primary/10 text-primary'
                                    : 'text-muted-foreground hover:bg-muted'
                                )}
                              >
                                <span className="font-medium">{instrument.symbol ?? '—'}</span>{' '}
                                ({baseLabel}/{quoteLabel})
                              </button>
                            );
                          })}
                        </div>
                        {totalInstrumentPages > 1 && (
                          <div className="flex flex-col gap-2 pt-2 text-xs text-muted-foreground sm:flex-row sm:items-center sm:justify-between">
                            <span>
                              Showing {pageDisplayStart.toLocaleString()}–
                              {pageDisplayEnd.toLocaleString()} of {totalInstrumentCount.toLocaleString()}
                            </span>
                            <div className="flex items-center gap-2">
                              <Button
                                variant="outline"
                                size="sm"
                                onClick={() =>
                                  setInstrumentPage((prev) => Math.max(prev - 1, 0))
                                }
                                disabled={effectiveInstrumentPage === 0}
                              >
                                Previous
                              </Button>
                              <Button
                                variant="outline"
                                size="sm"
                                onClick={() =>
                                  setInstrumentPage((prev) =>
                                    totalInstrumentPages === 0
                                      ? 0
                                      : Math.min(prev + 1, totalInstrumentPages - 1),
                                  )
                                }
                                disabled={
                                  totalInstrumentPages === 0 ||
                                  effectiveInstrumentPage >= totalInstrumentPages - 1
                                }
                              >
                                Next
                              </Button>
                            </div>
                          </div>
                        )}
                      </>
                    )}
                  </>
                )}
              </div>

              {selectedInstrument && selectedInstrumentDisplay && (
                <div className="space-y-2 rounded-lg border bg-muted/30 p-4 text-sm">
                  <p className="font-medium text-foreground">Instrument details</p>
                  <div className="grid gap-1 text-muted-foreground">
                    <div className="flex justify-between">
                      <span className="font-medium text-foreground">Symbol</span>
                      <span>{selectedInstrument.symbol ?? '—'}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="font-medium text-foreground">Type</span>
                      <span>{selectedInstrumentDisplay.type}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="font-medium text-foreground">Base asset</span>
                      <span>{selectedInstrumentDisplay.base}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="font-medium text-foreground">Quote asset</span>
                      <span>{selectedInstrumentDisplay.quote}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="font-medium text-foreground">Price precision</span>
                      <span>{selectedInstrumentDisplay.pricePrecision}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="font-medium text-foreground">Quantity precision</span>
                      <span>{selectedInstrumentDisplay.quantityPrecision}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="font-medium text-foreground">Price increment</span>
                      <span>{selectedInstrumentDisplay.priceIncrement}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="font-medium text-foreground">Quantity increment</span>
                      <span>{selectedInstrumentDisplay.quantityIncrement}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="font-medium text-foreground">Min quantity</span>
                      <span>{selectedInstrumentDisplay.minQuantity}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="font-medium text-foreground">Max quantity</span>
                      <span>{selectedInstrumentDisplay.maxQuantity}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="font-medium text-foreground">Notional precision</span>
                      <span>{selectedInstrumentDisplay.notionalPrecision}</span>
                    </div>
                  </div>
                </div>
              )}
            </div>
          ) : (
            <div className="text-muted-foreground">Select a provider to view details.</div>
          )}
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
