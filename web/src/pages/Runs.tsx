import '../job-run-workspace.css';
import { HistoryWindow } from '../HistoryWindow';
import { queryWindow } from '../api/query-window';
import { runPhaseLabel, when } from '../format';
import { Back } from '../monitoring-ui';
import { forwardState, useBack } from '../monitoring-state';
import { Button, Dialog, Flex, Select, Text } from '@radix-ui/themes';
import { RefreshCw } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useLocation, useParams, useSearchParams } from 'react-router-dom';
import { phases, terminal } from '../api/contracts';
import { APIError, OBSERVATION_MS, observationDelay } from '../api/transport';
import {
  Empty,
  ErrorState,
  Loading,
  PageHeading,
  Paging,
  PhaseBadge,
  RunTable,
} from '../components';
import { useSession } from '../session';
import { pageLimit, pageLimitParams, pageOffset, pageParams } from '../list-state';
import { useOperations } from '../operation-state';
import { AttemptTimeline } from '../AttemptTimeline';
import { ExecutionLogs } from '../ExecutionLogs';
import { withCredential } from '../credential-requests';
import {
  administrationAuthenticationError,
  rejectAdministrationCredential,
  useAdministrationAuthenticationError,
} from '../administration-state';

function focusEvidence(id: 'run-output' | 'attempt-diagnostics') {
  const target = document.getElementById(id);
  target?.focus({ preventScroll: true });
  target?.scrollIntoView({ block: 'start' });
}

export function Runs() {
  const location = useLocation();
  const { client, generation } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const [params, setParams] = useSearchParams();
  const phase = params.get('phase') ?? '';
  const source = params.get('source_uid') ?? '';
  const occurrence = params.get('occurrence_key') ?? '';
  const limit = pageLimit(params);
  const offset = pageOffset(params);
  const window = queryWindow(params);
  const filter = {
    phase,
    source_uid: source,
    occurrence_key: occurrence,
    created_from: window.created_from,
    created_before: window.created_before,
  };
  const summary = useQuery({
    queryKey: ['run-summary', generation, filter],
    queryFn: () => withCredential(client!, () => client!.runSummary(filter)),
    enabled: !window.error && !rejected,
  });
  const runs = useQuery({
    queryKey: ['runs', generation, filter, offset, limit],
    queryFn: () => withCredential(client!, () => client!.runs({ ...filter, offset, limit })),
    enabled: !window.error && !rejected,
  });
  if (rejected)
    return (
      <>
        <PageHeading title="Runs" />
        <ErrorState error={rejected} />
      </>
    );
  return (
    <>
      <PageHeading
        title="Runs"
        action={
          <Button
            size="1"
            variant="outline"
            color="gray"
            disabled={runs.isFetching || summary.isFetching || Boolean(window.error)}
            onClick={() => {
              if (!window.error) {
                void runs.refetch();
                void summary.refetch();
              }
            }}
          >
            <RefreshCw size={13} />
            Refresh
          </Button>
        }
      />
      <HistoryWindow
        params={params}
        change={setParams}
        timestamp="Created time"
        leading={
          <div className="run-phase-filter">
            <Text as="label" htmlFor="run-phase" size="1">
              Phase
            </Text>
            <Select.Root
              size="2"
              value={phase || 'all'}
              onValueChange={(value) => {
                const next = new URLSearchParams(params);
                next.delete('page');
                next.delete('offset');
                if (value === 'all') next.delete('phase');
                else next.set('phase', value);
                setParams(next);
              }}
            >
              <Select.Trigger
                id="run-phase"
                aria-label="Run phase"
                variant="surface"
                color="gray"
              />
              <Select.Content>
                <Select.Item value="all">All phases</Select.Item>
                {phases.map((item) => (
                  <Select.Item key={item} value={item}>
                    {runPhaseLabel(item)}
                  </Select.Item>
                ))}
              </Select.Content>
            </Select.Root>
          </div>
        }
      />
      {(source || occurrence) && (
        <Flex gap="3" wrap="wrap" align="center" mb="3" style={{ overflowWrap: 'anywhere' }}>
          <details className="run-filter-identity">
            <summary>
              {source && occurrence
                ? 'Source and request filters applied'
                : source
                  ? 'Source filter applied'
                  : 'Request filter applied'}
            </summary>
            <dl className="definition-list">
              {source && (
                <>
                  <dt>Source UID</dt>
                  <dd className="mono">{source}</dd>
                </>
              )}
              {occurrence && (
                <>
                  <dt>Occurrence key</dt>
                  <dd className="mono">{occurrence}</dd>
                </>
              )}
            </dl>
          </details>
          <Button
            size="1"
            color="gray"
            variant="ghost"
            onClick={() => {
              const next = new URLSearchParams(params);
              ['source_uid', 'occurrence_key', 'page', 'offset'].forEach((key) => next.delete(key));
              setParams(next);
            }}
          >
            Clear execution filter
          </Button>
        </Flex>
      )}
      {!window.error && (
        <section aria-label="Filtered run summary" className="detail-section">
          {summary.isPending ? (
            <Text size="1" color="gray">
              Loading filtered count…
            </Text>
          ) : summary.error ? (
            <ErrorState error={summary.error} retry={() => void summary.refetch()} />
          ) : (
            <Flex gap="4" wrap="wrap" role="status">
              <Text size="2">{summary.data.total} matching runs</Text>
              {Object.entries(summary.data.phases)
                .filter(([, count]) => BigInt(count) > 0n)
                .map(([phase, count]) => (
                  <Text key={phase} size="1" color="gray">
                    {runPhaseLabel(phase)}: {count}
                  </Text>
                ))}
            </Flex>
          )}
        </section>
      )}
      {window.error ? (
        <Text as="p" color="red" role="alert">
          {window.error}
        </Text>
      ) : runs.isPending ? (
        <Loading />
      ) : runs.error ? (
        <ErrorState error={runs.error} retry={() => void runs.refetch()} />
      ) : (
        <>
          {runs.data.length ? (
            <RunTable runs={runs.data} />
          ) : (
            <Empty
              title={
                offset
                  ? 'No runs on this page'
                  : phase || source || occurrence || window.created_from || window.created_before
                    ? 'No matching runs'
                    : 'No runs yet'
              }
            >
              {offset ? (
                'Go to the previous page to find earlier results.'
              ) : phase || source || occurrence || window.created_from || window.created_before ? (
                'Change the filters or time range to find other runs.'
              ) : (
                <>
                  Start a job to see its progress and output here.{' '}
                  <Link className="record-link" to="/jobs" state={forwardState(location)}>
                    Browse jobs
                  </Link>
                </>
              )}
            </Empty>
          )}
          <Paging
            limit={limit}
            setLimit={(n) => setParams(pageLimitParams(params, n), { state: location.state })}
            offset={offset}
            count={runs.data.length}
            setOffset={(value) => setParams(pageParams(params, value), { state: location.state })}
          />
        </>
      )}
    </>
  );
}

