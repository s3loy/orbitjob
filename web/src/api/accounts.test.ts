import { afterEach, describe, expect, it, vi } from 'vitest';
import { accountClient, accountEvents } from './accounts';
import { cookieClient } from './transport';

afterEach(() => vi.unstubAllGlobals());
const anonymous = { authenticated: false, initialized: true, workspaces: [] };
describe('browser account transport', () => {
  it.each(['', 'workspace-a'])(
    'refreshes workspace membership after creation with selected target %j',
    async (target) => {
      const row = { id: 'workspace-b', name: 'New workspace', slug: 'new', status: 'active' };
      const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify(row), { status: 201 }));
      vi.stubGlobal('fetch', fetcher);
      const refresh = vi.fn();
      accountEvents.addEventListener('refresh', refresh);
      const client = cookieClient('csrf', target);
      try {
        await expect(client.adminCreateTenant({ name: row.name, slug: row.slug })).resolves.toEqual(
          row,
        );
        const url = new URL(fetcher.mock.calls[0][0], 'http://console.invalid');
        expect(url.pathname).toBe('/api/v1/tenants');
        expect(url.searchParams.get('tenant_id')).toBe(target || null);
        expect(refresh).toHaveBeenCalledTimes(1);
      } finally {
        accountEvents.removeEventListener('refresh', refresh);
        client.dispose();
      }
    },
  );

  it.each([200, 401])(
    'discards a late account status %s after the session binding changes',
    async (status) => {
      let complete!: (response: Response) => void;
      const fetcher = vi
        .fn<typeof fetch>()
        .mockImplementationOnce(
          () =>
            new Promise<Response>((resolve) => {
              complete = resolve;
            }),
        )
        .mockResolvedValueOnce(new Response('{}'));
      vi.stubGlobal('fetch', fetcher);
      const unauthorized = vi.fn();
      accountEvents.addEventListener('unauthorized', unauthorized);
      try {
        const client = accountClient();
        client.setCSRF('old-session-binding');
        const pending = client.sessions();
        const oldSignal = fetcher.mock.calls[0][1]?.signal;
        client.setCSRF('new-session-binding');
        complete(new Response(JSON.stringify({ items: [] }), { status }));
        await expect(pending).rejects.toMatchObject({ name: 'AbortError' });
        expect(oldSignal?.aborted).toBe(true);
        expect(unauthorized).not.toHaveBeenCalled();
        await client.logout();
        expect(fetcher.mock.calls[1][1]?.headers).toEqual({
          'X-CSRF-Token': 'new-session-binding',
        });
        expect(fetcher.mock.calls[1][1]?.signal?.aborted).toBe(false);
      } finally {
        accountEvents.removeEventListener('unauthorized', unauthorized);
      }
    },
  );

  it('preserves an in-flight account response when a refresh keeps the same binding', async () => {
    let complete!: (response: Response) => void;
    const fetcher = vi.fn<typeof fetch>().mockImplementation(
      () =>
        new Promise<Response>((resolve) => {
          complete = resolve;
        }),
    );
    vi.stubGlobal('fetch', fetcher);
    const client = accountClient();
    client.setCSRF('unchanged-session-binding');
    const pending = client.sessions();
    client.setCSRF('unchanged-session-binding');
    expect(fetcher.mock.calls[0][1]?.signal?.aborted).toBe(false);
    complete(new Response(JSON.stringify({ items: [] })));
    await expect(pending).resolves.toEqual({ items: [] });
  });

  it('ignores an old unauthorized account response after the client lifetime changes', async () => {
    let complete!: (response: Response) => void;
    vi.stubGlobal(
      'fetch',
      vi.fn(
        () =>
          new Promise<Response>((resolve) => {
            complete = resolve;
          }),
      ),
    );
    const unauthorized = vi.fn();
    accountEvents.addEventListener('unauthorized', unauthorized);
    const client = accountClient();
    const pending = client.sessions();
    client.cancel();
    complete(new Response('{}', { status: 401 }));
    await expect(pending).rejects.toMatchObject({ name: 'AbortError' });
    expect(unauthorized).not.toHaveBeenCalled();
    accountEvents.removeEventListener('unauthorized', unauthorized);
  });

  it('restores via same-origin cookies without exposing a credential', async () => {
    const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify(anonymous)));
    vi.stubGlobal('fetch', fetcher);
    expect(await accountClient().restore()).toEqual(anonymous);
    expect(fetcher).toHaveBeenCalledWith(
      '/api/v1/auth/session',
      expect.objectContaining({ credentials: 'same-origin', method: 'GET', headers: {} }),
    );
  });
  it('sends the explicit remember choice and CSRF only on authenticated writes', async () => {
    const fetcher = vi
      .fn()
      .mockImplementation(() => Promise.resolve(new Response(JSON.stringify(anonymous))));
    vi.stubGlobal('fetch', fetcher);
    const client = accountClient();
    await client.login('alice', 'private-password', false);
    expect(JSON.parse(fetcher.mock.calls[0][1].body)).toEqual({
      username: 'alice',
      password: 'private-password',
      remember: false,
    });
    client.setCSRF('memory-only-csrf');
    await client.logout();
    expect(fetcher.mock.calls[1][1].headers).toEqual({ 'X-CSRF-Token': 'memory-only-csrf' });
  });
  it('uses cookie and CSRF on business writes and explicit workspace targeting', async () => {
    const fetcher = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          namespace: 'default',
          name: 'run',
          occurrence_key: 'occurrence',
          phase: 'Canceling',
        }),
      ),
    );
    vi.stubGlobal('fetch', fetcher);
    const client = cookieClient('memory-only-csrf', 'workspace-a');
    await client.cancel('123');
    expect(fetcher.mock.calls[0][0]).toBe('/api/v1/instances/123/cancel?tenant_id=workspace-a');
    expect(fetcher.mock.calls[0][1]).toMatchObject({
      credentials: 'same-origin',
      headers: { 'X-CSRF-Token': 'memory-only-csrf' },
    });
    expect(fetcher.mock.calls[0][1].headers).not.toHaveProperty('Authorization');
    client.dispose();
  });
  it.each([
    { label: 'selected workspace', managementTenant: undefined, expectedTenant: 'workspace-a' },
    {
      label: 'explicit management workspace',
      managementTenant: 'workspace-b',
      expectedTenant: 'workspace-b',
    },
  ])(
    'targets key rename and revoke at the $label without changing workload scope',
    async ({ managementTenant, expectedTenant }) => {
      const id = 'opaque/key?value';
      const fetcher = vi
        .fn<typeof fetch>()
        .mockResolvedValueOnce(new Response(JSON.stringify({ id, name: 'Renamed service' })))
        .mockResolvedValueOnce(new Response(JSON.stringify({ revoked: true })))
        .mockResolvedValueOnce(new Response(JSON.stringify({ total: 0, phases: {} })));
      vi.stubGlobal('fetch', fetcher);
      const client = cookieClient('memory-only-csrf', 'workspace-a');
      try {
        await client.adminRenameKey(id, 'Renamed service', managementTenant);
        await client.adminRevokeKey(id, managementTenant);
        const paths = fetcher.mock.calls.map(
          ([path]) => new URL(String(path), 'http://console.invalid'),
        );
        expect(paths.map((path) => path.pathname)).toEqual([
          '/api/v1/api_keys/opaque%2Fkey%3Fvalue',
          '/api/v1/api_keys/opaque%2Fkey%3Fvalue/revoke',
        ]);
        expect(paths.map((path) => path.searchParams.get('tenant_id'))).toEqual([
          expectedTenant,
          expectedTenant,
        ]);
        expect(fetcher.mock.calls.map(([, init]) => init?.method)).toEqual(['PATCH', 'POST']);
        expect(JSON.parse(String(fetcher.mock.calls[0][1]?.body))).toEqual({
          name: 'Renamed service',
        });
        for (const [, init] of fetcher.mock.calls) {
          expect(init).toMatchObject({ credentials: 'same-origin', redirect: 'error' });
          expect(init?.headers).toMatchObject({ 'X-CSRF-Token': 'memory-only-csrf' });
          expect(init?.headers).not.toHaveProperty('Authorization');
        }
        await client.runSummary();
        expect(
          new URL(String(fetcher.mock.calls[2][0]), 'http://console.invalid').searchParams.get(
            'tenant_id',
          ),
        ).toBe('workspace-a');
      } finally {
        client.dispose();
      }
    },
  );
  it.each([403, 500])('preserves a browser session on status %s', async (status) => {
    const handler = vi.fn();
    accountEvents.addEventListener('unauthorized', handler);
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ error: { code: 'forbidden', message: 'Denied' } }), {
          status,
        }),
      ),
    );
    const client = cookieClient('csrf');
    await expect(client.jobs()).rejects.toMatchObject({ status });
    expect(handler).not.toHaveBeenCalled();
    accountEvents.removeEventListener('unauthorized', handler);
    client.dispose();
  });
  it('clears protected state only after an actual unauthorized response', async () => {
    const handler = vi.fn();
    accountEvents.addEventListener('unauthorized', handler);
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{}', { status: 401 })));
    const client = cookieClient('csrf');
    await expect(client.jobs()).rejects.toMatchObject({ status: 401 });
    expect(handler).toHaveBeenCalledTimes(1);
    accountEvents.removeEventListener('unauthorized', handler);
    client.dispose();
  });
});

it('allows an explicit workspace for identity metadata while business reads retain the selected workspace', async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          key_id: '',
          subject_type: 'user',
          installation_admin: false,
          kind: 'tenant',
          tenant_id: 'workspace-b',
          group_id: '',
          boundary_policy_id: '',
          capabilities: [],
        }),
      ),
    )
    .mockResolvedValueOnce(new Response(JSON.stringify({ total: 0, phases: {} })));
  vi.stubGlobal('fetch', fetcher);
  const client = cookieClient('csrf', 'workspace-a');
  await client.identity(
    [{ action: 'apikey:Create', resource: 'orbitjob:workspace-b:*:apikey/*' }],
    'workspace-b',
  );
  expect(
    new URL(fetcher.mock.calls[0][0], 'http://console.invalid').searchParams.get('tenant_id'),
  ).toBe('workspace-b');
  const filter = { phase: 'Running', tenant_id: 'workspace-b' };
  await client.runSummary(filter);
  expect(
    new URL(fetcher.mock.calls[1][0], 'http://console.invalid').searchParams.get('tenant_id'),
  ).toBe('workspace-a');
  client.dispose();
});
