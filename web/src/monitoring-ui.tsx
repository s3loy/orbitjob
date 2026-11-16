import { when } from './format';
import { returnState, useAlive } from './monitoring-state';
import { useRef, useState, type ReactNode } from 'react';
import { Button, Dialog, Flex, Heading, Text, TextField } from '@radix-ui/themes';
import { RefreshCw } from 'lucide-react';
import { useQueryClient } from '@tanstack/react-query';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import { useSession } from './session';
import { APIError } from './api/transport';
import type { Resource, Budget } from './api/monitoring';
import { ErrorState } from './components';
import { useAdministrationAuthenticationError } from './administration-state';
import { withCredential } from './credential-requests';

export function State({ value }: { value: string }) {
  const good = value === 'success' || value === 'healthy';
  const bad = value === 'failed' || value === 'exhausted' || value === 'critical';
  return (
    <span
      className={`phase-label ${good ? 'success' : bad ? 'failure' : ['warning', 'at_risk'].includes(value) ? 'warning' : 'neutral'}`}
    >
      <span className="status-dot" />
      {(value[0]?.toUpperCase() || '') + value.slice(1).replaceAll('_', ' ')}
    </span>
  );
}
export function Refresh({
  run,
  busy = false,
  label = 'Refresh',
}: {
  run: () => unknown;
  busy?: boolean;
  label?: string;
}) {
  return (
    <Button size="1" variant="outline" color="gray" disabled={busy} onClick={() => void run()}>
      <RefreshCw size={13} aria-hidden="true" />
      {label}
    </Button>
  );
}
export function Facts({ rows }: { rows: [string, ReactNode][] }) {
  return (
    <dl className="definition-list">
      {rows.map(([label, value]) => (
        <div className="fact-pair" key={label}>
          <dt>{label}</dt>
          <dd>{value ?? 'Not recorded'}</dd>
        </div>
      ))}
    </dl>
  );
}
export function Raw({ value, label = 'Technical details' }: { value: unknown; label?: string }) {
  return (
    <details>
      <summary>{label}</summary>
      <pre className="record-json">{JSON.stringify(value, null, 2)}</pre>
    </details>
  );
}
export function Back({ to, children }: { to: string; children: ReactNode }) {
  const location = useLocation();
  const label = /^\/workflows\/[^/?]+\/runs\/[^/?]+(?:\?|$)/.test(to)
    ? 'Back to Workflow run'
    : /^\/workflows\/[^/?]+(?:\?|$)/.test(to)
      ? to.includes('tab=runs')
        ? 'Back to Workflow runs'
        : 'Back to Workflow'
      : /^\/functions\/[^/?]+\/runs\/[^/?]+(?:\?|$)/.test(to)
        ? 'Back to Invocation'
        : /^\/functions\/[^/?]+(?:\?|$)/.test(to)
          ? to.includes('tab=configuration')
            ? 'Back to Function'
            : 'Back to Invocations'
          : /^\/jobs(?:\?|$)/.test(to)
            ? 'Back to Jobs'
            : /^\/jobs\//.test(to)
              ? 'Back to Job'
              : /^\/checks(?:\?|$)/.test(to)
                ? 'Back to Checks'
                : /^\/slis(?:\?|$)/.test(to)
                  ? 'Back to SLIs'
                  : /^\/slos(?:\?|$)/.test(to)
                    ? 'Back to Objectives'
                    : /^\/slo-alerts(?:\?|$)/.test(to)
                      ? 'Back to Alerts'
                      : /^\/slis\//.test(to)
                        ? 'Back to SLI'
                        : /^\/slo-alerts\//.test(to)
                          ? 'Back to Alert'
                          : /^\/slos\//.test(to)
                            ? 'Back to Objective'
                            : children;
  return (
    <Link className="record-link detail-back" to={to} state={returnState(location.state, to)}>
      {label}
    </Link>
  );
}
export function BudgetView({ budget }: { budget: Budget }) {
  const noData = budget.status === 'no_data';
  const comparable = !noData && budget.budget_total > 0;
  return (
    <section className="monitoring-budget" aria-label="Current error budget">
      <div className="budget-title">
        <Heading as="h2" size="3" weight="medium">
          {noData
            ? 'No budget recorded'
            : comparable
              ? 'Current error budget'
              : budget.budget_consumed > 0
                ? 'Zero allowance'
                : 'Coverage not established'}
        </Heading>
        {comparable && <State value={budget.status} />}
      </div>
      {budget.window_start && (
        <Text as="p" size="1" color="gray">
          {when(budget.window_start)} to {when(budget.window_end)}
        </Text>
      )}
      {!comparable && (
        <Text as="p" size="2" color="gray">
          {noData
            ? 'No evaluation budget is available yet. Refresh after the next evaluation.'
            : budget.budget_consumed > 0
              ? 'Failures were recorded against zero allowed events. A consumption ratio is not comparable.'
              : 'Zero allowed events does not establish observed coverage or health.'}
        </Text>
      )}
      {!noData && (
        <>
          <dl className="budget-metrics">
            <div>
              <dt>Remaining allowance</dt>
              <dd className={`budget-remaining ${budget.budget_remaining < 0 ? 'failure' : ''}`}>
                {budget.budget_remaining.toLocaleString(undefined, {
                  maximumSignificantDigits: 15,
                })}{' '}
                events
              </dd>
            </div>
            <div>
              <dt>Allowed</dt>
              <dd>
                {budget.budget_total.toLocaleString(undefined, { maximumSignificantDigits: 15 })}{' '}
                events
              </dd>
            </div>
            <div>
              <dt>Consumed</dt>
              <dd>
                {budget.budget_consumed.toLocaleString(undefined, { maximumSignificantDigits: 15 })}{' '}
                events
              </dd>
            </div>
            <div>
              <dt>Burn rate</dt>
              <dd>{comparable ? `${budget.burn_rate}×` : 'Not comparable'}</dd>
            </div>
          </dl>
          {comparable && (
            <div
              className={`budget-meter ${budget.budget_remaining < 0 ? 'is-exhausted' : ''}`}
              role="img"
              aria-label={`${budget.budget_consumed} of ${budget.budget_total} allowed failure events consumed`}
            >
              <span
                style={{
                  width: `${Math.min(100, Math.max(0, (budget.budget_consumed / budget.budget_total) * 100))}%`,
                }}
              />
            </div>
          )}
          {budget.budget_remaining < 0 && (
            <Text as="p" size="1" color="gray">
              {Math.abs(budget.budget_remaining).toLocaleString(undefined, {
                maximumSignificantDigits: 15,
              })}{' '}
              error events beyond the allowance.
            </Text>
          )}
        </>
      )}
      <Raw value={budget} label="Budget details" />
    </section>
  );
}
export function ResourceActions({
  resource,
  item,
  refresh,
  back,
  placement = 'all',
}: {
  placement?: 'primary' | 'manage' | 'all';
  resource: Resource;
  item: { id: number | string; name: string; version: number; status?: string };
  refresh: () => Promise<{ data?: { version: number; status?: string } }>;
  back: string;
}) {
  const { client } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const cache = useQueryClient();
  const navigate = useNavigate();
  const location = useLocation();
  const alive = useAlive();
  const focusTarget = useRef<HTMLButtonElement | null>(null);
  const [action, setAction] = useState<'pause' | 'resume' | 'delete' | null>(null);
  const [visible, setVisible] = useState(false);
  const [snapshot, setSnapshot] = useState({ ...item });
  const [typed, setTyped] = useState('');
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const [reviewing, setReviewing] = useState(false);
  const conflict = error instanceof APIError && error.status === 409;
  async function reload() {
    if (rejected) return;
    setReviewing(true);
    try {
      const result = await refresh();
      if (!alive.current || !result.data) return;
      setSnapshot((current) => ({
        ...current,
        version: result.data!.version,
        status: result.data!.status,
      }));
      setError(null);
      setTyped('');
    } finally {
      setReviewing(false);
    }
  }
  async function execute() {
    if (!visible || !action || pending || error || rejected) return;
    setPending(true);
    setError(null);
    try {
      if (action === 'delete') {
        await withCredential(client!, () =>
          client!.remove(resource, String(snapshot.id), snapshot.version),
        );
        if (!alive.current) return;
        // Mark stale without refetching an object we just deleted. The next
        // collection mount refreshes normally after navigation.
        void cache.invalidateQueries({ queryKey: ['monitoring'], refetchType: 'none' });
        navigate(back, { replace: true, state: returnState(location.state, back) });
        return;
      }
      if (resource !== 'slis')
        await withCredential(client!, () =>
          client!.change(resource, String(snapshot.id), action, snapshot.version),
        );
      await cache.invalidateQueries({ queryKey: ['monitoring'] });
      if (!alive.current) return;
      await refresh();
      if (alive.current) setVisible(false);
    } catch (cause) {
      setError(cause instanceof Error ? cause : new Error('Request failed'));
    } finally {
      setPending(false);
    }
  }
  function open(next: 'pause' | 'resume' | 'delete', target: HTMLButtonElement) {
    focusTarget.current = target;
    setSnapshot({ ...item });
    setTyped('');
    setError(null);
    setAction(next);
    setVisible(true);
  }
  const staleAction =
    action !== 'delete' &&
    ((action === 'pause' && snapshot.status !== 'active') ||
      (action === 'resume' && snapshot.status !== 'paused'));
  return (
    <>
      <Flex gap="2" wrap="wrap">
        {resource !== 'slis' && placement !== 'manage' && (
          <Button
            size="1"
            variant="outline"
            color="gray"
            disabled={!!rejected}
            onClick={(e) => open(item.status === 'active' ? 'pause' : 'resume', e.currentTarget)}
          >
            {item.status === 'active' ? 'Pause' : 'Resume'}
          </Button>
        )}
        <Button
          size="1"
          variant="ghost"
          color="red"
          disabled={!!rejected}
          style={placement === 'primary' ? { display: 'none' } : undefined}
          onClick={(e) => open('delete', e.currentTarget)}
        >
          Delete
        </Button>
      </Flex>
      <Dialog.Root
        open={visible}
        onOpenChange={(open) => {
          if (!open && !pending) setVisible(false);
        }}
      >
        <Dialog.Content
          onCloseAutoFocus={(e) => {
            e.preventDefault();
            focusTarget.current?.focus();
          }}
          maxWidth="460px"
          className="console-dialog"
          onEscapeKeyDown={(e) => {
            if (pending) e.preventDefault();
          }}
          onPointerDownOutside={(e) => {
            if (pending) e.preventDefault();
          }}
        >
          <Dialog.Title size="4">
            {action === 'delete' ? 'Delete' : action === 'pause' ? 'Pause' : 'Resume'}{' '}
            {snapshot.name}?
          </Dialog.Title>
          <Dialog.Description size="2">
            {action === 'delete'
              ? 'This permanently removes the definition. Type its name to confirm.'
              : 'Change future scheduling or evaluation. Existing records remain available.'}
          </Dialog.Description>
          <Flex align="center" gap="3" mt="3">
            <Text size="1" color="gray">
              Review version {snapshot.version}
            </Text>
            {snapshot.status && <State value={snapshot.status} />}
          </Flex>
          {action === 'delete' && (
            <label className="form-field">
              Definition name
              <TextField.Root
                aria-label="Definition name"
                value={typed}
                onChange={(e) => setTyped(e.target.value)}
                disabled={pending}
                autoComplete="off"
              />
            </label>
          )}
          {(rejected || error) && <ErrorState error={rejected ?? error!} />}
          {error && (
            <div className="operation-notice">
              {conflict ? 'This version was rejected.' : 'The change was not confirmed.'} Refresh,
              review the current version, then confirm again.
              <Button
                size="1"
                mt="2"
                variant="outline"
                disabled={reviewing}
                onClick={() => void reload()}
              >
                Refresh and review
              </Button>
            </div>
          )}
          {staleAction && (
            <Text as="p" size="2">
              The resource is already in the requested state. Close this dialog and review its
              current state.
            </Text>
          )}
          {error && !conflict && (
            <Text as="p" size="1" color="gray">
              The response may be uncertain. Refresh and review the definition before another
              change.
            </Text>
          )}
          <Flex justify="end" gap="3" mt="5">
            <Dialog.Close>
              <Button size="1" variant="soft" color="gray" disabled={pending}>
                Close
              </Button>
            </Dialog.Close>
            <Button
              size="1"
              color={action === 'delete' ? 'red' : 'gray'}
              disabled={
                !visible ||
                pending ||
                reviewing ||
                Boolean(error) ||
                Boolean(rejected) ||
                staleAction ||
                (action === 'delete' && typed !== snapshot.name)
              }
              onClick={() => void execute()}
            >
              {pending ? 'Saving…' : `Confirm ${action}`}
            </Button>
          </Flex>
        </Dialog.Content>
      </Dialog.Root>
    </>
  );
}