export function RunView() {
  const { id = '' } = useParams();
  const location = useLocation();
  const back = useBack('/runs');
  const backLink = (
    <Back to={back}>
      {back.startsWith('/workflows/') ? 'Back to Workflow run' : 'Back to Runs'}
    </Back>
  );
  const { client, generation, mode } = useSession();
  const hasArchive = Boolean(client?.archiveLogs);
  const rejectedCredential = useAdministrationAuthenticationError(client);
  const operations = useOperations();
  const cancellation = operations.cancellations[id];
  const cancel = {
    isPending: cancellation?.pending,
    isSuccess: Boolean(cancellation?.acceptedAt),
    error: cancellation?.error,
  };
  const cache = useQueryClient();
  const [manualSince, setSince] = useState(() => Date.now());
  const since = Math.max(manualSince, cancellation?.acceptedAt ?? 0);
  const deadline = useRef(since + OBSERVATION_MS);
  const [expiredSince, setExpiredSince] = useState(0);
  const paused = expiredSince === since;
  const [cancelOpen, setCancelOpen] = useState(false);
  const keep = useRef<HTMLButtonElement>(null);
  const notice = useRef<HTMLDivElement>(null);
  const query = useQuery({
    queryKey: ['run', generation, id],
    queryFn: async () => {
      const rejected = administrationAuthenticationError(client);
      if (rejected) throw rejected;
      try {
        return Date.now() >= Math.max(deadline.current, since + OBSERVATION_MS)
          ? (cache.getQueryData<import('../api/contracts').RunDetail>(['run', generation, id]) ??
              null)
          : await client!.run(id);
      } catch (error) {
        if (error instanceof Error) rejectAdministrationCredential(client!, error);
        throw error;
      }
    },
    enabled: !paused && !rejectedCredential,
    networkMode: 'always',
    refetchOnReconnect: false,
    refetchInterval: (state) =>
      paused || (state.state.data && terminal(state.state.data.phase))
        ? false
        : observationDelay(state.state.error, since),
    refetchIntervalInBackground: false,
  });
  const readError = rejectedCredential ?? query.error;
  const inaccessible = readError instanceof APIError && [401, 403, 404].includes(readError.status);
  const job = useQuery({
    queryKey: ['job', generation, query.data?.revision_id],
    enabled: Boolean(query.data) && !inaccessible,
    queryFn: async () => {
      try {
        return await client!.job(query.data!.revision_id);
      } catch (error) {
        if (error instanceof Error) rejectAdministrationCredential(client!, error);
        throw error;
      }
    },
  });
  const refetchRun = query.refetch;
  useEffect(() => {
    if (cancellation?.acceptedAt) void refetchRun();
  }, [cancellation?.acceptedAt, refetchRun]);
  useEffect(() => {
    const timer = setTimeout(
      () => setExpiredSince(since),
      Math.max(0, since + OBSERVATION_MS - Date.now()),
    );
    return () => clearTimeout(timer);
  }, [since]);
  function refresh() {
    if (administrationAuthenticationError(client)) return;
    deadline.current = Date.now() + OBSERVATION_MS;
    setSince(Date.now());
    void query.refetch();
  }
  const cancellationReceipt = (
    <>
      {cancel.isPending && (
        <div className="operation-notice" role="status">
          Submitting cancellation request.
        </div>
      )}
      {cancel.error && <ErrorState error={cancel.error} />}
      {cancel.isSuccess && (
        <div className="operation-notice" role="status" tabIndex={-1} ref={notice}>
          Cancel request accepted. The observed Run phase determines the outcome; acceptance alone
          does not confirm a stop.
        </div>
      )}
    </>
  );
  if (inaccessible)
    return (
      <>
        {backLink}
        <PageHeading
          title={readError.status === 404 ? 'Run no longer available' : 'Run unavailable'}
        />
        {cancellationReceipt}
        <ErrorState error={readError} retry={refresh} />
      </>
    );
  if (query.isPending)
    return (
      <>
        {cancellationReceipt}
        <Loading />
      </>
    );
  if (!query.data)
    return (
      <>
        {backLink}
        {cancellationReceipt}
        <ErrorState error={readError ?? new Error('Run unavailable')} retry={refresh} />
      </>
    );
  const run = query.data;
  const workload = job.error ? undefined : job.data;
  const blocked =
    terminal(run.phase) ||
    ['CancelRequested', 'CancelUnknown'].includes(run.phase) ||
    cancel.isPending ||
    cancel.isSuccess ||
    Boolean(readError);
  return (
    <>
      {backLink}
      <PageHeading
        title={`Run #${run.id}`}
        action={
          <Flex gap="3" wrap="wrap">
            <Button
              size="1"
              variant="outline"
              color="gray"
              disabled={query.isFetching}
              onClick={refresh}
            >
              <RefreshCw size={13} />
              Refresh
            </Button>
            <Dialog.Root open={cancelOpen} onOpenChange={setCancelOpen}>
              <Dialog.Trigger>
                <Button size="1" color="gray" variant="outline" disabled={blocked}>
                  {cancel.isSuccess ? 'Cancel requested' : 'Request cancel'}
                </Button>
              </Dialog.Trigger>
              <Dialog.Content
                maxWidth="450px"
                onOpenAutoFocus={(event) => {
                  event.preventDefault();
                  keep.current?.focus();
                }}
                onCloseAutoFocus={(event) => {
                  if (cancel.isSuccess) {
                    event.preventDefault();
                    notice.current?.focus();
                  }
                }}
              >
                <Dialog.Title>Request cancellation?</Dialog.Title>
                <Dialog.Description size="2">
                  Submit a request to stop Run #{run.id}
                  {workload ? ` for ${workload.name}` : ''}. The execution may still be active until
                  a stop is observed.
                </Dialog.Description>
                {mode === 'demo' && (
                  <Text as="p" size="1" color="gray" mt="3">
                    This action only changes the sample data.
                  </Text>
                )}
                {cancel.error && <ErrorState error={cancel.error} />}
                <Flex gap="3" justify="end" mt="5" wrap="wrap">
                  <Dialog.Close>
                    <Button ref={keep} size="1" variant="soft" color="gray">
                      Keep observing
                    </Button>
                  </Dialog.Close>
                  <Button
                    size="1"
                    color="red"
                    loading={cancel.isPending}
                    disabled={blocked}
                    onClick={async () => {
                      if (await operations.cancel(id)) setCancelOpen(false);
                    }}
                  >
                    Confirm cancel request
                  </Button>
                </Flex>
              </Dialog.Content>
            </Dialog.Root>
          </Flex>
        }
      />
      <div className="run-meta">
        <PhaseBadge phase={run.phase} />
        {workload && (
          <Link className="record-link" to={`/jobs/${workload.id}`} state={forwardState(location)}>
            {workload.name} ({workload.namespace})
          </Link>
        )}
        <Text size="2">
          {run.attempt} of {run.max_attempts} attempts
        </Text>
      </div>
      {readError && (
        <>
          <div className="operation-notice" role="status">
            Last observed snapshot. The latest read failed; these details do not confirm the current
            execution state. Refresh successfully before requesting cancellation.
          </div>
          <ErrorState error={readError} retry={refresh} />
        </>
      )}
      {cancellationReceipt}
      {run.phase === 'ExecutionUnknown' && (
        <div className="operation-notice" role="status">
          Execution outcome unknown. The workload may still be running. No retry or completed
          outcome is confirmed; request cancellation or keep observing this execution.
        </div>
      )}
      {run.phase === 'CancelUnknown' && (
        <div className="operation-notice" role="status">
          The stop could not be confirmed. This is not a confirmed cancellation.
        </div>
      )}
      <Flex gap="3" wrap="wrap" justify="between" mb="5">
        <Text size="1" color="gray">
          {run.trigger} trigger at {when(run.created_at)}
        </Text>
        {!terminal(run.phase) && (
          <Text size="1" color="gray" role="status">
            {query.error
              ? query.error instanceof APIError && query.error.status === 429
                ? 'Refresh after the rate limit clears.'
                : 'Observation stopped. Refresh to retry.'
              : paused
                ? 'Observation paused. Refresh to continue.'
                : 'Checking for updates'}
          </Text>
        )}
      </Flex>
      {run.phase === 'Failed' && (
        <section className="run-investigation" aria-label="Failure investigation">
          <h2>Investigate failure</h2>
          {!hasArchive && <p>Execution archive is unavailable in this connection.</p>}
          <Flex gap="2" wrap="wrap">
            {hasArchive && (
              <Button
                size="2"
                variant="ghost"
                color="blue"
                onClick={() => focusEvidence('run-output')}
              >
                Execution output
              </Button>
            )}
            <Button
              size="2"
              variant="ghost"
              color="blue"
              onClick={() => focusEvidence('attempt-diagnostics')}
            >
              {run.attempts.length ? 'Attempts and container logs' : 'Attempt details'}
            </Button>
          </Flex>
        </section>
      )}
      {run.phase === 'CancelUnknown' && (
        <Text as="p" size="2" color="gray" mb="4">
          Refresh to look for a confirmed stop, or inspect the retained output and attempt evidence
          below.
        </Text>
      )}
      {hasArchive && (
        <div id="run-output" tabIndex={-1}>
          <ExecutionLogs parent={{ kind: 'instance', run: String(run.id) }} />
        </div>
      )}
      <AttemptTimeline run={run} />
      <details className="run-identity">
        <summary>Run identity & trigger</summary>
        <dl className="definition-list">
          <dt>Source UID</dt>
          <dd className="mono">{run.source_uid}</dd>
          <dt>Occurrence key</dt>
          <dd className="mono">{run.occurrence_key}</dd>
          <dt>Triggered by</dt>
          <dd className="mono">{run.actor}</dd>
          <dt>Revision</dt>
          <dd>#{run.revision_id}</dd>
          <dt>Tenant</dt>
          <dd className="mono">{run.tenant_id}</dd>
          <dt>Last observed</dt>
          <dd>{when(run.updated_at)}</dd>
        </dl>
        <Text as="p" size="1" color="gray">
          Automatic updates run for up to two minutes. Refresh to check again after they pause.
        </Text>
      </details>
    </>
  );
}
