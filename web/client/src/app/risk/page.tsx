'use client';

import { KeyboardEvent, useEffect, useMemo, useState } from 'react';
import { apiClient } from '@/lib/api-client';
import { RiskConfig } from '@/lib/types';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Separator } from '@/components/ui/separator';
import { XIcon } from 'lucide-react';

type RiskPresence = {
  maxPositionSize: boolean;
  maxNotionalValue: boolean;
  notionalCurrency: boolean;
  orderThrottle: boolean;
  orderBurst: boolean;
  maxConcurrentOrders: boolean;
  priceBandPercent: boolean;
  allowedOrderTypes: boolean;
  killSwitchEnabled: boolean;
  maxRiskBreaches: boolean;
  circuitBreaker: {
    enabled: boolean;
    threshold: boolean;
    cooldown: boolean;
  };
};

const computePresence = (config?: Partial<RiskConfig> | null): RiskPresence => {
  const source = (config ?? {}) as Partial<RiskConfig>;
  const circuit = (source.circuitBreaker ?? {}) as Partial<RiskConfig['circuitBreaker']>;
  const has = <K extends keyof RiskConfig>(key: K) =>
    Object.prototype.hasOwnProperty.call(source, key) &&
    source[key] !== undefined &&
    source[key] !== null &&
    (typeof source[key] !== 'string' || (source[key] as unknown as string).trim() !== '');
  const hasCircuit = <K extends keyof RiskConfig['circuitBreaker']>(key: K) =>
    Object.prototype.hasOwnProperty.call(circuit, key) &&
    circuit[key] !== undefined &&
    circuit[key] !== null &&
    (typeof circuit[key] !== 'string' || (circuit[key] as unknown as string).trim() !== '');

  return {
    maxPositionSize: has('maxPositionSize'),
    maxNotionalValue: has('maxNotionalValue'),
    notionalCurrency: has('notionalCurrency'),
    orderThrottle: has('orderThrottle'),
    orderBurst: has('orderBurst'),
    maxConcurrentOrders: has('maxConcurrentOrders'),
    priceBandPercent: has('priceBandPercent'),
    allowedOrderTypes:
      Object.prototype.hasOwnProperty.call(source, 'allowedOrderTypes') &&
      Array.isArray(source.allowedOrderTypes) &&
      source.allowedOrderTypes.length > 0,
    killSwitchEnabled: Object.prototype.hasOwnProperty.call(source, 'killSwitchEnabled'),
    maxRiskBreaches: has('maxRiskBreaches'),
    circuitBreaker: {
      enabled: Object.prototype.hasOwnProperty.call(circuit, 'enabled'),
      threshold: hasCircuit('threshold'),
      cooldown: hasCircuit('cooldown'),
    },
  };
};

const normalizeOrderTypes = (types?: string[] | null): string[] => {
  if (!types || types.length === 0) {
    return [];
  }
  const seen = new Set<string>();
  const normalized: string[] = [];
  for (const raw of types) {
    const trimmed = raw?.trim() ?? '';
    if (!trimmed) {
      continue;
    }
    const key = trimmed.toLowerCase();
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    normalized.push(trimmed);
  }
  return normalized;
};

