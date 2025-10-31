'use client';

import { useEffect, useState } from 'react';
import { apiClient } from '@/lib/api-client';
import { RiskConfig } from '@/lib/types';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Separator } from '@/components/ui/separator';

export default function RiskPage() {
  const [limits, setLimits] = useState<RiskConfig | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [editMode, setEditMode] = useState(false);

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

  useEffect(() => {
    fetchLimits();
  }, []);

  const fetchLimits = async () => {
    try {
      const response = await apiClient.getRiskLimits();
      setLimits(response.limits);
      setFormData(response.limits);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch risk limits');
    } finally {
      setLoading(false);
    }
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      const response = await apiClient.updateRiskLimits(formData);
      setLimits(response.limits);
      setEditMode(false);
      alert('Risk limits updated successfully');
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to update risk limits');
    } finally {
      setSaving(false);
    }
  };

  const handleCancel = () => {
    if (limits) {
      setFormData(limits);
    }
    setEditMode(false);
  };

  const handleOrderTypesChange = (value: string) => {
    const types = value.split(',').map(t => t.trim()).filter(Boolean);
    setFormData({ ...formData, allowedOrderTypes: types });
  };

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
                <div className="text-sm text-muted-foreground">{limits?.maxPositionSize}</div>
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
                <div className="text-sm text-muted-foreground">{limits?.maxNotionalValue}</div>
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
                <div className="text-sm text-muted-foreground">{limits?.notionalCurrency}</div>
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
                <div className="text-sm text-muted-foreground">{limits?.orderThrottle}</div>
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
                <div className="text-sm text-muted-foreground">{limits?.orderBurst}</div>
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
                <div className="text-sm text-muted-foreground">{limits?.maxConcurrentOrders}</div>
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
                <div className="text-sm text-muted-foreground">{limits?.priceBandPercent}%</div>
              )}
            </div>
            <div className="grid gap-2">
              <Label htmlFor="allowedOrderTypes">Allowed Order Types</Label>
              {editMode ? (
                <Input
                  id="allowedOrderTypes"
                  value={formData.allowedOrderTypes.join(', ')}
                  onChange={(e) => handleOrderTypesChange(e.target.value)}
                  placeholder="e.g., LIMIT, MARKET"
                />
              ) : (
                <div className="flex flex-wrap gap-1">
                  {limits?.allowedOrderTypes.map((type) => (
                    <Badge key={type} variant="secondary">
                      {type}
                    </Badge>
                  ))}
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
                <div className="text-sm text-muted-foreground">{limits?.maxRiskBreaches}</div>
              )}
            </div>
            <Separator />
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <Label>Kill Switch</Label>
                <Badge variant={limits?.killSwitchEnabled ? 'destructive' : 'secondary'}>
                  {limits?.killSwitchEnabled ? 'Enabled' : 'Disabled'}
                </Badge>
              </div>
            </div>
            <Separator />
            <div className="space-y-3">
              <div className="flex items-center justify-between">
                <Label>Circuit Breaker</Label>
                <Badge variant={limits?.circuitBreaker.enabled ? 'default' : 'secondary'}>
                  {limits?.circuitBreaker.enabled ? 'Enabled' : 'Disabled'}
                </Badge>
              </div>
              {limits?.circuitBreaker.enabled && (
                <>
                  <div>
                    <span className="text-sm font-medium">Threshold:</span>{' '}
                    <span className="text-sm text-muted-foreground">{limits?.circuitBreaker.threshold}</span>
                  </div>
                  <div>
                    <span className="text-sm font-medium">Cooldown:</span>{' '}
                    <span className="text-sm text-muted-foreground">{limits?.circuitBreaker.cooldown}</span>
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
