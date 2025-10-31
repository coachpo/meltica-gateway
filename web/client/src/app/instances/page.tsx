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
import { CircleStopIcon, PlayIcon, PlusIcon, TrashIcon } from 'lucide-react';

export default function InstancesPage() {
  const [instances, setInstances] = useState<InstanceSummary[]>([]);
  const [strategies, setStrategies] = useState<Strategy[]>([]);
  const [providers, setProviders] = useState<Provider[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [createDialogOpen, setCreateDialogOpen] = useState(false);

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
      setConfigValues({});
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
  }, [selectedStrategy]);

  const resetForm = () => {
    setNewInstance({
      id: '',
      strategyIdentifier: '',
      provider: '',
      symbols: '',
    });
    setConfigValues({});
    setFormError(null);
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

  const handleCreate = async () => {
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

    try {
      setFormError(null);
      await apiClient.createInstance({
        id: newInstance.id.trim(),
        strategy: {
          identifier: newInstance.strategyIdentifier,
          config: configPayload,
        },
        scope: {
          [newInstance.provider]: { symbols },
        },
      });
      setCreateDialogOpen(false);
      resetForm();
      fetchData();
    } catch (err) {
      setFormError(err instanceof Error ? err.message : 'Failed to create instance');
    }
  };

  const handleStart = async (id: string) => {
    try {
      await apiClient.startInstance(id);
      fetchData();
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to start instance');
    }
  };

  const handleStop = async (id: string) => {
    try {
      await apiClient.stopInstance(id);
      fetchData();
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to stop instance');
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm(`Are you sure you want to delete instance "${id}"?`)) {
      return;
    }
    try {
      await apiClient.deleteInstance(id);
      fetchData();
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to delete instance');
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
            <Button>
              <PlusIcon className="mr-2 h-4 w-4" />
              Create Instance
            </Button>
          </DialogTrigger>
          <DialogContent className="max-w-2xl">
            <DialogHeader>
              <DialogTitle>Create Strategy Instance</DialogTitle>
              <DialogDescription>
                Configure and start a new trading strategy instance
              </DialogDescription>
            </DialogHeader>
            {formError && (
              <Alert variant="destructive">
                <AlertDescription>{formError}</AlertDescription>
              </Alert>
            )}
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
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="strategy">Strategy</Label>
                <Select
                  value={newInstance.strategyIdentifier}
                  onValueChange={(value) => {
                    setFormError(null);
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
                <Label htmlFor="provider">Provider</Label>
                <Select
                  value={newInstance.provider}
                  onValueChange={(value) => {
                    setFormError(null);
                    setNewInstance({ ...newInstance, provider: value });
                  }}
                >
                  <SelectTrigger>
                    <SelectValue placeholder="Select provider" />
                  </SelectTrigger>
                  <SelectContent>
                    {providers.map((provider) => (
                      <SelectItem key={provider.name} value={provider.name}>
                        {provider.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="grid gap-2">
                <Label htmlFor="symbols">Symbols (comma-separated)</Label>
                <Input
                  id="symbols"
                  value={newInstance.symbols}
                  onChange={(e) => {
                    setFormError(null);
                    setNewInstance({ ...newInstance, symbols: e.target.value });
                  }}
                  placeholder="BTC-USDT, ETH-USDT"
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
            <DialogFooter>
              <Button
                variant="outline"
                onClick={() => {
                  resetForm();
                  setCreateDialogOpen(false);
                }}
              >
                Cancel
              </Button>
              <Button onClick={handleCreate}>Create & Start</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {instances.map((instance) => (
          <Card key={instance.id}>
            <CardHeader>
              <div className="flex items-center justify-between">
                <CardTitle>{instance.id}</CardTitle>
                <Badge variant={instance.running ? 'default' : 'secondary'}>
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
                {instance.running ? (
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => handleStop(instance.id)}
                  >
                    <CircleStopIcon className="mr-1 h-3 w-3" />
                    Stop
                  </Button>
                ) : (
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => handleStart(instance.id)}
                  >
                    <PlayIcon className="mr-1 h-3 w-3" />
                    Start
                  </Button>
                )}
                <Button
                  size="sm"
                  variant="destructive"
                  onClick={() => handleDelete(instance.id)}
                >
                  <TrashIcon className="mr-1 h-3 w-3" />
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
