'use client';

import { useEffect, useMemo, useState } from 'react';
import { apiClient } from '@/lib/api-client';
import { InstanceSummary, Strategy, Provider } from '@/lib/types';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { CircleStopIcon, PlayIcon, PlusIcon, TrashIcon, PencilIcon, Loader2Icon } from 'lucide-react';

export default function InstancesPage() {
  const [instances, setInstances] = useState<InstanceSummary[]>([]);
  const [strategies, setStrategies] = useState<Strategy[]>([]);
  const [providers, setProviders] = useState<Provider[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [actionMessage, setActionMessage] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
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
    provider: '',
    symbols: '',
  });
  const [configValues, setConfigValues] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState<string | null>(null);

  const selectedStrategy = useMemo(
    () => strategies.find((strategy) => strategy.name === newInstance.strategyIdentifier),
    [strategies, newInstance.strategyIdentifier]
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
      provider: '',
      symbols: '',
    });
    setConfigValues({});
    setFormError(null);
    setEditingInstanceId(null);
    setDialogMode('create');
    setPrefilledConfig(false);
    setDialogSaving(false);
    setInstanceLoading(false);
  };

  const handleConfigChange = (field: string, value: string) => {
    setConfigValues((prev) => ({ ...prev, [field]: value }));
    setFormError(null);
  };

  useEffect(() => {
    fetchData();
  }, []);

  useEffect(() => {
    if (!actionMessage) {
      return;
    }
    if (typeof window === 'undefined') {
      return;
    }
    const timeout = window.setTimeout(() => setActionMessage(null), 4000);
    return () => {
      window.clearTimeout(timeout);
    };
  }, [actionMessage]);

  const fetchData = async () => {
    try {
      const [instancesRes, strategiesRes, providersRes] = await Promise.all([
        apiClient.getInstances(),
        apiClient.getStrategies(),
        apiClient.getProviders(),
      ]);
      setInstances(instancesRes.instances);
      setStrategies(strategiesRes.strategies);
      setProviders(providersRes.providers);
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
    if (!newInstance.provider) {
      setFormError('Provider selection is required');
      return;
    }

    const symbols = newInstance.symbols
      .split(',')
      .map((symbol) => symbol.trim())
      .filter(Boolean);
    if (symbols.length === 0) {
      setFormError('At least one symbol is required');
      return;
    }

    const strategyMeta = selectedStrategy;
    if (!strategyMeta) {
      setFormError('Strategy metadata is unavailable');
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
        identifier: newInstance.strategyIdentifier,
        config: configPayload,
      },
      scope: {
        [newInstance.provider]: { symbols },
      },
    };

    const mode = dialogMode;
    const targetId = newInstance.id.trim();
    try {
      setFormError(null);
      setDialogSaving(true);
      setActionMessage(null);
      setActionError(null);
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
      setActionMessage(
        mode === 'create'
          ? `Instance ${targetId} created successfully`
          : `Instance ${targetId} updated successfully`,
      );
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : '';
      
      // Check if error is about provider availability but instance might still be created
      if (errorMessage.includes('provider') && errorMessage.includes('unavailable')) {
        const providerName = newInstance.provider || errorMessage.match(/"([^"]+)"/)?.[1] || 'selected provider';
        setFormError(
          `Provider "${providerName}" is not running. Start the provider and try creating the instance again.`
        );
      } else if (errorMessage.includes('scope assignments are immutable')) {
        setFormError(
          'Provider and symbol assignments cannot be changed after creation. Only strategy configuration can be modified.'
        );
      } else {
        setFormError(
          errorMessage ||
          (dialogMode === 'edit'
            ? 'Failed to update instance'
            : 'Failed to create instance')
        );
      }
    } finally {
      setDialogSaving(false);
    }
  };

  const handleStart = async (id: string) => {
    setActionMessage(null);
    setActionError(null);
    setActionInProgress(prev => ({ ...prev, [`start-${id}`]: true }));
    try {
      await apiClient.startInstance(id);
      await fetchData();
      setActionMessage(`Instance ${id} started`);
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : `Failed to start ${id}`;
      if (errorMessage.includes('provider') && errorMessage.includes('unavailable')) {
        setActionError(`${errorMessage}. Make sure the provider is running before starting this instance.`);
      } else {
        setActionError(errorMessage);
      }
    } finally {
      setActionInProgress(prev => ({ ...prev, [`start-${id}`]: false }));
    }
  };

  const handleStop = async (id: string) => {
    setActionMessage(null);
    setActionError(null);
    setActionInProgress(prev => ({ ...prev, [`stop-${id}`]: true }));
    try {
      await apiClient.stopInstance(id);
      await fetchData();
      setActionMessage(`Instance ${id} stopped`);
    } catch (err) {
      setActionError(err instanceof Error ? err.message : `Failed to stop ${id}`);
    } finally {
      setActionInProgress(prev => ({ ...prev, [`stop-${id}`]: false }));
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm(`Are you sure you want to delete instance "${id}"?`)) {
      return;
    }
    setActionMessage(null);
    setActionError(null);
    setActionInProgress(prev => ({ ...prev, [`delete-${id}`]: true }));
    try {
      await apiClient.deleteInstance(id);
      await fetchData();
      setActionMessage(`Instance ${id} deleted`);
    } catch (err) {
      setActionError(err instanceof Error ? err.message : `Failed to delete ${id}`);
    } finally {
      setActionInProgress(prev => ({ ...prev, [`delete-${id}`]: false }));
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
    const providerEntries = Object.entries(instance.scope);
    const [providerName, providerScope] = providerEntries[0] ?? ['', { symbols: [] }];
    const symbolsValue = (providerScope?.symbols ?? []).join(', ');

    setNewInstance({
      id: instance.id,
      strategyIdentifier: instance.strategy.identifier,
      provider: providerName,
      symbols: symbolsValue,
    });

      const strategyMeta = strategies.find((strategy) => strategy.name === instance.strategy.identifier);
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
          Object.entries(instance.strategy.config).map(([key, value]) => [key, String(value)])
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

  return (
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
                    <Label htmlFor="strategy">Strategy</Label>
                    <Select
                      value={newInstance.strategyIdentifier}
                      onValueChange={(value) => {
                        setFormError(null);
                        setPrefilledConfig(false);
                        setNewInstance({ ...newInstance, strategyIdentifier: value });
                      }}
                    >
                      <SelectTrigger>
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
                    <Label htmlFor="provider">
                      Provider
                      {dialogMode === 'edit' && (
                        <span className="ml-2 text-xs font-normal text-muted-foreground">
                          (cannot be changed)
                        </span>
                      )}
                    </Label>
                    <Select
                      value={newInstance.provider}
                      onValueChange={(value) => {
                        setFormError(null);
                        setNewInstance({ ...newInstance, provider: value });
                      }}
                      disabled={dialogMode === 'edit'}
                    >
                      <SelectTrigger>
                        <SelectValue placeholder="Select provider" />
                      </SelectTrigger>
                      <SelectContent>
                        {providers.map((provider) => (
                          <SelectItem
                            key={provider.name}
                            value={provider.name}
                            disabled={!provider.running}
                          >
                            {provider.running ? provider.name : `${provider.name} (stopped)`}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    {providers.some((provider) => !provider.running) && (
                      <p className="text-xs text-muted-foreground">
                        Start a provider from the Providers page to enable it here.
                      </p>
                    )}
                  </div>
                  <div className="grid gap-2">
                    <Label htmlFor="symbols">
                      Symbols (comma-separated)
                      {dialogMode === 'edit' && (
                        <span className="ml-2 text-xs font-normal text-muted-foreground">
                          (cannot be changed)
                        </span>
                      )}
                    </Label>
                    <Input
                      id="symbols"
                      value={newInstance.symbols}
                      onChange={(e) => {
                        setFormError(null);
                        setNewInstance({ ...newInstance, symbols: e.target.value });
                      }}
                      placeholder="BTC-USDT, ETH-USDT"
                      disabled={dialogMode === 'edit'}
                    />
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

      {actionMessage && (
        <Alert>
          <AlertDescription>
            <div className="flex items-center justify-between gap-4">
              <span>{actionMessage}</span>
              <button
                type="button"
                className="text-sm font-medium text-primary hover:underline"
                onClick={() => setActionMessage(null)}
              >
                Dismiss
              </button>
            </div>
          </AlertDescription>
        </Alert>
      )}

      {actionError && (
        <Alert variant="destructive">
          <AlertDescription>
            <div className="flex items-center justify-between gap-4">
              <span>{actionError}</span>
              <button
                type="button"
                className="text-sm font-medium text-destructive hover:underline"
                onClick={() => setActionError(null)}
              >
                Dismiss
              </button>
            </div>
          </AlertDescription>
        </Alert>
      )}

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {instances.map((instance) => (
          <Card key={instance.id}>
            <CardHeader>
              <div className="flex items-center justify-between">
                <CardTitle>{instance.id}</CardTitle>
                <Badge 
                  variant={instance.running ? 'default' : 'secondary'}
                  className={instance.running ? 'bg-green-600 hover:bg-green-700' : 'bg-gray-500 hover:bg-gray-600'}
                >
                  {instance.running ? 'Running' : 'Stopped'}
                </Badge>
              </div>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="text-sm space-y-2">
                <div>
                  <span className="font-medium">Strategy:</span>{' '}
                  <span className="text-muted-foreground">{instance.strategyIdentifier}</span>
                </div>
                <div>
                  <span className="font-medium">Providers:</span>{' '}
                  <div className="flex flex-wrap gap-1 mt-1">
                    {instance.providers.map((provider) => (
                      <Badge key={provider} variant="outline">
                        {provider}
                      </Badge>
                    ))}
                  </div>
                </div>
                <div>
                  <span className="font-medium">Symbols:</span>{' '}
                  <div className="flex flex-wrap gap-1 mt-1">
                    {instance.aggregatedSymbols.map((symbol) => (
                      <Badge key={symbol} variant="outline">
                        {symbol}
                      </Badge>
                    ))}
                  </div>
                </div>
              </div>
              <div className="flex gap-2">
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => handleEdit(instance.id)}
                  disabled={Object.values(actionInProgress).some(Boolean)}
                >
                  <PencilIcon className="mr-1 h-3 w-3" />
                  Edit
                </Button>
                {instance.running ? (
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => handleStop(instance.id)}
                    disabled={actionInProgress[`stop-${instance.id}`] || Object.values(actionInProgress).some(Boolean)}
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
                    disabled={actionInProgress[`start-${instance.id}`] || Object.values(actionInProgress).some(Boolean)}
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
                  disabled={actionInProgress[`delete-${instance.id}`] || Object.values(actionInProgress).some(Boolean)}
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
        ))}
      </div>

      {instances.length === 0 && (
        <Card>
          <CardContent className="py-10 text-center text-muted-foreground">
            No strategy instances configured. Create one to get started.
          </CardContent>
        </Card>
      )}
    </div>
  );
}
