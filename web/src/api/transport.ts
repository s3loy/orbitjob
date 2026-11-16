import { definitionClient, type DefinitionClient } from './definitions';
import { archiveClient, type ArchiveClient } from './archive';
import { administrationClient, type AdministrationClient } from './administration';
import { identityClient, type IdentityClient } from './identity';
import { sourceControlClient, type SourceControlClient } from './source-controls';
import { parse, stringify, LosslessNumber } from 'lossless-json';
import { monitoringClient, type MonitoringClient } from './monitoring';
import { m4Client, type M4Client } from './m4';
import { z } from 'zod';
import { accountEvents } from './accounts';
import {
  type ID,
  type Job,
  type JobDetail,
  type Run,
  type RunDetail,
  type Receipt,
  jobsSchema,
  jobDetailSchema,
  runsSchema,
  runDetailSchema,
  receiptSchema,
  cancelSchema,
} from './contracts';

export class APIError extends Error {
  constructor(
    message: string,
    public status = 0,
    public code = 'NETWORK',
    public trace = '',
    public retryAfter = 0,
    public field = '',
  ) {
    super(message);
  }
}

export function decode(text: string): unknown {
  return parse(text, null, (raw) => {
    const value = Number(raw);
    return /^-?\d+$/.test(raw) && !Number.isSafeInteger(value) ? raw : value;
  });
}

export type RunFilter = {
  created_from?: string;
  created_before?: string;
  phase?: string;
  source_uid?: string;
  occurrence_key?: string;
  offset?: number;
  limit?: number;
};
export interface Client
  extends
    MonitoringClient,
    M4Client,
    AdministrationClient,
    IdentityClient,
    DefinitionClient,
    SourceControlClient,
    ArchiveClient {
  jobs(
    offset?: number,
    search?: string,
    filter?: { namespace?: string; source_uid?: string; limit?: number },
  ): Promise<Job[]>;
  job(id: ID): Promise<JobDetail>;
  runs(filter?: RunFilter): Promise<Run[]>;
  runSummary(
    filter?: RunFilter,
  ): Promise<{ total: number | string; phases: Record<string, number | string> }>;
  run(id: ID): Promise<RunDetail>;
  trigger(id: ID, key: string): Promise<Receipt>;
  cancel(id: ID): Promise<void>;
  supportsCorrelation(): Promise<boolean>;
  dispose(): void;
}
export const PAGE_SIZE = 20;
export const OBSERVATION_MS = 120_000;
export const POLL_MS = 2_000;

export function cookieClient(csrf: string, targetTenant = ''): Client {
  return liveClient('', targetTenant, csrf);
}

