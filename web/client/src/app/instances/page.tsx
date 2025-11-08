'use client';

import Link from 'next/link';
import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  BalanceRecord,
  ExecutionRecord,
  InstanceSpec,
  InstanceSummary,
  OrderRecord,
  Provider,
} from '@/lib/types';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { ChartLegend, StackedBarChart } from '@/components/ui/chart';
import { ScrollArea } from '@/components/ui/scroll-area';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { CircleStopIcon, Clock3Icon, PlayIcon, PlusIcon, TrashIcon, PencilIcon, Loader2Icon, Copy, RotateCcwIcon } from 'lucide-react';
import { useToast } from '@/components/ui/toast-provider';
import { Checkbox } from '@/components/ui/checkbox';
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from '@/components/ui/tabs';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { ConfirmDialog } from '@/components/confirm-dialog';
import { CodeEditor } from '@/components/code';
import { formatInstanceSpec, parseInstanceSpecDraft } from './spec-utils';
import {
  useInstancesQuery,
  useInstanceOrdersQuery,
  useInstanceExecutionsQuery,
  useCreateInstanceMutation,
  useUpdateInstanceMutation,
  useDeleteInstanceMutation,
  useStartInstanceMutation,
  useStopInstanceMutation,
  useInstanceLoader,
  useInstanceBalancesQuery,
  useStrategiesQuery,
  useStrategyModulesQuery,
  useProvidersQuery,
  useProviderLoader,
} from '@/lib/hooks';

type ProviderInstrumentStatus = {
  symbols: string[];
  loading: boolean;
  error: string | null;
};

type ProviderAvailability = {
  missing: string[];
  stopped: string[];
};

type ParsedSelector = {
  identifier: string;
  selector: string;
  tag?: string;
  hash?: string;
};

type HistoryTab = 'orders' | 'executions' | 'balances';

function parseStrategySelector(raw: string): ParsedSelector {
  const selector = raw.trim();
  if (!selector) {
    return { identifier: '', selector: '' };
  }
  if (selector.includes('@')) {
    const [identifierPart, hashPart] = selector.split('@');
    return {
      identifier: identifierPart.trim(),
      selector,
      hash: hashPart.trim(),
    };
  }
  if (selector.includes(':')) {
    const [identifierPart, ...rest] = selector.split(':');
    return {
      identifier: identifierPart.trim(),
      selector,
      tag: rest.join(':').trim(),
    };
  }
  return { identifier: selector, selector };
}

function formatHash(hash: string | undefined | null, length = 12): string {
  if (!hash) {
    return '—';
  }
  if (hash.length <= length) {
    return hash;
  }
  return `${hash.slice(0, length)}…`;
}

function canonicalUsageSelector(name: string, hash?: string | null, tag?: string | null): string {
  const trimmed = name.trim();
  if (hash && hash.trim()) {
    return `${trimmed}@${hash.trim()}`;
  }
  if (tag && tag.trim()) {
    return `${trimmed}:${tag.trim()}`;
  }
  return trimmed;
}

