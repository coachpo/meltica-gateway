import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
describe('Galaxy UI primitives', () => {
  it('applies gradient surface styles to the primary button', () => {
    const { getByRole } = render(<Button>Primary</Button>);
    const button = getByRole('button', { name: 'Primary' });

    expect(button.className).toContain('bg-[linear-gradient(135deg');
    expect(button.className).toContain('rounded-xl');
    expect(button.className).toContain('before:bg-[radial-gradient');
  });

  it('wraps card content with galaxy border effects', () => {
    const { getByText } = render(
      <Card>
        <CardHeader>
          <CardTitle>Galaxy Card</CardTitle>
        </CardHeader>
        <CardContent>
          <p>Surface body</p>
        </CardContent>
      </Card>,
    );

    const title = getByText('Galaxy Card');
    const container = title.closest('[data-slot="card"]');

    expect(container?.className).toContain('rounded-2xl');
    expect(container?.className).toContain('before:bg-[conic-gradient');
    expect(container?.className).toContain('after:bg-card/85');
  });
  it('renders badges with gradient backgrounds by default', () => {
    const { getByText } = render(<Badge>Active</Badge>);
    const badge = getByText('Active');

    expect(badge.className).toContain('bg-[linear-gradient(120deg');
    expect(badge.className).toContain('tracking-[0.08em]');
  });

  it('styles tables with galaxy chrome', () => {
    const { getByRole, getByText } = render(
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Name</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow>
            <TableCell>Apollo</TableCell>
          </TableRow>
        </TableBody>
      </Table>,
    );

    const table = getByRole('table');
    expect(table.className).toContain('text-foreground');

    const head = getByText('Name');
    expect(head.className).toContain('tracking-[0.12em]');
  });
});
