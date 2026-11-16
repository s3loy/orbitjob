import { useEffect, useRef, useState } from 'react';
import {
  Button,
  Callout,
  Flex,
  Heading,
  Select,
  Text,
  TextArea,
  TextField,
} from '@radix-ui/themes';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useLocation, useSearchParams } from 'react-router-dom';
import { Plus, RefreshCw } from 'lucide-react';
import { copyText, selectTextArea } from './clipboard';
import { useSession } from './session';
import { APIError } from './api/transport';
import type { AdministrationClient, Key } from './api/administration';
import { ErrorState, Loading, Paging } from './components';
import { pageLimit, pageLimitParams, pageOffset, pageParams } from './list-state';
import { NamedPicker, PolicyPicker } from './AdministrationFields';
import { WorkspacePicker, type WorkspaceLabel } from './WorkspacePicker';
import { AdministrationPolicyBuilder } from './AdministrationPolicyBuilder';
import { AdministrationRecords, AdministrationDialogs } from './AdministrationRecords';
import { parsePolicyDocument, policyDocument, policySummary } from './administration-policy';
import {
  administrationCreateActions,
  administrationCreateAllowed,
  administrationCreateProbes,
} from './administration-access';
import {
  useAdministrationCredential,
  useAdministrationCreations,
  createAdministrationKey,
  discardAdministrationKey,
  reconcileAdministrationKey,
  copyAdministrationKey,
  recordAdministrationCreation,
  useAdministrationKeyDraft,
  updateAdministrationKeyDraft,
  useAdministrationMutation,
  runAdministrationMutation,
  reconcileAdministrationMutation,
  dismissAdministrationMutationError,
  rejectAdministrationCredential,
  useAdministrationRecordDraft,
  updateAdministrationRecordDraft,
  resetAdministrationRecordDraft,
} from './administration-state';

