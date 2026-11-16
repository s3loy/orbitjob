import { clearDefinitionSession } from './definition-state';
import { clearAdministrationSession } from './administration-state';
import { clearMonitoringSession } from './monitoring-creations';
import { useEffect, useRef, useState, useSyncExternalStore } from 'react';
import { Theme } from '@radix-ui/themes';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BrowserRouter, HashRouter } from 'react-router-dom';
import { publicDemo } from './deployment';
import { createClient } from '#console-profile';
import { APIError, type Client } from './api/transport';
import type { Identity } from './api/identity';
import { accountClient, accountEvents, type AccountSnapshot } from './api/accounts';
import { SessionContext, type Mode } from './session';
import { App } from './App';
import { clearAccountCapability } from './account-capability';
import { Reauthentication } from './Reauthentication';

const media = window.matchMedia('(prefers-color-scheme: dark)');
const subscribeTheme = (listener: () => void) => {
  media.addEventListener('change', listener);
  return () => media.removeEventListener('change', listener);
};
const themeSnapshot = () => (media.matches ? ('dark' as const) : ('light' as const));
const Router = publicDemo ? HashRouter : BrowserRouter;

export function Root() {
  const appearance = useSyncExternalStore(subscribeTheme, themeSnapshot);
  const counter = useRef(0);
  const [account, setAccount] = useState<AccountSnapshot>();
  const accountRef = useRef<AccountSnapshot | undefined>(undefined);
  const accountRevision = useRef(0);
  const [accounts] = useState(accountClient);
  function updateAccount(snapshot?: AccountSnapshot) {
    if (accountRef.current?.user?.id !== snapshot?.user?.id) accounts.cancel();
    ++accountRevision.current;
    accountRef.current = snapshot;
    accounts.setCSRF(snapshot?.csrf_token || '');
    setAccount(snapshot);
  }
  const [restoring, setRestoring] = useState(true);
  const [restoreError, setRestoreError] = useState<Error>();
  const [reauthenticate, setReauthenticate] = useState(false);
  const connectionAttempt = useRef(0);
  const pendingClient = useRef<Client | null>(null);
  const restoreAttempt = useRef(0);
  const [session, setSession] = useState<{
    generation: number;
    mode: Mode;
    client: Client | null;
    targetTenant?: string;
    identity?: Identity;
  }>(() => ({
    generation: 0,
    mode: 'disconnected',
    client: null,
  }));
  const sessionRef = useRef(session);
  const [queryClient] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: { retry: false, staleTime: 10_000, refetchOnWindowFocus: false },
          mutations: { retry: false },
        },
      }),
  );
  async function change(mode: Mode, csrf = '', targetTenant = '') {
    const attempt = ++connectionAttempt.current;
    const accountBinding = accountRef.current;
    const current = () =>
      attempt === connectionAttempt.current &&
      accountBinding?.authenticated === accountRef.current?.authenticated &&
      accountBinding?.user?.id === accountRef.current?.user?.id &&
      accountBinding?.csrf_token === accountRef.current?.csrf_token;
    pendingClient.current?.dispose();
    pendingClient.current = null;
    if (publicDemo) {
      mode = mode === 'disconnected' ? 'disconnected' : 'demo';
      csrf = '';
      targetTenant = '';
    }
    const client = createClient(mode, csrf, targetTenant);
    pendingClient.current = client;
    let identity: Identity | undefined;
    try {
      if (client) {
        identity = await client.identity();
        if (
          mode === 'live' &&
          targetTenant &&
          identity.target_tenant_id !== targetTenant &&
          identity.tenant_id !== targetTenant
        )
          throw new APIError(
            'The installation did not confirm the selected workspace. Refresh your account and try again.',
            200,
            'INVALID_RESPONSE',
          );
      }
    } catch (error) {
      client?.dispose();
      if (pendingClient.current === client) pendingClient.current = null;
      if (!current()) return;
      throw error;
    }
    if (pendingClient.current === client) pendingClient.current = null;
    if (!current()) {
      client?.dispose();
      return;
    }
    const previous = sessionRef.current;
    if (previous.client) {
      clearAdministrationSession(previous.client);
      clearDefinitionSession(previous.client);
      clearMonitoringSession(previous.client);
    }
    previous.client?.dispose();

    void queryClient.cancelQueries();
    queryClient.clear();
    if (identity) queryClient.setQueryData(['identity', counter.current + 1], identity);
    const next = { generation: ++counter.current, mode, client, targetTenant, identity };
    sessionRef.current = next;
    setSession(next);
    return identity;
  }
  async function authenticate(snapshot: AccountSnapshot) {
    if (!snapshot.authenticated && accountRef.current?.authenticated) clearAccountCapability();
    updateAccount(snapshot);
    setRestoreError(undefined);
    if (!snapshot.authenticated || !snapshot.user) {
      await change('disconnected');
      return;
    }
    let preferred = '';
    try {
      preferred = sessionStorage.getItem(`orbitjob.workspace.${snapshot.user.id}`) || '';
    } catch {
      /* Storage is optional. */
    }
    const tenant =
      snapshot.workspaces.find((item) => item.id === preferred)?.id ||
      snapshot.workspaces[0]?.id ||
      '';
    await change('live', snapshot.csrf_token || '', tenant);
  }
  async function restore() {
    const attempt = ++restoreAttempt.current;
    const revision = accountRevision.current;
    setRestoring(true);
    setRestoreError(undefined);
    try {
      if (publicDemo) await change('demo');
      else {
        const snapshot = await accounts.restore();
        if (attempt !== restoreAttempt.current || revision !== accountRevision.current) return;
        await authenticate(snapshot);
      }
    } catch (error) {
      if (attempt === restoreAttempt.current)
        setRestoreError(
          error instanceof Error ? error : new Error('Could not restore your session.'),
        );
    } finally {
      if (attempt === restoreAttempt.current) setRestoring(false);
    }
  }
  const latest = useRef({ change, restore, session, authenticate });
  useEffect(() => {
    latest.current = { change, restore, session, authenticate };
  });
  useEffect(() => {
    void latest.current.restore();
    const cancel = () => {
      ++restoreAttempt.current;
      ++connectionAttempt.current;
      pendingClient.current?.dispose();
      pendingClient.current = null;
      sessionRef.current.client?.dispose();
      accounts.cancel();
    };
    if (publicDemo) return cancel;
    const clear = () => {
      ++restoreAttempt.current;
      setRestoring(false);
      setRestoreError(
        new APIError('Your session has ended. Sign in again to continue.', 401, 'unauthorized'),
      );
      clearAccountCapability();
      updateAccount(undefined);
      setReauthenticate(false);
      void latest.current.change('disconnected');
    };
    const reauth = () => setReauthenticate(true);
    const refresh = async () => {
      const revision = accountRevision.current;
      try {
        const next = await accounts.restore();
        if (revision !== accountRevision.current) return;
        const target = latest.current.session.targetTenant;
        const previousRole = accountRef.current?.workspaces.find((row) => row.id === target)?.role;
        const nextRole = next.workspaces.find((row) => row.id === target)?.role;
        const membershipRevoked = accountRef.current?.workspaces.some(
          (previous) =>
            !next.workspaces.some(
              (current) => current.id === previous.id && current.role === previous.role,
            ),
        );
        if (
          membershipRevoked ||
          accountRef.current?.user?.installation_admin !== next.user?.installation_admin ||
          accountRef.current?.user?.id !== next.user?.id ||
          accountRef.current?.csrf_token !== next.csrf_token ||
          previousRole !== nextRole ||
          !next.authenticated ||
          (target && !next.workspaces.some((row) => row.id === target))
        )
          await latest.current.authenticate(next);
        else {
          updateAccount(next);
        }
      } catch {
        /* A temporary outage does not invalidate the session. */
      }
    };
    accountEvents.addEventListener('unauthorized', clear);
    accountEvents.addEventListener('reauthenticate', reauth);
    accountEvents.addEventListener('refresh', refresh);
    return () => {
      cancel();
      accountEvents.removeEventListener('unauthorized', clear);
      accountEvents.removeEventListener('reauthenticate', reauth);
      accountEvents.removeEventListener('refresh', refresh);
    };
    // Session changes use refs so restoration runs once per mounted installation.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [accounts]);
  return (
    <Theme
      appearance={appearance}
      accentColor="gray"
      grayColor="gray"
      radius="medium"
      scaling="100%"
      panelBackground="solid"
    >
      <QueryClientProvider client={queryClient}>
        <SessionContext
          value={{
            ...session,
            restoring,
            restoreError,
            account,
            accounts,
            restore,
            authenticated: authenticate,
            connect: (mode) => change(mode),
            selectTenant: async (tenant) => {
              if (!accountRef.current?.workspaces.some((row) => row.id === tenant))
                throw new Error('You are not an active member of this workspace.');
              const result = await change('live', accountRef.current.csrf_token || '', tenant);
              try {
                sessionStorage.setItem(`orbitjob.workspace.${accountRef.current.user!.id}`, tenant);
              } catch {
                /* Storage is optional. */
              }
              return result;
            },
            disconnect: async () => {
              ++restoreAttempt.current;
              ++connectionAttempt.current;
              accounts.cancel();
              setRestoring(false);
              let failure: unknown;
              if (session.mode === 'live') {
                try {
                  await accounts.logout();
                } catch (error) {
                  if (!(error instanceof APIError && error.status === 401)) failure = error;
                }
              }
              clearAccountCapability();
              updateAccount(undefined);
              setReauthenticate(false);
              await change(publicDemo ? 'demo' : 'disconnected');
              if (failure)
                setRestoreError(
                  new Error(
                    'Local data was cleared, but the server could not confirm sign out. Your server session may still be active. Retry restoring the session and sign out when the installation is reachable.',
                  ),
                );
            },
          }}
        >
          <Router basename={publicDemo ? undefined : '/console'}>
            <App key={session.generation} />
            {reauthenticate && (
              <Reauthentication
                accounts={accounts}
                onDone={(snapshot) => {
                  updateAccount(snapshot);
                  setReauthenticate(false);
                }}
                onCancel={() => setReauthenticate(false)}
              />
            )}
          </Router>
        </SessionContext>
      </QueryClientProvider>
    </Theme>
  );
}
