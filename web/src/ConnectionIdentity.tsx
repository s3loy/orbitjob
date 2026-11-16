import { useState, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { Button, Flex, Heading, Text, TextField } from '@radix-ui/themes';
import { useSession } from './session';
import { ErrorState, Loading } from './components';
import { Facts } from './monitoring-ui';
import { namespaceCatalogKnown, workspaceName } from './workspace';
import { WorkspaceReference } from './WorkspaceReference';
import { Network } from 'lucide-react';
import { WorkspacePicker, type WorkspaceLabel } from './WorkspacePicker';
import {
  administrationScopeLocked,
  rejectAdministrationCredential,
  useAdministrationAuthenticationError,
} from './administration-state';

export function ConnectionIdentity({ onReady }: { onReady?: () => void }) {
  const session = useSession();
  const human = session.identity?.subject_type === 'user';
  const directoryFailure = useAdministrationAuthenticationError(session.client);
  const own = session.identity?.tenant_id || '';
  const initial = session.identity?.target_tenant_id || own;
  const [target, setTarget] = useState(initial);
  const [selected, setSelected] = useState<WorkspaceLabel | undefined>(
    initial
      ? {
          id: initial,
          name: session.identity?.tenant_name || 'Current workspace',
          slug: session.identity?.tenant_slug || '',
        }
      : undefined,
  );
  const [selectionError, setSelectionError] = useState<Error>();
  const [selecting, setSelecting] = useState(false);
  const [action, setAction] = useState('');
  const [resource, setResource] = useState('');
  const [probe, setProbe] = useState<{ action: string; resource: string }[] | undefined>();
  const identity = useQuery({
    queryKey: ['identity', session.generation],
    queryFn: () => session.client!.identity(),
    enabled: Boolean(session.client && !directoryFailure),
  });
  const decision = useQuery({
    queryKey: ['identity-probe', session.generation, probe],
    queryFn: () => session.client!.identity(probe),
    enabled: Boolean(session.client && probe && !directoryFailure),
  });
  if (!session.client) return null;
  if (directoryFailure)
    return (
      <section aria-label={human ? 'Account access' : 'Credential identity'}>
        <ErrorState error={directoryFailure} />
      </section>
    );
  return (
    <section
      className={human ? 'account-access' : 'detail-section'}
      aria-label={human ? 'Account access' : 'Credential identity'}
    >
      {!human && (
        <Heading as="h2" size="3" weight="medium">
          Workspace access
        </Heading>
      )}
      {identity.isPending ? (
        <Loading />
      ) : identity.error ? (
        <ErrorState error={identity.error} retry={() => void identity.refetch()} />
      ) : (
        <>
          {!human && (
            <Facts
              rows={[
                [
                  'Workspace',
                  identity.data.target_tenant_id || identity.data.tenant_id ? (
                    <span className="connection-workspace-name">
                      <strong>{workspaceName(identity.data)}</strong>
                      {identity.data.tenant_slug &&
                        identity.data.tenant_slug.toLowerCase() !==
                          workspaceName(identity.data).toLowerCase() && (
                          <small>{identity.data.tenant_slug}</small>
                        )}
                    </span>
                  ) : (
                    'Choose a workspace below'
                  ),
                ],
                ['Connected key', identity.data.key_name || 'API key'],
                [
                  'Access scope',
                  identity.data.group_id
                    ? 'One resource group'
                    : identity.data.kind === 'platform'
                      ? identity.data.target_tenant_id
                        ? 'Platform access in the selected workspace'
                        : 'Workspace selection required'
                      : 'This workspace',
                ],
              ]}
            />
          )}
          {namespaceCatalogKnown(identity.data) && !identity.data.workload_namespaces.length && (
            <section
              className="connection-environment connection-environment-missing"
              aria-label="Execution environment"
            >
              <Network size={17} />
              <div>
                <Text as="p" size="2" weight="medium">
                  No execution namespace is connected
                </Text>
                <Text as="p" size="2" color="gray">
                  Ask your installation administrator to map a Kubernetes namespace to this
                  workspace, then sign in again to refresh its configuration.
                </Text>
              </div>
            </section>
          )}
          {!human && identity.data.group_id && (
            <Text as="p" size="2" color="gray" mb="3">
              This key is limited to a resource group. Functions and monitoring may be available;
              the general Jobs and Runs pages require an ungrouped key.
            </Text>
          )}
          <details className="advanced-details account-access-details">
            <summary>
              {human ? 'Workspace access and environment' : 'Key details and environment'}
            </summary>
            <Facts
              rows={[
                ...(human
                  ? [
                      [
                        'Workspace role',
                        identity.data.membership_role === 'admin'
                          ? 'Administrator'
                          : identity.data.membership_role === 'operator'
                            ? 'Operator'
                            : identity.data.membership_role === 'viewer'
                              ? 'Viewer'
                              : 'No membership',
                      ] as [string, ReactNode],
                    ]
                  : []),
                ...(namespaceCatalogKnown(identity.data) && identity.data.workload_namespaces.length
                  ? [
                      [
                        'Execution namespaces',
                        <span className="account-namespace-list">
                          {identity.data.workload_namespaces.map((namespace) => (
                            <code key={namespace}>{namespace}</code>
                          ))}
                        </span>,
                      ] as [string, ReactNode],
                    ]
                  : []),
                [
                  'Workspace ID',
                  identity.data.target_tenant_id || identity.data.tenant_id ? (
                    <WorkspaceReference
                      label="Workspace ID"
                      value={identity.data.target_tenant_id || identity.data.tenant_id}
                    />
                  ) : (
                    'Not selected'
                  ),
                ],
                ...(identity.data.tenant_slug
                  ? [['Workspace slug', identity.data.tenant_slug] as [string, ReactNode]]
                  : []),
                ...(!human ? [['Key kind', identity.data.kind] as [string, ReactNode]] : []),
                [
                  human ? 'User ID' : 'Key ID',
                  <WorkspaceReference
                    label={human ? 'User ID' : 'Key ID'}
                    value={human ? identity.data.user_id || '' : identity.data.key_id}
                  />,
                ],
                ...(!human
                  ? [
                      [
                        'Key ownership',
                        identity.data.tenant_id ? 'This workspace' : 'The installation',
                      ] as [string, ReactNode],
                    ]
                  : []),
                ...(identity.data.group_id
                  ? [
                      [
                        'Resource group',
                        <WorkspaceReference
                          label="Resource group ID"
                          value={identity.data.group_id}
                        />,
                      ] as [string, ReactNode],
                    ]
                  : []),
                ...(identity.data.boundary_policy_id
                  ? [
                      [
                        'Boundary policy',
                        <WorkspaceReference
                          label="Boundary policy ID"
                          value={identity.data.boundary_policy_id}
                        />,
                      ] as [string, ReactNode],
                    ]
                  : []),
              ]}
            />
            {namespaceCatalogKnown(identity.data) && !!identity.data.workload_namespaces.length && (
              <Text as="p" size="1" color="gray">
                Workload creation checks namespace configuration and access.
              </Text>
            )}
          </details>
          {!human && (
            <section aria-label="Available workspaces">
              <Heading as="h3" size="3" mt="4" mb="2">
                {identity.data.kind === 'platform' ? 'Choose a workspace' : 'Available workspaces'}
              </Heading>
              <Text as="p" size="2" color="gray" mb="3">
                {identity.data.kind === 'platform'
                  ? 'Find a workspace by name or slug, then open it with this key. Each operation still checks its own permissions.'
                  : 'Your workload connection stays in your own workspace. Other visible workspaces can be selected for access management.'}
              </Text>
              <WorkspacePicker
                value={target}
                selected={selected}
                disabled={selecting || !!directoryFailure}
                onChange={(workspace) => {
                  setTarget(workspace.id);
                  setSelected(workspace);
                  setSelectionError(undefined);
                }}
                onAuthenticationError={(error) => {
                  rejectAdministrationCredential(session.client!, error);
                }}
              />
              {identity.data.kind === 'platform' ? (
                <form
                  onSubmit={async (event) => {
                    event.preventDefault();
                    if (selecting || directoryFailure || !target.trim()) return;
                    setSelectionError(undefined);
                    if (administrationScopeLocked(session.client!)) {
                      setSelectionError(
                        new Error(
                          'Finish the pending operation, save and discard any revealed key, or reconcile its outcome before changing the connection workspace.',
                        ),
                      );
                      return;
                    }
                    setSelecting(true);
                    try {
                      const verified = await session.selectTenant?.(target.trim());
                      if (verified) onReady?.();
                    } catch (cause) {
                      setSelectionError(
                        cause instanceof Error
                          ? cause
                          : new Error('Unable to select this workspace.'),
                      );
                    } finally {
                      setSelecting(false);
                    }
                  }}
                >
                  <details className="workspace-target-advanced">
                    <summary>Use an exact workspace ID</summary>
                    <TargetTenantField value={target} onChange={setTarget} />
                  </details>
                  <Button
                    mt="3"
                    size="2"
                    color="blue"
                    type="submit"
                    disabled={selecting || !!directoryFailure || !target.trim()}
                  >
                    Open selected workspace
                  </Button>
                  <Text as="p" size="1" color="gray" mt="2">
                    Changing the connection workspace clears loaded records and local drafts. Save
                    any key and reconcile pending operations first.
                  </Text>
                </form>
              ) : target && target !== own && !directoryFailure ? (
                <section className="operation-notice" aria-label="Manage another workspace">
                  <Text as="p" size="2" mb="3">
                    Workloads in {selected?.name || 'this workspace'} require a key issued for that
                    workspace. Managing its access keeps your current workload connection.
                  </Text>
                  <Button type="button" variant="outline" asChild>
                    <Link to={`/administration?panel=keys&workspace=${encodeURIComponent(target)}`}>
                      Manage access
                    </Link>
                  </Button>
                </section>
              ) : null}
              {selectionError && <ErrorState error={selectionError} />}
            </section>
          )}
          {(identity.data.target_tenant_id || identity.data.kind !== 'platform') && (
            <details className="advanced-details">
              <summary>Check a permission</summary>
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  setProbe([{ action: action.trim(), resource: resource.trim() }]);
                }}
              >
                <Flex gap="3" align="end" wrap="wrap" my="4">
                  <label>
                    <Text as="div" size="1" mb="2">
                      Exact action
                    </Text>
                    <TextField.Root
                      aria-label="Capability action"
                      placeholder="job:Get"
                      value={action}
                      onChange={(e) => setAction(e.target.value)}
                      required
                    />
                  </label>
                  <label style={{ flex: 1, minWidth: 180 }}>
                    <Text as="div" size="1" mb="2">
                      Exact resource ARN
                    </Text>
                    <TextField.Root
                      aria-label="Capability resource"
                      value={resource}
                      onChange={(e) => setResource(e.target.value)}
                      required
                    />
                  </label>
                  <Button type="submit" size="2" color="gray" variant="outline">
                    Check policy grant
                  </Button>
                </Flex>
                {decision.error && <ErrorState error={decision.error} />}
                {decision.isFetching && (
                  <Text as="p" size="1" role="status">
                    Checking policy grant…
                  </Text>
                )}
                {decision.data?.capabilities.map((p) => (
                  <Text as="p" size="2" key={p.action + p.resource} role="status">
                    {p.action}: {p.allowed ? 'Allowed by policy' : 'Denied by policy'}
                  </Text>
                ))}
                <Text as="p" size="1" color="gray">
                  A grant does not prove that a record exists or passes all endpoint constraints.
                  Each operation still authorizes its exact resource.
                </Text>
              </form>
            </details>
          )}
        </>
      )}
    </section>
  );
}

function TargetTenantField({ value, onChange }: { value: string; onChange(value: string): void }) {
  return (
    <label className="workspace-target-field">
      <Text as="div" size="1" mb="2">
        Workspace ID
      </Text>
      <TextField.Root
        aria-label="Workspace ID"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        required
        pattern="[0-9A-HJKMNP-TV-Z]{26}"
      />
    </label>
  );
}
