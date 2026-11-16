import { SourceControlPanel } from '../SourceControlPanel';
import { WorkflowSnapshot } from '../WorkflowSnapshot';
import { Button, Flex, Heading, Table, Tabs, Text, TextField } from '@radix-ui/themes';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Search } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { Link, useLocation, useParams, useSearchParams } from 'react-router-dom';
import { useSession } from '../session';
import { forwardState, useBack } from '../monitoring-state';
import { Back, Facts, Raw, Refresh } from '../monitoring-ui';
import { Empty, ErrorState, Loading, PageHeading, Paging, PhaseBadge } from '../components';
import { CurrentGraph, M4Notice, M4ReadError, M4Time } from '../m4-ui';
import { M4Action, WorkflowCancel, WorkflowCancelNotice } from '../m4-actions';
import { useM4Operations } from '../m4-state';
import { APIError, OBSERVATION_MS, PAGE_SIZE, observationDelay } from '../api/transport';
import { pageLimit, pageLimitParams, pageOffset, pageParams } from '../list-state';
import type { WorkflowRun, WorkflowRunDetail } from '../api/m4';
import { PinnedWorkflowGraph } from '../workflow-history';
import { HistoryWindow } from '../HistoryWindow';
import { queryWindow, collectionSearch, collectionSearchParams } from '../api/query-window';
import { useAdministrationAuthenticationError } from '../administration-state';
import { withCredential } from '../credential-requests';

const CURRENT_DEFINITION_LOOKUP_LIMIT = 200;
const concurrencyLabels = new Map([
  ['Allow', 'Allow overlapping runs'],
  ['Forbid', 'Prevent overlapping runs'],
]);
const failureLabels = new Map([
  ['FailFast', 'Stop on failure'],
  ['fail_fast', 'Stop on failure'],
  ['Continue', 'Continue other tasks'],
  ['continue', 'Continue other tasks'],
]);

