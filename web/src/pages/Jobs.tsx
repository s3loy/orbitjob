import '../job-run-workspace.css';
import { when } from '../format';
import { Box, Button, Flex, Table, Tabs, Text, TextField, Tooltip } from '@radix-ui/themes';
import { RefreshCw, Search } from 'lucide-react';
import { useLayoutEffect, useRef } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useLocation, useParams, useSearchParams } from 'react-router-dom';
import { forwardState, useBack } from '../monitoring-state';
import { Back } from '../monitoring-ui';
import { workloadOrigin } from '../workload-origin';
import { pageLimit, pageLimitParams, pageOffset, pageParams } from '../list-state';
import { useSession } from '../session';
import { Empty, ErrorState, Loading, PageHeading, Paging, RunTable } from '../components';
import { TriggerAction } from './TriggerAction';
import { useAdministrationAuthenticationError } from '../administration-state';
import { withCredential } from '../credential-requests';
import { SourceControlPanel } from '../SourceControlPanel';

const concurrencyLabels = new Map([
  ['Allow', 'Allow overlapping runs'],
  ['Forbid', 'Prevent overlapping runs'],
  ['Replace', 'Stop earlier runs for scheduled starts'],
]);
const missedStartLabels = new Map([
  ['Skip', 'Skip missed starts'],
  ['FireOnce', 'Run once after a missed start'],
  ['CatchUpBounded', 'Catch up within the limit'],
]);

export function Jobs() {
  const location = useLocation();
  const { client, generation } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const [params, setParams] = useSearchParams();
  const pendingParams = useRef(params);
  useLayoutEffect(() => {
    pendingParams.current = params;
  }, [params]);
  const limit = pageLimit(params);
  const offset = pageOffset(params);
  const search = params.get('search') ?? '';
  const namespace = params.get('namespace') || '';
  function filter(name: string, value: string) {
    // Router updates can share one render; compose input changes until the URL commits.
    const next = new URLSearchParams(pendingParams.current);
    next.delete('offset');
    next.delete('page');
    if (value) next.set(name, value);
    else next.delete(name);
    pendingParams.current = next;
    setParams(next, { replace: true });
  }
  const query = useQuery({
    queryKey: ['jobs', generation, offset, limit, search, namespace],
    queryFn: () =>
      withCredential(client!, () => client!.jobs(offset, search, { namespace, limit })),
    enabled: !rejected,
  });
  const items = query.data ?? [];
  if (rejected)
    return (
      <>
        <PageHeading title="Jobs" />
        <ErrorState error={rejected} />
      </>
    );
  return (
    <>
      <PageHeading
        title="Jobs"
        action={
          <Flex gap="3" wrap="wrap">
            <Button
              size="1"
              variant="outline"
              color="gray"
              disabled={query.isFetching}
              onClick={() => void query.refetch()}
            >
              <RefreshCw size={13} />
              Refresh
            </Button>
            <Button asChild size="1">
              <Link to="/manifests/job" state={forwardState(location)}>
                Create a job
              </Link>
            </Button>
          </Flex>
        }
      />
      <Flex className="list-tools" gap="3" align="center" wrap="wrap">
        <TextField.Root
          size="2"
          aria-label="Search jobs"
          maxLength={128}
          placeholder="Search jobs…"
          value={search}
          onChange={(event) => filter('search', event.target.value)}
        >
          <TextField.Slot>
            <Search size={14} aria-hidden />
          </TextField.Slot>
        </TextField.Root>
        <TextField.Root
          size="2"
          aria-label="Namespace"
          placeholder="All namespaces"
          value={namespace}
          onChange={(event) => filter('namespace', event.target.value)}
        />
      </Flex>
      {query.isPending ? (
        <Loading />
      ) : query.error ? (
        <ErrorState error={query.error} retry={() => void query.refetch()} />
      ) : !items.length ? (
        <Empty title={search || namespace ? 'No matching jobs' : 'No jobs yet'}>
          {search || namespace ? (
            <>
              <Text as="p" size="2" color="gray">
                Try another name or namespace, or clear the filters.
              </Text>
              <Button
                size="1"
                variant="outline"
                mt="3"
                onClick={() => {
                  const next = new URLSearchParams(pendingParams.current);
                  ['search', 'namespace', 'offset', 'page'].forEach((key) => next.delete(key));
                  pendingParams.current = next;
                  setParams(next, { replace: true, state: location.state });
                }}
              >
                Clear filters
              </Button>
            </>
          ) : (
            <>
              <Text as="p" size="2" color="gray" mb="3">
                Create a job to run a container on a schedule or when needed.
              </Text>
              <Button asChild size="2">
                <Link to="/manifests/job" state={forwardState(location)}>
                  Create your first job
                </Link>
              </Button>
            </>
          )}
        </Empty>
      ) : (
        <Table.Root className="ledger-table jobs-table" size="1">
          <Table.Header>
            <Table.Row>
              <Table.ColumnHeaderCell className="job-name-col">Job</Table.ColumnHeaderCell>
              <Table.ColumnHeaderCell className="hide-mobile">Schedule</Table.ColumnHeaderCell>
              <Table.ColumnHeaderCell className="job-mode-col">Run mode</Table.ColumnHeaderCell>
              <Table.ColumnHeaderCell className="hide-tablet">
                Overlapping runs
              </Table.ColumnHeaderCell>
              <Table.ColumnHeaderCell className="job-action-col">Action</Table.ColumnHeaderCell>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {items.map((job) => {
              const origin = workloadOrigin(job);
              return (
                <Table.Row key={String(job.id)}>
                  <Table.RowHeaderCell>
                    <Link
                      className="record-link"
                      to={origin.to || `/jobs/${job.id}`}
                      state={forwardState(location)}
                    >
                      {job.name}
                    </Link>
                    <Text as="p" size="1" color="gray">
                      {job.namespace}
                    </Text>
                  </Table.RowHeaderCell>
                  <Table.Cell className="hide-mobile">
                    <Tooltip content={job.schedule || 'No automatic schedule'}>
                      <Text size="2">{job.schedule_summary || job.schedule || 'Manual only'}</Text>
                    </Tooltip>
                  </Table.Cell>
                  <Table.Cell>
                    <span
                      className={`phase-label job-mode ${job.suspend ? 'is-paused' : 'neutral'}`}
                    >
                      <span className="status-dot" />
                      {origin.actionable
                        ? job.suspend
                          ? 'Paused'
                          : job.schedule
                            ? 'Scheduled'
                            : 'Manual'
                        : origin.label}
                    </span>
                  </Table.Cell>
                  <Table.Cell className="hide-tablet">
                    <Text size="2">
                      {job.concurrency_policy === 'Forbid'
                        ? 'Prevent overlap'
                        : job.concurrency_policy === 'Allow'
                          ? 'Allow overlap'
                          : job.concurrency_policy === 'Replace'
                            ? 'Replace previous'
                            : job.concurrency_policy}
                    </Text>
                  </Table.Cell>
                  <Table.Cell>
                    {origin.actionable ? (
                      <TriggerAction job={job} compact />
                    ) : origin.to ? (
                      <Link className="record-link" to={origin.to} state={forwardState(location)}>
                        Open {origin.label}
                      </Link>
                    ) : (
                      <Text size="1" color="gray">
                        Read only
                      </Text>
                    )}
                  </Table.Cell>
                </Table.Row>
              );
            })}
          </Table.Body>
        </Table.Root>
      )}
      {query.data && (
        <Paging
          limit={limit}
          setLimit={(n) => setParams(pageLimitParams(params, n), { state: location.state })}
          offset={offset}
          count={query.data.length}
          setOffset={(value) => {
            const next = pageParams(params, value);

            setParams(next, { state: location.state });
          }}
        />
      )}
    </>
  );
}