export type Panel = 'tenants' | 'keys' | 'policies' | 'groups';
export function AdministrationPanel({
  panel,
  tenant,
  setTenant,
  workspace,
  onPanel,
  authenticationError,
}: {
  panel: Panel;
  tenant: string;
  setTenant(value: string, label?: WorkspaceLabel): void;
  workspace?: WorkspaceLabel;
  onPanel(value: Panel): void;
  authenticationError?: APIError;
}) {
  const session = useSession();
  const client = session.client as NonNullable<typeof session.client> & AdministrationClient;
  const cache = useQueryClient();
  const credential = useAdministrationCredential(client);
  const creations = useAdministrationCreations(client);
  const credentialBusy = credential.stage === 'pending' || credential.stage === 'revealed';
  const credentialLocked =
    credentialBusy || credential.stage === 'unknown' || credential.stage === 'closed';
  const alive = useRef(true);
  const createSection = useRef<HTMLElement>(null);
  const secretHeading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  const location = useLocation();
  const [params, setParams] = useSearchParams();
  const limit = pageLimit(params);
  const offset = pageOffset(params);
  const workspaceSearch = params.get('search') ?? '';
  const [tenantDraft, setTenantDraft] = useState(tenant);
  const draft = useAdministrationKeyDraft(client, tenant);
  const recordDraft = useAdministrationRecordDraft(client, panel, tenant);
  const name = panel === 'keys' ? draft.name : recordDraft.name;
  const setName = (value: string) =>
    panel === 'keys'
      ? updateAdministrationKeyDraft(client, tenant, { name: value })
      : updateAdministrationRecordDraft(client, panel, tenant, { name: value });
  const { slug, status, description } = recordDraft;
  const document = recordDraft.document ?? policyDocument();
  const setSlug = (slug: string) =>
    updateAdministrationRecordDraft(client, panel, tenant, { slug });
  const setStatus = (status: string) =>
    updateAdministrationRecordDraft(client, panel, tenant, { status });
  const setDescription = (description: string) =>
    updateAdministrationRecordDraft(client, panel, tenant, { description });
  const setDocument = (document: string) =>
    updateAdministrationRecordDraft(client, panel, tenant, { document });
  const { policies, boundary, group } = draft;
  const setPolicies = (value: string[]) =>
    updateAdministrationKeyDraft(client, tenant, { policies: value });
  const setBoundary = (value: string) =>
    updateAdministrationKeyDraft(client, tenant, { boundary: value });
  const setGroup = (value: string) =>
    updateAdministrationKeyDraft(client, tenant, { group: value });
  const [copy, setCopy] = useState('');
  const mutationScope = panel === 'tenants' ? panel : `${panel}:${tenant}`;
  const mutation = useAdministrationMutation(client, mutationScope);
  const pending = mutation.stage === 'pending';
  const unknown = mutation.stage === 'unknown';
  const [formError, setError] = useState<Error>();
  const error = formError ?? mutation.error;
  const [notice, setNotice] = useState('');
  const [confirm, setConfirm] = useState<string>();
  const [rename, setRename] = useState<Key>();
  const canProbe = !!tenant || panel === 'tenants';
  const capabilities = useQuery({
    queryKey: ['administration', session.generation, 'create-capabilities', tenant],
    enabled: !authenticationError && session.mode === 'live' && canProbe,
    retry: false,
    queryFn: () => client.identity(administrationCreateProbes(tenant), tenant),
  });
  const deniedScope = capabilities.error instanceof APIError && capabilities.error.status === 403;
  const createAllowed = deniedScope
    ? false
    : administrationCreateAllowed(capabilities.data, panel, tenant);
  const policyCreateAllowed = deniedScope
    ? false
    : administrationCreateAllowed(capabilities.data, 'policies', tenant);
  const checkingAccess = session.mode === 'live' && canProbe && capabilities.isPending;
  const query = useQuery({
    queryKey: [
      'administration',
      session.generation,
      panel,
      tenant,
      panel === 'tenants' ? offset : 0,
      panel === 'tenants' ? limit : undefined,
      panel === 'tenants' ? workspaceSearch.trim() : '',
      panel === 'keys' ? credential.stage : 'none',
    ],
    enabled: !authenticationError && (panel === 'tenants' || !!tenant),
    retry: false,
    queryFn: async () => {
      if (panel === 'tenants') return client.workspaceDirectory(offset, workspaceSearch, limit);
      const items =
        panel === 'keys'
          ? await client.adminKeys(tenant)
          : panel === 'policies'
            ? await client.adminPolicies(tenant)
            : await client.adminGroups(tenant);
      return { items, has_more: false };
    },
  });
  const policyOptions = useQuery({
    queryKey: ['administration', session.generation, 'policy-options', tenant],
    enabled: !authenticationError && panel === 'keys' && !!tenant,
    retry: false,
    queryFn: () => client.adminPolicies(tenant),
  });
  const groupOptions = useQuery({
    queryKey: ['administration', session.generation, 'group-options', tenant],
    enabled: !authenticationError && (panel === 'keys' || panel === 'policies') && !!tenant,
    retry: false,
    queryFn: () => client.adminGroups(tenant),
  });
  const authenticationFailure = [
    authenticationError,
    capabilities.error,
    query.error,
    panel === 'keys' ? policyOptions.error : undefined,
    panel === 'keys' || panel === 'policies' ? groupOptions.error : undefined,
    error,
    credential.error,
  ].find((reason): reason is APIError => reason instanceof APIError && reason.status === 401);
  useEffect(() => {
    if (authenticationFailure) rejectAdministrationCredential(client, authenticationFailure);
  }, [authenticationFailure, client]);
  const keySafetyVisible = panel === 'keys' || !!authenticationFailure;
  const secret = keySafetyVisible && credential.stage === 'revealed' ? (credential.key ?? '') : '';
  useEffect(() => {
    if (secret) secretHeading.current?.focus();
  }, [secret]);
  async function act(operation: () => Promise<void>) {
    if (authenticationFailure) return;
    setError(undefined);
    setNotice('');
    if (await runAdministrationMutation(client, mutationScope, operation)) {
      if (alive.current) {
        setNotice('Operation completed.');
      }
      await cache.invalidateQueries({ queryKey: ['administration', session.generation] });
      if (panel === 'tenants')
        await cache.invalidateQueries({ queryKey: ['workspaces', session.generation] });
    }
  }
  function create() {
    if (disabled || checkingAccess || createAllowed === false) return;
    setError(undefined);
    if (!name.trim()) {
      setError(new Error('Enter a name.'));
      return;
    }
    if (panel === 'policies') {
      try {
        const parsed = parsePolicyDocument(document);
        void act(async () => {
          const result = await client.adminCreatePolicy(
            {
              name: name.trim(),
              description,
              document: parsed,
            },
            tenant,
          );
          recordAdministrationCreation(client, 'policies', result, tenant);
          resetAdministrationRecordDraft(client, 'policies', tenant);
        });
      } catch (reason) {
        setError(reason instanceof Error ? reason : new Error('Enter a valid policy document.'));
      }
    } else if (panel === 'tenants')
      void act(async () => {
        const result = await client.adminCreateTenant({
          name: name.trim(),
          slug,
          status: status as 'active' | 'suspended',
        });
        recordAdministrationCreation(client, 'tenants', result);
        resetAdministrationRecordDraft(client, 'tenants', tenant);
      });
    else if (panel === 'groups')
      void act(async () => {
        const result = await client.adminCreateGroup({ name: name.trim(), slug }, tenant);
        recordAdministrationCreation(client, 'groups', result, tenant);
        resetAdministrationRecordDraft(client, 'groups', tenant);
      });
    else {
      if (!policies.length) {
        setError(new Error('Choose at least one policy.'));
        return;
      }
      void createAdministrationKey(client, tenant, {
        name: name.trim(),
        policies,
        ...(boundary ? { boundary_policy_id: boundary } : {}),
        ...(group ? { resource_group_id: group } : {}),
      }).then(() => {
        if (alive.current)
          void cache.invalidateQueries({
            queryKey: ['administration', session.generation, 'keys'],
          });
      });
    }
  }
  const disabled =
    !!authenticationFailure || pending || unknown || (panel === 'keys' && credentialLocked);
  const groups = (groupOptions.data ?? []).map((row) => ({
    id: row.id,
    name: row.name,
    description: row.slug,
  }));
  for (const row of creations.filter(
    (receipt) => receipt.kind === 'groups' && (!receipt.tenant || receipt.tenant === tenant),
  ))
    if (!groups.some((option) => option.id === row.id))
      groups.push({ id: row.id, name: row.name, description: 'Created in this session' });
  const policyNames = (policyOptions.data ?? []).map((row) => ({
    id: row.id,
    name: row.name,
    description: policySummary(row.document),
  }));
  const createLabel = {
    keys: 'API key',
    policies: 'policy',
    groups: 'resource group',
    tenants: 'workspace',
  }[panel];
  const showCreate = !checkingAccess && createAllowed !== false;
  return (
    <div className="administration-panel">
      {authenticationFailure && (
        <section aria-label="Administration connection recovery">
          <ErrorState error={authenticationFailure} />
          <Text as="p" size="2" color="gray" mt="3">
            Sign in again to view records. Confirmed changes and unfinished key operations remain
            available here.
          </Text>
        </section>
      )}
      {secret && (
        <section className="administration-secret" aria-label="One-time API key">
          <Heading as="h2" size="3" ref={secretHeading} tabIndex={-1}>
            Save this key now
          </Heading>
          <Text as="p">
            Store it in your credential manager before discarding it or disconnecting.
          </Text>
          <TextArea
            id="administration-generated-key"
            aria-label="Generated API key"
            readOnly
            value={secret}
            rows={3}
          />
          <Flex gap="2" mt="2">
            <Button
              onClick={() => {
                void copyAdministrationKey(secret, {
                  writeText: async (text) => {
                    if (!(await copyText(text))) throw new Error('Copy unavailable.');
                  },
                }).then((message) => {
                  if (alive.current) setCopy(message);
                });
              }}
            >
              Copy key
            </Button>
            <Button
              type="button"
              variant="outline"
              onClick={() => selectTextArea('administration-generated-key')}
            >
              Select key
            </Button>
            <Button
              variant="outline"
              onClick={() => {
                discardAdministrationKey(client);
                setCopy('');
              }}
            >
              Discard key
            </Button>
          </Flex>
          <Text as="p" role="status">
            {copy}
          </Text>
        </section>
      )}
      {!authenticationFailure && (
        <>
          {panel !== 'tenants' && (
            <section className="administration-tenant" aria-label="Management workspace">
              <WorkspacePicker
                label="Management workspace"
                value={tenant}
                selected={workspace}
                onChange={(row) => setTenant(row.id, row)}
                disabled={pending || unknown || credentialLocked}
                onAuthenticationError={(failure) => rejectAdministrationCredential(client, failure)}
              />
              {tenant && !workspace && (
                <Text as="p" size="1" color="gray">
                  Workspace name unavailable. Find and select it by name.
                </Text>
              )}
              <details className="admin-advanced">
                <summary>Use an exact workspace ID</summary>
                <form
                  onSubmit={(event) => {
                    event.preventDefault();
                    if (!credentialLocked) setTenant(tenantDraft.trim());
                  }}
                >
                  <label>
                    Workspace ID
                    <TextField.Root
                      aria-label="Workspace ID"
                      required
                      value={tenantDraft}
                      onChange={(event) => setTenantDraft(event.target.value)}
                    />
                  </label>
                  <Button
                    type="submit"
                    variant="outline"
                    disabled={pending || unknown || credentialLocked}
                  >
                    Select workspace
                  </Button>
                </form>
              </details>
            </section>
          )}
          <Flex className="admin-list-toolbar" justify="between" align="center" gap="3" wrap="wrap">
            <Heading as="h2" size="3">
              {
                {
                  tenants: 'Workspaces',
                  keys: 'API keys',
                  policies: 'Policies',
                  groups: 'Resource groups',
                }[panel]
              }
            </Heading>
            <Flex align="center" gap="2" wrap="wrap">
              <Button
                type="button"
                size="1"
                variant="outline"
                color="gray"
                disabled={query.isFetching || (panel !== 'tenants' && !tenant)}
                onClick={() => {
                  void query.refetch();
                  void cache.invalidateQueries({ queryKey: ['workspaces', session.generation] });
                  if (panel === 'keys') void policyOptions.refetch();
                  if (panel === 'keys' || panel === 'policies') void groupOptions.refetch();
                }}
              >
                <RefreshCw size={13} aria-hidden="true" />
                {query.isFetching && query.data ? 'Refreshing…' : 'Refresh'}
              </Button>
              {showCreate && (
                <Button
                  type="button"
                  size="1"
                  variant="soft"
                  aria-controls="administration-create"
                  disabled={disabled || (panel !== 'tenants' && !tenant)}
                  onClick={() => {
                    createSection.current?.focus({ preventScroll: true });
                    createSection.current?.scrollIntoView({ block: 'start' });
                  }}
                >
                  <Plus size={13} aria-hidden="true" />
                  New {createLabel}
                </Button>
              )}
            </Flex>
          </Flex>
          {panel === 'tenants' && (
            <div className="admin-list-search">
              <TextField.Root
                type="search"
                aria-label="Search workspaces"
                placeholder="Search names or slugs"
                value={workspaceSearch}
                onChange={(event) => {
                  if (Array.from(event.target.value.trim()).length <= 128) {
                    const next = new URLSearchParams(params);
                    next.delete('page');
                    next.delete('offset');
                    if (event.target.value) next.set('search', event.target.value);
                    else next.delete('search');
                    setParams(next, { replace: true, state: location.state });
                  }
                }}
              />
            </div>
          )}
          {query.isLoading && <Loading />}
          {query.error && <ErrorState error={query.error} retry={() => void query.refetch()} />}
          {panel !== 'tenants' && !tenant && (
            <Text as="p" size="2" color="gray">
              Choose a workspace to manage its{' '}
              {panel === 'keys' ? 'API keys' : panel === 'groups' ? 'resource groups' : 'policies'}.
            </Text>
          )}
          {query.data && !query.error && (
            <div aria-busy={query.isFetching}>
              <AdministrationRecords
                panel={panel}
                rows={query.data.items}
                workspaceSearch={panel === 'tenants' ? workspaceSearch : ''}
                onWorkspace={(row) => {
                  setTenant(row.id, row);
                  onPanel('keys');
                }}
                disabled={disabled || query.isFetching}
                keyActionsDisabled={pending || unknown || credentialBusy || query.isFetching}
                keyManagementAllowed={
                  session.mode === 'demo' ||
                  session.identity?.subject_type === 'user' ||
                  session.identity?.kind === 'platform' ||
                  tenant === session.identity?.tenant_id
                }
                policyDeleteAllowed={
                  session.mode === 'demo' ||
                  tenant ===
                    (session.targetTenant ||
                      session.identity?.target_tenant_id ||
                      session.identity?.tenant_id)
                }
                onConfirm={(id) => {
                  setError(undefined);
                  dismissAdministrationMutationError(client, mutationScope);
                  setConfirm(id);
                }}
                onRename={(key) => {
                  setError(undefined);
                  dismissAdministrationMutationError(client, mutationScope);
                  setRename(key);
                }}
              />
            </div>
          )}
          {panel === 'tenants' && query.data && !query.error && (
            <Paging
              limit={limit}
              setLimit={(size) =>
                setParams(pageLimitParams(params, size), { state: location.state })
              }
              offset={offset}
              count={query.data.items.length}
              hasMore={query.data.has_more}
              disabled={query.isFetching}
              setOffset={(value) => setParams(pageParams(params, value), { state: location.state })}
            />
          )}
          {checkingAccess && <p role="status">Checking management permissions…</p>}
          {capabilities.error && (
            <ErrorState
              error={capabilities.error}
              retry={() => {
                void capabilities.refetch();
              }}
            />
          )}
          {createAllowed === false && (
            <section className="administration-create" aria-label="Management access required">
              <Heading as="h2" size="3">
                Management access required
              </Heading>
              <Text as="p" size="2">
                You cannot create {createLabel === 'policy' ? 'policies' : `${createLabel}s`} here.
                Ask {panel === 'tenants' ? 'an installation' : 'a workspace'} administrator for
                access.
              </Text>
              <details className="admin-advanced">
                <summary>Required permission</summary>
                <code>{administrationCreateActions[panel]}</code>
              </details>
              <Button mt="3" variant="outline" asChild>
                <Link to="/jobs">Back to workloads</Link>
              </Button>
            </section>
          )}
          {showCreate && (panel === 'tenants' || !!tenant) && (
            <section
              className="administration-create"
              id="administration-create"
              aria-labelledby="administration-create-title"
              ref={createSection}
              tabIndex={-1}
            >
              <Heading as="h2" size="3" id="administration-create-title">
                Create {createLabel}
              </Heading>
              <form
                onSubmit={(event) => {
                  event.preventDefault();
                  create();
                }}
              >
                <fieldset disabled={disabled}>
                  <label>
                    {panel === 'keys' ? 'Key name' : 'Name'}
                    <TextField.Root
                      aria-label={panel === 'keys' ? 'Key name' : 'Name'}
                      required
                      maxLength={128}
                      pattern=".*\S.*"
                      placeholder={panel === 'keys' ? 'Production deploy service' : undefined}
                      value={name}
                      onChange={(event) => setName(event.target.value)}
                    />
                  </label>
                  {(panel === 'tenants' || panel === 'groups') && (
                    <label>
                      Slug
                      <TextField.Root
                        aria-label="Slug"
                        required
                        value={slug}
                        onChange={(event) => setSlug(event.target.value)}
                      />
                    </label>
                  )}
                  {panel === 'tenants' && (
                    <label>
                      Status
                      <Select.Root disabled={disabled} value={status} onValueChange={setStatus}>
                        <Select.Trigger />
                        <Select.Content>
                          <Select.Item value="active">Active</Select.Item>
                          <Select.Item value="suspended">Suspended</Select.Item>
                        </Select.Content>
                      </Select.Root>
                    </label>
                  )}
                  {panel === 'policies' && (
                    <>
                      <label>
                        Description
                        <TextField.Root
                          value={description}
                          onChange={(event) => setDescription(event.target.value)}
                        />
                      </label>
                      <AdministrationPolicyBuilder
                        value={document}
                        onChange={setDocument}
                        groups={groups}
                        advanced={recordDraft.advanced}
                        onAdvancedChange={(advanced) =>
                          updateAdministrationRecordDraft(client, panel, tenant, { advanced })
                        }
                      />
                    </>
                  )}
                  {panel === 'keys' && (
                    <>
                      <PolicyPicker
                        knownNames={Object.fromEntries(
                          creations
                            .filter(
                              (receipt) =>
                                receipt.kind === 'policies' &&
                                (!receipt.tenant || receipt.tenant === tenant),
                            )
                            .map((receipt) => [receipt.id, receipt.name]),
                        )}
                        policies={policyOptions.data ?? []}
                        selected={policies}
                        onChange={setPolicies}
                        unavailable={!!policyOptions.error}
                        disabled={disabled}
                        onCreate={() => onPanel('policies')}
                        canCreate={policyCreateAllowed !== false}
                        loading={policyOptions.isPending}
                        onRetry={() => {
                          void policyOptions.refetch();
                        }}
                      />
                      <details className="admin-advanced" open={group ? true : undefined}>
                        <summary>Advanced access restrictions</summary>
                        <NamedPicker
                          label="Permission boundary"
                          emptyLabel="No permission boundary"
                          value={boundary}
                          options={policyNames}
                          onChange={setBoundary}
                          optional
                          disabled={disabled}
                          loading={policyOptions.isPending}
                          unavailable={!!policyOptions.error}
                          onRetry={() => {
                            void policyOptions.refetch();
                          }}
                        />
                        <Text as="p" size="1">
                          The key can only use actions allowed by the boundary and its policies.
                        </Text>
                        {!!policyOptions.error && (
                          <label>
                            Boundary policy ID (optional)
                            <TextField.Root
                              value={boundary}
                              onChange={(event) => setBoundary(event.target.value)}
                            />
                          </label>
                        )}
                        <NamedPicker
                          label="Resource group"
                          emptyLabel="No resource group"
                          value={group}
                          options={groups}
                          onChange={setGroup}
                          optional
                          disabled={disabled}
                          loading={groupOptions.isPending}
                          unavailable={!!groupOptions.error}
                          onRetry={() => {
                            void groupOptions.refetch();
                          }}
                        />
                        <Text as="p" size="1">
                          Group keys work with group-aware Functions and monitoring. Jobs, Runs and
                          online Apply require an ungrouped key.
                        </Text>
                        {!!groupOptions.error && (
                          <label>
                            Resource group ID (optional)
                            <TextField.Root
                              value={group}
                              onChange={(event) => setGroup(event.target.value)}
                            />
                          </label>
                        )}
                      </details>
                    </>
                  )}
                  <Button
                    className="admin-primary"
                    type="submit"
                    disabled={disabled || (panel === 'keys' && !tenant)}
                  >
                    {pending ? 'Creating…' : `Create ${createLabel}`}
                  </Button>
                </fieldset>
              </form>
            </section>
          )}
        </>
      )}
      {keySafetyVisible && credential.stage === 'pending' && (
        <Text as="p" role="status">
          Creating API key. You can navigate away and return to review the result.
        </Text>
      )}
      {keySafetyVisible && credential.error && !authenticationFailure && (
        <ErrorState error={credential.error} />
      )}
      {keySafetyVisible && credential.stage === 'unknown' && (
        <Callout.Root color="amber">
          <Callout.Text>
            The create outcome is unknown. Refresh the key list, identify and revoke unwanted keys
            before creating again. The original secret cannot be recovered. No request will be
            retried automatically.
            <Button mt="2" variant="outline" onClick={() => reconcileAdministrationKey(client)}>
              I reconciled the key creation
            </Button>
          </Callout.Text>
        </Callout.Root>
      )}
      {error && !authenticationFailure && !confirm && !rename && <ErrorState error={error} />}
      {notice && (
        <Text className="admin-success" as="p" role="status">
          {notice}
        </Text>
      )}
      {unknown && (
        <Callout.Root color="amber">
          <Callout.Text>
            The outcome is unknown. Refresh and inspect records before trying another mutation. A
            created key cannot be recovered; identify and revoke unwanted keys first.
            <Button
              mt="2"
              variant="outline"
              onClick={() => {
                reconcileAdministrationMutation(client, mutationScope);
              }}
            >
              I reconciled the outcome
            </Button>
          </Callout.Text>
        </Callout.Root>
      )}
      {panel === 'keys' && !authenticationFailure && (
        <details className="admin-advanced admin-key-help">
          <summary>Lost a key or no longer need it?</summary>
          <Text as="p" size="2">
            An API key’s secret cannot be recovered. Create a replacement with the required
            policies, update the person or service using it, then revoke the old key. A name change
            only changes its label; it does not rotate the secret.
          </Text>
        </details>
      )}
      {!authenticationFailure && (
        <AdministrationDialogs
          panel={panel}
          tenant={tenant}
          confirm={confirm}
          rename={rename}
          pending={pending}
          unknown={unknown}
          onConfirmChange={setConfirm}
          onRenameChange={setRename}
          act={act}
          client={client}
          error={error}
        />
      )}
    </div>
  );
}
