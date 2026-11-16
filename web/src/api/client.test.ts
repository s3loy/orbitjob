import { afterEach, describe, expect, it, vi } from 'vitest';
import { demoClient } from './demo';
import { correlate, jobDetailSchema, jobsSchema, runDetailSchema, runsSchema } from './contracts';
import { APIError, decode, liveClient } from './transport';

afterEach(() => vi.unstubAllGlobals());
describe('contract and identifier boundary', () => {
  it('preserves int64 identifiers without rounding', () => {
    expect(decode('{"id":9007199254740993,"attempt":2}')).toEqual({
      id: '9007199254740993',
      attempt: 2,
    });
  });
  it('keeps Demo data within the generated contract', async () => {
    const demo = demoClient();
    const jobs = await demo.jobs();
    expect(jobsSchema.safeParse({ items: jobs }).success).toBe(true);
    expect(jobDetailSchema.safeParse(await demo.job(jobs[0].id)).success).toBe(true);
    const runs = await demo.runs();
    expect(runsSchema.safeParse({ items: runs }).success).toBe(true);
    expect(runDetailSchema.safeParse(await demo.run(runs[0].id)).success).toBe(true);
    demo.dispose();
  });
  it('never associates an unrelated or duplicate row', async () => {
    const demo = demoClient();
    const [run] = await demo.runs();
    expect(() => correlate([run], 'wrong-source', run.occurrence_key)).toThrow();
    expect(() => correlate([run], run.source_uid, 'wrong-occurrence')).toThrow();
    expect(() => correlate([run, run], run.source_uid, run.occurrence_key)).toThrow();
    expect(correlate([], run.source_uid, run.occurrence_key)).toBeUndefined();
    expect(correlate([run], run.source_uid, run.occurrence_key)?.id).toBe(run.id);
    demo.dispose();
  });
});

describe('Live requests', () => {
  it.each([401, 403, 409, 429, 500])('preserves status %s, trace and backoff', async (status) => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(
        new Response(
          JSON.stringify({ error: { code: 'TEST_ERROR', message: 'Request refused' } }),
          { status, headers: { 'X-Trace-ID': 'trace-fixture', 'Retry-After': '3' } },
        ),
      );
    vi.stubGlobal('fetch', fetcher);
    const client = liveClient('otj_fixture_key');
    await expect(client.jobs()).rejects.toMatchObject({
      status,
      trace: 'trace-fixture',
      retryAfter: 3000,
    });
    expect(fetcher).toHaveBeenCalledTimes(1);
    client.dispose();
  });
  it('uses only same-origin paths and keeps trigger intent across retries', async () => {
    const receipt = {
      namespace: 'default',
      name: 'run',
      occurrence_key: 'intent',
      trigger: 'Manual',
      phase: '',
      created: false,
    };
    const fetcher = vi
      .fn()
      .mockImplementation(() => Promise.resolve(new Response(JSON.stringify(receipt))));
    vi.stubGlobal('fetch', fetcher);
    const client = liveClient('otj_fixture_key');
    await client.trigger('9007199254740993', 'same-intent');
    await client.trigger('9007199254740993', 'same-intent');
    expect(fetcher.mock.calls[0][0]).toBe('/api/v1/jobs/9007199254740993/trigger');
    expect(fetcher.mock.calls[1][1].headers['X-OrbitJob-Idempotency-Key']).toBe('same-intent');
    expect(fetcher.mock.calls[0][1].credentials).toBe('omit');
    expect(fetcher.mock.calls[0][1].redirect).toBe('error');
    client.dispose();
  });
  it('does not treat unknown-filter HTTP 200 as declared support', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            paths: {
              '/api/v1/instances': { get: { parameters: [{ name: 'phase', in: 'query' }] } },
            },
          }),
        ),
      ),
    );
    const client = liveClient('otj_fixture_key');
    expect(await client.supportsCorrelation()).toBe(false);
    client.dispose();
  });
  it('rejects malformed successful responses', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{"items":[{"id":-1}]}')));
    const client = liveClient('otj_fixture_key');
    await expect(client.jobs()).rejects.toBeInstanceOf(APIError);
    client.dispose();
  });
  it.each([201, 429])(
    'classifies an interrupted write response body at status %s',
    async (status) => {
      const fetcher = vi.fn().mockImplementation(
        async () =>
          new Response(
            new ReadableStream({
              start(controller) {
                controller.error(new TypeError('Connection terminated after headers'));
              },
            }),
            { status, headers: { 'X-Trace-ID': 'body-fixture', 'Retry-After': '3' } },
          ),
      );
      vi.stubGlobal('fetch', fetcher);
      const client = liveClient('otj_fixture_key');
      const pending = client.create('checks', {
        name: 'uncertain-create',
        check_type: 'http_health',
        check_config: { url: 'https://example.com/health' },
        schedule_type: 'interval',
        interval_sec: 60,
      });
      await expect(pending).rejects.toBeInstanceOf(APIError);
      await expect(pending).rejects.toMatchObject({
        status,
        code: 'INVALID_RESPONSE',
        trace: 'body-fixture',
        retryAfter: 3000,
      });
      expect(fetcher).toHaveBeenCalledTimes(1);
      if (status === 429) {
        await expect(client.jobs()).rejects.toMatchObject({ status: 429 });
        expect(fetcher).toHaveBeenCalledTimes(1);
      }
      client.dispose();
    },
  );
  it('keeps disconnection silent while consuming a response body', async () => {
    let reading!: () => void;
    const started = new Promise<void>((resolve) => {
      reading = resolve;
    });
    vi.stubGlobal(
      'fetch',
      vi.fn(async (_path, init: RequestInit) => {
        const response = new Response(
          new ReadableStream({
            start(controller) {
              init.signal!.addEventListener('abort', () =>
                controller.error(new DOMException('closed', 'AbortError')),
              );
            },
          }),
        );
        const consume = response.text.bind(response);
        vi.spyOn(response, 'text').mockImplementation(() => {
          const body = consume();
          reading();
          return body;
        });
        return response;
      }),
    );
    const client = liveClient('otj_fixture_key');
    const pending = client.jobs();
    await started;
    client.dispose();
    await expect(pending).rejects.toMatchObject({ name: 'AbortError' });
  });
  it('cancels a previous connection request', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(
        (_path, init: RequestInit) =>
          new Promise((_resolve, reject) => {
            init.signal!.addEventListener('abort', () =>
              reject(new DOMException('closed', 'AbortError')),
            );
          }),
      ),
    );
    const client = liveClient('otj_fixture_key');
    const pending = client.jobs();
    client.dispose();
    await expect(pending).rejects.toMatchObject({ name: 'AbortError' });
  });
});