export function liveClient(key: string, targetTenant = '', csrf?: string): Client {
  if (import.meta.env.MODE === 'pages')
    throw new Error('Live connections are disabled in the public Demo.');
  const lifetime = new AbortController();
  let limitedUntil = 0;
  async function request<T>(
    path: string,
    schema: z.ZodType<T>,
    method = 'GET',
    intent?: string,
    payload?: unknown,
  ): Promise<T> {
    if (Date.now() < limitedUntil)
      throw new APIError(
        'Wait for the API rate limit before retrying.',
        429,
        'RATE_LIMITED',
        '',
        limitedUntil - Date.now(),
      );
    if (
      targetTenant &&
      (/^\/api\/v1\/(identity|definitions|jobs|instances|workflows|functions|checks|check-runs|slis|slos|slo-alerts|policies|resource_groups|api_keys)([/?]|$)/.test(
        path,
      ) ||
        /^\/api\/v1\/tenants(?:\/[^/?]+)?(?:\?|$)/.test(path))
    ) {
      const url = new URL(path, 'http://console.invalid');
      const explicitTarget =
        (url.pathname === '/api/v1/identity' || url.pathname.startsWith('/api/v1/api_keys/')) &&
        url.searchParams.has('tenant_id');
      if (!explicitTarget) url.searchParams.set('tenant_id', targetTenant);
      path = url.pathname + url.search;
    }
    let response: Response;
    try {
      response = await fetch(path, {
        method,
        credentials: csrf === undefined ? 'omit' : 'same-origin',
        redirect: 'error',
        signal: AbortSignal.any([lifetime.signal, AbortSignal.timeout(15_000)]),
        headers: {
          ...(csrf === undefined ? { Authorization: `Bearer ${key}` } : {}),
          ...(csrf !== undefined && method !== 'GET' ? { 'X-CSRF-Token': csrf } : {}),
          ...(intent ? { 'X-OrbitJob-Idempotency-Key': intent } : {}),
          ...(intent || payload !== undefined ? { 'Content-Type': 'application/json' } : {}),
        },
        ...(payload !== undefined
          ? {
              body: stringify(payload, (name, value) =>
                name === 'sli_id' && typeof value === 'string' && /^[1-9]\d*$/.test(value)
                  ? new LosslessNumber(value)
                  : value,
              ),
            }
          : intent
            ? { body: '{}' }
            : {}),
      });
    } catch {
      if (lifetime.signal.aborted) throw new DOMException('Connection closed', 'AbortError');
      throw new APIError('Could not reach the API. Check the connection and try again.');
    }
    const trace = response.headers.get('X-Trace-ID') ?? '';
    const retryAfter = retryDelay(response.headers.get('Retry-After'));
    if (response.status === 429) limitedUntil = Date.now() + retryAfter;
    let body: unknown;
    try {
      const text = await response.text();
      body = text ? decode(text) : {};
    } catch {
      if (lifetime.signal.aborted) throw new DOMException('Connection closed', 'AbortError');
      throw new APIError(
        'The API returned an unreadable response.',
        response.status,
        'INVALID_RESPONSE',
        trace,
        retryAfter,
      );
    }
    if (!response.ok) {
      if (csrf !== undefined && response.status === 401)
        accountEvents.dispatchEvent(new Event('unauthorized'));
      if (csrf !== undefined && response.status === 403)
        accountEvents.dispatchEvent(new Event('refresh'));
      const error = z
        .object({
          error: z.object({ message: z.string(), code: z.string(), field: z.string().optional() }),
        })
        .safeParse(body);
      if (
        csrf !== undefined &&
        error.success &&
        error.data.error.code.toLowerCase() === 'recent_auth_required'
      )
        accountEvents.dispatchEvent(new Event('reauthenticate'));
      throw new APIError(
        error.success ? error.data.error.message : 'The API request was rejected.',
        response.status,
        error.success ? error.data.error.code : 'HTTP_ERROR',
        trace,
        retryAfter,
        error.success ? (error.data.error.field ?? '') : '',
      );
    }
    const result = schema.safeParse(body);
    if (!result.success)
      throw new APIError(
        'The API response does not match the supported contract.',
        response.status,
        'INVALID_RESPONSE',
        trace,
      );
    if (
      csrf !== undefined &&
      method === 'POST' &&
      new URL(path, 'http://console.invalid').pathname === '/api/v1/tenants'
    )
      accountEvents.dispatchEvent(new Event('refresh'));
    return result.data;
  }
  const list = (filter: RunFilter = {}) => {
    const params = new URLSearchParams({ limit: String(PAGE_SIZE) });
    Object.entries(filter).forEach(([name, value]) => {
      if (value !== undefined && value !== '') params.set(name, String(value));
    });
    return `/api/v1/instances?${params}`;
  };
  return {
    ...administrationClient(request, PAGE_SIZE),
    ...definitionClient(request),
    ...archiveClient(request),
    ...identityClient(request),
    ...sourceControlClient(request),
    ...monitoringClient(request, PAGE_SIZE),
    ...m4Client(request, PAGE_SIZE),
    jobs: async (offset = 0, search = '', filter = {}) =>
      (
        await request(
          `/api/v1/jobs?limit=${filter.limit ?? PAGE_SIZE}&offset=${offset}${search ? `&search=${encodeURIComponent(search)}` : ''}${filter.namespace ? `&namespace=${encodeURIComponent(filter.namespace)}` : ''}${filter.source_uid ? `&source_uid=${encodeURIComponent(filter.source_uid)}` : ''}`,
          jobsSchema,
        )
      ).items,
    job: (id) => request(`/api/v1/jobs/${id}`, jobDetailSchema),
    runs: async (filter) => (await request(list(filter), runsSchema)).items,
    runSummary: (filter = {}) => {
      const query = new URLSearchParams();
      for (const [k, v] of Object.entries(filter))
        if (v !== undefined && v !== '' && !['offset', 'limit'].includes(k))
          query.set(k, String(v));
      const count = z.union([
        z.number().int().nonnegative().refine(Number.isSafeInteger),
        z.string().regex(/^\d+$/),
      ]);
      return request(
        `/api/v1/instances/summary?${query}`,
        z.object({ total: count, phases: z.record(z.string(), count) }),
      );
    },
    run: (id) => request(`/api/v1/instances/${id}`, runDetailSchema),
    trigger: (id, intent) => request(`/api/v1/jobs/${id}/trigger`, receiptSchema, 'POST', intent),
    cancel: async (id) => {
      await request(`/api/v1/instances/${id}/cancel`, cancelSchema, 'POST');
    },
    supportsCorrelation: async () => {
      const doc = await request(
        '/openapi.json',
        z.object({
          paths: z.record(z.string(), z.unknown()),
        }),
      );
      const operation = z
        .object({
          get: z.object({
            parameters: z.array(z.object({ name: z.string(), in: z.string() })).optional(),
          }),
        })
        .safeParse(doc.paths['/api/v1/instances']);
      return (
        operation.success &&
        ['source_uid', 'occurrence_key'].every((name) =>
          operation.data.get.parameters?.some((p) => p.name === name && p.in === 'query'),
        )
      );
    },
    dispose: () => lifetime.abort(),
  };
}

export function retryDelay(header: string | null, now = Date.now()): number {
  if (!header) return 0;
  const delay = Number.isFinite(Number(header)) ? Number(header) * 1000 : Date.parse(header) - now;
  return Number.isFinite(delay) ? Math.max(0, delay) : 0;
}

export function observationDelay(
  error: Error | null,
  since: number,
  now = Date.now(),
): number | false {
  if (error && !(error instanceof APIError && error.status === 429)) return false;
  const delay = error instanceof APIError ? Math.max(POLL_MS, error.retryAfter) : POLL_MS;
  return now + delay < since + OBSERVATION_MS ? delay : false;
}
