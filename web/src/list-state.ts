import { PAGE_SIZE } from './api/transport';

export function pageLimit(params: URLSearchParams) {
  const value = params.get('limit');
  return value && ['10', '20', '50', '100'].includes(value) ? Number(value) : PAGE_SIZE;
}

// Keep collection context in navigation history without accepting invalid offsets.
export function pageOffset(params: URLSearchParams) {
  const page = Number(params.get('page') ?? 1);
  const offset = (page - 1) * pageLimit(params);
  return Number.isSafeInteger(page) && page > 0 && offset <= 2_147_483_647 ? offset : 0;
}

export function pageParams(params: URLSearchParams, offset: number) {
  const next = new URLSearchParams(params);
  const limit = pageLimit(params);
  next.delete('offset');
  if (Number.isSafeInteger(offset) && offset > 0 && offset <= 2_147_483_647 && offset % limit === 0)
    next.set('page', String(offset / limit + 1));
  else next.delete('page');
  return next;
}

export function pageLimitParams(params: URLSearchParams, limit: number) {
  const next = new URLSearchParams(params);
  next.delete('page');
  next.delete('offset');
  if ([10, 50, 100].includes(limit)) next.set('limit', String(limit));
  else next.delete('limit');
  return next;
}
