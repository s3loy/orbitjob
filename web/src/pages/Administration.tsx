import { AccountAdministration } from '../AccountAdministration';
import { useEffect, useState } from 'react';
import { Button, Tabs, Text } from '@radix-ui/themes';
import { useQueryClient } from '@tanstack/react-query';
import { useSearchParams } from 'react-router-dom';
import { isValid } from 'ulid';
import { useSession } from '../session';
import { ErrorState, PageHeading } from '../components';
import type { WorkspaceLabel } from '../WorkspacePicker';
import { cachedWorkspace } from '../workspace-directory';
import { sampleWorkspace } from '../sample-workspace';
import { AdministrationActivity } from '../AdministrationActivity';
import { AdministrationPanel, type Panel } from '../AdministrationPanel';
import {
  useAdministrationCredential,
  useAdministrationAuthenticationError,
  useAdministrationCreations,
  useAdministrationPendingMutations,
  type AdministrationCreation,
  administrationKeyDraft,
  administrationScopeLocked,
  updateAdministrationKeyDraft,
} from '../administration-state';
import '../administration.css';

type AdministrationSection = Panel | 'accounts';

export function Administration() {
  const { generation } = useSession();
  return <AdministrationSession key={generation} />;
}
function AdministrationSession() {
  const session = useSession();
  const client = session.client!;
  const credential = useAdministrationCredential(client);
  const rejectedCredential = useAdministrationAuthenticationError(client);
  const creations = useAdministrationCreations(client);
  const unfinished = useAdministrationPendingMutations(client);
  const protectedScope = unfinished[0]?.scope.split(':');
  const [params, setParams] = useSearchParams();
  const cache = useQueryClient();
  const [intent] = useState(() => {
    const target = params.get('workspace');
    if (!target) return {};
    if (!isValid(target))
      return { error: new Error('The workspace link is invalid. Choose a workspace by name.') };
    if (administrationScopeLocked(client))
      return {
        error: new Error(
          'Finish the pending operation, save and discard any revealed key, or reconcile its outcome before switching workspaces.',
        ),
      };
    return rejectedCredential ? {} : { target };
  });
  const [selectedPanel, setPanel] = useState<AdministrationSection>(() => {
    if (['pending', 'revealed', 'unknown'].includes(credential.stage)) return 'keys';
    if (protectedScope && ['tenants', 'keys', 'policies', 'groups'].includes(protectedScope[0]))
      return protectedScope[0] as Panel;
    const requested = params.get('panel');
    if (
      [
        'tenants',
        'keys',
        'policies',
        'groups',
        ...(session.account?.authenticated ? ['accounts'] : []),
      ].includes(requested ?? '')
    )
      return requested as AdministrationSection;
    return session.account?.authenticated && session.targetTenant ? 'accounts' : 'tenants';
  });
  const [tenant, setTenant] = useState(
    (['pending', 'revealed', 'unknown'].includes(credential.stage) ? credential.tenant : '') ||
      protectedScope?.[1] ||
      intent.target ||
      credential.tenant ||
      session.targetTenant ||
      session.identity?.target_tenant_id ||
      session.identity?.tenant_id ||
      (session.mode === 'demo' ? sampleWorkspace.id : ''),
  );
  const [scopeError, setScopeError] = useState<Error | undefined>(intent.error);
  const [chosen, setChosen] = useState<WorkspaceLabel>();
  const panel: AdministrationSection = params.get('review') === 'key' ? 'keys' : selectedPanel;
  const authenticationError = rejectedCredential;
  const metadata = session.identity;
  const current =
    session.targetTenant || session.identity?.target_tenant_id || session.identity?.tenant_id;
  const created = creations.find((row) => row.kind === 'tenants' && row.id === tenant);
  const workspace =
    chosen?.id === tenant
      ? chosen
      : tenant === current
        ? {
            id: tenant,
            name: metadata?.tenant_name || 'Current workspace',
            slug: metadata?.tenant_slug || '',
          }
        : created
          ? { id: tenant, name: created.name, slug: '' }
          : cachedWorkspace(cache, session.generation, tenant);
  const requested = params.get('workspace');
  useEffect(() => {
    if (!requested) return;
    const next = new URLSearchParams(params);
    next.delete('workspace');
    setParams(next, { replace: true });
  }, [requested, params, setParams]);
  function selectPanel(value: AdministrationSection) {
    setPanel(value);
    const next = new URLSearchParams(params);
    next.delete('review');
    next.delete('page');
    next.delete('offset');
    next.set('panel', value);
    setParams(next, { replace: true });
  }
  function selectWorkspace(value: string, label?: WorkspaceLabel) {
    if (authenticationError) return;
    if (value === tenant) return;
    setScopeError(undefined);
    if (administrationScopeLocked(client)) {
      setScopeError(
        new Error(
          'Finish the pending operation, save and discard any revealed key, or reconcile its outcome before switching workspaces.',
        ),
      );
      return;
    }
    setTenant(value);
    setChosen(label);
    const next = new URLSearchParams(params);
    next.delete('page');
    next.delete('offset');
    setParams(next, { replace: true });
  }
  function follow(receipt: AdministrationCreation) {
    if (authenticationError) return;
    if (administrationScopeLocked(client)) {
      setScopeError(
        new Error(
          'Finish or reconcile the current operation and save and discard any revealed key before preparing another key.',
        ),
      );
      return;
    }
    if (receipt.kind === 'tenants') {
      void selectWorkspace(receipt.id);
    } else {
      const target = receipt.tenant || tenant;
      setTenant(target);
      const draft = administrationKeyDraft(client, target);
      updateAdministrationKeyDraft(client, target, {
        ...(receipt.kind === 'policies'
          ? { policies: [...new Set([...draft.policies, receipt.id])] }
          : { group: receipt.id }),
      });
    }
    selectPanel('keys');
    document.getElementById('administration-work')?.scrollIntoView({ block: 'start' });
  }
  return (
    <>
      <PageHeading title="Administration" />
      {scopeError && !authenticationError && <ErrorState error={scopeError} />}
      {!!unfinished.length && (
        <section className="operation-notice" aria-label="Unfinished management operations">
          <Text as="p" size="2" weight="medium">
            Review unfinished operations
          </Text>
          {unfinished.map((operation) => {
            const [scopePanel, scopeTenant] = operation.scope.split(':');
            const label =
              scopeTenant === current
                ? metadata?.tenant_name || 'Current workspace'
                : cachedWorkspace(cache, session.generation, scopeTenant)?.name ||
                  creations.find((row) => row.kind === 'tenants' && row.id === scopeTenant)?.name ||
                  'Selected workspace';
            return (
              <Button
                key={operation.scope}
                type="button"
                variant="outline"
                size="1"
                mr="2"
                mb="2"
                disabled={['pending', 'revealed', 'unknown'].includes(credential.stage)}
                onClick={() => {
                  if (!unfinished.some((item) => item.scope === operation.scope)) return;
                  if (!['tenants', 'keys', 'policies', 'groups'].includes(scopePanel)) return;
                  setTenant(scopeTenant || current || '');
                  setChosen(undefined);
                  setScopeError(undefined);
                  selectPanel(scopePanel as Panel);
                }}
              >
                Review {operation.stage}{' '}
                {scopePanel === 'tenants' ? 'workspace creation' : `${scopePanel} in ${label}`}
              </Button>
            );
          })}
        </section>
      )}
      <Tabs.Root
        value={panel}
        onValueChange={(value) => selectPanel(value as AdministrationSection)}
        id="administration-work"
      >
        <Tabs.List className="administration-tabs">
          {session.account?.authenticated && (
            <Tabs.Trigger value="accounts" disabled={!!authenticationError}>
              Accounts
            </Tabs.Trigger>
          )}
          {(['tenants', 'keys', 'policies', 'groups'] as const).map((p) => (
            <Tabs.Trigger key={p} value={p} disabled={!!authenticationError}>
              {
                {
                  tenants: 'Workspaces',
                  keys: 'API keys',
                  policies: 'Policies',
                  groups: 'Resource groups',
                }[p]
              }
            </Tabs.Trigger>
          ))}
        </Tabs.List>
        {session.account?.authenticated && (
          <Tabs.Content value="accounts" forceMount hidden={panel !== 'accounts'}>
            <AccountAdministration />
          </Tabs.Content>
        )}
        {panel !== 'accounts' && (
          <Tabs.Content value={panel}>
            <AdministrationPanel
              key={`${panel}:${tenant}`}
              panel={panel}
              tenant={tenant}
              setTenant={(value, label) => {
                void selectWorkspace(value, label);
              }}
              workspace={workspace}
              onPanel={selectPanel}
              authenticationError={authenticationError}
            />
          </Tabs.Content>
        )}
      </Tabs.Root>
      <AdministrationActivity
        onContinue={follow}
        disabled={!!authenticationError}
        highlight={params.get('review') === 'created'}
      />
    </>
  );
}
