import { http, HttpResponse } from 'msw';
import type { Provider, Strategy } from '@/lib/types';

const providers: Provider[] = [
  {
    name: 'binance-spot',
    adapter: 'binance',
    identifier: 'binance',
    instrumentCount: 128,
    settings: {},
    running: true,
    status: 'running',
    startupError: undefined,
    dependentInstances: ['momentum-eur'],
    dependentInstanceCount: 1,
  },
];

const strategies: Strategy[] = [
  {
    name: 'momentum',
    displayName: 'Momentum',
    description: 'Demo strategy',
    tag: 'v1.0.0',
    config: [
      {
        name: 'lookback',
        type: 'duration',
        description: 'Window size',
        default: '5m',
        required: true,
      },
    ],
    events: ['Ticker'],
  },
];

export const handlers = [
  http.get('http://localhost:8880/providers', () => {
    return HttpResponse.json({ providers });
  }),
  http.get('http://localhost:8880/providers/:name', ({ params }) => {
    const provider = providers.find((entry) => entry.name === params.name);
    if (!provider) {
      return new HttpResponse(null, { status: 404 });
    }
    return HttpResponse.json({ ...provider, instruments: [], adapter: {
      identifier: provider.identifier,
      displayName: 'Binance',
      venue: 'binance',
      description: 'Binance spot API',
      capabilities: ['spot'],
      settingsSchema: [],
    } });
  }),
  http.get('http://localhost:8880/strategies', () => {
    return HttpResponse.json({ strategies });
  }),
  http.get('http://localhost:8880/strategies/:name', ({ params }) => {
    const strategy = strategies.find((entry) => entry.name === params.name);
    if (!strategy) {
      return new HttpResponse(null, { status: 404 });
    }
    return HttpResponse.json(strategy);
  }),
];