describe('rate-limit and cancellation boundary', () => {
  it('honors Retry-After longer than a minute without making an early manual request', async () => {
    const fetcher = vi.fn().mockResolvedValue(
      new Response('{"error":{"code":"RATE_LIMITED","message":"wait","field":"phase"}}', {
        status: 429,
        headers: { 'Retry-After': '180' },
      }),
    );
    vi.stubGlobal('fetch', fetcher);
    const client = liveClient('otj_fixture_key');
    await expect(client.jobs()).rejects.toMatchObject({ retryAfter: 180_000, field: 'phase' });
    await expect(client.jobs()).rejects.toMatchObject({ status: 429 });
    expect(fetcher).toHaveBeenCalledTimes(1);
    client.dispose();
  });
  it('rejects a malformed cancellation receipt', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{}')));
    const client = liveClient('otj_fixture_key');
    await expect(client.cancel(1042)).rejects.toMatchObject({ code: 'INVALID_RESPONSE' });
    client.dispose();
  });
});

it('does not schedule a rate-limited observation beyond its finite budget', async () => {
  const { observationDelay, retryDelay } = await import('./transport');
  expect(observationDelay(new APIError('wait', 429, 'RATE_LIMITED', '', 180_000), 0, 0)).toBe(
    false,
  );
  expect(observationDelay(new APIError('denied', 403), 0, 0)).toBe(false);
  expect(observationDelay(null, 0, 119_000)).toBe(false);
  expect(retryDelay('Thu, 01 Jan 1970 00:03:00 GMT', 0)).toBe(180_000);
});

it('honors an HTML rate-limit response before decoding its body', async () => {
  const fetcher = vi.fn().mockImplementation(() =>
    Promise.resolve(
      new Response('<h1>Wait</h1>', {
        status: 429,
        headers: { 'Retry-After': '180', 'X-Trace-ID': 'proxy-fixture' },
      }),
    ),
  );
  vi.stubGlobal('fetch', fetcher);
  const client = liveClient('otj_fixture_key');
  await expect(client.jobs()).rejects.toMatchObject({
    status: 429,
    retryAfter: 180_000,
    trace: 'proxy-fixture',
  });
  await expect(client.jobs()).rejects.toMatchObject({ status: 429 });
  expect(fetcher).toHaveBeenCalledTimes(1);
  client.dispose();
});