export function Workflows() {
  const { limitedUntil } = useM4Operations();
  const { client, generation } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const location = useLocation();
  const [params, setParams] = useSearchParams();
  const limit = pageLimit(params);
  const offset = pageOffset(params);
  const { search, error: searchError } = collectionSearch(params);
  const query = useQuery({
    queryKey: ['m4', generation, 'workflows', offset, limit, search],
    enabled: !searchError && !rejected,
    queryFn: () => {
      if (searchError) throw new Error(searchError);
      return withCredential(client!, () => client!.workflows({ offset, search, limit }));
    },
  });
  const items = query.data ?? [];
  return (
    <>
      <PageHeading
        title="Workflows"
        action={
          <Flex gap="3" wrap="wrap">
            <Refresh
              run={() => query.refetch()}
              busy={query.isFetching || Boolean(limitedUntil) || !!searchError || !!rejected}
            />
            {rejected ? (
              <Button size="2" disabled>
                Create a workflow
              </Button>
            ) : (
              <Button asChild size="2">
                <Link to="/manifests/workflow" state={forwardState(location)}>
                  Create a workflow
                </Link>
              </Button>
            )}
          </Flex>
        }
      />
      <Flex className="list-tools" align="center" gap="3" wrap="wrap">
        <TextField.Root
          size="2"
          type="search"
          aria-label="Search workflows"
          placeholder="Search name or namespace…"
          value={search}
          maxLength={128}
          disabled={!!rejected}
          onChange={(event) =>
            setParams(collectionSearchParams(params, event.target.value), { replace: true })
          }
        >
          <TextField.Slot>
            <Search size={14} aria-hidden="true" />
          </TextField.Slot>
        </TextField.Root>
      </Flex>
      {rejected ? (
        <ErrorState error={rejected} />
      ) : searchError ? (
        <M4Notice title="Invalid search">{searchError}</M4Notice>
      ) : query.isPending ? (
        <Loading />
      ) : query.error ? (
        <M4ReadError kind="workflow" error={query.error} retry={() => void query.refetch()} />
      ) : (
        <>
          {!items.length ? (
            <Empty
              title={
                search
                  ? 'No matching workflows'
                  : offset
                    ? 'No records on this page'
                    : 'No workflows yet'
              }
            >
              {search ? (
                'Try a different name or namespace.'
              ) : offset ? (
                'Go to the previous page.'
              ) : (
                <>
                  <Text as="p" size="2" color="gray" mb="3">
                    Connect jobs into tasks that run in dependency order.
                  </Text>
                  <Button asChild>
                    <Link to="/manifests/workflow" state={forwardState(location)}>
                      Create your first workflow
                    </Link>
                  </Button>
                </>
              )}
            </Empty>
          ) : (
            <Table.Root className="ledger-table m4-collection workflow-collection" size="1">
              <Table.Header>
                <Table.Row>
                  {['Workflow', 'Schedule', 'Namespace', 'Tasks'].map((h, i) => (
                    <Table.ColumnHeaderCell key={h} className={i > 1 ? 'hide-mobile' : ''}>
                      {h}
                    </Table.ColumnHeaderCell>
                  ))}
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {items.map((w) => (
                  <Table.Row key={String(w.id)}>
                    <Table.RowHeaderCell>
                      <Link
                        className="record-link"
                        to={`/workflows/${w.id}`}
                        state={forwardState(location)}
                      >
                        {w.name}
                      </Link>
                    </Table.RowHeaderCell>
                    <Table.Cell>
                      {w.schedule ? (
                        <Flex gap="2" wrap="wrap" align="center">
                          <code>{w.schedule}</code>
                          <Text size="1" color="gray">
                            {w.time_zone || 'UTC'}
                          </Text>
                          {w.suspend && (
                            <Text size="1" color="gray">
                              Paused
                            </Text>
                          )}
                        </Flex>
                      ) : (
                        'Manual only'
                      )}
                    </Table.Cell>
                    <Table.Cell className="hide-mobile">{w.namespace}</Table.Cell>
                    <Table.Cell className="hide-mobile">{w.task_count}</Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </Table.Root>
          )}
          <Paging
            limit={limit}
            setLimit={(n) => setParams(pageLimitParams(params, n), { state: location.state })}
            offset={offset}
            count={query.data.length}
            setOffset={(n) => setParams(pageParams(params, n), { state: location.state })}
          />
        </>
      )}
    </>
  );
}

function WorkflowHistory({ id }: { id: string }) {
  const { limitedUntil } = useM4Operations();
  const { client, generation } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const location = useLocation();
  const [params, setParams] = useSearchParams();
  const limit = pageLimit(params);
  const offset = pageOffset(params);
  const range = queryWindow(params);
  const query = useQuery({
    queryKey: [
      'm4',
      generation,
      'workflow-runs',
      id,
      offset,
      limit,
      range.created_from,
      range.created_before,
    ],
    enabled: !range.error && !rejected,
    queryFn: () => {
      if (range.error) throw new Error(range.error);
      return withCredential(client!, () =>
        client!.workflowRuns(id, {
          offset,
          limit,
          created_from: range.created_from,
          created_before: range.created_before,
        }),
      );
    },
  });
  if (rejected) return <ErrorState error={rejected} />;
  return (
    <>
      <Flex className="list-tools" justify="between" gap="3" wrap="wrap" align="center">
        <Heading as="h2" size="3" weight="medium">
          Recorded runs
        </Heading>
        <Refresh
          run={() => query.refetch()}
          busy={query.isFetching || Boolean(limitedUntil) || !!range.error}
        />
      </Flex>
      <HistoryWindow
        params={params}
        change={(next) => setParams(next, { state: location.state })}
      />
      {range.error ? (
        <M4Notice title="Invalid history range">{range.error}</M4Notice>
      ) : query.isPending ? (
        <Loading />
      ) : query.error ? (
        <ErrorState error={query.error} retry={() => void query.refetch()} />
      ) : (
        <>
          {!query.data.length ? (
            <Empty title="No recorded workflow runs">
              Trigger this workflow to start a run. If a request was just accepted, refresh in a
              moment or adjust the time range to find earlier runs.
            </Empty>
          ) : (
            <Table.Root className="ledger-table m4-records" size="1">
              <Table.Header>
                <Table.Row>
                  {['Run', 'Phase', 'Revision', 'Trigger', 'Created (UTC)'].map((h) => (
                    <Table.ColumnHeaderCell key={h}>{h}</Table.ColumnHeaderCell>
                  ))}
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {query.data.map((r: WorkflowRun) => (
                  <Table.Row key={String(r.id)}>
                    <Table.RowHeaderCell data-label="Run">
                      <Link
                        className="record-link"
                        to={`/workflows/${id}/runs/${r.id}`}
                        state={forwardState(location)}
                      >
                        #{r.id}
                      </Link>
                    </Table.RowHeaderCell>
                    <Table.Cell data-label="Phase">
                      <PhaseBadge phase={r.phase} />
                    </Table.Cell>
                    <Table.Cell data-label="Pinned revision">#{r.revision_id}</Table.Cell>
                    <Table.Cell data-label="Trigger">{r.trigger}</Table.Cell>
                    <Table.Cell data-label="Created (UTC)">
                      <M4Time value={r.created_at} />
                      {r.scheduled_for && (
                        <Text as="p" size="1" color="gray">
                          Planned <M4Time value={r.scheduled_for} />
                        </Text>
                      )}
                    </Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </Table.Root>
          )}
          <Paging
            limit={limit}
            setLimit={(n) => setParams(pageLimitParams(params, n), { state: location.state })}
            offset={offset}
            count={query.data.length}
            setOffset={(n) => setParams(pageParams(params, n), { state: location.state })}
          />
        </>
      )}
    </>
  );
}

export function WorkflowView() {
  const { limitedUntil } = useM4Operations();
  const { id = '' } = useParams();
  const { client, generation } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const location = useLocation();
  const back = useBack('/workflows');
  const [params, setParams] = useSearchParams();
  const tab = params.get('tab') === 'runs' ? 'runs' : 'definition';
  const query = useQuery({
    queryKey: ['m4', generation, 'workflow', id],
    enabled: !rejected,
    queryFn: () => withCredential(client!, () => client!.workflow(id)),
  });
  if (rejected)
    return (
      <>
        <Back to={back}>Back to Workflows</Back>
        <ErrorState error={rejected} />
      </>
    );
  if (query.isPending) return <Loading />;
  const item = query.error ? undefined : query.data;
  const concurrencyPolicy = item?.concurrency_policy || 'Allow';
  const failurePolicy = item?.fail_policy || '';
  return (
    <>
      <Back to={back}>Back to Workflows</Back>
      <PageHeading
        title={item?.name ?? `Workflow #${id}`}
        action={
          <Flex gap="2">
            <Refresh run={() => query.refetch()} busy={query.isFetching || Boolean(limitedUntil)} />
            {item && (
              <>
                <Button size="1" variant="outline" color="gray" asChild>
                  <Link
                    to={`/definitions/workflows?${new URLSearchParams({ namespace: item.namespace, name: item.name })}`}
                    state={forwardState(location)}
                  >
                    Edit definition
                  </Link>
                </Button>
                <M4Action kind="workflow" item={item} disabled={Boolean(limitedUntil)} />
              </>
            )}
          </Flex>
        }
      />
      {query.error && (
        <M4ReadError kind="workflow" error={query.error} retry={() => void query.refetch()} />
      )}
      {item && (
        <div className="run-meta">
          <Text size="2">
            {item.suspend
              ? 'Schedule paused'
              : item.schedule
                ? 'Automatic and manual starts'
                : 'Manual only'}
          </Text>
          <Text size="1" color="gray">
            Namespace {item.namespace}
          </Text>
        </div>
      )}
      {item?.suspend && (
        <M4Notice title="Automatic starts paused">
          Manual runs remain available. Existing runs continue.
        </M4Notice>
      )}
      {item && <SourceControlPanel kind="workflows" id={String(item.id)} />}
      <Tabs.Root
        className="job-tabs"
        value={tab}
        onValueChange={(value) => {
          const next = new URLSearchParams(params);
          next.set('tab', value);
          setParams(next, { state: location.state });
        }}
      >
        <Tabs.List size="1">
          <Tabs.Trigger value="definition">Definition</Tabs.Trigger>
          <Tabs.Trigger value="runs">Runs</Tabs.Trigger>
        </Tabs.List>
        <Tabs.Content className="tab-content" value="definition">
          {item ? (
            <>
              <CurrentGraph key={id} workflow={item} />
              <section className="m4-section">
                <Heading as="h2" size="3" weight="medium">
                  Configuration
                </Heading>
                <div className="definition m4-configuration">
                  <Facts
                    rows={[
                      ['Schedule', item.schedule ? <code>{item.schedule}</code> : 'Manual only'],
                      ...(item.schedule
                        ? [['Time zone', item.time_zone || 'UTC'] as [string, string]]
                        : []),
                      [
                        'Concurrent runs',
                        <>
                          {concurrencyLabels.get(concurrencyPolicy) ?? concurrencyPolicy}
                          {concurrencyPolicy === 'Forbid' && (
                            <Text as="p" size="1" color="gray">
                              Scheduled starts are skipped while another run is open. Accepted
                              manual starts wait for it to finish.
                            </Text>
                          )}
                        </>,
                      ],
                      [
                        'On task failure',
                        <>
                          {failureLabels.get(failurePolicy) ?? (failurePolicy || 'Not recorded')}
                          {['FailFast', 'fail_fast'].includes(failurePolicy) && (
                            <Text as="p" size="1" color="gray">
                              No new tasks start after a task finishes with failure. Running tasks
                              continue.
                            </Text>
                          )}
                        </>,
                      ],
                      ['Timeout', `${item.timeout_seconds} seconds`],
                    ]}
                  />
                  {item.schedule && (
                    <Text as="p" size="1" color="gray">
                      Missed automatic starts are skipped.
                    </Text>
                  )}
                  <details className="m4-detail-disclosure">
                    <summary>Definition identity</summary>
                    <Facts
                      rows={[
                        ['Revision', `#${item.id}`],
                        ['Generation', item.generation],
                        ['Source UID', <code>{item.source_uid}</code>],
                      ]}
                    />
                  </details>
                  <Raw value={item} label="Definition fields" />
                </div>
              </section>
            </>
          ) : (
            <Empty title="Definition unavailable">
              You can still check the Runs tab for executions available to your account.
            </Empty>
          )}
        </Tabs.Content>
        <Tabs.Content className="tab-content" value="runs">
          <WorkflowHistory id={id} />
        </Tabs.Content>
      </Tabs.Root>
    </>
  );
}

export function WorkflowRunView() {
  const { id = '', runId = '' } = useParams();
  return <WorkflowRunContent key={`${id}/${runId}`} id={id} runId={runId} />;
}

function WorkflowRunContent({ id, runId }: { id: string; runId: string }) {
  const { limitedUntil } = useM4Operations();
  const { client, generation } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const location = useLocation();
  const back = useBack(`/workflows/${id}?tab=runs`);
  const [since, setSince] = useState(() => Date.now());
  const [expired, setExpired] = useState(0);
  const paused = expired === since;
  const cache = useQueryClient();
  const deadline = useRef(since + OBSERVATION_MS);
  const query = useQuery({
    queryKey: ['m4', generation, 'workflow-run', id, runId],
    queryFn: () =>
      withCredential(client!, () => {
        if (Date.now() >= Math.max(deadline.current, since + OBSERVATION_MS)) {
          const recorded = cache.getQueryData<WorkflowRunDetail>([
            'm4',
            generation,
            'workflow-run',
            id,
            runId,
          ]);
          if (recorded) return Promise.resolve(recorded);
          throw new Error('Observation window ended. Refresh to resume.');
        }
        return client!.workflowRun(id, runId);
      }),
    enabled: !paused && !rejected,
    refetchInterval: (state) =>
      paused ||
      Date.now() >= since + OBSERVATION_MS ||
      (state.state.data &&
        ['Succeeded', 'Failed', 'Canceled', 'Rejected'].includes(state.state.data.phase))
        ? false
        : observationDelay(state.state.error, since),
    refetchIntervalInBackground: false,
    refetchOnReconnect: false,
  });
  const definition = useQuery({
    queryKey: [
      'm4',
      generation,
      'workflow-run-current-definition',
      id,
      query.data?.workflow_source_uid,
    ],
    enabled: !!query.data && !query.error && !rejected,
    retry: false,
    queryFn: () =>
      withCredential(client!, async () => {
        const sourceUID = query.data!.workflow_source_uid;
        if (!sourceUID) return null;
        async function readCurrent(currentID: string) {
          const current = await client!.workflow(currentID);
          if (current.source_uid !== sourceUID)
            throw new Error('The current definition does not match this recorded workflow.');
          return current;
        }
        try {
          return await readCurrent(id);
        } catch (error) {
          if (!(error instanceof APIError) || error.status !== 404) throw error;
        }
        // List only active, authorized definitions; never infer identity from a name.
        for (let offset = 0; offset < CURRENT_DEFINITION_LOOKUP_LIMIT; offset += PAGE_SIZE) {
          const workflows = await client!.workflows({ offset });
          const current = workflows
            .slice(0, PAGE_SIZE)
            .find((workflow) => workflow.source_uid === sourceUID);
          if (current) return readCurrent(String(current.id));
          if (workflows.length < PAGE_SIZE) return null;
        }
        return null;
      }),
  });
  const currentDefinition = definition.error ? undefined : definition.data;
  useEffect(() => {
    const timer = setTimeout(
      () => setExpired(since),
      Math.max(0, since + OBSERVATION_MS - Date.now()),
    );
    return () => clearTimeout(timer);
  }, [since]);
  function refresh() {
    deadline.current = Date.now() + OBSERVATION_MS;
    setSince(Date.now());
    return query.refetch();
  }
  const readError = rejected ?? query.error;
  const unavailable =
    !!rejected || (readError instanceof APIError && [401, 403, 404].includes(readError.status));
  if (query.isPending && !readError) return <Loading />;
  if (unavailable || !query.data)
    return (
      <>
        <Back to={back}>Back to Workflow runs</Back>
        <M4ReadError
          kind="workflow"
          error={readError ?? new Error('Workflow run unavailable')}
          retry={() => void refresh()}
        />
        {query.data && <WorkflowCancelNotice workflowId={id} run={query.data} />}
      </>
    );
  const item = query.data;
  const ended = ['Succeeded', 'Failed', 'Canceled', 'Rejected'].includes(item.phase);
  return (
    <>
      <Back to={back}>Back to Workflow runs</Back>
      <PageHeading
        title={`Workflow run #${item.id}`}
        action={
          <Flex gap="2">
            <Refresh run={refresh} busy={query.isFetching || Boolean(limitedUntil) || !!rejected} />
            <WorkflowCancel
              workflowId={id}
              run={item}
              refresh={refresh}
              disabled={Boolean(limitedUntil) || !!readError || paused}
            />
          </Flex>
        }
      />
      {readError && (
        <M4Notice title="Last observed workflow execution">
          The latest read failed. The snapshot below is not confirmed current; refresh successfully
          before requesting cancellation.
        </M4Notice>
      )}
      <div className="run-meta">
        <PhaseBadge phase={item.phase} />
        {currentDefinition && (
          <Link
            className="record-link"
            to={`/workflows/${currentDefinition.id}`}
            state={forwardState(location)}
          >
            {currentDefinition.name}
          </Link>
        )}
        <Text size="1" color="gray">
          {item.trigger} trigger at <M4Time value={item.created_at} /> UTC
        </Text>
      </div>
      {currentDefinition && String(currentDefinition.id) !== String(item.revision_id) && (
        <M4Notice title="This run uses a different revision">
          This run is pinned to revision #{item.revision_id}. The current definition is revision{' '}
          <Link
            className="record-link"
            to={`/workflows/${currentDefinition.id}`}
            state={forwardState(location)}
          >
            #{currentDefinition.id}
          </Link>
          .
        </M4Notice>
      )}
      {item.phase === 'Rejected' && (
        <M4Notice
          title={item.trigger === 'Schedule' ? 'Scheduled start skipped' : 'Execution rejected'}
        >
          {item.reason === 'concurrency_forbid'
            ? 'Another execution was still open. No steps were started, and this occurrence will not be queued for later.'
            : item.reason || 'The execution ended before starting steps.'}
        </M4Notice>
      )}
      <WorkflowCancelNotice workflowId={id} run={item} />
      {item.phase === 'ExecutionUnknown' && (
        <M4Notice title="Execution outcome unknown">
          A step's execution has not been confirmed stopped. New steps and retries remain blocked;
          request cancellation or keep observing. This is not a completed workflow outcome.
        </M4Notice>
      )}
      {item.phase === 'Pending' && item.reason === 'ConcurrencyBlocked' && (
        <M4Notice title="Waiting for admission">
          This request is accepted and waiting for another execution to stop. No new steps have
          started for this request. Keep observing this request before starting another.
        </M4Notice>
      )}
      {readError && <ErrorState error={readError} retry={() => void refresh()} />}
      {item.phase === 'CancelUnknown' && (
        <M4Notice title="Cancellation outcome unknown">
          A stop has not been confirmed. Automatic observation is bounded; refresh to read the
          observed state.
        </M4Notice>
      )}
      {paused && !ended && (
        <M4Notice title="Observation paused">Refresh to resume checking this run.</M4Notice>
      )}
      <PinnedWorkflowGraph key={String(item.revision_id)} run={item} />
      <WorkflowSnapshot run={item} />
      <Heading as="h2" size="3" weight="medium" mt="5" mb="3">
        Recorded steps
      </Heading>
      {!item.steps.length ? (
        <Empty title="No recorded steps">
          Only created step runs appear here. Refresh to read recorded executions.
        </Empty>
      ) : (
        <Table.Root className="ledger-table m4-records" size="1">
          <Table.Header>
            <Table.Row>
              {['Step run', 'Task', 'Phase', 'Attempt', 'Updated (UTC)'].map((h) => (
                <Table.ColumnHeaderCell key={h}>{h}</Table.ColumnHeaderCell>
              ))}
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {item.steps.map((step) => (
              <Table.Row key={String(step.id)}>
                <Table.RowHeaderCell data-label="Step run">
                  <Link
                    className="record-link"
                    to={`/runs/${step.id}`}
                    state={forwardState(location)}
                  >
                    #{step.id}
                  </Link>
                </Table.RowHeaderCell>
                <Table.Cell data-label="Task">{step.task_name || 'Not recorded'}</Table.Cell>
                <Table.Cell data-label="Phase">
                  <PhaseBadge phase={step.phase} />
                </Table.Cell>
                <Table.Cell data-label="Attempt">{step.attempt}</Table.Cell>
                <Table.Cell data-label="Updated (UTC)">
                  <M4Time value={step.updated_at} />
                </Table.Cell>
              </Table.Row>
            ))}
          </Table.Body>
        </Table.Root>
      )}
      {(definition.error || (!definition.isPending && !currentDefinition)) && (
        <Text as="p" size="2" color="gray" mt="2" mb="3">
          The current definition is unavailable. This run’s recorded details remain available.{' '}
          <Link className="record-link" to="/workflows" state={forwardState(location)}>
            Browse workflows
          </Link>
        </Text>
      )}
      <details
        className="m4-detail-disclosure m4-section"
        aria-label="Current workflow definition context"
      >
        <summary>Run identity and observation</summary>
        <div className="definition m4-configuration">
          <Facts
            rows={[
              ['Workflow', currentDefinition?.name ?? 'Not currently readable'],
              ['Source UID', <code>{item.workflow_source_uid || 'Not recorded'}</code>],
              ['Pinned revision', `#${item.revision_id}`],
              [
                'Current definition',
                currentDefinition ? (
                  <Link
                    className="record-link"
                    to={`/workflows/${currentDefinition.id}`}
                    state={forwardState(location)}
                  >
                    Revision #{currentDefinition.id}
                  </Link>
                ) : definition.isPending ? (
                  'Checking current context…'
                ) : (
                  'Not currently readable'
                ),
              ],
              ['Trigger', item.trigger],
              ['Actor', item.actor || 'Not recorded'],
              ...(item.scheduled_for
                ? [
                    ['Planned trigger (UTC)', <M4Time value={item.scheduled_for} />] as [
                      string,
                      React.ReactNode,
                    ],
                  ]
                : []),
              ['Created (UTC)', <M4Time value={item.created_at} />],
              ['Updated (UTC)', <M4Time value={item.updated_at} />],
              [
                'Last observed (UTC)',
                <M4Time value={new Date(query.dataUpdatedAt).toISOString()} />,
              ],
            ]}
          />
          <Text as="p" size="1" color="gray">
            Current-definition lookup checks at most {CURRENT_DEFINITION_LOOKUP_LIMIT} workflows
            available to your account.
          </Text>
          <Raw value={item} label="All recorded fields" />
        </div>
      </details>
    </>
  );
}