export default function RiskPage() {
  const normalizeRiskConfig = (config?: Partial<RiskConfig> | null): RiskConfig => ({
    maxPositionSize: config?.maxPositionSize ?? '',
    maxNotionalValue: config?.maxNotionalValue ?? '',
    notionalCurrency: config?.notionalCurrency ?? '',
    orderThrottle: Number(config?.orderThrottle ?? 0),
    orderBurst: Number(config?.orderBurst ?? 0),
    maxConcurrentOrders: Number(config?.maxConcurrentOrders ?? 0),
    priceBandPercent: Number(config?.priceBandPercent ?? 0),
    allowedOrderTypes: normalizeOrderTypes(config?.allowedOrderTypes),
    killSwitchEnabled: Boolean(config?.killSwitchEnabled ?? false),
    maxRiskBreaches: Number(config?.maxRiskBreaches ?? 0),
    circuitBreaker: {
      enabled: Boolean(config?.circuitBreaker?.enabled ?? false),
      threshold: Number(config?.circuitBreaker?.threshold ?? 0),
      cooldown: config?.circuitBreaker?.cooldown ?? '',
    },
  });

  const [limits, setLimits] = useState<RiskConfig | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [editMode, setEditMode] = useState(false);
  const [presence, setPresence] = useState<RiskPresence | null>(null);
  const [actionMessage, setActionMessage] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  const [formData, setFormData] = useState<RiskConfig>({
    maxPositionSize: '',
    maxNotionalValue: '',
    notionalCurrency: '',
    orderThrottle: 0,
    orderBurst: 0,
    maxConcurrentOrders: 0,
    priceBandPercent: 0,
    allowedOrderTypes: [],
    killSwitchEnabled: false,
    maxRiskBreaches: 0,
    circuitBreaker: {
      enabled: false,
      threshold: 0,
      cooldown: '',
    },
  });
  const [orderTypeInput, setOrderTypeInput] = useState('');

  useEffect(() => {
    const loadLimits = async () => {
      try {
        const response = await apiClient.getRiskLimits();
        const normalized = normalizeRiskConfig(response.limits);
        const resolvedPresence = computePresence(response.limits as Partial<RiskConfig>);
        setLimits(normalized);
        setFormData(normalized);
        setPresence(resolvedPresence);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to fetch risk limits');
      } finally {
        setLoading(false);
      }
    };

    void loadLimits();
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

  const handleSave = async () => {
    setSaving(true);
    try {
      setActionMessage(null);
      setActionError(null);
      const response = await apiClient.updateRiskLimits(formData);
      const normalized = normalizeRiskConfig(response.limits);
      const resolvedPresence = computePresence(response.limits as Partial<RiskConfig>);
      setLimits(normalized);
      setEditMode(false);
      setPresence(resolvedPresence);
      setActionMessage('Risk limits updated successfully');
    } catch (err) {
      setActionError(err instanceof Error ? err.message : 'Failed to update risk limits');
    } finally {
      setSaving(false);
    }
  };

  const handleCancel = () => {
    if (limits) {
      setFormData(normalizeRiskConfig(limits));
    }
    setEditMode(false);
    setActionError(null);
  };

  const addOrderType = (raw: string) => {
    const trimmed = raw.trim();
    if (!trimmed) {
      setOrderTypeInput('');
      return;
    }
    setFormData((prev) => {
      const lowered = trimmed.toLowerCase();
      const exists = prev.allowedOrderTypes.some(
        (existing) => existing.toLowerCase() === lowered,
      );
      if (exists) {
        return prev;
      }
      return {
        ...prev,
        allowedOrderTypes: [...prev.allowedOrderTypes, trimmed],
      };
    });
    setOrderTypeInput('');
  };

  const removeOrderType = (type: string) => {
    setFormData((prev) => ({
      ...prev,
      allowedOrderTypes: prev.allowedOrderTypes.filter((entry) => entry !== type),
    }));
  };

  const handleOrderTypeKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Enter' || event.key === ',') {
      event.preventDefault();
      addOrderType(orderTypeInput);
    }
  };

  const missingFields = useMemo(() => {
    if (!presence) {
      return [] as string[];
    }
    const fields: string[] = [];
    if (!presence.maxPositionSize) fields.push('Max position size');
    if (!presence.maxNotionalValue) fields.push('Max notional value');
    if (!presence.notionalCurrency) fields.push('Notional currency');
    if (!presence.orderThrottle) fields.push('Order throttle');
    if (!presence.orderBurst) fields.push('Order burst');
    if (!presence.maxConcurrentOrders) fields.push('Max concurrent orders');
    if (!presence.priceBandPercent) fields.push('Price band percent');
    if (!presence.allowedOrderTypes) fields.push('Allowed order types');
    if (!presence.killSwitchEnabled) fields.push('Kill switch');
    if (!presence.maxRiskBreaches) fields.push('Max risk breaches');
    if (!presence.circuitBreaker.enabled) {
      fields.push('Circuit breaker');
    } else {
      if (!presence.circuitBreaker.threshold) fields.push('Circuit breaker threshold');
      if (!presence.circuitBreaker.cooldown) fields.push('Circuit breaker cooldown');
    }
    return fields;
  }, [presence]);

  if (loading) {
    return <div>Loading risk limits...</div>;
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
          <h1 className="text-3xl font-bold tracking-tight">Risk Limits</h1>
          <p className="text-muted-foreground">
            Configure position limits, order throttling, and circuit breakers
          </p>
        </div>
        {!editMode ? (
          <Button onClick={() => setEditMode(true)}>Edit Limits</Button>
        ) : (
          <div className="flex gap-2">
            <Button variant="outline" onClick={handleCancel} disabled={saving}>
              Cancel
            </Button>
            <Button onClick={handleSave} disabled={saving}>
              {saving ? 'Saving...' : 'Save Changes'}
            </Button>
          </div>
        )}
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

      {missingFields.length > 0 && !editMode && (
        <Alert>
          <AlertDescription>
            <div className="space-y-2">
              <div className="font-medium text-foreground">Action recommended</div>
              <div className="text-sm text-muted-foreground">
                Configure the following risk limits: {missingFields.join(', ')}.
              </div>
            </div>
          </AlertDescription>
        </Alert>
      )}

      <div className="grid gap-6 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Position Limits</CardTitle>
            <CardDescription>Maximum position size and notional value constraints</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid gap-2">
              <Label htmlFor="maxPositionSize">Max Position Size</Label>
              {editMode ? (
                <Input
                  id="maxPositionSize"
                  value={formData.maxPositionSize}
                  onChange={(e) => setFormData({ ...formData, maxPositionSize: e.target.value })}
                  placeholder="e.g., 1000"
                />
              ) : (
                <div className="text-sm text-muted-foreground">
                  {presence?.maxPositionSize && limits?.maxPositionSize
                    ? limits.maxPositionSize
                    : <span className="italic">Not configured</span>}
                </div>
              )}
            </div>
            <div className="grid gap-2">
              <Label htmlFor="maxNotionalValue">Max Notional Value</Label>
              {editMode ? (
                <Input
                  id="maxNotionalValue"
                  value={formData.maxNotionalValue}
                  onChange={(e) => setFormData({ ...formData, maxNotionalValue: e.target.value })}
                  placeholder="e.g., 10000"
                />
              ) : (
                <div className="text-sm text-muted-foreground">
                  {presence?.maxNotionalValue && limits?.maxNotionalValue
                    ? limits.maxNotionalValue
                    : <span className="italic">Not configured</span>}
                </div>
              )}
            </div>
            <div className="grid gap-2">
              <Label htmlFor="notionalCurrency">Notional Currency</Label>
              {editMode ? (
                <Input
                  id="notionalCurrency"
                  value={formData.notionalCurrency}
                  onChange={(e) => setFormData({ ...formData, notionalCurrency: e.target.value })}
                  placeholder="e.g., USDT"
                />
              ) : (
                <div className="text-sm text-muted-foreground">
                  {presence?.notionalCurrency && limits?.notionalCurrency
                    ? limits.notionalCurrency
                    : <span className="italic">Not configured</span>}
                </div>
              )}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Order Throttling</CardTitle>
            <CardDescription>Rate limiting and concurrency controls</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid gap-2">
              <Label htmlFor="orderThrottle">Order Throttle (orders/sec)</Label>
              {editMode ? (
                <Input
                  id="orderThrottle"
                  type="number"
                  value={formData.orderThrottle}
                  onChange={(e) => setFormData({ ...formData, orderThrottle: Number(e.target.value) })}
                />
              ) : (
                <div className="text-sm text-muted-foreground">
                  {presence?.orderThrottle
                    ? `${limits?.orderThrottle ?? 0}`
                    : <span className="italic">Not configured</span>}
                </div>
              )}
            </div>
            <div className="grid gap-2">
              <Label htmlFor="orderBurst">Order Burst</Label>
              {editMode ? (
                <Input
                  id="orderBurst"
                  type="number"
                  value={formData.orderBurst}
                  onChange={(e) => setFormData({ ...formData, orderBurst: Number(e.target.value) })}
                />
              ) : (
                <div className="text-sm text-muted-foreground">
                  {presence?.orderBurst
                    ? `${limits?.orderBurst ?? 0}`
                    : <span className="italic">Not configured</span>}
                </div>
              )}
            </div>
            <div className="grid gap-2">
              <Label htmlFor="maxConcurrentOrders">Max Concurrent Orders</Label>
              {editMode ? (
                <Input
                  id="maxConcurrentOrders"
                  type="number"
                  value={formData.maxConcurrentOrders}
                  onChange={(e) => setFormData({ ...formData, maxConcurrentOrders: Number(e.target.value) })}
                />
              ) : (
                <div className="text-sm text-muted-foreground">
                  {presence?.maxConcurrentOrders
                    ? `${limits?.maxConcurrentOrders ?? 0}`
                    : <span className="italic">Not configured</span>}
                </div>
              )}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Order Controls</CardTitle>
            <CardDescription>Price bands and allowed order types</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid gap-2">
              <Label htmlFor="priceBandPercent">Price Band Percent</Label>
              {editMode ? (
                <Input
                  id="priceBandPercent"
                  type="number"
                  step="0.01"
                  value={formData.priceBandPercent}
                  onChange={(e) => setFormData({ ...formData, priceBandPercent: Number(e.target.value) })}
                />
              ) : (
                <div className="text-sm text-muted-foreground">
                  {presence?.priceBandPercent
                    ? `${limits?.priceBandPercent ?? 0}%`
                    : <span className="italic">Not configured</span>}
                </div>
              )}
            </div>
            <div className="grid gap-2">
              <Label htmlFor="allowedOrderTypes">Allowed Order Types</Label>
              {editMode ? (
                <div className="space-y-2 rounded-md border p-2">
                  <div className="flex flex-wrap items-center gap-2">
                    {formData.allowedOrderTypes.map((type) => (
                      <Badge key={type} variant="secondary" className="flex items-center gap-1">
                        {type}
                        <button
                          type="button"
                          onClick={() => removeOrderType(type)}
                          className="inline-flex h-4 w-4 items-center justify-center rounded-full text-muted-foreground hover:text-foreground"
                          aria-label={`Remove ${type}`}
                        >
                          <XIcon className="h-3 w-3" />
                        </button>
                      </Badge>
                    ))}
                    <Input
                      id="allowedOrderTypes"
                      value={orderTypeInput}
                      onChange={(event) => setOrderTypeInput(event.target.value)}
                      onKeyDown={handleOrderTypeKeyDown}
                      placeholder="Type and press Enter"
                      className="flex-1 min-w-[8rem] border-none bg-transparent p-0 text-sm focus-visible:ring-0 focus-visible:border-none"
                    />
                  </div>
                  <p className="text-xs text-muted-foreground">
                    Press Enter or comma to add, or click an order type to remove it.
                  </p>
                </div>
              ) : (
                <div className="flex flex-wrap gap-1">
                  {presence?.allowedOrderTypes && limits?.allowedOrderTypes?.length ? (
                    limits.allowedOrderTypes.map((type) => (
                      <Badge key={type} variant="secondary">
                        {type}
                      </Badge>
                    ))
                  ) : (
                    <span className="text-sm italic text-muted-foreground">Not configured</span>
                  )}
                </div>
              )}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Safety Controls</CardTitle>
            <CardDescription>Kill switch and circuit breaker configuration</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid gap-2">
              <Label htmlFor="maxRiskBreaches">Max Risk Breaches</Label>
              {editMode ? (
                <Input
                  id="maxRiskBreaches"
                  type="number"
                  value={formData.maxRiskBreaches}
                  onChange={(e) => setFormData({ ...formData, maxRiskBreaches: Number(e.target.value) })}
                />
              ) : (
                <div className="text-sm text-muted-foreground">
                  {presence?.maxRiskBreaches
                    ? `${limits?.maxRiskBreaches ?? 0}`
                    : <span className="italic">Not configured</span>}
                </div>
              )}
            </div>
            <Separator />
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <Label>Kill Switch</Label>
                {presence?.killSwitchEnabled ? (
                  <Badge variant={limits?.killSwitchEnabled ? 'destructive' : 'secondary'}>
                    {limits?.killSwitchEnabled ? 'Enabled' : 'Disabled'}
                  </Badge>
                ) : (
                  <Badge variant="secondary">Not configured</Badge>
                )}
              </div>
            </div>
            <Separator />
            <div className="space-y-3">
              <div className="flex items-center justify-between">
                <Label>Circuit Breaker</Label>
                {presence?.circuitBreaker.enabled ? (
                  <Badge variant={limits?.circuitBreaker?.enabled ? 'default' : 'secondary'}>
                    {limits?.circuitBreaker?.enabled ? 'Enabled' : 'Disabled'}
                  </Badge>
                ) : (
                  <Badge variant="secondary">Not configured</Badge>
                )}
              </div>
              {presence?.circuitBreaker.enabled && limits?.circuitBreaker?.enabled && (
                <>
                  <div>
                    <span className="text-sm font-medium">Threshold:</span>{' '}
                    {presence.circuitBreaker.threshold ? (
                      <span className="text-sm text-muted-foreground">{limits?.circuitBreaker?.threshold}</span>
                    ) : (
                      <span className="text-sm italic text-muted-foreground">Not configured</span>
                    )}
                  </div>
                  <div>
                    <span className="text-sm font-medium">Cooldown:</span>{' '}
                    {presence.circuitBreaker.cooldown ? (
                      <span className="text-sm text-muted-foreground">{limits?.circuitBreaker?.cooldown}</span>
                    ) : (
                      <span className="text-sm italic text-muted-foreground">Not configured</span>
                    )}
                  </div>
                </>
              )}
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
