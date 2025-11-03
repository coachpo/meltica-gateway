'use client';

import { useCallback, useEffect, useMemo, useState } from 'react';
import { apiClient } from '@/lib/api-client';
import { InstanceSummary, Strategy, Provider, StrategyModuleSummary } from '@/lib/types';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { CircleStopIcon, PlayIcon, PlusIcon, TrashIcon, PencilIcon, Loader2Icon, Copy } from 'lucide-react';
import { useToast } from '@/components/ui/toast-provider';
import { Checkbox } from '@/components/ui/checkbox';
import { ConfirmDialog } from '@/components/confirm-dialog';

type ProviderInstrumentStatus = {
  symbols: string[];
  loading: boolean;
  error: string | null;
};

type ParsedSelector = {
  identifier: string;
  tag?: string;
  hash?: string;
};

function parseStrategySelector(raw: string): ParsedSelector {
  const selector = raw.trim();
  if (!selector) {
    return { identifier: '' };
  }
  if (selector.includes('@')) {
    const [identifierPart, hashPart] = selector.split('@');
    return {
      identifier: identifierPart.trim(),
      hash: hashPart.trim(),
    };
  }
  if (selector.includes(':')) {
    const [identifierPart, ...rest] = selector.split(':');
    return {
      identifier: identifierPart.trim(),
      tag: rest.join(':').trim(),
    };
  }
  return { identifier: selector };
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

export default function InstancesPage() {
  const [instances, setInstances] = useState<InstanceSummary[]>([]);
  const [strategies, setStrategies] = useState<Strategy[]>([]);
  const [providers, setProviders] = useState<Provider[]>([]);
  const [modules, setModules] = useState<StrategyModuleSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [dialogMode, setDialogMode] = useState<'create' | 'edit'>('create');
  const [editingInstanceId, setEditingInstanceId] = useState<string | null>(null);
  const [prefilledConfig, setPrefilledConfig] = useState(false);
  const [dialogSaving, setDialogSaving] = useState(false);
  const [instanceLoading, setInstanceLoading] = useState(false);
  const [actionInProgress, setActionInProgress] = useState<Record<string, boolean>>({});

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
      const detail = await apiClient.getProvider(providerName);
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
  }, []);

  const toggleProviderSelection = (providerName: string, checked: boolean) => {
    setSelectedProviders((prev) => {
      if (checked) {
        if (prev.includes(providerName)) {
          return prev;
        }
        return [...prev, providerName];
      }
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
  };

  const handleConfigChange = (field: string, value: string) => {
    setConfigValues((prev) => ({ ...prev, [field]: value }));
    setFormError(null);
  };

  useEffect(() => {
    fetchData();
  }, []);

  const fetchData = async () => {
    try {
      const [instancesRes, strategiesRes, providersRes, modulesRes] = await Promise.all([
        apiClient.getInstances(),
        apiClient.getStrategies(),
        apiClient.getProviders(),
        apiClient.getStrategyModules(),
      ]);
      setInstances(instancesRes.instances);
      setStrategies(strategiesRes.strategies);
      setProviders(providersRes.providers);
      setModules(Array.isArray(modulesRes.modules) ? modulesRes.modules : []);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch data');
    } finally {
      setLoading(false);
    }
  };

  const handleSubmit = async () => {
    if (!newInstance.id.trim()) {
      setFormError('Instance ID is required');
      return;
    }
    if (!newInstance.strategyIdentifier) {
      setFormError('Strategy selection is required');
      return;
    }
    if (selectedProviders.length === 0) {
      setFormError('Select at least one provider');
      return;
    }

    const selectorCandidate = strategySelectorInput.trim() || newInstance.strategyIdentifier.trim();
    const parsedSelector = parseStrategySelector(selectorCandidate);
    let identifier = parsedSelector.identifier || newInstance.strategyIdentifier.trim();
    if (!identifier) {
      setFormError('Strategy identifier is required');
      return;
    }
    if (
      dialogMode === 'edit' &&
      identifier.toLowerCase() !== newInstance.strategyIdentifier.trim().toLowerCase()
    ) {
      setFormError('Strategy identifier cannot be changed after creation.');
      return;
    }
    if (dialogMode === 'edit') {
      identifier = newInstance.strategyIdentifier.trim();
    }
    const tag = parsedSelector.tag?.trim() ? parsedSelector.tag.trim() : undefined;
    const hash = parsedSelector.hash?.trim() ? parsedSelector.hash.trim() : undefined;
    const selectorValue = hash
      ? `${identifier}@${hash}`
      : tag
        ? `${identifier}:${tag}`
        : identifier;

    const strategyMeta = strategies.find((strategy) => strategy.name === identifier);
    if (!strategyMeta) {
      setFormError('Strategy metadata is unavailable');
      return;
    }

    const scope: Record<string, { symbols: string[] }> = {};
    const allSymbols = new Set<string>();
    for (const providerName of selectedProviders) {
      const selectedSymbols = providerSymbols[providerName] ?? [];
      const parsed = Array.from(
        new Set(
          selectedSymbols
            .map((symbol) => symbol.trim().toUpperCase())
            .filter((symbol) => symbol.length > 0),
        ),
      );
      if (parsed.length === 0) {
        setFormError(`Provider "${providerName}" requires at least one symbol`);
        return;
      }
      parsed.forEach((symbol) => allSymbols.add(symbol));
      scope[providerName] = { symbols: parsed };
    }

    if (allSymbols.size === 0) {
      setFormError('At least one instrument symbol is required');
      return;
    }

    const configPayload: Record<string, unknown> = {};
    for (const field of strategyMeta.config) {
      const rawValue = configValues[field.name] ?? '';
      if (field.type === 'bool') {
        configPayload[field.name] = rawValue === 'true';
        continue;
      }
      if (rawValue === '') {
        if (field.required) {
          setFormError(`Configuration field "${field.name}" is required`);
          return;
        }
        continue;
      }
      if (field.type === 'int') {
        const parsed = parseInt(rawValue, 10);
        if (Number.isNaN(parsed)) {
          setFormError(`Configuration field "${field.name}" must be an integer`);
          return;
        }
        configPayload[field.name] = parsed;
        continue;
      }
      if (field.type === 'float') {
        const parsed = parseFloat(rawValue);
        if (Number.isNaN(parsed)) {
          setFormError(`Configuration field "${field.name}" must be a number`);
          return;
        }
        configPayload[field.name] = parsed;
        continue;
      }
      configPayload[field.name] = rawValue;
    }

    const payload = {
      id: newInstance.id.trim(),
      strategy: {
        identifier,
        selector: selectorValue,
        tag,
        hash,
        config: configPayload,
      },
      scope,
    };

    const mode = dialogMode;
    const targetId = newInstance.id.trim();
    try {
      setFormError(null);
      setDialogSaving(true);
      if (dialogMode === 'edit') {
        if (!editingInstanceId) {
          setFormError('No instance selected for update');
          setDialogSaving(false);
          return;
        }
        await apiClient.updateInstance(editingInstanceId, payload);
      } else {
        await apiClient.createInstance(payload);
      }
      setCreateDialogOpen(false);
      resetForm();
      await fetchData();
      showToast({
        title: mode === 'create' ? 'Instance created' : 'Instance updated',
        description: `Instance ${targetId} ${mode === 'create' ? 'created' : 'updated'} successfully.`,
        variant: 'success',
      });
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : '';
      if (errorMessage.includes('provider') && errorMessage.includes('unavailable')) {
        const matchedProvider =
          selectedProviders.find((name) =>
            errorMessage.toLowerCase().includes(name.toLowerCase()),
          ) ?? selectedProviders[0] ?? 'selected provider';
        setFormError(
          `Provider "${matchedProvider}" is not running. Start the provider and try creating the instance again.`,
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

  const handleStart = async (id: string) => {
    setActionInProgress(prev => ({ ...prev, [`start-${id}`]: true }));
    try {
      await apiClient.startInstance(id);
      await fetchData();
      showToast({
        title: 'Instance started',
        description: `${id} is now running.`,
        variant: 'success',
      });
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : `Failed to start ${id}`;
      const description = errorMessage.includes('provider') && errorMessage.includes('unavailable')
        ? `${errorMessage}. Start the provider before starting this instance.`
        : errorMessage;
      showToast({
        title: 'Failed to start instance',
        description,
        variant: 'destructive',
      });
    } finally {
      setActionInProgress(prev => ({ ...prev, [`start-${id}`]: false }));
    }
  };

  const handleStop = async (id: string) => {
    setActionInProgress(prev => ({ ...prev, [`stop-${id}`]: true }));
    try {
      await apiClient.stopInstance(id);
      await fetchData();
      showToast({
        title: 'Instance stopped',
        description: `${id} is now stopped.`,
        variant: 'success',
      });
    } catch (err) {
      const message = err instanceof Error ? err.message : `Failed to stop ${id}`;
      showToast({
        title: 'Failed to stop instance',
        description: message,
        variant: 'destructive',
      });
    } finally {
      setActionInProgress(prev => ({ ...prev, [`stop-${id}`]: false }));
    }
  };

  const performDelete = async (id: string) => {
    setActionInProgress(prev => ({ ...prev, [`delete-${id}`]: true }));
    try {
      await apiClient.deleteInstance(id);
      await fetchData();
      setConfirmState(null);
      showToast({
        title: 'Instance deleted',
        description: `${id} has been removed.`,
        variant: 'success',
      });
    } catch (err) {
      const message = err instanceof Error ? err.message : `Failed to delete ${id}`;
      showToast({
        title: 'Failed to delete instance',
        description: message,
        variant: 'destructive',
      });
      setConfirmState(null);
    } finally {
      setActionInProgress(prev => ({ ...prev, [`delete-${id}`]: false }));
    }
  };
  const handleDelete = (id: string) => {
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

  const handleEdit = async (id: string) => {
    setDialogMode('edit');
    setEditingInstanceId(id);
    setPrefilledConfig(true);
    setFormError(null);
    setConfigValues({});
    setInstanceLoading(true);
    setCreateDialogOpen(true);
    try {
      const instance = await apiClient.getInstance(id);
      const scopeEntries = Object.entries(instance.scope ?? {});
      const providerList = scopeEntries.map(([name]) => name);
      const symbolMap: Record<string, string[]> = {};
      scopeEntries.forEach(([name, assignment]) => {
        const symbols = Array.isArray(assignment?.symbols) ? assignment.symbols : [];
        const normalised = Array.from(
          new Set(
            symbols
              .map((symbol) => (typeof symbol === 'string' ? symbol : String(symbol ?? '')))
              .map((symbol) => symbol.trim().toUpperCase())
              .filter((symbol) => symbol.length > 0),
          ),
        );
        symbolMap[name] = normalised;
      });

      setNewInstance({
        id: instance.id,
        strategyIdentifier: instance.strategy.identifier,
      });
      const selectorRaw =
        (instance.strategy.selector && instance.strategy.selector.trim()) ||
        (instance.strategy.hash
          ? `${instance.strategy.identifier}@${instance.strategy.hash}`
          : instance.strategy.tag
            ? `${instance.strategy.identifier}:${instance.strategy.tag}`
            : instance.strategy.identifier);
      setStrategySelectorInput(selectorRaw);
      setSelectedProviders(providerList);
      setProviderSymbols(symbolMap);

      const strategyMeta = strategies.find(
        (strategy) => strategy.name === instance.strategy.identifier,
      );
      if (strategyMeta) {
        const values: Record<string, string> = {};
        strategyMeta.config.forEach((field) => {
          const raw = instance.strategy.config[field.name];
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
          Object.entries(instance.strategy.config).map(([key, value]) => [key, String(value)]),
        );
        setConfigValues(values);
      }
    } catch (err) {
      setFormError(err instanceof Error ? err.message : 'Failed to load instance');
      setPrefilledConfig(false);
    } finally {
      setInstanceLoading(false);
    }
  };

  if (loading) {
    return <div>Loading instances...</div>;
  }

  if (error) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{error}</AlertDescription>
      </Alert>
    );
  }

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
          <DialogContent className="max-w-2xl sm:max-w-3xl sm:max-h-[85vh] flex flex-col">
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
            <div className="flex-1 overflow-y-auto pr-1">
              {instanceLoading ? (
                <div className="flex items-center justify-center py-10 text-muted-foreground">
                  <Loader2Icon className="mr-2 h-5 w-5 animate-spin" />
                  Loading instance...
                </div>
              ) : (
                <div className="grid gap-4 py-4">
                  <div className="grid gap-2">
                    <Label htmlFor="id">Instance ID</Label>
                    <Input
                      id="id"
                      value={newInstance.id}
                      onChange={(e) => {
                        setFormError(null);
                        setNewInstance({ ...newInstance, id: e.target.value });
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
                                        <div className="max-h-48 space-y-1 overflow-y-auto pr-1">
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
                                                      setProviderSymbols((prev) => {
                                                        const current = new Set(prev[providerName] ?? []);
                                                        if (symbolChecked) {
                                                          current.add(symbol);
                                                        } else {
                                                          current.delete(symbol);
                                                        }
                                                        return {
                                                          ...prev,
                                                          [providerName]: Array.from(current).sort((a, b) =>
                                                            a.localeCompare(b),
                                                          ),
                                                        };
                                                      });
                                                    }}
                                                  />
                                                  <span>{symbol}</span>
                                                </label>
                                              );
                                            })
                                          )}
                                        </div>
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
                </div>
              )}
            </div>
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
              <Button onClick={handleSubmit} disabled={dialogSaving || instanceLoading}>
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
          return (
            <Card key={instance.id}>
              <CardHeader>
                <div className="flex items-center justify-between">
                  <CardTitle>{instance.id}</CardTitle>
                  <div className="flex items-center gap-2">
                    {drift ? (
                      <Badge variant="destructive" className="bg-amber-500 text-black hover:bg-amber-600">
                        Out of date
                      </Badge>
                    ) : null}
                    <Badge
                      variant={instance.running ? 'default' : 'secondary'}
                      className={
                        instance.running
                          ? 'bg-green-600 hover:bg-green-700'
                          : 'bg-gray-500 hover:bg-gray-600'
                      }
                    >
                      {instance.running ? 'Running' : 'Stopped'}
                    </Badge>
                  </div>
                </div>
              </CardHeader>
              <CardContent className="space-y-4">
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
                </div>
                {drift ? (
                  <Alert>
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
                    variant="outline"
                    onClick={() => handleEdit(instance.id)}
                    disabled={anyActionInFlight}
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
                      onClick={() => handleStart(instance.id)}
                      disabled={actionInProgress[`start-${instance.id}`] || anyActionInFlight}
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
                    disabled={actionInProgress[`delete-${instance.id}`] || anyActionInFlight}
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