export function JobView() {
  const location = useLocation();
  const back = useBack('/jobs');
  const { id = '' } = useParams();
  const { client, generation } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const [params, setParams] = useSearchParams();
  const limit = pageLimit(params);
  const offset = pageOffset(params);
  const job = useQuery({
    queryKey: ['job', generation, id],
    queryFn: () => withCredential(client!, () => client!.job(id)),
    enabled: !rejected,
  });
  const support = useQuery({
    queryKey: ['correlation-support', generation],
    queryFn: () => withCredential(client!, () => client!.supportsCorrelation()),
    enabled: !rejected,
  });
  const runs = useQuery({
    queryKey: ['job-runs', generation, job.data?.source_uid, offset, limit],
    enabled: !rejected && Boolean(job.data) && support.data === true,
    queryFn: () =>
      withCredential(client!, () =>
        client!.runs({ source_uid: job.data!.source_uid, offset, limit }),
      ),
  });
  if (rejected)
    return (
      <>
        <Back to={back}>Back to Jobs</Back>
        <PageHeading title="Job unavailable" />
        <ErrorState error={rejected} />
      </>
    );
  if (job.isPending) return <Loading />;
  if (job.error) return <ErrorState error={job.error} retry={() => void job.refetch()} />;
  const item = job.data;
  const origin = workloadOrigin(item);
  return (
    <>
      <Back to={back}>{back.startsWith('/runs/') ? 'Back to Run' : 'Back to Jobs'}</Back>
      <PageHeading
        title={item.name}
        action={
          <Flex gap="3" wrap="wrap">
            {origin.actionable ? (
              <>
                <Button size="1" variant="outline" color="gray" asChild>
                  <Link
                    to={`/definitions/jobs?${new URLSearchParams({ namespace: item.namespace, name: item.name })}`}
                    state={forwardState(location)}
                  >
                    Edit definition
                  </Link>
                </Button>
                <TriggerAction job={item} />
              </>
            ) : origin.to ? (
              <Button size="1" variant="outline" color="gray" asChild>
                <Link to={origin.to} state={forwardState(location)}>
                  Open {origin.label}
                </Link>
              </Button>
            ) : null}
          </Flex>
        }
      />
      {!origin.actionable && (
        <div className="operation-notice">
          This execution definition comes from {origin.label}. Use its own page to configure and run
          it.
        </div>
      )}
      {item.suspend && origin.actionable && (
        <div className="operation-notice">
          This definition is suspended. Edit the job to resume it before triggering.
        </div>
      )}
      {origin.actionable && <SourceControlPanel kind="jobs" id={String(item.id)} />}
      <Tabs.Root
        value={params.get('tab') === 'definition' ? 'definition' : 'runs'}
        onValueChange={(value) => {
          const next = new URLSearchParams(params);
          if (value === 'definition') next.set('tab', value);
          else next.delete('tab');
          setParams(next, { state: location.state });
        }}
        className="job-tabs"
      >
        <Tabs.List size="1">
          <Tabs.Trigger value="runs">Runs</Tabs.Trigger>
          <Tabs.Trigger value="definition">Definition</Tabs.Trigger>
        </Tabs.List>
        <Tabs.Content value="runs" className="tab-content">
          <Flex justify="end" mb="3">
            <Button
              variant="ghost"
              color="gray"
              size="1"
              disabled={support.data !== true || runs.isFetching}
              onClick={() => void runs.refetch()}
            >
              <RefreshCw size={13} />
              Refresh runs
            </Button>
          </Flex>
          {support.error ? (
            <ErrorState error={support.error} retry={() => void support.refetch()} />
          ) : support.data === false ? (
            <Empty title="Per-job history unavailable">
              Open the full run history to find this job’s executions.{' '}
              <Link className="record-link" to="/runs">
                Open Runs
              </Link>
            </Empty>
          ) : runs.isPending ? (
            <Loading />
          ) : runs.error ? (
            <ErrorState error={runs.error} retry={() => void runs.refetch()} />
          ) : (
            <>
              <RunTable
                runs={runs.data.filter((run) => run.source_uid === item.source_uid)}
                jobs={[item]}
                scoped
              />
              <Paging
                limit={limit}
                setLimit={(n) => setParams(pageLimitParams(params, n), { state: location.state })}
                offset={offset}
                count={runs.data.length}
                setOffset={(value) =>
                  setParams(pageParams(params, value), { state: location.state })
                }
              />
            </>
          )}
        </Tabs.Content>
        <Tabs.Content value="definition" className="tab-content">
          <Box className="definition">
            <dl className="definition-list">
              <dt>Namespace</dt>
              <dd>{item.namespace}</dd>
              <dt>Schedule</dt>
              <dd>{item.schedule ? item.schedule_summary || item.schedule : 'Manual only'}</dd>
              <dt>Overlapping runs</dt>
              <dd>
                {concurrencyLabels.get(item.concurrency_policy) ??
                  (item.concurrency_policy || 'Not recorded')}
                <Text as="p" size="1" color="gray">
                  {item.concurrency_policy === 'Forbid'
                    ? 'Scheduled, manual and workflow-task runs cannot overlap. Accepted manual starts wait for earlier runs to finish.'
                    : item.concurrency_policy === 'Replace'
                      ? 'A scheduled start requests cancellation and waits for prior runs to finish. Manual and workflow-task starts leave running work in place.'
                      : null}
                </Text>
              </dd>
              <dt>Missed starts</dt>
              <dd>
                {missedStartLabels.get(item.misfire_policy) ??
                  (item.misfire_policy || 'Not recorded')}
              </dd>
              <dt>Attempt budget</dt>
              <dd>{item.retry_max_attempts} attempts</dd>
              <dt>Timeout</dt>
              <dd>{item.timeout_seconds ? `${item.timeout_seconds} seconds` : 'No deadline'}</dd>
              <dt>Container</dt>
              <dd className="mono">{item.job_template.image}</dd>
              <dt>Command</dt>
              <dd className="mono">
                {item.job_template.command?.length
                  ? JSON.stringify(item.job_template.command)
                  : 'Image default'}
              </dd>
              <dt>Arguments</dt>
              <dd className="mono">
                {item.job_template.args?.length
                  ? JSON.stringify(item.job_template.args)
                  : 'Image default'}
              </dd>
            </dl>
            <details>
              <summary>Definition identity</summary>
              <dl className="definition-list">
                <dt>Revision</dt>
                <dd>#{item.id}</dd>
                <dt>Generation</dt>
                <dd>{item.generation}</dd>
                <dt>Source UID</dt>
                <dd className="mono">{item.source_uid}</dd>
                <dt>Declared by</dt>
                <dd>{item.actor}</dd>
                <dt>Projected</dt>
                <dd>{when(item.created_at)}</dd>
              </dl>
            </details>
          </Box>
        </Tabs.Content>
      </Tabs.Root>
    </>
  );
}
