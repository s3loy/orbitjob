import { SourceControlPanel } from '../SourceControlPanel';
import { AttemptLogs } from '../AttemptLogs';
import { ExecutionLogs } from '../ExecutionLogs';
import { Button, Flex, Heading, Select, Table, Tabs, Text, TextField } from '@radix-ui/themes';
import { useQuery } from '@tanstack/react-query';
import { Search } from 'lucide-react';
import { Link, useLocation, useParams, useSearchParams } from 'react-router-dom';
import { useSession } from '../session';
import { forwardState, returnState, useBack } from '../monitoring-state';
import { Back, Facts, Raw, Refresh, State } from '../monitoring-ui';
import { Empty, ErrorState, Loading, PageHeading, Paging } from '../components';
import { ContainerEntries, M4Notice, M4ReadError, M4Time } from '../m4-ui';
import { M4Action } from '../m4-actions';
import { useM4Operations } from '../m4-state';
import { pageLimit, pageLimitParams, pageOffset, pageParams } from '../list-state';
import type { FunctionDefinition, FunctionRun } from '../api/m4';
import { HistoryWindow } from '../HistoryWindow';
import { useAdministrationAuthenticationError } from '../administration-state';
import { withCredential } from '../credential-requests';
import { useObservationWindow } from '../observation-window';
import { observationDelay } from '../api/transport';
import {
  collectionSearch,
  collectionSearchParams,
  queryWindow,
  historyOffset,
  historyPageParams,
} from '../api/query-window';

