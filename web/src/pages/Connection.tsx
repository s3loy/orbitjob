import { Button, Flex, Heading, Text, TextField } from '@radix-ui/themes';
import { useEffect, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { useSession } from '../session';
import { ErrorState, Loading, PageHeading } from '../components';
import { fixtureProfile, publicDemo, projectHome } from '../deployment';
import { allowDemo } from '#console-profile';
import { APIError } from '../api/transport';
import { sampleWorkspace } from '../sample-workspace';
import { takeAccountCapability, clearAccountCapability } from '../account-capability';
import { AccountWorkspace } from '../AccountWorkspace';
import { newAccountPasswordError } from '../account-validation';

export function Connection() {
  const session = useSession();
  const navigate = useNavigate();
  const location = useLocation();
  const [capability, setCapability] = useState(takeAccountCapability);
  const [username, setUsername] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [password, setPassword] = useState('');
  const [confirmation, setConfirmation] = useState('');
  const [existingAccount, setExistingAccount] = useState(false);
  const [remember, setRemember] = useState(true);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<Error>();
  const [retryAt, setRetryAt] = useState(0);
  useEffect(() => {
    if (!retryAt) return;
    const timer = setTimeout(() => setRetryAt(0), Math.max(0, retryAt - Date.now()));
    return () => clearTimeout(timer);
  }, [retryAt]);
  const [notice, setNotice] = useState(
    () => (location.state as { notice?: string } | null)?.notice || '',
  );
  useEffect(() => {
    if (publicDemo) return;
    const receive = () => {
      const next = takeAccountCapability();
      if (!next) return;
      setCapability(next);
      setPassword('');
      setConfirmation('');
      setError(undefined);
      setNotice('');
    };
    window.addEventListener('hashchange', receive);
    return () => window.removeEventListener('hashchange', receive);
  }, []);
  const visibleNotice = notice || (location.state as { notice?: string } | null)?.notice || '';
  const destination = (location.state as { returnTo?: string } | null)?.returnTo;
  const enter = () =>
    navigate(
      destination &&
        /^\/(overview|jobs|runs|checks|check-results|slos|slis|slo-alerts|workflows|functions|administration|manifests|definitions)([/?]|$)/.test(
          destination,
        )
        ? destination
        : fixtureProfile
          ? '/jobs'
          : '/overview',
    );
  const connected = session.mode === 'live';
  const demo = session.mode === 'demo';
  const firstUse = session.account?.initialized === false;
  const creating =
    capability?.kind === 'setup' ||
    (capability?.kind === 'invite' && !connected && !existingAccount);
  const newPassword = creating || capability?.kind === 'reset';
  if (session.restoring) return <Loading />;
  return (
    <div
      className={`connection-page connection-onboarding${connected ? ' connection-details' : ''}`}
    >
      <PageHeading
        title={
          publicDemo || demo
            ? sampleWorkspace.name
            : capability?.kind === 'setup'
              ? 'Create the first administrator'
              : capability?.kind === 'invite'
                ? 'Accept workspace invitation'
                : capability?.kind === 'reset'
                  ? 'Reset your password'
                  : connected
                    ? 'Account'
                    : 'Sign in to OrbitJob'
        }
        action={
          (connected || demo) && (
            <Flex gap="3" wrap="wrap" aria-label="Account actions">
              {demo && <Button onClick={enter}>Open workspace</Button>}
              <Button
                variant="outline"
                disabled={pending}
                onClick={async () => {
                  setPending(true);
                  clearAccountCapability();
                  setCapability(undefined);
                  setPassword('');
                  setConfirmation('');
                  try {
                    await session.disconnect();
                  } finally {
                    setPending(false);
                  }
                }}
              >
                {publicDemo ? 'Reset sample data' : demo ? 'Leave sample data' : 'Sign out'}
              </Button>
            </Flex>
          )
        }
      />
      {session.restoreError && (
        <section className="connection-recovery" aria-label="Session recovery">
          <ErrorState error={session.restoreError} retry={() => void session.restore?.()} />
          <Text as="p" size="2" mt="2">
            {session.restoreError instanceof APIError && session.restoreError.status === 401
              ? 'Loaded records and local drafts have been cleared.'
              : 'A temporary connection failure does not end your server session.'}
          </Text>
        </section>
      )}
      {visibleNotice && (
        <Text as="p" role="status" size="2" my="3">
          {visibleNotice}
        </Text>
      )}
      {(!connected || capability) && !publicDemo && !demo && (
        <div className="connection-entry-layout">
          <section
            className="connection-signin"
            aria-label={capability ? 'Account setup' : 'Account sign in'}
          >
            {firstUse && !capability ? (
              <>
                <Heading as="h2" size="4">
                  Finish installation setup
                </Heading>
                <Text as="p" size="2" mt="3">
                  No administrator account has been created. Ask the installer to generate a
                  one-time setup link, then open that link in this browser. Account registration is
                  invitation only.
                </Text>
              </>
            ) : (
              <form
                className="connection-key-form"
                onSubmit={async (event) => {
                  event.preventDefault();
                  if (pending || retryAt > Date.now() || !session.accounts) return;
                  setPending(true);
                  setError(undefined);
                  setNotice('');
                  try {
                    const passwordError = newPassword && newAccountPasswordError(password);
                    if (passwordError) throw new Error(passwordError);
                    if (newPassword && password !== confirmation)
                      throw new Error('The passwords do not match.');
                    if (capability?.kind === 'reset') {
                      await session.accounts.reset(capability.token, password);
                      clearAccountCapability();
                      setCapability(undefined);
                      await session.disconnect();
                      navigate('/connection', {
                        replace: true,
                        state: { notice: 'Password reset. Sign in with your new password.' },
                      });
                    } else {
                      if (capability?.kind === 'invite' && !connected && existingAccount) {
                        const signedIn = await session.accounts.login(
                          username.trim(),
                          password,
                          remember,
                        );
                        await session.authenticated?.(signedIn);
                        return;
                      }
                      const snapshot =
                        capability?.kind === 'setup'
                          ? await session.accounts.setup(
                              capability.token,
                              username.trim(),
                              password,
                              displayName.trim(),
                            )
                          : capability?.kind === 'invite'
                            ? await session.accounts.accept({
                                token: capability.token,
                                ...(!connected && !existingAccount
                                  ? {
                                      username: username.trim(),
                                      password,
                                      display_name: displayName.trim(),
                                    }
                                  : {}),
                              })
                            : await session.accounts.login(username.trim(), password, remember);
                      clearAccountCapability();
                      setCapability(undefined);
                      await session.authenticated?.(snapshot);
                      if (snapshot.workspaces.length) enter();
                    }
                  } catch (cause) {
                    if (cause instanceof APIError && cause.status === 429)
                      setRetryAt(Date.now() + Math.max(cause.retryAfter, 1000));
                    setError(
                      cause instanceof APIError && cause.status === 401
                        ? new Error(
                            'Sign in failed. Check your username and password, or ask your administrator for a password reset link.',
                          )
                        : cause instanceof Error
                          ? cause
                          : new Error('Could not complete this request.'),
                    );
                  } finally {
                    setPassword('');
                    setConfirmation('');
                    setPending(false);
                  }
                }}
              >
                {capability?.kind === 'invite' && !connected && (
                  <label className="form-field">
                    <span>
                      <input
                        type="checkbox"
                        checked={existingAccount}
                        onChange={(event) => {
                          setExistingAccount(event.target.checked);
                          setPassword('');
                          setConfirmation('');
                        }}
                      />{' '}
                      I already have an account
                    </span>
                  </label>
                )}
                {capability?.kind === 'invite' && connected ? (
                  <Text as="p" size="2">
                    Accept this invitation as <strong>{session.account?.user?.username}</strong>. If
                    the invitation belongs to another account, sign out and sign in with that
                    account first.
                  </Text>
                ) : (
                  <>
                    {capability?.kind !== 'reset' && (
                      <label className="form-field">
                        Username
                        <TextField.Root
                          autoComplete="username"
                          required
                          value={username}
                          onChange={(event) => setUsername(event.target.value)}
                        />
                      </label>
                    )}
                    {creating && (
                      <label className="form-field">
                        Display name
                        <TextField.Root
                          autoComplete="name"
                          value={displayName}
                          onChange={(event) => setDisplayName(event.target.value)}
                        />
                      </label>
                    )}
                    <label className="form-field">
                      {newPassword ? 'New password' : 'Password'}
                      <TextField.Root
                        type="password"
                        autoComplete={newPassword ? 'new-password' : 'current-password'}
                        required
                        minLength={newPassword ? 15 : undefined}
                        aria-describedby={newPassword ? 'new-password-requirement' : undefined}
                        value={password}
                        onChange={(event) => setPassword(event.target.value)}
                      />
                    </label>
                    {newPassword && (
                      <Text id="new-password-requirement" size="1">
                        Use at least 15 characters.
                      </Text>
                    )}
                    {newPassword && (
                      <label className="form-field">
                        Confirm password
                        <TextField.Root
                          type="password"
                          autoComplete="new-password"
                          required
                          value={confirmation}
                          onChange={(event) => setConfirmation(event.target.value)}
                        />
                      </label>
                    )}
                    {!capability && (
                      <label className="form-field account-remember">
                        <span>
                          <input
                            type="checkbox"
                            checked={remember}
                            onChange={(event) => setRemember(event.target.checked)}
                          />{' '}
                          Keep me signed in for 7 days
                        </span>
                        <Text size="1" color="gray">
                          Uncheck for an 8-hour session. Sign out when using a shared device.
                        </Text>
                      </label>
                    )}
                  </>
                )}
                <Button type="submit" mt="4" size="3" disabled={pending || retryAt > 0}>
                  {pending
                    ? 'Please wait…'
                    : retryAt > 0
                      ? 'Wait before signing in'
                      : capability?.kind === 'setup'
                        ? 'Create administrator account'
                        : capability?.kind === 'invite'
                          ? 'Accept invitation'
                          : capability?.kind === 'reset'
                            ? 'Reset password'
                            : 'Sign in'}
                </Button>
                {capability && (
                  <Button
                    type="button"
                    variant="ghost"
                    mt="3"
                    ml="3"
                    onClick={() => {
                      clearAccountCapability();
                      setCapability(undefined);
                      setPassword('');
                      setConfirmation('');
                    }}
                  >
                    Discard link
                  </Button>
                )}
                {error && <ErrorState error={error} />}
              </form>
            )}
            {!capability && !firstUse && (
              <Text as="p" size="2" mt="4" color="gray">
                Need an account or forgot your password? Ask your workspace administrator for an
                invitation, or your installation administrator for a password reset link.
              </Text>
            )}
            {capability && (
              <Text as="p" size="1" mt="3" color="gray">
                This one-time link stays only in this page’s memory. Reloading or discarding it
                clears the link.
              </Text>
            )}
          </section>
        </div>
      )}
      {connected && !capability && <AccountWorkspace onReady={enter} />}
      {allowDemo && session.mode === 'disconnected' && (
        <section className="demo-entry">
          <Button
            variant="outline"
            onClick={async () => {
              await session.connect('demo');
              enter();
            }}
          >
            Explore sample data
          </Button>
        </section>
      )}
      {demo && (
        <Text as="p" size="2" mt="3">
          Changes to sample data stay in this session.
        </Text>
      )}
      {publicDemo && (
        <Text as="p" size="2" mt="4">
          <a className="text-link" href={`${projectHome}docs/getting-started/`}>
            Install OrbitJob and open your live console
          </a>
        </Text>
      )}
    </div>
  );
}
