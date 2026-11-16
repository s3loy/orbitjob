import { z } from 'zod';

export type QueryWindow = { created_from?: string; created_before?: string; error?: string };
const timestamp = z.string().datetime({ offset: true });
// Keep precision explicit: Go truncates fractions beyond nanoseconds; the UI
// rejects those inputs instead of silently merging distinct user bounds.
export function timestampNanos(value: string): bigint | undefined {
  const match = /^(.*T\d{2}:\d{2}:\d{2})(?:\.(\d{1,9}))?(Z|[+-]\d{2}:\d{2})$/.exec(value);
  if (!match || !timestamp.safeParse(value).success) return undefined;
  const date = new Date(`${match[1]}${match[3]}`);
  if (!Number.isFinite(date.getTime()) || date.getUTCFullYear() < 1 || date.getUTCFullYear() > 9999)
    return undefined;
  return BigInt(date.getTime()) * 1_000_000n + BigInt((match[2] ?? '').padEnd(9, '0'));
}
export function queryWindow(params: URLSearchParams): QueryWindow {
  const created_from = params.get('created_from') || undefined;
  const created_before = params.get('created_before') || undefined;
  const from = created_from ? timestampNanos(created_from) : undefined;
  const before = created_before ? timestampNanos(created_before) : undefined;
  for (const [name, value, parsed] of [
    ['From', created_from, from],
    ['Before', created_before, before],
  ] as const) {
    if (value && parsed === undefined)
      return {
        error: `${name} must be an RFC3339 timestamp with seconds, timezone and at most 9 fractional digits, such as 2026-10-19T00:00:00Z.`,
      };
  }
  if (from !== undefined && before !== undefined && from >= before)
    return { error: 'Before must be after From. From is inclusive and Before is exclusive.' };
  return { created_from, created_before };
}
export function queryWindowParams(params: URLSearchParams, from: string, before: string) {
  const next = new URLSearchParams(params);
  for (const [name, value] of [
    ['created_from', from.trim()],
    ['created_before', before.trim()],
  ]) {
    if (value) next.set(name, value);
    else next.delete(name);
  }
  next.delete('offset');
  next.delete('page');
  return next;
}
export function historyOffset(params: URLSearchParams, limit: number) {
  const raw = params.get('offset');
  const offset = raw === null ? (Number(params.get('page') ?? 1) - 1) * limit : Number(raw);
  return Number.isSafeInteger(offset) && offset >= 0 && offset <= 2_147_483_647 ? offset : 0;
}
export function historyPageParams(params: URLSearchParams, offset: number) {
  const next = new URLSearchParams(params);
  next.delete('page');
  if (offset) next.set('offset', String(offset));
  else next.delete('offset');
  return next;
}
export function collectionSearch(params: URLSearchParams) {
  const search = params.get('search') ?? '';
  return {
    search,
    error: search.includes('\0')
      ? 'Search must not contain a NUL character.'
      : Array.from(search).length > 128
        ? 'Search must be at most 128 characters.'
        : undefined,
  };
}
export function collectionSearchParams(params: URLSearchParams, search: string) {
  const next = new URLSearchParams(params);
  if (search) next.set('search', search);
  else next.delete('search');
  next.delete('offset');
  next.delete('page');
  return next;
}