function formatDateTime(value?: string | number | null): string {
  if (value === undefined || value === null || value === '') {
    return '—';
  }
  const date = typeof value === 'number' ? new Date(value) : new Date(value);
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

function formatMetadata(metadata?: Record<string, unknown> | null): string {
  if (!metadata || Object.keys(metadata).length === 0) {
    return '—';
  }
  try {
    const serialized = JSON.stringify(metadata);
    return serialized.length > 120 ? `${serialized.slice(0, 117)}…` : serialized;
  } catch {
    return '[unserializable]';
  }
}

function formatProviderIssueMessage(availability: ProviderAvailability): string | null {
  const segments: string[] = [];
  const { missing, stopped } = availability;
  if (missing.length > 0) {
    const missingList = missing.join(', ');
    segments.push(
      `Provider${missing.length > 1 ? 's' : ''} ${missingList} ${missing.length > 1 ? 'are' : 'is'} not configured.`,
    );
  }
  if (stopped.length > 0) {
    const stoppedList = stopped.join(', ');
    segments.push(
      `Start ${stoppedList} provider${stopped.length > 1 ? 's' : ''} before starting this instance.`,
    );
  }
  if (segments.length === 0) {
    return null;
  }
  return segments.join(' ');
}

const HISTORY_LIMITS: Record<HistoryTab, number> = {
  orders: 100,
  executions: 100,
  balances: 50,
};

const DEFAULT_INSTANCE_SPEC: InstanceSpec = {
  id: '',
  strategy: {
    identifier: '',
    selector: '',
    config: {},
  },
  scope: {},
};

const EMPTY_INSTANCE_JSON_TEMPLATE = formatInstanceSpec(DEFAULT_INSTANCE_SPEC);
const DEFAULT_INSTANCE_JSON = '';

const DIALOG_STATE_KEY = 'instances:dialog-state';

type PersistedDialogState = {
  open: boolean;
  mode: 'create' | 'edit';
  editingId?: string | null;
  draft?: string;
  formMode?: 'json' | 'guided';
};

const loadPersistedDialogState = (): PersistedDialogState | null => {
  if (typeof window === 'undefined') {
    return null;
  }
  try {
    const raw = window.sessionStorage.getItem(DIALOG_STATE_KEY);
    if (!raw) {
      return null;
    }
    return JSON.parse(raw) as PersistedDialogState;
  } catch {
    return null;
  }
};

export default function InstancesPage() {
  const instancesQuery = useInstancesQuery();
  const strategiesQuery = useStrategiesQuery();
  const providersQuery = useProvidersQuery();
  const strategyModulesQuery = useStrategyModulesQuery({ limit: 500, offset: 0 });
  const instances = useMemo(
    () => instancesQuery.data ?? [],
    [instancesQuery.data],
  );
  const strategies = useMemo(
    () => strategiesQuery.data ?? [],
    [strategiesQuery.data],
  );
  const providers = useMemo(
    () => providersQuery.data ?? [],
    [providersQuery.data],
  );
  const providerLookup = useMemo(() => {
    const map = new Map<string, Provider>();
    providers.forEach((provider) => {
      map.set(provider.name.toLowerCase(), provider);
    });
    return map;
  }, [providers]);
  const providerStatusReady = providersQuery.isSuccess;
  const evaluateProviderAvailability = useCallback(
    (names: string[]): ProviderAvailability => {
      const missing: string[] = [];
      const stopped: string[] = [];
      const missingSeen = new Set<string>();
      const stoppedSeen = new Set<string>();
      names.forEach((rawName) => {
        const trimmed = rawName.trim();
        if (!trimmed) {
          return;
        }
        const key = trimmed.toLowerCase();
        const provider = providerLookup.get(key);
        if (!provider) {
          if (!missingSeen.has(key)) {
            missing.push(trimmed);
            missingSeen.add(key);
          }
          return;
        }
        if (!provider.running && !stoppedSeen.has(key)) {
          stopped.push(trimmed);
          stoppedSeen.add(key);
        }
      });
      return { missing, stopped };
    },
    [providerLookup],
  );
  const modules = useMemo(
    () => strategyModulesQuery.data?.modules ?? [],
    [strategyModulesQuery.data],
  );
  const [createDialogOpen, setCreateDialogOpen] = useState<boolean>(false);
  const [dialogMode, setDialogMode] = useState<'create' | 'edit'>('create');
  const [editingInstanceId, setEditingInstanceId] = useState<string | null>(null);
  const [prefilledConfig, setPrefilledConfig] = useState(false);
  const [dialogSaving, setDialogSaving] = useState(false);
  const [instanceLoading, setInstanceLoading] = useState(false);
  const [actionInProgress, setActionInProgress] = useState<Record<string, boolean>>({});
  const [formMode, setFormMode] = useState<'json' | 'guided'>('guided');
  const [lastEditedMode, setLastEditedMode] = useState<'json' | 'guided'>('guided');
  const [instanceJsonDraft, setInstanceJsonDraft] = useState<string>(DEFAULT_INSTANCE_JSON);

  const [newInstance, setNewInstance] = useState({
    id: '',
    strategyIdentifier: '',
  });
  const [strategySelectorInput, setStrategySelectorInput] = useState('');
  const [selectedProviders, setSelectedProviders] = useState<string[]>([]);
  const [providerSymbols, setProviderSymbols] = useState<Record<string, string[]>>({});
  const [providerSymbolFilters, setProviderSymbolFilters] = useState<Record<string, string>>({});
  const [providerInstrumentState, setProviderInstrumentState] = useState<
    Record<string, ProviderInstrumentStatus>
  >({});
  const [configValues, setConfigValues] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [confirmState, setConfirmState] = useState<{ type: 'delete-instance'; id: string } | null>(
    null,
  );
  const { show: showToast } = useToast();
  const [historyDialogOpen, setHistoryDialogOpen] = useState(false);
  const [historyDialogInstance, setHistoryDialogInstance] = useState<InstanceSummary | null>(null);
  const [historyTab, setHistoryTab] = useState<HistoryTab>('orders');
  const markGuidedDirty = useCallback(() => {
    setLastEditedMode('guided');
  }, []);
  const markJsonDirty = useCallback(() => {
    setLastEditedMode('json');
  }, []);

  useEffect(() => {
    const persisted = loadPersistedDialogState();
    if (!persisted) {
      return;
    }
    setCreateDialogOpen(Boolean(persisted.open));
    setDialogMode(persisted.mode ?? 'create');
    setEditingInstanceId(persisted.editingId ?? null);
    if (persisted.mode === 'edit') {
      setPrefilledConfig(true);
    }
    if (typeof persisted.draft === 'string') {
      setInstanceJsonDraft(persisted.draft);
    }
    if (persisted.formMode) {
      setFormMode(persisted.formMode);
      setLastEditedMode(persisted.formMode);
    } else if (typeof persisted.draft === 'string' && persisted.draft.trim().length > 0) {
      setLastEditedMode('json');
    }
  }, []);

  useEffect(() => {
    if (typeof window === 'undefined') {
      return;
    }
    if (!createDialogOpen) {
      window.sessionStorage.removeItem(DIALOG_STATE_KEY);
      return;
    }
    const payload: PersistedDialogState = {
      open: true,
      mode: dialogMode,
      editingId: editingInstanceId,
      draft: instanceJsonDraft,
      formMode,
    };
    window.sessionStorage.setItem(DIALOG_STATE_KEY, JSON.stringify(payload));
  }, [createDialogOpen, dialogMode, editingInstanceId, instanceJsonDraft, formMode]);
  const baseLoading =
    instancesQuery.isLoading ||
    strategiesQuery.isLoading ||
    providersQuery.isLoading ||
    strategyModulesQuery.isLoading;
  const baseError =
    (instancesQuery.error as Error | null) ??
    (strategiesQuery.error as Error | null) ??
    (providersQuery.error as Error | null) ??
    (strategyModulesQuery.error as Error | null);
  const createInstanceMutation = useCreateInstanceMutation();
  const updateInstanceMutation = useUpdateInstanceMutation(editingInstanceId ?? '');
  const deleteInstanceMutation = useDeleteInstanceMutation();
  const startInstanceMutation = useStartInstanceMutation();
  const stopInstanceMutation = useStopInstanceMutation();
  const loadInstance = useInstanceLoader();
  const loadProviderDetail = useProviderLoader();

  const jsonDiagnostics = useMemo(() => {
    const trimmed = instanceJsonDraft.trim();
    if (!trimmed) {
      return {
        status: 'idle' as const,
        message: 'Provide a JSON instance specification to enable submission.',
      };
    }
    const result = parseInstanceSpecDraft(instanceJsonDraft, { strict: false });
    if (result.error) {
      return {
        status: 'error' as const,
        message: result.error,
      };
    }
    return {
      status: 'success' as const,
      message: 'JSON payload parses successfully.',
    };
  }, [instanceJsonDraft]);

  const jsonDiagnosticClass =
    jsonDiagnostics.status === 'success'
      ? 'text-xs text-success'
      : jsonDiagnostics.status === 'error'
        ? 'text-xs text-destructive'
        : 'text-xs text-muted-foreground';

  const submitDisabled =
    dialogSaving ||
    instanceLoading ||
    (lastEditedMode === 'json' && jsonDiagnostics.status !== 'success');

  const ordersHistoryQuery = useInstanceOrdersQuery(
    historyDialogInstance?.id,
    { limit: HISTORY_LIMITS.orders },
    Boolean(historyDialogOpen && historyDialogInstance?.id),
  );
  const executionsHistoryQuery = useInstanceExecutionsQuery(
    historyDialogInstance?.id,
    { limit: HISTORY_LIMITS.executions },
    Boolean(historyDialogOpen && historyDialogInstance?.id),
  );
  const balancesHistoryQuery = useInstanceBalancesQuery(
    historyDialogInstance?.providers,
    HISTORY_LIMITS.balances,
    Boolean(historyDialogOpen && historyDialogInstance?.id),
  );


  const selectedStrategy = useMemo(
    () => strategies.find((strategy) => strategy.name === newInstance.strategyIdentifier),
    [strategies, newInstance.strategyIdentifier]
  );

  const moduleStatusByName = useMemo(() => {
    const map = new Map<
      string,
      { pinnedHash: string | null; latestHash: string | null; latestTag: string | null }
    >();
    modules.forEach((module) => {
      const key = module.name.toLowerCase();
      const pinnedHash = module.hash || null;
      const latestHash = module.tagAliases?.latest ?? pinnedHash;
      let latestTag: string | null = null;
      if (latestHash) {
        const aliasEntries = Object.entries(module.tagAliases ?? {}).filter(
          ([tag, hash]) => tag !== 'latest' && hash === latestHash,
        );
        if (aliasEntries.length > 0) {
          latestTag = aliasEntries[0][0];
        } else if (module.version) {
          latestTag = module.version;
        }
      }
      map.set(key, {
        pinnedHash,
        latestHash,
        latestTag,
      });
    });
    return map;
  }, [modules]);

  const anyActionInFlight = useMemo(
    () => Object.values(actionInProgress).some(Boolean),
    [actionInProgress],
  );

  useEffect(() => {
    if (!selectedStrategy) {
      if (!prefilledConfig) {
        setConfigValues({});
      }
      return;
    }
    if (dialogMode === 'edit' && prefilledConfig) {
      return;
    }
    const defaults: Record<string, string> = {};
    selectedStrategy.config.forEach((field) => {
      if (typeof field.default === 'boolean') {
        defaults[field.name] = field.default ? 'true' : 'false';
        return;
      }
      if (field.default !== undefined && field.default !== null) {
        defaults[field.name] = String(field.default);
        return;
      }
      defaults[field.name] = field.type === 'bool' ? 'false' : '';
    });
    setConfigValues(defaults);
  }, [selectedStrategy, dialogMode, prefilledConfig]);

  const resetForm = () => {
    setNewInstance({
      id: '',
      strategyIdentifier: '',
    });
    setStrategySelectorInput('');
    setSelectedProviders([]);
    setProviderSymbols({});
    setProviderSymbolFilters({});
    setConfigValues({});
    setFormError(null);
    setEditingInstanceId(null);
    setDialogMode('create');
    setPrefilledConfig(false);
    setDialogSaving(false);
    setInstanceLoading(false);
    setFormMode('guided');
    setLastEditedMode('guided');
    setInstanceJsonDraft(DEFAULT_INSTANCE_JSON);
  };

  const populateFormFromSpec = useCallback(
    (spec: InstanceSpec, options?: { setPrefilled?: boolean }) => {
      setNewInstance({
        id: spec.id,
        strategyIdentifier: spec.strategy.identifier,
      });
      const hashedSelector = spec.strategy.hash?.trim()
        ? `${spec.strategy.identifier}@${spec.strategy.hash}`
        : null;
      const tagSelector = spec.strategy.tag?.trim()
        ? `${spec.strategy.identifier}:${spec.strategy.tag}`
        : null;
      const selectorValue =
        hashedSelector || spec.strategy.selector?.trim() || tagSelector || spec.strategy.identifier;
      setStrategySelectorInput(selectorValue);

      const providerNames = Object.keys(spec.scope ?? {});
      setSelectedProviders(providerNames);

      const symbolMap: Record<string, string[]> = {};
      providerNames.forEach((name) => {
        const assignment = spec.scope[name];
        const symbols = Array.isArray(assignment?.symbols) ? assignment.symbols : [];
        symbolMap[name] = symbols
          .map((symbol) => (typeof symbol === 'string' ? symbol : String(symbol ?? '')))
          .filter((symbol) => symbol.length > 0);
      });
      setProviderSymbols(symbolMap);
      setProviderSymbolFilters({});

      const strategyMeta = strategies.find((strategy) => strategy.name === spec.strategy.identifier);
      if (strategyMeta) {
        const values: Record<string, string> = {};
        strategyMeta.config.forEach((field) => {
          const raw = spec.strategy.config[field.name];
          if (raw === undefined || raw === null) {
            values[field.name] = field.type === 'bool' ? 'false' : '';
            return;
          }
          if (field.type === 'bool') {
            values[field.name] = raw === true ? 'true' : 'false';
            return;
          }
          values[field.name] = String(raw);
        });
        setConfigValues(values);
      } else {
        const values = Object.fromEntries(
          Object.entries(spec.strategy.config ?? {}).map(([key, value]) => [key, String(value)]),
        );
        setConfigValues(values);
      }

      if (options?.setPrefilled) {
        setPrefilledConfig(true);
      }
    },
    [strategies],
  );

  const buildPayloadFromBuilder = (strict: boolean): { spec?: InstanceSpec; error?: string } => {
    const idValue = newInstance.id.trim();
    if (strict && !idValue) {
      return { error: 'Instance ID is required' };
    }

    const selectorCandidate = strategySelectorInput.trim() || newInstance.strategyIdentifier.trim();
    const parsedSelector = parseStrategySelector(selectorCandidate);
    const originalIdentifier = newInstance.strategyIdentifier.trim();
    let identifier = parsedSelector.identifier || originalIdentifier;
    identifier = identifier.trim();

    if (dialogMode === 'edit') {
      identifier = originalIdentifier;
    }

    if (strict && !identifier) {
      return { error: 'Strategy identifier is required' };
    }

    if (
      strict &&
      dialogMode === 'edit' &&
      identifier.toLowerCase() !== originalIdentifier.toLowerCase()
    ) {
      return { error: 'Strategy identifier cannot be changed after creation.' };
    }

    const tag = parsedSelector.tag?.trim() ? parsedSelector.tag.trim() : undefined;
    const hash = parsedSelector.hash?.trim() ? parsedSelector.hash.trim() : undefined;

    let selectorValue = parsedSelector.selector?.trim() ?? '';
    if (!selectorValue) {
      if (hash) {
        selectorValue = `${identifier || originalIdentifier}@${hash}`;
      } else if (tag) {
        selectorValue = `${identifier || originalIdentifier}:${tag}`;
      } else {
        selectorValue = identifier;
      }
    }

    if (dialogMode === 'edit') {
      selectorValue = strategySelectorInput.trim() || selectorValue;
    }

    if (strict && selectedProviders.length === 0) {
      return { error: 'Select at least one provider' };
    }

    const scope: Record<string, { symbols: string[] }> = {};
    const aggregatedSymbols = new Set<string>();
    for (const providerName of selectedProviders) {
      const selectedSymbols = providerSymbols[providerName] ?? [];
      const parsedSymbols = Array.from(
        new Set(
          selectedSymbols
            .map((symbol) => symbol.trim().toUpperCase())
            .filter((symbol) => symbol.length > 0),
        ),
      );
      if (strict && parsedSymbols.length === 0) {
        return { error: `Provider "${providerName}" requires at least one symbol` };
      }
      parsedSymbols.forEach((symbol) => aggregatedSymbols.add(symbol));
      scope[providerName] = { symbols: parsedSymbols };
    }

    if (strict && aggregatedSymbols.size === 0) {
      return { error: 'At least one instrument symbol is required' };
    }

    const strategyMeta = strategies.find((strategy) => strategy.name === identifier);
    if (strict && !strategyMeta) {
      return { error: 'Strategy metadata is unavailable' };
    }

    const configPayload: Record<string, unknown> = {};
    if (strategyMeta) {
      for (const field of strategyMeta.config) {
        const rawValue = configValues[field.name] ?? '';
        if (field.type === 'bool') {
          configPayload[field.name] = rawValue === 'true';
          continue;
        }
        if (rawValue === '') {
          if (strict && field.required) {
            return { error: `Configuration field "${field.name}" is required` };
          }
          continue;
        }
        if (field.type === 'int') {
          const parsed = parseInt(rawValue, 10);
          if (strict && Number.isNaN(parsed)) {
            return { error: `Configuration field "${field.name}" must be an integer` };
          }
          if (!Number.isNaN(parsed)) {
            configPayload[field.name] = parsed;
          }
          continue;
        }
        if (field.type === 'float') {
          const parsed = parseFloat(rawValue);
          if (strict && Number.isNaN(parsed)) {
            return { error: `Configuration field "${field.name}" must be a number` };
          }
          if (!Number.isNaN(parsed)) {
            configPayload[field.name] = parsed;
          }
          continue;
        }
        configPayload[field.name] = rawValue;
      }
    } else if (!strict) {
      Object.entries(configValues).forEach(([key, value]) => {
        if (value !== '') {
          configPayload[key] = value;
        }
      });
    }

    const spec: InstanceSpec = {
      id: idValue,
      strategy: {
        identifier,
        selector: selectorValue,
        config: configPayload,
      },
      scope,
    };

    if (tag) {
      spec.strategy.tag = tag;
    }
    if (hash) {
      spec.strategy.hash = hash;
    }
    if (Object.keys(scope).length > 0) {
      spec.providers = Object.keys(scope);
      spec.aggregatedSymbols = Array.from(aggregatedSymbols);
    }

    return { spec };
  };

  const handleFormModeChange = (value: string) => {
    if (value !== 'json' && value !== 'guided') {
      return;
    }
    if (value === 'json') {
      const { spec } = buildPayloadFromBuilder(false);
      if (spec) {
        setInstanceJsonDraft(formatInstanceSpec(spec));
      } else if (!instanceJsonDraft.trim()) {
        setInstanceJsonDraft(EMPTY_INSTANCE_JSON_TEMPLATE);
      }
      setFormError(null);
    } else {
      const trimmedDraft = instanceJsonDraft.trim();
      if (!trimmedDraft) {
        populateFormFromSpec(DEFAULT_INSTANCE_SPEC);
        setFormError(null);
        setFormMode(value);
        return;
      }
      const result = parseInstanceSpecDraft(instanceJsonDraft, { strict: false });
      if (result.error) {
        setFormError(result.error);
        return;
      }
      if (result.spec) {
        populateFormFromSpec(result.spec);
      }
      setFormError(null);
    }
    setFormMode(value);
  };

  const loadProviderInstrumentSymbols = useCallback(async (providerName: string) => {
    let shouldFetch = true;
    setProviderInstrumentState((prev) => {
      const existing = prev[providerName];
      if (existing?.loading) {
        shouldFetch = false;
        return prev;
      }
      return {
        ...prev,
        [providerName]: {
          symbols: existing?.symbols ?? [],
          loading: true,
          error: null,
        },
      };
    });

    if (!shouldFetch) {
      return;
    }

    try {
      const detail = await loadProviderDetail(providerName);
      const instruments = Array.isArray(detail.instruments) ? detail.instruments : [];
      const symbols = Array.from(
        new Set(
          instruments
            .map((instrument) => instrument.symbol)
            .filter((symbol): symbol is string => typeof symbol === 'string' && symbol.trim().length > 0)
            .map((symbol) => symbol.trim().toUpperCase()),
        ),
      ).sort((a, b) => a.localeCompare(b));
      setProviderInstrumentState((prev) => ({
        ...prev,
        [providerName]: {
          symbols,
          loading: false,
          error: null,
        },
      }));
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load symbols';
      setProviderInstrumentState((prev) => {
        const existing = prev[providerName];
        return {
          ...prev,
          [providerName]: {
            symbols: existing?.symbols ?? [],
            loading: false,
            error: message,
          },
        };
      });
    }
  }, [loadProviderDetail]);

  const toggleProviderSelection = (providerName: string, checked: boolean) => {
    let changed = false;
    setSelectedProviders((prev) => {
      if (checked) {
        if (prev.includes(providerName)) {
          return prev;
        }
        changed = true;
        return [...prev, providerName];
      }
      if (!prev.includes(providerName)) {
        return prev;
      }
      changed = true;
      return prev.filter((name) => name !== providerName);
    });
    if (checked) {
      setProviderSymbols((prev) => ({
        ...prev,
        [providerName]: prev[providerName] ?? [],
      }));
      void loadProviderInstrumentSymbols(providerName);
    } else {
      setProviderSymbols((prev) => {
        if (!(providerName in prev)) {
          return prev;
        }
        const next = { ...prev };
        delete next[providerName];
        return next;
      });
      setProviderSymbolFilters((prev) => {
        if (!(providerName in prev)) {
          return prev;
        }
        const next = { ...prev };
        delete next[providerName];
        return next;
      });
    }
    if (changed) {
      markGuidedDirty();
    }
  };

  const handleConfigChange = (field: string, value: string) => {
    setConfigValues((prev) => ({ ...prev, [field]: value }));
    setFormError(null);
    markGuidedDirty();
  };

  const handleSubmit = async () => {
    const submissionMode = lastEditedMode ?? formMode;
    let payload: InstanceSpec | undefined;

    if (submissionMode === 'json') {
      const result = parseInstanceSpecDraft(instanceJsonDraft, { strict: true });
      if (!result.spec) {
        setFormError(result.error ?? 'Invalid instance specification');
        return;
      }
      if (dialogMode === 'edit') {
        if (!editingInstanceId) {
          setFormError('No instance selected for update');
          return;
        }
        const submittedId = result.spec.id.trim();
        if (submittedId && submittedId !== editingInstanceId.trim()) {
          setFormError('Instance ID cannot be changed during update.');
          return;
        }
      }
      if (!result.spec.id.trim()) {
        setFormError('Instance ID is required.');
        return;
      }
      payload = result.spec;
      populateFormFromSpec(result.spec);
    } else {
      const builderResult = buildPayloadFromBuilder(true);
      if (!builderResult.spec) {
        setFormError(builderResult.error ?? 'Invalid instance specification');
        return;
      }
      payload = builderResult.spec;
      setInstanceJsonDraft(formatInstanceSpec(builderResult.spec));
    }

    if (!payload) {
      setFormError('Instance specification is required.');
      return;
    }

    const targetId = payload.id.trim();
    if (!targetId) {
      setFormError('Instance ID is required.');
      return;
    }

    try {
      setFormError(null);
      setDialogSaving(true);
      if (dialogMode === 'edit') {
        if (!editingInstanceId) {
          setFormError('No instance selected for update');
          setDialogSaving(false);
          return;
        }
        await updateInstanceMutation.mutateAsync(payload);
      } else {
        await createInstanceMutation.mutateAsync(payload);
      }
      setCreateDialogOpen(false);
      resetForm();
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : '';
      const providerNames =
        payload.providers && payload.providers.length > 0
          ? payload.providers
          : Object.keys(payload.scope ?? {});
      if (errorMessage.includes('provider') && errorMessage.includes('unavailable')) {
        const matchedProvider =
          providerNames.find((name) =>
            errorMessage.toLowerCase().includes(name.toLowerCase()),
          ) ?? providerNames[0] ?? 'selected provider';
        setFormError(
          `Provider "${matchedProvider}" is not running. Start the provider and try again.`,
        );
      } else if (errorMessage.includes('scope assignments are immutable')) {
        setFormError(
          'Provider and symbol assignments cannot be changed after creation. Only strategy configuration can be modified.',
        );
      } else {
        setFormError(
          errorMessage ||
            (dialogMode === 'edit'
              ? 'Failed to update instance'
              : 'Failed to create instance'),
        );
      }
    } finally {
      setDialogSaving(false);
    }
  };

  const handleStart = async (instance: InstanceSummary) => {
    const trimmedId = instance.id.trim();
    if (!trimmedId) {
      return;
    }
    if (providerStatusReady) {
      const availability = evaluateProviderAvailability(instance.providers ?? []);
      if (availability.missing.length > 0 || availability.stopped.length > 0) {
        const description =
          formatProviderIssueMessage(availability) ??
          'Resolve provider issues before starting this instance.';
        showToast({
          title: 'Provider unavailable',
          description,
          variant: 'destructive',
        });
        return;
      }
    }
    const actionKey = `start-${trimmedId}`;
    setActionInProgress(prev => ({ ...prev, [actionKey]: true }));
    try {
      await startInstanceMutation.mutateAsync(trimmedId);
    } catch (err) {
      if (err instanceof Error && err.message.includes('provider') && err.message.includes('unavailable')) {
        showToast({
          title: 'Provider unavailable',
          description: `${err.message}. Start the provider before starting this instance.`,
          variant: 'destructive',
        });
      }
    } finally {
      setActionInProgress(prev => ({ ...prev, [actionKey]: false }));
    }
  };

  const handleStop = async (id: string) => {
    setActionInProgress(prev => ({ ...prev, [`stop-${id}`]: true }));
    try {
      await stopInstanceMutation.mutateAsync(id);
    } catch (err) {
      if (err instanceof Error) {
        showToast({
          title: 'Failed to stop instance',
          description: err.message,
          variant: 'destructive',
        });
      }
    } finally {
      setActionInProgress(prev => ({ ...prev, [`stop-${id}`]: false }));
    }
  };

  const performDelete = async (id: string) => {
    setActionInProgress(prev => ({ ...prev, [`delete-${id}`]: true }));
    try {
      await deleteInstanceMutation.mutateAsync(id);
      setConfirmState(null);
    } catch (err) {
      if (err instanceof Error) {
        showToast({
          title: 'Failed to delete instance',
          description: err.message,
          variant: 'destructive',
        });
      }
      setConfirmState(null);
    } finally {
      setActionInProgress(prev => ({ ...prev, [`delete-${id}`]: false }));
    }
  };
  const handleDelete = (id: string) => {
    const editingTarget = createDialogOpen && dialogMode === 'edit' && editingInstanceId === id;
    if (editingTarget) {
      setCreateDialogOpen(false);
      resetForm();
      setTimeout(() => setConfirmState({ type: 'delete-instance', id }), 0);
      return;
    }
    setConfirmState({ type: 'delete-instance', id });
  };

  const copyValue = async (value: string, label: string) => {
    if (!value) {
      return;
    }
    try {
      if (typeof navigator === 'undefined' || !navigator.clipboard) {
        throw new Error('Clipboard API unavailable');
      }
      await navigator.clipboard.writeText(value);
      showToast({
        title: `${label} copied`,
        description: value,
        variant: 'success',
      });
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Copy failed';
      showToast({
        title: 'Copy failed',
        description: message,
        variant: 'destructive',
      });
    }
  };


  const handleHistoryOpen = (instance: InstanceSummary) => {
    if (!instance?.id) {
      return;
    }
    setHistoryDialogInstance(instance);
    setHistoryDialogOpen(true);
    setHistoryTab('orders');
  };

  const handleHistoryRefresh = () => {
    if (historyTab === 'orders') {
      void ordersHistoryQuery.refetch();
      return;
    }
    if (historyTab === 'executions') {
      void executionsHistoryQuery.refetch();
      return;
    }
    void balancesHistoryQuery.refetch();
  };

  const handleEdit = async (id: string) => {
    setDialogMode('edit');
    setEditingInstanceId(id);
    setPrefilledConfig(true);
    setFormError(null);
    setConfigValues({});
    setInstanceLoading(true);
    setCreateDialogOpen(true);
    try {
      const instance = await loadInstance(id);
      populateFormFromSpec(instance, { setPrefilled: true });
      setInstanceJsonDraft(formatInstanceSpec(instance));
      setFormMode('json');
      setLastEditedMode('json');
    } catch (err) {
      setFormError(err instanceof Error ? err.message : 'Failed to load instance');
      setPrefilledConfig(false);
    } finally {
      setInstanceLoading(false);
    }
  };

  if (baseLoading) {
    return <div>Loading instances...</div>;
  }

  if (baseError) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{baseError.message}</AlertDescription>
      </Alert>
    );
  }

  const renderHistorySection = (tab: HistoryTab) => {
    if (!historyDialogInstance) {
      return (
        <div className="py-6 text-sm text-muted-foreground">
          Select an instance to view activity.
        </div>
      );
    }

    const sectionState =
      tab === 'orders'
        ? {
            records: (ordersHistoryQuery.data?.orders ?? []) as OrderRecord[],
            isLoading: ordersHistoryQuery.isLoading,
            isFetching: ordersHistoryQuery.isFetching,
            isFetched: ordersHistoryQuery.isFetched,
            error: ordersHistoryQuery.error as Error | null,
          }
        : tab === 'executions'
          ? {
              records: (executionsHistoryQuery.data?.executions ?? []) as ExecutionRecord[],
              isLoading: executionsHistoryQuery.isLoading,
              isFetching: executionsHistoryQuery.isFetching,
              isFetched: executionsHistoryQuery.isFetched,
              error: executionsHistoryQuery.error as Error | null,
            }
          : {
              records: (balancesHistoryQuery.data?.balances ?? []) as BalanceRecord[],
              isLoading: balancesHistoryQuery.isLoading,
              isFetching: balancesHistoryQuery.isFetching,
              isFetched: balancesHistoryQuery.isFetched ?? false,
              error: balancesHistoryQuery.error as Error | null,
            };

    if (sectionState.error) {
      return (
        <Alert variant="destructive">
          <AlertTitle>Failed to load {tab}</AlertTitle>
          <AlertDescription>{sectionState.error.message}</AlertDescription>
        </Alert>
      );
    }

    if (sectionState.isLoading && !sectionState.isFetched) {
      return (
        <div className="flex items-center gap-2 py-6 text-sm text-muted-foreground">
          <Loader2Icon className="h-4 w-4 animate-spin" />
          Loading {tab}…
        </div>
      );
    }

    if (sectionState.isFetched && sectionState.records.length === 0) {
      return (
        <p className="py-6 text-sm text-muted-foreground">
          No {tab === 'balances' ? 'balance' : tab} records yet.
        </p>
      );
    }

    const loadingOverlay =
      sectionState.isFetching && sectionState.isFetched ? (
        <div className="mb-2 flex items-center gap-2 text-xs text-muted-foreground">
          <Loader2Icon className="h-3 w-3 animate-spin" />
          Refreshing {tab}…
        </div>
      ) : null;

    if (tab === 'orders') {
      const records = sectionState.records as OrderRecord[];
      return (
        <>
          {loadingOverlay}
          <div className="overflow-x-auto rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Order</TableHead>
                  <TableHead>Client ID</TableHead>
                  <TableHead>Provider</TableHead>
                  <TableHead>Symbol</TableHead>
                  <TableHead>Side</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Quantity</TableHead>
                  <TableHead>Price</TableHead>
                  <TableHead>State</TableHead>
                  <TableHead>Placed</TableHead>
                  <TableHead>Acknowledged</TableHead>
                  <TableHead>Completed</TableHead>
                  <TableHead>Metadata</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {records.map((record) => (
                  <TableRow
                    key={`${record.id}-${record.provider}-${record.createdAt}`}
                  >
                    <TableCell className="font-mono text-xs">{record.id}</TableCell>
                    <TableCell className="font-mono text-xs">{record.clientOrderId}</TableCell>
                    <TableCell>{record.provider}</TableCell>
                    <TableCell>{record.symbol}</TableCell>
                    <TableCell className="capitalize">{record.side}</TableCell>
                    <TableCell className="uppercase">{record.type}</TableCell>
                    <TableCell>{record.quantity}</TableCell>
                    <TableCell>{record.price ?? '—'}</TableCell>
                    <TableCell className="capitalize">{record.state}</TableCell>
                    <TableCell>{formatDateTime(record.placedAt)}</TableCell>
                    <TableCell>{formatDateTime(record.acknowledgedAt ?? null)}</TableCell>
                    <TableCell>{formatDateTime(record.completedAt ?? null)}</TableCell>
                    <TableCell className="max-w-[220px] truncate font-mono text-[11px]">
                      {formatMetadata(record.metadata)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        </>
      );
    }

    if (tab === 'executions') {
      const records = sectionState.records as ExecutionRecord[];
      return (
        <>
          {loadingOverlay}
          <div className="overflow-x-auto rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Execution</TableHead>
                  <TableHead>Order</TableHead>
                  <TableHead>Provider</TableHead>
                  <TableHead>Quantity</TableHead>
                  <TableHead>Price</TableHead>
                  <TableHead>Fee</TableHead>
                  <TableHead>Liquidity</TableHead>
                  <TableHead>Traded</TableHead>
                  <TableHead>Metadata</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {records.map((record) => (
                  <TableRow
                    key={`${record.executionId}-${record.orderId}-${record.createdAt}`}
                  >
                    <TableCell className="font-mono text-xs">{record.executionId}</TableCell>
                    <TableCell className="font-mono text-xs">{record.orderId}</TableCell>
                    <TableCell>{record.provider}</TableCell>
                    <TableCell>{record.quantity}</TableCell>
                    <TableCell>{record.price}</TableCell>
                    <TableCell>
                      {record.fee ? `${record.fee}${record.feeAsset ? ` ${record.feeAsset}` : ''}` : '—'}
                    </TableCell>
                    <TableCell>{record.liquidity || '—'}</TableCell>
                    <TableCell>{formatDateTime(record.tradedAt)}</TableCell>
                    <TableCell className="max-w-[220px] truncate font-mono text-[11px]">
                      {formatMetadata(record.metadata)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        </>
      );
    }

    const records = sectionState.records as BalanceRecord[];
    return (
      <>
        {loadingOverlay}
        <div className="overflow-x-auto rounded-md border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Provider</TableHead>
                <TableHead>Asset</TableHead>
                <TableHead>Total</TableHead>
                <TableHead>Available</TableHead>
                <TableHead>Snapshot</TableHead>
                <TableHead>Updated</TableHead>
                <TableHead>Metadata</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {records.map((record, index) => (
                <TableRow
                  key={`${record.provider}-${record.asset}-${record.snapshotAt}-${index}`}
                >
                  <TableCell>{record.provider}</TableCell>
                  <TableCell>{record.asset}</TableCell>
                  <TableCell>{record.total}</TableCell>
                  <TableCell>{record.available}</TableCell>
                  <TableCell>{formatDateTime(record.snapshotAt)}</TableCell>
                  <TableCell>{formatDateTime(record.updatedAt)}</TableCell>
                  <TableCell className="max-w-[220px] truncate font-mono text-[11px]">
                    {formatMetadata(record.metadata)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </>
    );
  };

  const historySectionLoading =
    historyTab === 'orders'
      ? ordersHistoryQuery.isFetching
      : historyTab === 'executions'
        ? executionsHistoryQuery.isFetching
        : balancesHistoryQuery.isFetching;

  const confirmOpen = Boolean(confirmState);
  const confirmLoading =
    confirmState?.type === 'delete-instance'
      ? Boolean(actionInProgress[`delete-${confirmState.id}`])
      : false;

  return (
    <>
      <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Strategy Instances</h1>
          <p className="text-muted-foreground">
            Manage running strategy instances with full lifecycle control
          </p>
        </div>
        <Dialog
          open={createDialogOpen}
          onOpenChange={(open) => {
            setCreateDialogOpen(open);
            if (!open) {
              resetForm();
            }
          }}
        >
          <DialogTrigger asChild>
            <Button
              onClick={() => {
                resetForm();
              }}
            >
              <PlusIcon className="mr-2 h-4 w-4" />
              Create Instance
            </Button>
          </DialogTrigger>
          <DialogContent
            className="max-w-2xl sm:max-w-3xl flex min-h-0 flex-col"
            style={{ height: 'min(85vh, 720px)' }}
          >
            <DialogHeader>
              <DialogTitle>
                {dialogMode === 'create' ? 'Create Strategy Instance' : 'Edit Strategy Instance'}
              </DialogTitle>
              <DialogDescription>
                {dialogMode === 'create'
                  ? 'Configure a new trading strategy instance'
                  : 'Update the configuration for this trading strategy instance'}
              </DialogDescription>
            </DialogHeader>
            {formError && (
              <Alert variant="destructive">
                <AlertDescription>{formError}</AlertDescription>
              </Alert>
            )}
            <ScrollArea className="flex-1 min-h-0 h-full" type="auto">
              {instanceLoading ? (
                <div className="flex items-center justify-center py-10 text-muted-foreground">
                  <Loader2Icon className="mr-2 h-5 w-5 animate-spin" />
                  Loading instance...
                </div>
              ) : (
                <div className="grid gap-4 py-4 pr-1">
                  <Tabs value={formMode} onValueChange={handleFormModeChange}>
                    <TabsList className="grid w-full grid-cols-2">
                      <TabsTrigger value="json">JSON spec</TabsTrigger>
                      <TabsTrigger value="guided">Guided form</TabsTrigger>
                    </TabsList>
                    <TabsContent value="json" className="space-y-4 pt-4">
                      <div className="space-y-2">
                        <Label htmlFor="instance-json-editor">Instance specification</Label>
                        <p className="text-sm text-muted-foreground">
                          Provide the persisted strategy instance payload, including strategy selector, configuration values, and provider scope assignments.
                        </p>
                        <CodeEditor
                          id="instance-json-editor"
                          value={instanceJsonDraft}
                          onChange={(value) => {
                            setInstanceJsonDraft(value);
                            setFormError(null);
                            markJsonDirty();
                          }}
                          mode="json"
                          allowHorizontalScroll
                          wrapEnabled={false}
                          height="18rem"
                          className="rounded-md border"
                        />
                        <p className={jsonDiagnosticClass}>{jsonDiagnostics.message}</p>
                      </div>
                      <div className="space-y-2 rounded-md border bg-muted/40 p-3 text-sm text-muted-foreground">
                        <p className="font-medium text-foreground">Helpful context</p>
                        {providers.length === 0 ? (
                          <p>No providers are configured. Create and start a provider before assigning scope.</p>
                        ) : (
                          <ul className="list-disc space-y-1 pl-5">
                            {providers.map((provider) => (
                              <li key={provider.name}>
                                <span className="font-medium text-foreground">{provider.name}</span>{' '}
                                {provider.running ? '(running)' : '(stopped)'}
                              </li>
                            ))}
                          </ul>
                        )}
                        <p className="text-xs">
                          Scope entries map providers to uppercase instrument symbols, for example:
                        </p>
                        <pre className="overflow-x-auto rounded bg-background/70 p-2 text-xs font-mono text-foreground">
{`"scope": {
  "binance-demo": {
    "symbols": ["BTC-USDT"]
  }
}`}
                        </pre>
                      </div>
                    </TabsContent>
                    <TabsContent value="guided" className="space-y-4 pt-4">
                      <div className="grid gap-2">
                    <Label htmlFor="id">Instance ID</Label>
                    <Input
                      id="id"
                      value={newInstance.id}
                      onChange={(e) => {
                        setFormError(null);
                        setNewInstance({ ...newInstance, id: e.target.value });
                        markGuidedDirty();
                      }}
                      placeholder="my-strategy-instance"
                      disabled={dialogMode === 'edit'}
                    />
                  </div>
                  <div className="grid gap-2">
                    <Label htmlFor="strategy">
                      Strategy
                      {dialogMode === 'edit' && (
                        <span className="ml-2 text-xs font-normal text-muted-foreground">
                          (cannot be changed)
                        </span>
                      )}
                    </Label>
                    <Select
                      value={newInstance.strategyIdentifier}
                      onValueChange={(value) => {
                        if (dialogMode === 'edit') {
                          return;
                        }
                        setFormError(null);
                        setPrefilledConfig(false);
                        setNewInstance({ ...newInstance, strategyIdentifier: value });
                        if (
                          !strategySelectorInput.trim() ||
                          strategySelectorInput.trim() === newInstance.strategyIdentifier.trim()
                        ) {
                          setStrategySelectorInput(value);
                        }
                        markGuidedDirty();
                      }}
                    >
                      <SelectTrigger disabled={dialogMode === 'edit'}>
                        <SelectValue placeholder="Select strategy" />
                      </SelectTrigger>
                      <SelectContent>
                        {strategies.map((strategy) => (
                          <SelectItem key={strategy.name} value={strategy.name}>
                            {strategy.displayName}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                  <div className="grid gap-2">
                    <Label htmlFor="strategy-selector">Strategy selector</Label>
                    <Input
                      id="strategy-selector"
                      value={strategySelectorInput}
                      onChange={(event) => {
                        setStrategySelectorInput(event.target.value);
                        if (dialogMode === 'create') {
                          const parsed = parseStrategySelector(event.target.value);
                          if (parsed.identifier) {
                            setNewInstance((prev) => ({
                              ...prev,
                              strategyIdentifier: parsed.identifier,
                            }));
                          }
                        }
                        markGuidedDirty();
                      }}
                      placeholder="grid, grid:stable, or grid@sha256:abc123"
                      disabled={dialogMode === 'edit' && instanceLoading}
                    />
                    <p className="text-xs text-muted-foreground">
                      Use <span className="font-mono">name</span>, <span className="font-mono">name:tag</span>, or <span className="font-mono">name@hash</span> to pin a revision.
                    </p>
                  </div>
                  <div className="grid gap-2">
                    <Label>
                      Providers
                      {dialogMode === 'edit' && (
                        <span className="ml-2 text-xs font-normal text-muted-foreground">
                          (cannot be changed)
                        </span>
                      )}
                    </Label>
                    {dialogMode === 'edit' ? (
                      <div className="space-y-3 rounded-md border bg-muted/30 p-3">
                        {selectedProviders.length === 0 ? (
                          <p className="text-sm italic text-muted-foreground">
                            No providers assigned to this instance.
                          </p>
                        ) : (
                          selectedProviders.map((name) => {
                            const symbols = providerSymbols[name] ?? [];
                            return (
                              <div key={name} className="space-y-1">
                                <p className="text-sm font-medium text-foreground">{name}</p>
                                <p className="text-xs text-muted-foreground">
                                  {symbols.length > 0 ? symbols.join(', ') : 'No symbols assigned'}
                                </p>
                              </div>
                            );
                          })
                        )}
                        <p className="text-xs text-muted-foreground">
                          Provider and symbol assignments cannot be changed after creation.
                        </p>
                      </div>
                    ) : (
                      <div className="space-y-4 rounded-md border p-3">
                        <div className="space-y-2">
                          {providers.length === 0 ? (
                            <p className="text-sm text-muted-foreground">
                              No providers are configured yet. Create and start a provider from the
                              Providers page before creating an instance.
                            </p>
                          ) : (
                            <>
                              {providers.map((provider) => {
                              const checked = selectedProviders.includes(provider.name);
                              return (
                                <label
                                  key={provider.name}
                                  className="flex items-center gap-2 text-sm text-foreground"
                                >
                                  <Checkbox
                                    checked={checked}
                                    onChange={(event) => {
                                      if (!provider.running) {
                                        return;
                                      }
                                      setFormError(null);
                                      toggleProviderSelection(provider.name, event.target.checked);
                                    }}
                                    disabled={!provider.running}
                                  />
                                  <span className={provider.running ? '' : 'text-muted-foreground'}>
                                    {provider.running
                                      ? provider.name
                                      : `${provider.name} (stopped)`}
                                  </span>
                                </label>
                              );
                            })}
                              {providers.some((provider) => !provider.running) ? (
                                <p className="text-xs text-muted-foreground">
                                  Start a provider from the Providers page to enable it here.
                                </p>
                              ) : null}
                            </>
                          )}
                        </div>
                        {selectedProviders.length > 0 && (
                          <div className="space-y-3">
                            {selectedProviders.map((providerName) => {
                              const instrumentState = providerInstrumentState[providerName];
                              const selectedSymbols = providerSymbols[providerName] ?? [];
                              const filterTerm = providerSymbolFilters[providerName] ?? '';
                              const availableSymbols = instrumentState?.symbols ?? [];
                              const filteredSymbols =
                                filterTerm.trim().length > 0
                                  ? availableSymbols.filter((symbol) =>
                                      symbol.toLowerCase().includes(filterTerm.trim().toLowerCase()),
                                    )
                                  : availableSymbols;

                              return (
                                <div key={providerName} className="space-y-2">
                                  <div className="flex items-center justify-between gap-2">
                                    <Label
                                      htmlFor={`symbols-${providerName}`}
                                      className="text-xs uppercase text-muted-foreground"
                                    >
                                      Symbols for {providerName}
                                    </Label>
                                    {selectedSymbols.length > 0 ? (
                                      <span className="text-xs text-muted-foreground">
                                        {selectedSymbols.length} selected
                                      </span>
                                    ) : null}
                                  </div>
                                  <div className="space-y-2 rounded-md border p-3">
                                    {instrumentState?.loading ? (
                                      <div className="flex items-center justify-center gap-2 text-xs text-muted-foreground">
                                        <Loader2Icon className="h-4 w-4 animate-spin" />
                                        Loading symbols...
                                      </div>
                                    ) : instrumentState?.error ? (
                                      <div className="space-y-2">
                                        <p className="text-xs text-destructive">
                                          {instrumentState.error}
                                        </p>
                                        <Button
                                          type="button"
                                          variant="outline"
                                          size="sm"
                                          onClick={() => {
                                            setFormError(null);
                                          void loadProviderInstrumentSymbols(providerName);
                                          }}
                                        >
                                          Retry
                                        </Button>
                                      </div>
                                    ) : availableSymbols.length === 0 ? (
                                      <p className="text-xs text-muted-foreground">
                                        No symbols available for this provider.
                                      </p>
                                    ) : (
                                      <>
                                        <Input
                                          id={`symbols-${providerName}`}
                                          value={filterTerm}
                                          onChange={(event) => {
                                            const { value } = event.target;
                                            setProviderSymbolFilters((prev) => ({
                                              ...prev,
                                              [providerName]: value,
                                            }));
                                          }}
                                          placeholder="Search symbols"
                                        />
                                        <ScrollArea
                                          className="pr-1"
                                          type="auto"
                                          aria-label={`${providerName} available symbols`}
                                          viewportClassName="max-h-48"
                                        >
                                          <div className="space-y-1">
                                            {filteredSymbols.length === 0 ? (
                                              <p className="text-xs text-muted-foreground">
                                                No matching symbols found.
                                              </p>
                                            ) : (
                                              filteredSymbols.map((symbol) => {
                                                const checked = selectedSymbols.includes(symbol);
                                                return (
                                                  <label
                                                    key={symbol}
                                                    className="flex items-center gap-2 text-sm text-foreground"
                                                  >
                                                    <Checkbox
                                                      checked={checked}
                                                      onChange={(event) => {
                                                        const { checked: symbolChecked } = event.target;
                                                        setFormError(null);
                                                        let changed = false;
                                                        setProviderSymbols((prev) => {
                                                          const current = new Set(prev[providerName] ?? []);
                                                          if (symbolChecked) {
                                                            if (current.has(symbol)) {
                                                              return prev;
                                                            }
                                                            current.add(symbol);
                                                            changed = true;
                                                          } else if (current.has(symbol)) {
                                                            current.delete(symbol);
                                                            changed = true;
                                                          } else {
                                                            return prev;
                                                          }
                                                          return {
                                                            ...prev,
                                                            [providerName]: Array.from(current).sort((a, b) =>
                                                              a.localeCompare(b)
                                                            ),
                                                          };
                                                        });
                                                        if (changed) {
                                                          markGuidedDirty();
                                                        }
                                                      }}
                                                    />
                                                    <span>{symbol}</span>
                                                  </label>
                                                );
                                              })
                                            )}
                                          </div>
                                        </ScrollArea>
                                        {selectedSymbols.length > 0 ? (
                                          <div className="flex flex-wrap gap-1 pt-1">
                                            {selectedSymbols.map((symbol) => (
                                              <Badge key={symbol} variant="secondary">
                                                {symbol}
                                              </Badge>
                                            ))}
                                          </div>
                                        ) : null}
                                      </>
                                    )}
                                  </div>
                                </div>
                              );
                            })}
                          </div>
                        )}
                        <p className="text-xs text-muted-foreground">
                          Select one or more providers and choose symbols from the catalog for each.
                        </p>
                      </div>
                    )}
                  </div>
                  {selectedStrategy && selectedStrategy.config.length > 0 && (
                    <div className="grid gap-3">
                      <div className="text-sm font-medium">Configuration</div>
                      <div className="grid gap-4">
                        {selectedStrategy.config.map((field) => {
                          const value = configValues[field.name] ?? '';
                          return (
                            <div className="grid gap-2" key={field.name}>
                              <Label htmlFor={`config-${field.name}`}>
                                {field.name}
                                {!field.required && (
                                  <span className="ml-1 text-xs font-normal text-muted-foreground">
                                    (optional)
                                  </span>
                                )}
                              </Label>
                              {field.type === 'bool' ? (
                                <Select
                                  value={value || 'false'}
                                  onValueChange={(val) => handleConfigChange(field.name, val)}
                                >
                                  <SelectTrigger>
                                    <SelectValue placeholder="Select value" />
                                  </SelectTrigger>
                                  <SelectContent>
                                    <SelectItem value="true">True</SelectItem>
                                    <SelectItem value="false">False</SelectItem>
                                  </SelectContent>
                                </Select>
                              ) : (
                                <Input
                                  id={`config-${field.name}`}
                                  type={field.type === 'int' || field.type === 'float' ? 'number' : 'text'}
                                  step={field.type === 'float' ? 'any' : undefined}
                                  value={value}
                                  onChange={(e) => handleConfigChange(field.name, e.target.value)}
                                  placeholder={
                                    field.default !== undefined && field.default !== null
                                      ? String(field.default)
                                      : undefined
                                  }
                                />
                              )}
                              {field.description && (
                                <p className="text-xs text-muted-foreground">{field.description}</p>
                              )}
                            </div>
                          );
                        })}
                      </div>
                    </div>
                  )}
                    </TabsContent>
                  </Tabs>
                </div>
              )}
            </ScrollArea>
            <DialogFooter>
              <Button
                variant="outline"
                onClick={() => {
                  resetForm();
                  setCreateDialogOpen(false);
                }}
                disabled={dialogSaving}
              >
                Cancel
              </Button>
              <Button onClick={handleSubmit} disabled={submitDisabled} aria-disabled={submitDisabled}>
                {dialogSaving ? (
                  <>
                    <Loader2Icon className="mr-2 h-4 w-4 animate-spin" />
                    Saving
                  </>
                ) : (
                  dialogMode === 'create' ? 'Create' : 'Save changes'
                )}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {instances.map((instance) => {
          const moduleStatus = moduleStatusByName.get(
            instance.strategyIdentifier.trim().toLowerCase(),
          );
          const latestHash = moduleStatus?.latestHash ?? null;
          const pinnedHash = moduleStatus?.pinnedHash ?? null;
          const latestTag = moduleStatus?.latestTag ?? null;
          const selectorDisplay = instance.strategySelector || instance.strategyIdentifier;
          const drift = Boolean(
            latestHash &&
              instance.strategyHash &&
              latestHash.toLowerCase() !== instance.strategyHash.toLowerCase(),
          );
          const usageSummary = instance.usage ?? null;
          const usageSelector = canonicalUsageSelector(
            instance.strategyIdentifier,
            usageSummary?.hash ?? instance.strategyHash ?? null,
            instance.strategyTag ?? null,
          );
          const usageLink = `/strategies/modules?usage=${encodeURIComponent(usageSelector)}`;
          const isBaseline = Boolean(instance.baseline);
          const isDynamic = Boolean(instance.dynamic);
          const providerAvailability = providerStatusReady
            ? evaluateProviderAvailability(instance.providers)
            : { missing: [], stopped: [] };
          const providerIssuesPresent =
            providerStatusReady &&
            (providerAvailability.missing.length > 0 || providerAvailability.stopped.length > 0);
          const providerIssueHint = providerIssuesPresent
            ? formatProviderIssueMessage(providerAvailability)
            : null;
          const startBlocked = !instance.running && Boolean(providerIssueHint);
          const totalProviders = instance.providers.length;
          const missingCount = providerAvailability.missing.length;
          const stoppedCount = providerAvailability.stopped.length;
          const readyCount = Math.max(totalProviders - (missingCount + stoppedCount), 0);
          const providerSegments =
            totalProviders > 0
              ? [
                  { label: 'Ready', value: readyCount, color: 'success' as const },
                  { label: 'Missing', value: missingCount, color: 'warning' as const },
                  { label: 'Stopped', value: stoppedCount, color: 'destructive' as const },
                ]
              : [];
          return (
            <Card key={instance.id}>
              <CardHeader>
                <div className="flex flex-col gap-2">
                  <div className="flex flex-wrap items-center justify-between gap-3">
                    <CardTitle>{instance.id}</CardTitle>
                    <div className="flex flex-wrap items-center justify-end gap-2">
                      {isBaseline ? <Badge variant="warning">Baseline</Badge> : null}
                      {isDynamic && !isBaseline ? <Badge variant="info">Dynamic</Badge> : null}
                      {drift ? <Badge variant="warning">Out of date</Badge> : null}
                      <Badge variant={instance.running ? 'success' : 'muted'}>
                        {instance.running ? 'Running' : 'Stopped'}
                      </Badge>
                    </div>
                  </div>
                  <p className="text-xs text-muted-foreground">
                    {instance.providers.length} provider{instance.providers.length === 1 ? '' : 's'} ·{' '}
                    {instance.aggregatedSymbols.length} instrument
                    {instance.aggregatedSymbols.length === 1 ? '' : 's'}
                  </p>
                  {providerSegments.length > 0 ? (
                    <div className="space-y-1 text-[11px] text-muted-foreground">
                      <StackedBarChart segments={providerSegments} />
                      <ChartLegend segments={providerSegments} className="text-[11px]" />
                    </div>
                  ) : null}
                </div>
              </CardHeader>
              <CardContent className="space-y-4">
                {isBaseline ? (
                  <Alert variant="info">
                    <AlertDescription className="text-xs">
                      Baseline instances come from persisted specifications. Editing and deletion are disabled here.
                    </AlertDescription>
                  </Alert>
                ) : null}
                <div className="text-sm space-y-2">
                  <div>
                    <span className="font-medium">Strategy:</span>{' '}
                    <span className="text-muted-foreground">{instance.strategyIdentifier}</span>
                  </div>
                  <div>
                    <span className="font-medium">Selector:</span>{' '}
                    <span className="font-mono text-xs text-muted-foreground">{selectorDisplay}</span>
                  </div>
                  <div>
                    <span className="font-medium">Tag:</span>{' '}
                    <span className="text-muted-foreground">{instance.strategyTag || latestTag || '—'}</span>
                  </div>
                  <div>
                    <span className="font-medium">Version:</span>{' '}
                    <span className="text-muted-foreground">{instance.strategyVersion || '—'}</span>
                  </div>
                  <div className="flex items-center gap-2">
                    <span className="font-medium">Hash:</span>
                    {instance.strategyHash ? (
                      <button
                        type="button"
                        className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
                        onClick={() => void copyValue(instance.strategyHash, 'Strategy hash')}
                      >
                        <span className="font-mono">{formatHash(instance.strategyHash, 18)}</span>
                        <Copy className="h-3 w-3" />
                      </button>
                    ) : (
                      <span className="text-muted-foreground">—</span>
                    )}
                  </div>
                  {latestHash ? (
                    <div className="text-xs text-muted-foreground">
                      Latest revision: <span className="font-mono">{formatHash(latestHash, 18)}</span>
                      {latestTag ? ` (${latestTag})` : ''}
                      {pinnedHash && pinnedHash !== latestHash ? (
                        <span>
                          {' '}| Pinned: <span className="font-mono">{formatHash(pinnedHash, 18)}</span>
                        </span>
                      ) : null}
                    </div>
                  ) : null}
                  <div>
                    <span className="font-medium">Providers:</span>
                    <div className="mt-1 flex flex-wrap gap-1">
                      {instance.providers.length > 0 ? (
                        instance.providers.map((provider) => (
                          <Badge key={provider} variant="outline">
                            {provider}
                          </Badge>
                        ))
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </div>
                    {providerIssueHint ? (
                      <p className="mt-1 text-xs text-destructive">{providerIssueHint}</p>
                    ) : null}
                  </div>
                  <div>
                    <span className="font-medium">Symbols:</span>
                    <div className="mt-1 flex flex-wrap gap-1">
                      {instance.aggregatedSymbols.length > 0 ? (
                        instance.aggregatedSymbols.map((symbol) => (
                          <Badge key={symbol} variant="outline">
                            {symbol}
                          </Badge>
                        ))
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </div>
                  </div>
                  {usageSummary ? (
                    <div className="rounded-md border px-3 py-2 text-xs shadow-sm">
                      <div className="flex flex-wrap items-center justify-between gap-3">
                        <div className="space-y-1">
                          <p className="text-muted-foreground">
                            <span className="font-medium text-foreground">{usageSummary.count}</span>{' '}
                            running instance{usageSummary.count === 1 ? '' : 's'} pinned to this hash.
                          </p>
                          <p className="text-muted-foreground">
                            First seen {formatDateTime(usageSummary.firstSeen)} · Last seen {formatDateTime(usageSummary.lastSeen)}
                          </p>
                        </div>
                        <Button variant="outline" size="sm" asChild>
                          <Link href={usageLink}>View usage</Link>
                        </Button>
                      </div>
                    </div>
                  ) : null}
                </div>
                {drift ? (
                  <Alert variant="warning">
                    <AlertTitle>Revision drift detected</AlertTitle>
                    <AlertDescription>
                      Latest module hash {formatHash(latestHash, 18)} differs from the instance hash{' '}
                      {formatHash(instance.strategyHash, 18)}. Stop and restart this instance to deploy
                      the latest revision.
                    </AlertDescription>
                  </Alert>
                ) : null}
                <div className="flex flex-wrap gap-2">
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => handleHistoryOpen(instance)}
                    disabled={anyActionInFlight}
                  >
                    <Clock3Icon className="mr-1 h-3 w-3" />
                    History
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => handleEdit(instance.id)}
                    disabled={anyActionInFlight || isBaseline}
                    title={isBaseline ? 'Baseline instances cannot be edited here' : undefined}
                  >
                    <PencilIcon className="mr-1 h-3 w-3" />
                    Edit
                  </Button>
                  {instance.running ? (
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => handleStop(instance.id)}
                      disabled={actionInProgress[`stop-${instance.id}`] || anyActionInFlight}
                    >
                      {actionInProgress[`stop-${instance.id}`] ? (
                        <Loader2Icon className="mr-1 h-3 w-3 animate-spin" />
                      ) : (
                        <CircleStopIcon className="mr-1 h-3 w-3" />
                      )}
                      Stop
                    </Button>
                  ) : (
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => handleStart(instance)}
                      disabled={
                        actionInProgress[`start-${instance.id}`] ||
                        anyActionInFlight ||
                        startBlocked
                      }
                      title={startBlocked ? providerIssueHint ?? undefined : undefined}
                    >
                      {actionInProgress[`start-${instance.id}`] ? (
                        <Loader2Icon className="mr-1 h-3 w-3 animate-spin" />
                      ) : (
                        <PlayIcon className="mr-1 h-3 w-3" />
                      )}
                      Start
                    </Button>
                  )}
                  <Button
                    size="sm"
                    variant="destructive"
                    onClick={() => handleDelete(instance.id)}
                    disabled={isBaseline || actionInProgress[`delete-${instance.id}`] || anyActionInFlight}
                    title={isBaseline ? 'Baseline instances cannot be deleted' : undefined}
                  >
                    {actionInProgress[`delete-${instance.id}`] ? (
                      <Loader2Icon className="mr-1 h-3 w-3 animate-spin" />
                    ) : (
                      <TrashIcon className="mr-1 h-3 w-3" />
                    )}
                    Delete
                  </Button>
                </div>
              </CardContent>
            </Card>
          );
        })}
      </div>

      {instances.length === 0 && (
        <Card>
          <CardContent className="py-10 text-center text-muted-foreground">
            No strategy instances configured. Create one to get started.
          </CardContent>
        </Card>
      )}
      <Dialog
        open={historyDialogOpen}
        onOpenChange={(open) => {
          setHistoryDialogOpen(open);
          if (!open) {
            setHistoryDialogInstance(null);
            setHistoryTab('orders');
          }
        }}
      >
        <DialogContent className="max-w-5xl">
          <DialogHeader>
            <DialogTitle>
              Instance history{historyDialogInstance ? ` · ${historyDialogInstance.id}` : ''}
            </DialogTitle>
            <DialogDescription>
              Inspect persisted orders, executions, and provider balances restored from PostgreSQL.
            </DialogDescription>
          </DialogHeader>
          {historyDialogInstance ? (
            <>
              <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
                <div className="space-x-2">
                  {historyDialogInstance.providers.length > 0 ? (
                    <>
                      <span>Providers:</span>
                      <span>{historyDialogInstance.providers.join(', ')}</span>
                    </>
                  ) : (
                    <span>No providers assigned</span>
                  )}
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  {historyDialogInstance.baseline ? <Badge variant="warning">Baseline</Badge> : null}
                  {historyDialogInstance.dynamic ? <Badge variant="info">Dynamic</Badge> : null}
                </div>
              </div>
              <Tabs value={historyTab} onValueChange={(value) => setHistoryTab(value as HistoryTab)}>
                <TabsList>
                  <TabsTrigger value="orders">Orders</TabsTrigger>
                  <TabsTrigger value="executions">Executions</TabsTrigger>
                  <TabsTrigger value="balances">Balances</TabsTrigger>
                </TabsList>
                <div className="mt-3 flex flex-wrap items-center justify-between gap-2">
                  <p className="text-xs text-muted-foreground">
                    Showing up to {HISTORY_LIMITS[historyTab]} recent {historyTab}.
                  </p>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={handleHistoryRefresh}
                    disabled={!historyDialogInstance || historySectionLoading}
                  >
                    {historySectionLoading ? (
                      <Loader2Icon className="mr-2 h-3 w-3 animate-spin" />
                    ) : (
                      <RotateCcwIcon className="mr-2 h-3 w-3" />
                    )}
                    Refresh
                  </Button>
                </div>
                <TabsContent value="orders">{renderHistorySection('orders')}</TabsContent>
                <TabsContent value="executions">{renderHistorySection('executions')}</TabsContent>
                <TabsContent value="balances">{renderHistorySection('balances')}</TabsContent>
              </Tabs>
            </>
          ) : (
            <p className="py-6 text-sm text-muted-foreground">Select an instance to view history.</p>
          )}
        </DialogContent>
      </Dialog>
      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={(open) => {
          if (!open) {
            setConfirmState(null);
          }
        }}
        title="Delete instance?"
        description={
          confirmState ? (
            <span>
              This action will permanently remove{' '}
              <span className="font-medium text-foreground">{confirmState.id}</span>.
            </span>
          ) : undefined
        }
        confirmLabel="Delete"
        confirmVariant="destructive"
        loading={confirmLoading}
        confirmDisabled={confirmLoading}
        onConfirm={() => {
          if (confirmState) {
            void performDelete(confirmState.id);
          }
        }}
      />
    </div>
    </>
  );
}
