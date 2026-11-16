import './administration.css';
import { ConnectionIdentity } from './ConnectionIdentity';
import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Button, Heading, Text, TextField } from '@radix-ui/themes';
import { Link, useNavigate } from 'react-router-dom';
import { APIError } from './api/transport';
import { newAccountPasswordError } from './account-validation';
import { useSession } from './session';
import { ErrorState, Loading } from './components';
import { administrationScopeLocked } from './administration-state';

export function AccountWorkspace({ onReady }: { onReady(): void }) {
  const session = useSession();
  const navigate = useNavigate();
  const [target, setTarget] = useState(session.targetTenant || '');
  const [error, setError] = useState<Error>();
  const [pending, setPending] = useState(false);
  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmation, setConfirmation] = useState('');
  const [passwordError, setPasswordError] = useState<Error>();
  const user = session.account?.user;
  const accountName = user?.display_name || user?.username;
  const sessions = useQuery({
    queryKey: ['account-sessions', session.generation],
    queryFn: () => session.accounts!.sessions(),
    enabled: !!session.accounts,
  });
  return (
    <section className="account-workspace" aria-label="Account and workspace">
      <header className="account-profile">
        <Heading as="h2" size="4">
          {accountName}
        </Heading>
        {user?.username && user.username !== accountName && (
          <Text as="p" size="2" color="gray">
            {user.username}
          </Text>
        )}
        {user?.installation_admin && (
          <Text as="p" size="2" color="gray">
            Installation administrator
          </Text>
        )}
      </header>
      <form
        className="account-workspace-form"
        onSubmit={async (event) => {
          event.preventDefault();
          if (pending) return;
          setPending(true);
          setError(undefined);
          try {
            if (session.client && administrationScopeLocked(session.client))
              throw new Error(
                'Finish pending access changes and save or discard revealed credentials before switching workspaces.',
              );
            await session.selectTenant?.(target);
            onReady();
          } catch (cause) {
            setError(cause instanceof Error ? cause : new Error('Could not select workspace.'));
          } finally {
            setPending(false);
          }
        }}
      >
        {!!session.account?.workspaces.length && (
          <label className="form-field">
            Workspace
            <select
              aria-label="Workspace"
              value={target}
              onChange={(event) => setTarget(event.target.value)}
              required
              disabled={pending}
            >
              <option value="" disabled>
                Choose a workspace
              </option>
              {session.account?.workspaces.map((row) => (
                <option key={row.id} value={row.id}>
                  {row.name} ({row.role})
                </option>
              ))}
            </select>
          </label>
        )}
        {!!session.account?.workspaces.length && (
          <Button type="submit" disabled={pending || !target}>
            Open workspace
          </Button>
        )}
        {target && target !== session.targetTenant && (
          <Text as="p" size="1" color="gray" className="account-workspace-notice">
            Switching workspaces clears loaded records and local drafts.
          </Text>
        )}
        {!session.account?.workspaces.length && (
          <Text as="p" size="2" mt="3">
            {user?.installation_admin ? (
              <>
                <Link to="/administration" className="text-link">
                  Create a workspace in Administration
                </Link>{' '}
                to begin.
              </>
            ) : (
              'You do not belong to an active workspace. Ask an administrator for an invitation.'
            )}
          </Text>
        )}
        {error && <ErrorState error={error} />}
      </form>
      <ConnectionIdentity />
      <Heading as="h2" size="3" className="account-security-heading">
        Security
      </Heading>
      <details className="advanced-details account-security-details">
        <summary>Change password</summary>
        <Text as="p" size="2" color="gray" mt="3">
          Changing your password signs out every session. Sign in again with your new password.
        </Text>
        <form
          onSubmit={async (event) => {
            event.preventDefault();
            if (pending) return;
            setPending(true);
            setPasswordError(undefined);
            try {
              const passwordError = newAccountPasswordError(newPassword);
              if (passwordError) throw new Error(passwordError);
              if (newPassword !== confirmation) throw new Error('The passwords do not match.');
              await session.accounts!.changePassword(currentPassword, newPassword);
              await session.authenticated?.({
                authenticated: false,
                initialized: true,
                workspaces: [],
              });
              navigate('/connection', {
                replace: true,
                state: {
                  notice:
                    'Password changed. All sessions were signed out. Sign in with your new password.',
                },
              });
            } catch (cause) {
              if (cause instanceof APIError && cause.status === 401) {
                try {
                  const snapshot = await session.accounts!.restore();
                  if (!snapshot.authenticated) await session.authenticated?.(snapshot);
                } catch {
                  /* A failed confirmation does not prove the browser session was revoked. */
                }
              }
              setPasswordError(
                cause instanceof APIError && cause.status === 401
                  ? new Error(
                      'Your current password was not accepted. Enter it again to change your password.',
                    )
                  : cause instanceof Error
                    ? cause
                    : new Error('Could not change your password.'),
              );
            } finally {
              setCurrentPassword('');
              setNewPassword('');
              setConfirmation('');
              setPending(false);
            }
          }}
        >
          <label className="form-field">
            Current password
            <TextField.Root
              type="password"
              autoComplete="current-password"
              value={currentPassword}
              onChange={(event) => setCurrentPassword(event.target.value)}
              required
            />
          </label>
          <label className="form-field">
            New password
            <TextField.Root
              type="password"
              autoComplete="new-password"
              value={newPassword}
              onChange={(event) => setNewPassword(event.target.value)}
              required
              minLength={15}
            />
          </label>
          <Text as="p" size="1" color="gray" mt="2">
            Use at least 15 characters for your new password.
          </Text>
          <label className="form-field">
            Confirm new password
            <TextField.Root
              type="password"
              autoComplete="new-password"
              value={confirmation}
              onChange={(event) => setConfirmation(event.target.value)}
              required
            />
          </label>
          <Button type="submit" mt="3" disabled={pending}>
            Change password and sign out
          </Button>
          {passwordError && <ErrorState error={passwordError} />}
        </form>
      </details>
      <details className="advanced-details account-security-details">
        <summary>Signed-in sessions</summary>
        <Text as="p" size="2" color="gray" mt="2">
          Revoke a session to sign that browser out. Revoking this session clears this console’s
          local data.
        </Text>
        {sessions.isPending ? (
          <Loading />
        ) : sessions.error ? (
          <ErrorState error={sessions.error} retry={() => void sessions.refetch()} />
        ) : (
          <ul className="account-session-list">
            {sessions.data?.items.map((row) => (
              <li key={row.id}>
                <div>
                  <Text as="p" size="2" weight="medium">
                    {row.current ? 'This session' : 'Another browser session'}
                  </Text>
                  <Text as="p" size="1" color="gray" mt="1">
                    Expires {new Date(row.expires_at).toLocaleString()}
                  </Text>
                </div>
                <Button
                  size="1"
                  variant="outline"
                  disabled={pending}
                  onClick={async () => {
                    setPending(true);
                    setError(undefined);
                    try {
                      await session.accounts!.revoke(row.id);
                      if (row.current) await session.restore?.();
                      else await sessions.refetch();
                    } catch (cause) {
                      setError(
                        cause instanceof Error ? cause : new Error('Could not revoke session.'),
                      );
                    } finally {
                      setPending(false);
                    }
                  }}
                >
                  Revoke{row.current ? ' this session' : ' session'}
                </Button>
              </li>
            ))}
          </ul>
        )}
      </details>
    </section>
  );
}
