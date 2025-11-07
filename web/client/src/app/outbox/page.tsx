'use client';

import { useMemo, useState } from 'react';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { useOutboxQuery, useDeleteOutboxEventMutation } from '@/lib/hooks';
import { Checkbox } from '@/components/ui/checkbox';

const LIMIT_OPTIONS = [25, 50, 100, 200, 500];

const formatTimestamp = (value?: string | null) => {
  if (!value) {
    return '—';
  }
  return new Date(value).toLocaleString();
};

export default function OutboxPage() {
  const [limit, setLimit] = useState(100);
  const [aggregateType, setAggregateType] = useState('');
  const [aggregateId, setAggregateId] = useState('');
  const [showDelivered, setShowDelivered] = useState(false);

  const queryParams = useMemo(
    () => ({
      limit,
      delivered: showDelivered ? undefined : false,
      aggregateType: aggregateType.trim() || undefined,
      aggregateID: aggregateId.trim() || undefined,
    }),
    [aggregateId, aggregateType, limit, showDelivered],
  );

  const { data, isLoading, isError, error, refetch, isFetching } = useOutboxQuery(queryParams);
  const deleteMutation = useDeleteOutboxEventMutation(queryParams);

  const events = data?.events ?? [];

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-2 md:flex-row md:items-center md:justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Outbox</h1>
          <p className="text-muted-foreground">
            Inspect pending control-plane events and delete failed deliveries.
          </p>
        </div>
        <Button variant="outline" onClick={() => refetch()} disabled={isFetching}>
          {isFetching ? 'Refreshing…' : 'Refresh'}
        </Button>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Filters</CardTitle>
          <CardDescription>Limit events and scope by aggregate identifiers.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4 md:grid-cols-4">
          <div className="space-y-2">
            <Label htmlFor="limit">Limit</Label>
            <select
              id="limit"
              className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
              value={limit}
              onChange={(event) => setLimit(Number(event.target.value))}
            >
              {LIMIT_OPTIONS.map((value) => (
                <option key={value} value={value}>
                  {value}
                </option>
              ))}
            </select>
          </div>
          <div className="space-y-2">
            <Label htmlFor="aggregate-type">Aggregate type</Label>
            <Input
              id="aggregate-type"
              placeholder="provider"
              value={aggregateType}
              onChange={(event) => setAggregateType(event.target.value)}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="aggregate-id">Aggregate ID</Label>
            <Input
              id="aggregate-id"
              placeholder="binance-spot"
              value={aggregateId}
              onChange={(event) => setAggregateId(event.target.value)}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="delivered-toggle">Show delivered</Label>
            <div className="flex h-10 items-center gap-3 rounded-md border border-input px-3">
              <Checkbox
                id="delivered-toggle"
                checked={showDelivered}
                onChange={(event) => setShowDelivered(event.currentTarget.checked)}
              />
              <span className="text-sm text-muted-foreground">Include delivered</span>
            </div>
          </div>
        </CardContent>
      </Card>

      {isLoading ? (
        <div>Loading outbox events…</div>
      ) : isError ? (
        <Alert variant="destructive">
          <AlertDescription>
            {error instanceof Error ? error.message : 'Failed to load outbox events.'}
          </AlertDescription>
        </Alert>
      ) : (
        <Card>
          <CardHeader>
            <CardTitle>Events</CardTitle>
            <CardDescription>{events.length} events loaded</CardDescription>
          </CardHeader>
          <CardContent className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>ID</TableHead>
                  <TableHead>Aggregate</TableHead>
                  <TableHead>Event</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Attempts</TableHead>
                  <TableHead>Available</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {events.map((event) => (
                  <TableRow key={event.id}>
                    <TableCell className="font-mono text-xs">{event.id}</TableCell>
                    <TableCell>
                      <div className="flex flex-col">
                        <span className="font-medium">{event.aggregateType}</span>
                        <span className="text-xs text-muted-foreground">{event.aggregateID}</span>
                      </div>
                    </TableCell>
                    <TableCell>{event.eventType}</TableCell>
                    <TableCell>
                      {event.delivered ? (
                        <Badge variant="secondary">Delivered</Badge>
                      ) : (
                        <Badge variant="outline">Pending</Badge>
                      )}
                    </TableCell>
                    <TableCell>{event.attempts}</TableCell>
                    <TableCell className="text-sm">{formatTimestamp(event.availableAt)}</TableCell>
                    <TableCell className="text-right">
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={deleteMutation.isPending}
                        onClick={() => deleteMutation.mutateAsync(event.id).catch(() => undefined)}
                      >
                        Delete
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            {events.length === 0 && (
              <p className="mt-4 text-sm text-muted-foreground">
                No outbox events match the selected filters.
              </p>
            )}
          </CardContent>
        </Card>
      )}
    </div>
  );
}
