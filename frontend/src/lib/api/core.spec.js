import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { concurrencyRetryDelayMs, fetchAPI, fetchAPIV2 } from './core.js';

const json = (body, status = 200, headers = {}) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json', ...headers },
  });
const tooMany = () =>
  json({ error: { code: 'TOO_MANY_REQUESTS', message: 'Too many concurrent requests' } }, 429, {
    'Retry-After': '1',
  });

describe('concurrency limit retries', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('retries a GET rejected with 429 and resolves with the later response', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(tooMany())
      .mockResolvedValueOnce(json({ data: [{ id: 1 }] }));
    vi.stubGlobal('fetch', fetchMock);

    const result = fetchAPIV2('/custom-fields');
    await vi.runAllTimersAsync();

    await expect(result).resolves.toEqual({ data: [{ id: 1 }] });
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('gives up after the bounded number of retries and surfaces the 429', async () => {
    const fetchMock = vi.fn().mockImplementation(() => Promise.resolve(tooMany()));
    vi.stubGlobal('fetch', fetchMock);

    const result = fetchAPIV2('/milestones');
    const assertion = expect(result).rejects.toMatchObject({ status: 429 });
    await vi.runAllTimersAsync();

    await assertion;
    expect(fetchMock).toHaveBeenCalledTimes(3);
  });

  it('never retries writes', async () => {
    const fetchMock = vi.fn().mockImplementation(() => Promise.resolve(tooMany()));
    vi.stubGlobal('fetch', fetchMock);

    const result = fetchAPI('/items/1', { method: 'PATCH', body: '{}' });
    const assertion = expect(result).rejects.toMatchObject({ status: 429 });
    await vi.runAllTimersAsync();

    await assertion;
    expect(fetchMock).toHaveBeenCalledOnce();
  });

  it('stops waiting when the caller aborts', async () => {
    const fetchMock = vi.fn().mockImplementation(() => Promise.resolve(tooMany()));
    vi.stubGlobal('fetch', fetchMock);
    const controller = new AbortController();

    const result = fetchAPIV2('/iterations', { signal: controller.signal });
    const assertion = expect(result).rejects.toMatchObject({ name: 'AbortError' });
    await vi.advanceTimersByTimeAsync(0);
    controller.abort();

    await assertion;
    expect(fetchMock).toHaveBeenCalledOnce();
  });

  it('derives the delay from Retry-After, falling back to capped backoff', () => {
    const noJitter = () => 0;
    expect(concurrencyRetryDelayMs({ retryAfter: '1' }, 0, noJitter)).toBe(1000);
    expect(concurrencyRetryDelayMs({ retryAfter: '60' }, 0, noJitter)).toBe(5000);
    expect(concurrencyRetryDelayMs({ retryAfter: null }, 0, noJitter)).toBe(500);
    expect(concurrencyRetryDelayMs({}, 2, noJitter)).toBe(2000);
  });
});