export function Functions() {
  const { limitedUntil } = useM4Operations();
  const { client, generation } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const location = useLocation();
  const [params, setParams] = useSearchParams();
  const limit = pageLimit(params);
  const offset = pageOffset(params);
  const { search, error: searchError } = collectionSearch(params);
  const query = useQuery({
    queryKey: ['m4', generation, 'functions', offset, limit, search],
    enabled: !searchError && !rejected,
    queryFn: () => {
      if (searchError) throw new Error(searchError);
      return withCredential(client!, () => client!.functions({ offset, search, limit }));
    },
  });
  const items = query.data ?? [];
  return (
    <>
      <PageHeading
        title="Functions"
        action={
          <Flex gap="3">
            <Refresh
              run={() => query.refetch()}
              busy={query.isFetching || Boolean(limitedUntil) || Boolean(searchError) || !!rejected}
            />
            {rejected ? (
              <Button size="2" disabled>
                Create a function
              </Button>
            ) : (
              <Button asChild size="2">
                <Link to="/functions/new" state={forwardState(location)}>
                  Create a function
                </Link>
              </Button>
            )}
          </Flex>
        }
      />
      <Flex className="list-tools" align="center" gap="3" wrap="wrap">
        <TextField.Root
          type="search"
          aria-label="Search functions"
          placeholder="Search name or description…"
          maxLength={128}
          disabled={!!rejected}
          value={search}
          onChange={(e) =>
            setParams(collectionSearchParams(params, e.target.value), { replace: true })
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
        <ErrorState error={new Error(searchError)} />
      ) : query.isPending ? (
        <Loading />
      ) : query.error ? (
        <M4ReadError kind="function" error={query.error} retry={() => void query.refetch()} />
      ) : (
        <>
          {!items.length ? (
            <Empty
              title={
                search
                  ? 'No matching functions'
                  : offset
                    ? 'No records on this page'
                    : 'No functions yet'
              }
            >
              {search ? (
                'Try a different name or description.'
              ) : offset ? (
                'Go to the previous page.'
              ) : (
                <>
                  <Text as="p" size="2" color="gray" mb="3">
                    Save a container configuration to invoke it whenever needed.
                  </Text>
                  <Button asChild size="2">
                    <Link to="/functions/new" state={forwardState(location)}>
                      Create your first function
                    </Link>
                  </Button>
                </>
              )}
            </Empty>
          ) : (
            <Table.Root className="ledger-table m4-collection function-collection" size="1">
              <Table.Header>
                <Table.Row>
                  {['Function', 'State', 'Container image', 'Group'].map((h, i) => (
                    <Table.ColumnHeaderCell key={h} className={i > 1 ? 'hide-mobile' : ''}>
                      {h}
                    </Table.ColumnHeaderCell>
                  ))}
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {items.map((f) => (
                  <Table.Row key={String(f.id)}>
                    <Table.RowHeaderCell>
                      <Link
                        className="record-link"
                        to={`/functions/${f.id}`}
                        state={forwardState(location)}
                      >
                        {f.name}
                      </Link>
                    </Table.RowHeaderCell>
                    <Table.Cell>
                      <State value={f.status} />
                    </Table.Cell>
                    <Table.Cell className="hide-mobile">
                      <code>{f.image}</code>
                    </Table.Cell>
                    <Table.Cell className="hide-mobile">
                      {f.resource_group_id ? <code>{f.resource_group_id}</code> : 'Ungrouped'}
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

function FunctionHistory({ id }: { id: string }) {
  const { limitedUntil } = useM4Operations();
  const { client, generation } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const location = useLocation();
  const [params, setParams] = useSearchParams();
  const candidate = Number(params.get('limit') ?? '20');
  const limit = [20, 50, 100].includes(candidate) ? candidate : 20;
  const offset = historyOffset(params, limit);
  const window = queryWindow(params);
  const query = useQuery({
    queryKey: [
      'm4',
      generation,
      'function-runs',
      id,
      limit,
      offset,
      window.created_from,
      window.created_before,
    ],
    enabled: !window.error && !rejected,
    queryFn: () => {
      if (window.error) throw new Error(window.error);
      return withCredential(client!, () =>
        client!.functionRuns(id, {
          limit,
          offset,
          created_from: window.created_from,
          created_before: window.created_before,
        }),
      );
    },
  });
  if (rejected) return <ErrorState error={rejected} />;
  return (
    <>
      <Flex className="list-tools" gap="3" align="center" wrap="wrap" justify="between">
        <Heading as="h2" size="3" weight="medium">
          Recorded invocations
        </Heading>
        <Flex gap="3" align="center" wrap="wrap">
          <Text as="label" size="1" color="gray" htmlFor="function-history-limit">
            Rows per page
          </Text>
          <Select.Root
            disabled={Boolean(limitedUntil)}
            size="1"
            value={String(limit)}
            onValueChange={(v) => {
              const next = new URLSearchParams(params);
              next.set('limit', v);
              next.delete('offset');
              next.delete('page');
              setParams(next, { state: location.state });
            }}
          >
            <Select.Trigger id="function-history-limit" aria-label="History limit" />
            <Select.Content>
              {[20, 50, 100].map((n) => (
                <Select.Item key={n} value={String(n)}>
                  {n}
                </Select.Item>
              ))}
            </Select.Content>
          </Select.Root>
          <Refresh
            label="Refresh invocations"
            run={() => query.refetch()}
            busy={query.isFetching || Boolean(limitedUntil) || Boolean(window.error)}
          />
        </Flex>
      </Flex>
      <HistoryWindow
        params={params}
        change={(next) => setParams(next, { state: location.state })}
        timestamp="Invocation time"
        disabled={Boolean(limitedUntil)}
      />
      {window.error ? (
        <ErrorState error={new Error(window.error)} />
      ) : query.isPending ? (
        <Loading />
      ) : query.error ? (
        <ErrorState error={query.error} retry={() => void query.refetch()} />
      ) : !query.data.length ? (
        <Empty title="No recorded invocations">
          Invoke this function to start a run. Completed invocations appear here; adjust the time
          range to find older results.
        </Empty>
      ) : (
        <Table.Root className="ledger-table m4-records function-invocation-records" size="1">
          <Table.Header>
            <Table.Row>
              {['Invocation', 'Outcome', 'Duration', 'Triggered (UTC)'].map((h) => (
                <Table.ColumnHeaderCell key={h}>{h}</Table.ColumnHeaderCell>
              ))}
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {query.data.map((r: FunctionRun) => (
              <Table.Row key={String(r.id)}>
                <Table.RowHeaderCell data-label="Invocation">
                  <Link
                    className="record-link"
                    title={r.run_id}
                    to={`/functions/${id}/runs/${encodeURIComponent(r.run_id)}`}
                    state={forwardState(location)}
                  >
                    <code>
                      {r.run_id.slice(0, 8)}…{r.run_id.slice(-6)}
                    </code>
                  </Link>
                </Table.RowHeaderCell>
                <Table.Cell data-label="Outcome">
                  <State value={r.status} />
                </Table.Cell>
                <Table.Cell data-label="Duration">
                  {r.duration_ms == null ? 'Not recorded' : `${r.duration_ms.toLocaleString()} ms`}
                </Table.Cell>
                <Table.Cell data-label="Triggered (UTC)">
                  <M4Time value={r.triggered_at} />
                </Table.Cell>
              </Table.Row>
            ))}
          </Table.Body>
        </Table.Root>
      )}
      {!window.error && query.data && (
        <Flex className="paging" justify="between" align="center" gap="3" wrap="wrap">
          <Text size="1" color="gray">
            {query.data.length
              ? `${offset + 1}–${offset + query.data.length} matching outcomes`
              : 'No outcomes on this page'}
          </Text>
          <Flex gap="3">
            <Button
              variant="soft"
              size="1"
              disabled={!offset || query.isFetching || Boolean(limitedUntil)}
              onClick={() =>
                setParams(historyPageParams(params, Math.max(0, offset - limit)), {
                  state: location.state,
                })
              }
            >
              Previous
            </Button>
            <Button
              variant="soft"
              size="1"
              disabled={query.data.length < limit || query.isFetching || Boolean(limitedUntil)}
              onClick={() =>
                setParams(historyPageParams(params, offset + limit), { state: location.state })
              }
            >
              Next
            </Button>
          </Flex>
        </Flex>
      )}
      {Boolean(query.data?.length) && (
        <Text as="p" size="1" color="gray" mt="4">
          Only finished invocations appear here. Duration is for the latest recorded attempt.
        </Text>
      )}
    </>
  );
}

function FunctionConfig({ item }: { item: FunctionDefinition }) {
  return (
    <div className="definition m4-configuration">
      <Heading as="h2" size="3" weight="medium">
        Execution configuration
      </Heading>
      <Facts
        rows={[
          ...(item.description ? [['Description', item.description] as [string, string]] : []),
          ['Image', <code>{item.image}</code>],
          ['Command', <ContainerEntries entries={item.command} empty="Image default" />],
          ['Arguments', <ContainerEntries entries={item.args} />],
          ['Timeout', `${item.timeout_seconds} seconds`],
          ['Retry limit', item.retry_limit],
          ['Group', item.resource_group_id || 'Ungrouped'],
        ]}
      />
      <details className="m4-detail-disclosure">
        <summary>Version and history retention</summary>
        <Facts
          rows={[
            ['Definition version', item.version],
            ['Successful invocations to retain', item.history_success],
            ['Failed invocations to retain', item.history_failed],
          ]}
        />
      </details>
      <Raw value={item} label="Definition fields" />
    </div>
  );
}

export function FunctionView() {
  const { limitedUntil } = useM4Operations();
  const { id = '' } = useParams();
  const { client, generation } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const observation = useObservationWindow();
  const location = useLocation();
  const back = useBack('/functions');
  const [params, setParams] = useSearchParams();
  const tab = params.get('tab') === 'configuration' ? 'configuration' : 'invocations';
  const query = useQuery({
    queryKey: ['m4', generation, 'function', id],
    enabled: !rejected,
    queryFn: () => withCredential(client!, () => client!.functionDefinition(id)),
    refetchInterval: (state) =>
      rejected ||
      observation.paused ||
      state.state.data?.ready ||
      state.state.data?.status === 'paused'
        ? false
        : observationDelay(state.state.error, observation.since),
    refetchIntervalInBackground: false,
    refetchOnReconnect: false,
  });
  function refresh() {
    observation.restart();
    return query.refetch();
  }
  if (rejected)
    return (
      <>
        <Back to={back}>Back to Functions</Back>
        <ErrorState error={rejected} />
      </>
    );
  if (query.isPending) return <Loading />;
  const item = query.error ? undefined : query.data;
  const versionDiffers = Boolean(
    item && item.active_revision_version != null && item.active_revision_version !== item.version,
  );
  return (
    <>
      <Back to={back}>Back to Functions</Back>
      <PageHeading
        title={item?.name ?? `Function #${id}`}
        action={
          <Flex gap="2" wrap="wrap">
            <Refresh
              label="Refresh definition"
              run={refresh}
              busy={query.isFetching || Boolean(limitedUntil)}
            />
            {item && (
              <>
                <Button asChild size="2" variant="outline">
                  <Link to={`/functions/${id}/edit`} state={forwardState(location)}>
                    Edit function
                  </Link>
                </Button>
                <M4Action
                  kind="function"
                  item={item}
                  disabled={Boolean(limitedUntil) || item.ready === false}
                />
              </>
            )}
          </Flex>
        }
      />
      {query.error && (
        <M4ReadError kind="function" error={query.error} retry={() => void refresh()} />
      )}
      {item && (
        <div className="run-meta">
          <State value={item.status} />
          {item.resource_group_id && (
            <Text size="1" color="gray">
              Group {item.resource_group_id}
            </Text>
          )}
        </div>
      )}
      {item?.status === 'paused' && (
        <M4Notice title="Function paused">New invocations are disabled.</M4Notice>
      )}
      {item && <SourceControlPanel kind="functions" id={String(item.id)} />}
      {item?.status === 'active' && (
        <section
          className={`m4-readiness ${item.ready && !versionDiffers ? 'is-ready' : 'needs-attention'}`}
          aria-label="Function execution readiness"
        >
          <Text as="p" size="2" weight="medium">
            {versionDiffers
              ? 'Execution version differs'
              : item.ready
                ? 'Ready to invoke'
                : observation.paused
                  ? 'Readiness check paused'
                  : 'Preparing this version'}
          </Text>
          {versionDiffers && (
            <Text as="p" size="1" color="gray" mt="2">
              The active execution version differs from this definition. Refresh to check progress.
            </Text>
          )}
          {!item.ready && (
            <Text as="p" size="1" color="gray" mt="2">
              {observation.paused
                ? 'Automatic checks stopped after two minutes. Refresh definition to check again.'
                : 'Invoke becomes available when this version is ready.'}
            </Text>
          )}
          <details className="m4-detail-disclosure" open={!item.ready || versionDiffers}>
            <summary>Version and observation</summary>
            <Facts
              rows={[
                ['Definition version', item.version],
                ['Active execution version', item.active_revision_version ?? 'Not observed'],
                [
                  'Last checked (UTC)',
                  <M4Time value={new Date(query.dataUpdatedAt).toISOString()} />,
                ],
              ]}
            />
          </details>
        </section>
      )}
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
          <Tabs.Trigger value="invocations">Invocations</Tabs.Trigger>
          <Tabs.Trigger value="configuration">Configuration</Tabs.Trigger>
        </Tabs.List>
        <Tabs.Content value="invocations" className="tab-content">
          <FunctionHistory id={id} />
        </Tabs.Content>
        <Tabs.Content value="configuration" className="tab-content">
          {item ? (
            <FunctionConfig item={item} />
          ) : (
            <Empty title="Configuration unavailable">
              You can still check the Invocations tab for results available to your account.
            </Empty>
          )}
        </Tabs.Content>
      </Tabs.Root>
    </>
  );
}

export function FunctionRunView() {
  const { limitedUntil } = useM4Operations();
  const { id = '', runId = '' } = useParams();
  const { client, generation } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const location = useLocation();
  const back = useBack(`/functions/${id}`);
  const query = useQuery({
    queryKey: ['m4', generation, 'function-run', id, runId],
    enabled: !rejected,
    queryFn: () => withCredential(client!, () => client!.functionRun(id, runId)),
  });
  if (rejected)
    return (
      <>
        <Back to={back}>Back to Invocations</Back>
        <ErrorState error={rejected} />
      </>
    );
  if (query.isPending) return <Loading />;
  if (query.error)
    return (
      <>
        <Back to={back}>Back to Invocations</Back>
        <M4ReadError kind="function" error={query.error} retry={() => void query.refetch()} />
      </>
    );
  const item = query.data;
  return (
    <>
      <Back to={back}>Back to Invocations</Back>
      <PageHeading
        title={`Invocation ${item.run_id.slice(0, 8)}…${item.run_id.slice(-6)}`}
        action={
          <Refresh run={() => query.refetch()} busy={query.isFetching || Boolean(limitedUntil)} />
        }
      />
      <div className="run-meta">
        <State value={item.status} />
      </div>
      <div className="definition m4-configuration">
        <Heading as="h2" size="3" weight="medium">
          Execution outcome
        </Heading>
        <Facts
          rows={[
            [
              'Function',
              <Link
                className="record-link"
                to={`/functions/${item.function_id}`}
                state={
                  back.split('?')[0] === `/functions/${item.function_id}`
                    ? returnState(location.state, back)
                    : forwardState(location)
                }
              >
                Function #{item.function_id}
              </Link>,
            ],
            ['Triggered (UTC)', <M4Time value={item.triggered_at} />],
            ['Latest attempt started (UTC)', <M4Time value={item.started_at} />],
            ['Latest attempt finished (UTC)', <M4Time value={item.finished_at} />],
            [
              'Latest attempt duration',
              item.duration_ms == null ? 'Not recorded' : `${item.duration_ms.toLocaleString()} ms`,
            ],
          ]}
        />
        <Raw value={item} label="Invocation identity and recorded fields" />
        <ExecutionLogs
          parent={{ kind: 'function', id: String(item.function_id), run: item.run_id }}
        />
        <AttemptLogs
          key={item.run_id}
          parent={{ kind: 'function', id, run: runId }}
          attempts={item.attempts}
          lastAttempt={item.last_attempt}
        />
      </div>
    </>
  );
}
