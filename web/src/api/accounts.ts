import { z } from 'zod';
import { APIError, retryDelay } from './transport';

export const accountEvents = new EventTarget();
const userSchema = z.object({
  id: z.string(),
  username: z.string(),
  display_name: z.string().optional(),
  installation_admin: z.boolean(),
});
export const sessionSchema = z.object({
  authenticated: z.boolean(),
  initialized: z.boolean(),
  user: userSchema.optional(),
  expires_at: z.string().optional(),
  csrf_token: z.string().optional(),
  recent_auth_until: z.string().optional(),
  workspaces: z.array(
    z.object({ id: z.string(), name: z.string(), slug: z.string(), role: z.string() }),
  ),
});
export type AccountSnapshot = z.infer<typeof sessionSchema>;
export type AccountUser = z.infer<typeof userSchema> & { status?: string };
export type Member = { user: AccountUser; tenant_id: string; role: string; status: string };
export type AccountSession = {
  id: string;
  created_at: string;
  expires_at: string;
  last_seen_at?: string;
  current: boolean;
};
export type OneTimeLink = { token: string; expires_at: string };

export function accountClient() {
  let csrfToken = '';
  let lifetime = new AbortController();
  function cancel() {
    lifetime.abort();
    lifetime = new AbortController();
  }
  async function request<T>(
    path: string,
    method = 'GET',
    input?: unknown,
    protectedRoute = true,
  ): Promise<T> {
    if (import.meta.env.MODE === 'pages')
      throw new Error('Accounts are unavailable in the public demo.');
    const requestLifetime = lifetime;
    let response: Response;
    try {
      response = await fetch(`/api/v1/auth${path}`, {
        method,
        credentials: 'same-origin',
        redirect: 'error',
        signal: AbortSignal.any([requestLifetime.signal, AbortSignal.timeout(15_000)]),
        headers: {
          ...(input !== undefined ? { 'Content-Type': 'application/json' } : {}),
          ...(method !== 'GET' && csrfToken ? { 'X-CSRF-Token': csrfToken } : {}),
        },
        ...(input !== undefined ? { body: JSON.stringify(input) } : {}),
      });
    } catch {
      if (requestLifetime.signal.aborted)
        throw new DOMException('Account session changed', 'AbortError');
      throw new APIError(
        'Could not reach the installation. Your server session has not been signed out.',
      );
    }
    const body = await response.json().catch(() => ({}));
    if (requestLifetime.signal.aborted)
      throw new DOMException('Account session changed', 'AbortError');
    if (!response.ok) {
      if (protectedRoute && response.status === 401)
        accountEvents.dispatchEvent(new Event('unauthorized'));
      if (body.error?.code?.toLowerCase() === 'recent_auth_required')
        accountEvents.dispatchEvent(new Event('reauthenticate'));
      throw new APIError(
        body.error?.message || 'The account request was rejected.',
        response.status,
        body.error?.code || 'HTTP_ERROR',
        response.headers.get('X-Trace-ID') || '',
        retryDelay(response.headers.get('Retry-After')),
      );
    }
    if (method === 'PATCH' && /^\/(members|users)\//.test(path))
      accountEvents.dispatchEvent(new Event('refresh'));
    return body as T;
  }
  async function snapshot(path: string, input?: unknown) {
    return sessionSchema.parse(await request(path, input ? 'POST' : 'GET', input, false));
  }
  return {
    cancel,
    setCSRF: (token: string) => {
      if (token !== csrfToken) cancel();
      csrfToken = token;
    },
    restore: () => snapshot('/session'),
    login: (username: string, password: string, remember: boolean) =>
      snapshot('/login', { username, password, remember }),
    setup: (token: string, username: string, password: string, display_name: string) =>
      snapshot('/setup', { token, username, password, display_name }),
    accept: (input: {
      token: string;
      username?: string;
      password?: string;
      display_name?: string;
    }) => snapshot('/invitations/accept', input),
    reset: (token: string, password: string) =>
      request<void>('/password/reset', 'POST', { token, password }, false),
    changePassword: (current_password: string, password: string) =>
      request<void>('/password/change', 'POST', { current_password, password }, false),
    logout: () => request<void>('/logout', 'POST'),
    reauth: (password: string) => snapshot('/reauth', { password }),
    sessions: () => request<{ items: AccountSession[] }>('/sessions'),
    revoke: (id: string) => request<void>(`/sessions/${encodeURIComponent(id)}`, 'DELETE'),
    members: (tenant: string) =>
      request<{ items: Member[] }>(`/members?tenant_id=${encodeURIComponent(tenant)}`),
    invite: (tenant_id: string, role: string, username?: string) =>
      request<OneTimeLink>('/invitations', 'POST', {
        tenant_id,
        role,
        ...(username ? { username } : {}),
      }),
    updateMember: (id: string, tenant_id: string, changes: { role?: string; status?: string }) =>
      request<void>(`/members/${encodeURIComponent(id)}`, 'PATCH', { tenant_id, ...changes }),
    resetMember: (id: string, tenant_id: string) =>
      request<OneTimeLink>(`/members/${encodeURIComponent(id)}/password-reset`, 'POST', {
        tenant_id,
      }),
    users: () => request<{ items: AccountUser[] }>('/users'),
    updateUser: (id: string, changes: { status?: string; installation_admin?: boolean }) =>
      request<void>(`/users/${encodeURIComponent(id)}`, 'PATCH', changes),
  };
}
export type AccountClient = ReturnType<typeof accountClient>;
