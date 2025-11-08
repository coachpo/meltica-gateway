'use client';

import { ChangeEvent, useCallback, useEffect, useMemo, useState } from 'react';
import type {
  AdapterMetadata,
  Instrument,
  InstanceSummary,
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
import { ScrollArea } from '@/components/ui/scroll-area';
import { Separator } from '@/components/ui/separator';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { Loader2 } from 'lucide-react';
import { cn } from '@/lib/utils';
import { Checkbox } from '@/components/ui/checkbox';
import { ConfirmDialog } from '@/components/confirm-dialog';
import {
  useAdaptersQuery,
  useCreateProviderMutation,
  useDeleteProviderMutation,
  useInstancesQuery,
  useProviderBalancesQuery,
  useProviderQuery,
  useProvidersQuery,
  useStartProviderMutation,
  useStopProviderMutation,
  useUpdateProviderMutation,
} from '@/lib/hooks';

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

function buildInstanceIndex(instances: InstanceSummary[] = []): Record<string, InstanceSummary> {
  return instances.reduce<Record<string, InstanceSummary>>((acc, instance) => {
    acc[instance.id] = instance;
    return acc;
  }, {});
}

function partitionDependents(
  ids: string[],
  index: Record<string, InstanceSummary>,
): { visible: string[]; hidden: string[] } {
  const visible: string[] = [];
  const hidden: string[] = [];
  ids.forEach((id) => {
    const summary = index[id];
    if (summary && summary.baseline && !summary.dynamic) {
      hidden.push(id);
    } else {
      visible.push(id);
    }
  });
  return { visible, hidden };
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

const DEFAULT_PRIVATE_NOTE =
  'Leave blank to disable private subscriptions such as balances and execution reports.';

const BALANCE_LIMIT_OPTIONS = [25, 50, 100, 200];
const DEFAULT_BALANCE_LIMIT = 50;

type AuthFieldHints = Record<
  string,
  {
    label?: string;
    note?: string;
  }
>;

const AUTH_FIELD_HINTS: Record<string, AuthFieldHints> = {
  binance: {
    api_key: {
      label: 'API key (optional)',
      note: 'Provide both API key and secret to enable private subscriptions (balances, execution reports).',
    },
    api_secret: {
      label: 'API secret (optional)',
      note: 'Provide both API key and secret to enable private subscriptions (balances, execution reports).',
    },
  },
  okx: {
    api_key: {
      label: 'API key (optional)',
      note: 'Provide API key, secret, and passphrase to enable private subscriptions (balances, execution reports).',
    },
    api_secret: {
      label: 'API secret (optional)',
      note: 'Provide API key, secret, and passphrase to enable private subscriptions (balances, execution reports).',
    },
    passphrase: {
      label: 'API passphrase (optional)',
      note: 'Provide API key, secret, and passphrase to enable private subscriptions (balances, execution reports).',
    },
  },
};

const AUTH_SETTING_LABELS: Record<string, string> = {
  api_key: 'API key (optional)',
  api_secret: 'API secret (optional)',
  passphrase: 'API passphrase (optional)',
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

function resolveErrorMessage(error: unknown, fallback: string): string {
  if (error instanceof Error) {
    return error.message;
  }
  if (typeof error === 'string' && error.trim()) {
    return error.trim();
  }
  return fallback;
}

function formatTimestamp(value?: number | string | null): string {
  if (value === undefined || value === null || value === '') {
    return '—';
  }
  const timestamp = typeof value === 'number' ? value : Number(value);
  const date = Number.isFinite(timestamp) ? new Date(timestamp) : new Date(value as string);
  if (Number.isNaN(date.getTime())) {
    return '—';
  }
  return new Intl.DateTimeFormat(undefined, {
    year: 'numeric',
    month: 'short',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  }).format(date);
}

export default function ProvidersPage() {
  const providersQuery = useProvidersQuery();
  const adaptersQuery = useAdaptersQuery();
  const instancesQuery = useInstancesQuery();
  const providers = providersQuery.data ?? [];
  const adapters = useMemo(() => adaptersQuery.data ?? [], [adaptersQuery.data]);
  const instanceIndex = useMemo(
    () => buildInstanceIndex(instancesQuery.data ?? []),
    [instancesQuery.data],
  );
  const loading = providersQuery.isLoading || adaptersQuery.isLoading || instancesQuery.isLoading;
  const queryError =
    (providersQuery.error as Error | null) ??
    (adaptersQuery.error as Error | null) ??
    (instancesQuery.error as Error | null);

  const [formOpen, setFormOpen] = useState(false);
  const [formMode, setFormMode] = useState<FormMode>('create');
  const [formState, setFormState] = useState<FormState>(defaultFormState);
  const [formError, setFormError] = useState<string | null>(null);
  const [formTargetProvider, setFormTargetProvider] = useState<string | null>(null);
  const [formPrefilledProvider, setFormPrefilledProvider] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const [detailOpen, setDetailOpen] = useState(false);
  const [detailTarget, setDetailTarget] = useState<string | null>(null);
  const [detailTab, setDetailTab] = useState<'overview' | 'balances'>('overview');
  const [balanceLimit, setBalanceLimit] = useState(DEFAULT_BALANCE_LIMIT);
  const [balanceAssetFilter, setBalanceAssetFilter] = useState('');
  const [selectedInstrument, setSelectedInstrument] = useState<Instrument | null>(null);
  const [instrumentQuery, setInstrumentQuery] = useState('');
  const [instrumentPage, setInstrumentPage] = useState(0);
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null);

  const editProviderQuery = useProviderQuery(
    formTargetProvider ?? undefined,
    Boolean(formOpen && formMode === 'edit' && formTargetProvider),
  );

  const detailProviderQuery = useProviderQuery(
    detailTarget ?? undefined,
    Boolean(detailOpen && detailTarget),
  );

  const detail = useMemo(() => {
    if (!detailProviderQuery.data) {
      return null;
    }
    return maskProviderDetail(detailProviderQuery.data);
  }, [detailProviderQuery.data]);

  const detailLoading = Boolean(detailOpen && detailTarget && detailProviderQuery.isLoading);
  const detailError = detailProviderQuery.error && detailOpen && detailTarget
    ? resolveErrorMessage(detailProviderQuery.error, 'Failed to load provider details')
    : null;
  const formLoading = Boolean(formOpen && formMode === 'edit' && formTargetProvider && editProviderQuery.isLoading);

  const providerBalancesQuery = useProviderBalancesQuery(
    detailTarget ?? undefined,
    {
      limit: balanceLimit,
      asset: balanceAssetFilter.trim() || undefined,
    },
    Boolean(detailOpen && detailTab === 'balances' && detailTarget),
  );
  const balanceRows = providerBalancesQuery.data?.balances ?? [];
  const balanceCount = providerBalancesQuery.data?.count ?? balanceRows.length;
  const balanceLoading = providerBalancesQuery.isLoading;
  const balanceError =
    detailTab === 'balances' && providerBalancesQuery.error
      ? resolveErrorMessage(providerBalancesQuery.error, 'Failed to load balances')
      : null;

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
  const createProviderMutation = useCreateProviderMutation();
  const updateProviderMutation = useUpdateProviderMutation();
  const deleteProviderMutation = useDeleteProviderMutation();
  const startProviderMutation = useStartProviderMutation();
  const stopProviderMutation = useStopProviderMutation();

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

  const resetForm = () => {
    setFormState(defaultFormState);
    setFormError(null);
    setFormTargetProvider(null);
    setFormPrefilledProvider(null);
  };

  const handleFormOpenChange = (open: boolean) => {
    setFormOpen(open);
    if (!open) {
      resetForm();
      setFormMode('create');
    }
  };

  const handleDetailOpenChange = (open: boolean) => {
    setDetailOpen(open);
    if (!open) {
      setDetailTarget(null);
      setDetailTab('overview');
      setBalanceAssetFilter('');
      setBalanceLimit(DEFAULT_BALANCE_LIMIT);
      setSelectedInstrument(null);
      setInstrumentQuery('');
      setInstrumentPage(0);
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

  const handleEdit = (name: string) => {
    setFormMode('edit');
    setFormError(null);
    setFormPrefilledProvider(null);
    setFormTargetProvider(name);
    setFormState(() => ({
      ...defaultFormState,
      name,
    }));
    setFormOpen(true);
  };

  const handleDetail = (name: string) => {
    setDetailTarget(name);
    setDetailTab('overview');
    setBalanceAssetFilter('');
    setBalanceLimit(DEFAULT_BALANCE_LIMIT);
    setSelectedInstrument(null);
    setInstrumentQuery('');
    setInstrumentPage(0);
    setDetailOpen(true);
  };

  useEffect(() => {
    if (!formOpen || formMode !== 'edit' || !formTargetProvider) {
      return;
    }
    if (!editProviderQuery.data || formPrefilledProvider === formTargetProvider) {
      return;
    }
    const detailResponse = editProviderQuery.data;
    setFormState({
      name: detailResponse.name,
      adapter: detailResponse.adapter.identifier,
      configValues: buildConfigValues(detailResponse.adapter, detailResponse.settings),
      enabled: detailResponse.running,
    });
    setFormError(null);
    setFormPrefilledProvider(formTargetProvider);
  }, [
    formOpen,
    formMode,
    formTargetProvider,
    formPrefilledProvider,
    editProviderQuery.data,
  ]);

  useEffect(() => {
    if (!formOpen || formMode !== 'edit' || !formTargetProvider) {
      return;
    }
    if (!editProviderQuery.error) {
      return;
    }
    setFormError(resolveErrorMessage(editProviderQuery.error, 'Failed to load provider'));
  }, [formOpen, formMode, formTargetProvider, editProviderQuery.error]);

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
        await createProviderMutation.mutateAsync(payload);
      } else {
        await updateProviderMutation.mutateAsync({ name: trimmedName, payload });
      }
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
      await startProviderMutation.mutateAsync(name);
    } catch {
      // Notification handled by mutation hook
    } finally {
      setPending('start', null);
    }
  };

  const handleStop = async (name: string) => {
    setPending('stop', name);
    try {
      await stopProviderMutation.mutateAsync(name);
    } catch {
      // Notification handled by mutation hook
    } finally {
      setPending('stop', null);
    }
  };

  const performDelete = async (name: string) => {
    setPending('delete', name);
    try {
      await deleteProviderMutation.mutateAsync(name);
    } catch {
      // Notification handled by mutation hook
    } finally {
      setPending('delete', null);
      setDeleteTarget(null);
    }
  };
  const handleDelete = (name: string) => {
    setDeleteTarget(name);
  };

  if (loading) {
    return <div>Loading providers...</div>;
  }

  if (queryError) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{queryError.message ?? 'Failed to load providers'}</AlertDescription>
      </Alert>
    );
  }

  const deleteConfirmOpen = deleteTarget !== null;
  const deleteConfirmLoading =
    deleteTarget !== null && pendingActions.delete === deleteTarget;

  return (
    <>
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
          const isStarting = provider.status === 'starting';
          const disableActions = isStartPending || isStopPending || isDeletePending || isStarting;
          const dependentInstances = provider.dependentInstances ?? [];
          const dependentCount = provider.dependentInstanceCount ?? dependentInstances.length;
          const { visible: dynamicDependents, hidden: baselineDependents } = partitionDependents(
            dependentInstances,
            instanceIndex,
          );
          const dynamicCount = dynamicDependents.length;
          const baselineCount = baselineDependents.length;
          const deleteDisabled =
            dependentCount > 0 || isDeletePending || isStartPending || isStopPending || isStarting;
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
                  <Badge 
                    variant={
                      provider.status === 'running' ? 'default' :
                      provider.status === 'failed' ? 'destructive' :
                      provider.status === 'starting' ? 'default' :
                      'secondary'
                    }
                  >
                    {provider.status === 'starting' ? 'Starting…' :
                     provider.status === 'pending' ? 'Pending' :
                     provider.status === 'running' ? 'Running' :
                     provider.status === 'stopped' ? 'Stopped' :
                     provider.status === 'failed' ? 'Failed' :
                     'Unknown'}
                  </Badge>
                </div>
              </CardHeader>
              <CardContent className="space-y-4 text-sm">
                {provider.startupError && (
                  <Alert variant="destructive">
                    <AlertDescription className="text-xs">
                      <span className="font-medium">Startup failed:</span> {provider.startupError}
                    </AlertDescription>
                  </Alert>
                )}
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
                    {dynamicCount > 0 ? (
                      <span className="text-muted-foreground">
                        {dynamicCount} dynamic instance{dynamicCount === 1 ? '' : 's'}
                      </span>
                    ) : baselineCount > 0 ? (
                      <span className="text-muted-foreground">
                        {baselineCount} baseline instance{baselineCount === 1 ? '' : 's'}
                      </span>
                    ) : (
                      <span className="text-muted-foreground">No dynamic instances</span>
                    )}
                  </div>
                </div>
                {dynamicDependents.length > 0 && (
                  <div className="text-xs text-muted-foreground">
                    Dynamic instances: {dynamicDependents.join(', ')}
                  </div>
                )}
                {baselineCount > 0 && (
                  <div className="text-[11px] text-muted-foreground">
                    {baselineCount}{' '}
                    {baselineCount === 1 ? 'baseline instance' : 'baseline instances'} hidden (managed outside
                    this UI)
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
                      {isStarting ? 'Starting…' : isStartPending ? 'Starting…' : 'Start'}
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
        <DialogContent
          className="max-w-2xl sm:max-w-3xl flex min-h-0 flex-col"
          style={{ height: 'min(85vh, 720px)' }}
        >
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

          <ScrollArea className="flex-1 min-h-0 h-full" type="auto">
            {formLoading ? (
              <div className="py-10 text-sm text-muted-foreground">Loading provider…</div>
            ) : (
              <div className="space-y-4 pb-4 pr-1">
                <div className="space-y-2">
                  <Label htmlFor="provider-name">Name</Label>
                  <Input
                    id="provider-name"
                    value={formState.name}
                    onChange={(event) =>
                      setFormState((prev) => ({ ...prev, name: event.target.value }))
                    }
                    placeholder="new-provider"
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
                    {selectedAdapter.settingsSchema.map((setting) => {
                      const normalizedName = setting.name.trim().toLowerCase();
                      const adapterHints = AUTH_FIELD_HINTS[selectedAdapter.identifier] ?? {};
                      const fieldHint = adapterHints[normalizedName] ?? adapterHints[setting.name];
                      const defaultLabel = AUTH_SETTING_LABELS[normalizedName] ?? setting.name;
                      const labelText = fieldHint?.label ?? defaultLabel;
                      const noteText =
                        fieldHint?.note ??
                        (fieldHint ? undefined : AUTH_SETTING_LABELS[normalizedName] ? DEFAULT_PRIVATE_NOTE : undefined);
                      return (
                        <div key={setting.name} className="space-y-2">
                          <Label htmlFor={`setting-${setting.name}`}>
                            {labelText}
                            {setting.required && <span className="text-destructive">*</span>}
                          </Label>
                      <Input
                        id={`setting-${setting.name}`}
                        type={['int', 'integer', 'float', 'double', 'number'].includes(setting.type.toLowerCase()) ? 'number' : 'text'}
                        value={formState.configValues[setting.name] ?? ''}
                        onChange={(event) => handleConfigChange(setting.name, event.target.value)}
                      />
                          {noteText && <p className="text-xs text-muted-foreground">{noteText}</p>}
                          <p className="text-xs text-muted-foreground">Type: {setting.type}</p>
                        </div>
                      );
                    })}
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
                        Provider will start asynchronously. Check status to monitor readiness.
                      </p>
                    </div>
                    <label className="flex items-center gap-2 text-sm font-medium text-foreground">
                      <Checkbox
                        checked={formState.enabled}
                        onChange={(event: ChangeEvent<HTMLInputElement>) =>
                          setFormState((prev) => ({ ...prev, enabled: event.target.checked }))
                        }
                      />
                      <span className="whitespace-nowrap">Start immediately</span>
                    </label>
                  </div>
                </div>
              </div>
            )}
          </ScrollArea>

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


<Dialog open={detailOpen} onOpenChange={handleDetailOpenChange}>
  <DialogContent className="max-w-3xl sm:max-h-[85vh] flex flex-col">
    <DialogHeader>
      <DialogTitle>Provider details</DialogTitle>
      <DialogDescription>Inspect adapter configuration, balances, and subscribed instruments.</DialogDescription>
    </DialogHeader>

    {detailError && (
      <Alert variant="destructive">
        <AlertDescription>{detailError}</AlertDescription>
      </Alert>
    )}

    <ScrollArea className="flex-1" type="auto">
      {detailLoading ? (
        <div className="py-6 text-sm text-muted-foreground">Loading provider…</div>
      ) : detail ? (
        <Tabs
          value={detailTab}
          onValueChange={(value) => setDetailTab(value as 'overview' | 'balances')}
          className="flex flex-col gap-4 pr-1"
        >
          <TabsList className="w-full justify-start">
            <TabsTrigger value="overview">Overview</TabsTrigger>
            <TabsTrigger value="balances">Balances</TabsTrigger>
          </TabsList>
          <TabsContent value="overview" className="mt-0">
            <div className="space-y-4 text-sm">
              {(() => {
                const dependentInstances = detail.dependentInstances ?? [];
                const { visible: dynamicDependents, hidden: baselineDependents } = partitionDependents(
                  dependentInstances,
                  instanceIndex,
                );
                const dynamicCount = dynamicDependents.length;
                const baselineCount = baselineDependents.length;
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
                      {dynamicCount === 0 && baselineCount === 0 ? (
                        <p className="text-muted-foreground">No instances depend on this provider.</p>
                      ) : (
                        <div className="space-y-2 text-muted-foreground">
                          {dynamicCount > 0 && (
                            <>
                              <p>
                                {dynamicCount} dynamic instance{dynamicCount === 1 ? '' : 's'} requiring this provider
                              </p>
                              <ul className="list-disc space-y-1 pl-5">
                                {dynamicDependents.map((instance) => (
                                  <li key={instance}>{instance}</li>
                                ))}
                              </ul>
                            </>
                          )}
                          {baselineCount > 0 && (
                            <p className="text-xs">
                              {baselineCount}{' '}
                              {baselineCount === 1 ? 'baseline instance' : 'baseline instances'} hidden (managed outside this UI)
                            </p>
                          )}
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
                <p className="font-medium text-foreground">Instruments ({detail.instruments.length})</p>
                <div className="flex flex-col gap-2 py-2">
                  <Input
                    placeholder="Search symbols…"
                    value={instrumentQuery}
                    onChange={(event) => setInstrumentQuery(event.target.value)}
                  />
                  <div className="text-xs text-muted-foreground">
                    {filteredInstruments.length} matching instrument{filteredInstruments.length === 1 ? '' : 's'}
                  </div>
                </div>
                <div className="rounded-md border">
                  <div className="grid gap-2 p-3 text-xs sm:grid-cols-2">
                    {currentPageInstruments.length === 0 ? (
                      <div className="text-muted-foreground">No instruments match the current filter.</div>
                    ) : (
                      currentPageInstruments.map((instrument) => (
                        <button
                          type="button"
                          key={instrument.symbol}
                          onClick={() => setSelectedInstrument(instrument)}
                          className={cn(
                            'rounded-md border px-3 py-2 text-left transition hover:border-foreground/40',
                            selectedInstrument?.symbol === instrument.symbol && 'border-primary text-primary',
                          )}
                        >
                          <div className="text-sm font-medium">{instrument.symbol}</div>
                          <div className="text-xs text-muted-foreground">
                            {instrumentBaseValue(instrument)} / {instrumentQuoteValue(instrument)}
                          </div>
                        </button>
                      ))
                    )}
                  </div>
                </div>
                {totalInstrumentPages > 1 && (
                  <div className="mt-3 flex items-center justify-between text-xs text-muted-foreground">
                    <div>
                      Showing {pageDisplayStart} – {pageDisplayEnd}{' '}
                      of {totalInstrumentCount} instrument{totalInstrumentCount === 1 ? '' : 's'}
                    </div>
                    <div className="flex items-center gap-2">
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={effectiveInstrumentPage === 0}
                        onClick={() => setInstrumentPage((page) => Math.max(0, page - 1))}
                      >
                        Prev
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={effectiveInstrumentPage >= totalInstrumentPages - 1}
                        onClick={() =>
                          setInstrumentPage((page) =>
                            Math.min(totalInstrumentPages - 1, page + 1),
                          )
                        }
                      >
                        Next
                      </Button>
                    </div>
                  </div>
                )}

                {selectedInstrumentDisplay && (
                  <div className="mt-4 space-y-2 rounded-md border p-3">
                    <p className="text-sm font-medium text-foreground">Instrument details</p>
                    <div className="grid gap-2 text-xs sm:grid-cols-2">
                      <div>
                        <p className="text-muted-foreground">Base</p>
                        <p className="font-medium">{selectedInstrumentDisplay.base}</p>
                      </div>
                      <div>
                        <p className="text-muted-foreground">Quote</p>
                        <p className="font-medium">{selectedInstrumentDisplay.quote}</p>
                      </div>
                      <div>
                        <p className="text-muted-foreground">Type</p>
                        <p className="font-medium">{selectedInstrumentDisplay.type}</p>
                      </div>
                      <div>
                        <p className="text-muted-foreground">Price precision</p>
                        <p className="font-medium">{selectedInstrumentDisplay.pricePrecision}</p>
                      </div>
                      <div>
                        <p className="text-muted-foreground">Quantity precision</p>
                        <p className="font-medium">{selectedInstrumentDisplay.quantityPrecision}</p>
                      </div>
                      <div>
                        <p className="text-muted-foreground">Price increment</p>
                        <p className="font-medium">{selectedInstrumentDisplay.priceIncrement}</p>
                      </div>
                      <div>
                        <p className="text-muted-foreground">Quantity increment</p>
                        <p className="font-medium">{selectedInstrumentDisplay.quantityIncrement}</p>
                      </div>
                      <div>
                        <p className="text-muted-foreground">Min quantity</p>
                        <p className="font-medium">{selectedInstrumentDisplay.minQuantity}</p>
                      </div>
                      <div>
                        <p className="text-muted-foreground">Max quantity</p>
                        <p className="font-medium">{selectedInstrumentDisplay.maxQuantity}</p>
                      </div>
                      <div>
                        <p className="text-muted-foreground">Notional precision</p>
                        <p className="font-medium">{selectedInstrumentDisplay.notionalPrecision}</p>
                      </div>
                    </div>
                  </div>
                )}
              </div>
            </div>
          </TabsContent>
          <TabsContent value="balances" className="mt-0">
            <div className="space-y-4">
              <div className="flex flex-wrap gap-3">
                <div className="flex min-w-[180px] flex-1 flex-col gap-1">
                  <Label htmlFor="balance-asset">Asset filter</Label>
                  <Input
                    id="balance-asset"
                    placeholder="e.g. USDT"
                    value={balanceAssetFilter}
                    onChange={(event) => setBalanceAssetFilter(event.target.value.toUpperCase())}
                  />
                </div>
                <div className="flex w-full flex-col gap-1 sm:w-auto">
                  <Label>Page size</Label>
                  <Select
                    value={String(balanceLimit)}
                    onValueChange={(value) => {
                      const numeric = Number(value);
                      if (Number.isFinite(numeric) && numeric > 0) {
                        setBalanceLimit(numeric);
                      }
                    }}
                  >
                    <SelectTrigger className="h-9 w-[5rem]">
                      <SelectValue placeholder={`${DEFAULT_BALANCE_LIMIT}`} />
                    </SelectTrigger>
                    <SelectContent>
                      {BALANCE_LIMIT_OPTIONS.map((option) => (
                        <SelectItem key={option} value={String(option)}>
                          {option}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="flex flex-1 flex-col justify-end text-xs text-muted-foreground sm:text-right">
                  <span>{balanceCount} record{balanceCount === 1 ? '' : 's'}</span>
                  <span>
                    Last snapshot {balanceRows[0] ? formatTimestamp(balanceRows[0].snapshotAt) : '—'}
                  </span>
                </div>
              </div>

              {balanceError && (
                <Alert variant="destructive">
                  <AlertDescription>{balanceError}</AlertDescription>
                </Alert>
              )}

              {balanceLoading ? (
                <div className="flex items-center gap-2 text-sm text-muted-foreground">
                  <Loader2 className="h-4 w-4 animate-spin" /> Fetching balances…
                </div>
              ) : balanceRows.length === 0 ? (
                      <p className="text-sm text-muted-foreground">
                        No balances reported for this provider{balanceAssetFilter ? ` and asset ${balanceAssetFilter}` : ''}.
                      </p>
              ) : (
                <div className="rounded-md border">
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead className="w-[20%]">Asset</TableHead>
                        <TableHead>Provider</TableHead>
                        <TableHead>Total</TableHead>
                        <TableHead>Available</TableHead>
                        <TableHead>Snapshot</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {balanceRows.map((entry) => (
                        <TableRow key={`${entry.provider}-${entry.asset}-${entry.snapshotAt}`}>
                          <TableCell className="font-mono text-sm">{entry.asset}</TableCell>
                          <TableCell>{entry.provider}</TableCell>
                          <TableCell className="font-mono text-xs">{entry.total}</TableCell>
                          <TableCell className="font-mono text-xs">{entry.available}</TableCell>
                          <TableCell className="text-xs text-muted-foreground">
                            {formatTimestamp(entry.snapshotAt)}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </div>
              )}
            </div>
          </TabsContent>
        </Tabs>
      ) : (
        <div className="text-sm text-muted-foreground">Select a provider to view details.</div>
      )}
    </ScrollArea>
  </DialogContent>
</Dialog>
      </div>
      <ConfirmDialog
        open={deleteConfirmOpen}
        onOpenChange={(open) => {
          if (!open) {
            setDeleteTarget(null);
          }
        }}
        title="Delete provider?"
        description={
          deleteTarget ? (
            <span>
              This action will permanently remove{' '}
              <span className="font-medium text-foreground">{deleteTarget}</span>.
            </span>
          ) : undefined
        }
        confirmLabel="Delete"
        confirmVariant="destructive"
        loading={deleteConfirmLoading}
        confirmDisabled={deleteConfirmLoading}
        onConfirm={() => {
          if (deleteTarget) {
            void performDelete(deleteTarget);
          }
        }}
      />
    </>
  );
}
