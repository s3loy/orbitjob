import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Button, Flex, Heading, Text, TextField } from '@radix-ui/themes';
import { useSession } from './session';
import { ErrorState, Loading } from './components';
import type { OneTimeLink } from './api/accounts';

export function AccountAdministration() {
  const session = useSession();
  const workspace = session.account?.workspaces.find((row) => row.id === session.targetTenant);
  const administrator = workspace?.role === 'admin' || session.account?.user?.installation_admin;
  const [username, setUsername] = useState('');
  const [role, setRole] = useState('viewer');
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<Error>();
  const [revealed, setRevealed] = useState<{ kind: 'invite' | 'reset'; value: OneTimeLink }>();
  const [copyStatus, setCopyStatus] = useState('');
  const members = useQuery({
    queryKey: ['account-members', session.generation],
    queryFn: () => session.accounts!.members(session.targetTenant!),
    enabled: !!session.accounts && !!workspace && !!administrator,
  });
  const users = useQuery({
    queryKey: ['account-users', session.generation],
    queryFn: () => session.accounts!.users(),
    enabled: !!session.accounts && !!session.account?.user?.installation_admin,
  });
  if (!session.accounts || !session.account?.authenticated) return null;
  async function perform(work: () => Promise<unknown>) {
    if (pending || revealed) return;
    setPending(true);
    setError(undefined);
    try {
      await work();
      await Promise.all([
        ...(workspace ? [members.refetch()] : []),
        ...(session.account?.user?.installation_admin ? [users.refetch()] : []),
      ]);
    } catch (cause) {
      setError(cause instanceof Error ? cause : new Error('Could not update account access.'));
    } finally {
      setPending(false);
    }
  }
  const locked = pending || !!revealed;
  const link = revealed
    ? `${location.origin}/console/connection#${revealed.kind}=${encodeURIComponent(revealed.value.token)}`
    : '';
  return (
    <section className="account-administration" aria-label="Account administration">
      <Heading as="h2" size="3">
        Accounts and members
      </Heading>
      {error && <ErrorState error={error} />}
      {revealed && (
        <section className="operation-notice" aria-label="One-time account link">
          <Heading as="h3" size="3">
            {revealed.kind === 'invite' ? 'Invitation link' : 'Password reset link'}
          </Heading>
          <Text as="p" size="2" mt="2">
            Copy this link and share it through your approved private channel. It is displayed once
            and expires {new Date(revealed.value.expires_at).toLocaleString()}.
          </Text>
          <TextField.Root aria-label="One-time account link" value={link} readOnly mt="3" />
          <Flex gap="3" mt="3" wrap="wrap">
            <Button
              type="button"
              onClick={async () => {
                try {
                  await navigator.clipboard.writeText(link);
                  setCopyStatus('Link copied.');
                } catch {
                  setCopyStatus('Select and copy the link above.');
                }
              }}
            >
              Copy link
            </Button>
            <Button
              type="button"
              variant="outline"
              onClick={() => {
                setRevealed(undefined);
                setCopyStatus('');
              }}
            >
              Discard link
            </Button>
          </Flex>
          {copyStatus && (
            <Text as="p" size="1" role="status" mt="2">
              {copyStatus}
            </Text>
          )}
        </section>
      )}
      {workspace && (
        <>
          <Heading as="h3" size="3" mt="4">
            Members of {workspace.name}
          </Heading>
          {!administrator ? (
            <Text as="p" size="2" mt="2">
              Workspace administrators manage invitations and member access.
            </Text>
          ) : (
            <>
              <form
                onSubmit={(event) => {
                  event.preventDefault();
                  void perform(async () => {
                    const value = await session.accounts!.invite(
                      workspace!.id,
                      role,
                      username.trim() || undefined,
                    );
                    setRevealed({ kind: 'invite', value });
                    setUsername('');
                  });
                }}
              >
                <Flex gap="3" align="end" wrap="wrap" mt="3">
                  <label className="form-field">
                    Username (optional)
                    <TextField.Root
                      value={username}
                      onChange={(event) => setUsername(event.target.value)}
                      disabled={locked}
                    />
                  </label>
                  <label className="form-field">
                    Invitation role
                    <select
                      value={role}
                      onChange={(event) => setRole(event.target.value)}
                      disabled={locked}
                    >
                      <option value="viewer">Viewer</option>
                      <option value="operator">Operator</option>
                      <option value="admin">Administrator</option>
                    </select>
                  </label>
                  <Button type="submit" disabled={locked}>
                    Create invitation
                  </Button>
                </Flex>
              </form>
              {members.isPending ? (
                <Loading />
              ) : members.error ? (
                <ErrorState error={members.error} retry={() => void members.refetch()} />
              ) : (
                <div className="account-table-wrap">
                  <table className="account-table">
                    <thead>
                      <tr>
                        <th>Member</th>
                        <th>Role</th>
                        <th>Status</th>
                        <th>Actions</th>
                      </tr>
                    </thead>
                    <tbody>
                      {members.data?.items.map((row) => (
                        <tr key={row.user.id}>
                          <td>
                            {row.user.display_name || row.user.username}
                            {row.user.display_name &&
                              row.user.display_name !== row.user.username && (
                                <Text as="p" size="1" color="gray">
                                  {row.user.username}
                                </Text>
                              )}
                          </td>
                          <td>
                            <select
                              aria-label={`Role for ${row.user.username}`}
                              value={row.role}
                              disabled={locked}
                              onChange={(event) =>
                                void perform(() =>
                                  session.accounts!.updateMember(row.user.id, workspace!.id, {
                                    role: event.target.value,
                                  }),
                                )
                              }
                            >
                              <option value="viewer">Viewer</option>
                              <option value="operator">Operator</option>
                              <option value="admin">Administrator</option>
                            </select>
                          </td>
                          <td>
                            {row.status === 'active'
                              ? 'Active'
                              : row.status === 'disabled'
                                ? 'Disabled'
                                : row.status}
                          </td>
                          <td>
                            <Flex gap="2" wrap="wrap">
                              <Button
                                size="1"
                                variant="outline"
                                disabled={locked}
                                onClick={() =>
                                  void perform(() =>
                                    session.accounts!.updateMember(row.user.id, workspace!.id, {
                                      status: row.status === 'active' ? 'disabled' : 'active',
                                    }),
                                  )
                                }
                              >
                                {row.status === 'active' ? 'Disable member' : 'Enable member'}
                              </Button>
                              {session.account?.user?.installation_admin && (
                                <Button
                                  size="1"
                                  variant="outline"
                                  disabled={locked}
                                  onClick={() =>
                                    void perform(async () => {
                                      const value = await session.accounts!.resetMember(
                                        row.user.id,
                                        workspace!.id,
                                      );
                                      setRevealed({ kind: 'reset', value });
                                    })
                                  }
                                >
                                  Create password reset link
                                </Button>
                              )}
                            </Flex>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </>
          )}
        </>
      )}
      {session.account?.user?.installation_admin && (
        <>
          <Heading as="h3" size="3" mt="5">
            Installation accounts
          </Heading>
          <Text as="p" size="2" color="gray" mt="2">
            Disabling an account revokes its sessions. The final active installation administrator
            cannot be disabled or demoted.
          </Text>
          {users.isPending ? (
            <Loading />
          ) : users.error ? (
            <ErrorState error={users.error} retry={() => void users.refetch()} />
          ) : (
            <div className="account-table-wrap">
              <table className="account-table">
                <thead>
                  <tr>
                    <th>Account</th>
                    <th>Installation administrator</th>
                    <th>Status</th>
                    <th>Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {users.data?.items.map((row) => (
                    <tr key={row.id}>
                      <td>
                        {row.display_name || row.username}
                        {row.display_name && row.display_name !== row.username && (
                          <Text as="p" size="1" color="gray">
                            {row.username}
                          </Text>
                        )}
                      </td>
                      <td>
                        <input
                          type="checkbox"
                          aria-label={`Installation administrator for ${row.username}`}
                          checked={row.installation_admin}
                          disabled={locked}
                          onChange={(event) =>
                            void perform(() =>
                              session.accounts!.updateUser(row.id, {
                                installation_admin: event.target.checked,
                              }),
                            )
                          }
                        />
                      </td>
                      <td>
                        {row.status === 'active'
                          ? 'Active'
                          : row.status === 'disabled'
                            ? 'Disabled'
                            : row.status}
                      </td>
                      <td>
                        <Button
                          size="1"
                          variant="outline"
                          disabled={locked}
                          onClick={() =>
                            void perform(() =>
                              session.accounts!.updateUser(row.id, {
                                status: row.status === 'active' ? 'disabled' : 'active',
                              }),
                            )
                          }
                        >
                          {row.status === 'active' ? 'Disable account' : 'Enable account'}
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}
    </section>
  );
}
