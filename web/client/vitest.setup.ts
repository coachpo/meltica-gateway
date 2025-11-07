import '@testing-library/jest-dom/vitest';
import fetch, { Headers, Request, Response } from 'cross-fetch';
import { server } from './src/mocks/server';

globalThis.fetch = fetch as unknown as typeof globalThis.fetch;
globalThis.Headers = Headers as typeof globalThis.Headers;
globalThis.Request = Request as typeof globalThis.Request;
globalThis.Response = Response as typeof globalThis.Response;

beforeAll(() => {
  process.env.NEXT_PUBLIC_API_URL = process.env.NEXT_PUBLIC_API_URL ?? 'http://localhost:8880';
  server.listen();
});

afterEach(() => {
  server.resetHandlers();
});

afterAll(() => {
  server.close();
});
