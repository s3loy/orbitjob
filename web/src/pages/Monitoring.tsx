import { SourceControlPanel } from '../SourceControlPanel';
import { forwardState, returnState, useBack } from '../monitoring-state';
import { Flex, Heading, Select, Table, Tabs, Text, TextField } from '@radix-ui/themes';
import { Search } from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { Link, useLocation, useParams, useSearchParams } from 'react-router-dom';
import { useSession } from '../session';
import { pageLimit, pageLimitParams, pageOffset, pageParams } from '../list-state';
import { PageHeading, Loading, ErrorState, Empty, Paging } from '../components';
import { Back, BudgetView, Facts, Raw, Refresh, ResourceActions, State } from '../monitoring-ui';
import { MonitoringCreate } from './MonitoringCreate';
import { MonitoringReceipts } from '../MonitoringReceipts';
import { MonitoringSourceLink } from '../MonitoringSourceLink';
import { when } from '../format';
import type { Resource } from '../api/monitoring';
import { APIError } from '../api/transport';
import { useAdministrationAuthenticationError } from '../administration-state';
import { withCredential } from '../credential-requests';
import '../monitoring.css';

function windowLabel(type: string, duration: string) {
  return type === 'calendar'
    ? 'Calendar month'
    : type === 'quarterly'
      ? 'Calendar quarter'
      : `${duration} rolling`;
}
export function MonitoringList({ resource }: { resource: Resource | 'slo-alerts' }) {
  const { client, generation } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const location = useLocation();
  const [params, setParams] = useSearchParams();
  const offset = pageOffset(params);
  const limit = pageLimit(params);
  const search = params.get('search') ?? '';
  const supportedStatuses =
    resource === 'checks'
      ? ['active', 'paused']
      : resource === 'slo-alerts'
        ? ['active', 'resolved']
        : [];
  const status = supportedStatuses.includes(params.get('status') ?? '')
    ? params.get('status')!
    : '';
  const filter = { limit, offset, status, ...(resource !== 'slo-alerts' ? { search } : {}) };
  const query = useQuery({
    queryKey: ['monitoring', generation, resource, filter],
    enabled: !rejected,
    queryFn: () =>
      withCredential(client!, async () => {
        if (resource === 'checks')
          return (await client!.checks(filter)).map((i) => ({
            id: i.id,
            name: i.name,
            type: i.check_type.replaceAll('_', ' '),
            state: i.status,
            detail: i.status === 'active' && i.next_run_at ? when(i.next_run_at) : 'Not scheduled',
            extra: i.schedule_type,
            reference: null,
          }));
        if (resource === 'slis')
          return (await client!.slis(filter)).map((i) => ({
            id: i.id,
            name: i.name,
            type: i.sli_type,
            state: '',
            detail: i.source_type,
            extra: i.aggregation,
            reference: null,
          }));
        if (resource === 'slos')
          return (await client!.slos(filter)).map((i) => ({
            id: i.id,
            name: i.name,
            type: `${Number((i.target * 100).toFixed(6))}%`,
            state: i.status,
            detail: windowLabel(i.window_type, i.window_duration),
            extra: '',
            reference: { to: `/slis/${i.sli_id}`, name: `SLI #${i.sli_id}` },
          }));
        return (await client!.alerts(filter)).map((i) => ({
          id: i.id,
          name: `Alert #${i.id}`,
          type: i.alert_type.replaceAll('_', ' '),
          state: i.status,
          detail: when(i.triggered_at),
          extra: `${i.burn_rate}×`,
          reference: { to: `/slos/${i.slo_id}`, name: `SLO #${i.slo_id}` },
        }));
      }),
  });
  const items =
    resource === 'slo-alerts'
      ? (query.data?.filter((item) =>
          `${item.name} ${item.id}`.toLowerCase().includes(search.toLowerCase()),
        ) ?? [])
      : (query.data ?? []);
  function update(key: string, value: string) {
    const next = new URLSearchParams(params);
    if (value) next.set(key, value);
    else next.delete(key);
    if (resource !== 'slo-alerts' || key !== 'search') {
      next.delete('page');
      next.delete('offset');
    }
    setParams(next, { replace: true, state: location.state });
  }
  return (
    <>
      <PageHeading
        title={
          resource === 'checks'
            ? 'Checks'
            : resource === 'slis'
              ? 'Indicators'
              : resource === 'slo-alerts'
                ? 'Alerts'
                : 'Service objectives'
        }
        action={
          <Flex gap="2">
            <Refresh run={() => query.refetch()} busy={query.isFetching || !!rejected} />
            {resource !== 'slo-alerts' && (
              <MonitoringCreate
                resource={resource}
                authorizationError={
                  rejected ??
                  (query.error instanceof APIError && query.error.status === 401
                    ? query.error
                    : undefined)
                }
              />
            )}
          </Flex>
        }
      />
      {resource !== 'slo-alerts' && <MonitoringReceipts resource={resource} />}
      {resource !== 'checks' && (
        <nav className="module-tabs monitoring-tabs" aria-label="SLO sections">
          {[
            ['slos', 'Objectives'],
            ['slis', 'SLIs'],
            ['slo-alerts', 'Alerts'],
          ].map(([path, name]) => (
            <Link key={path} to={`/${path}`} aria-current={resource === path ? 'page' : undefined}>
              {name}
            </Link>
          ))}
        </nav>
      )}
      <Flex className="list-tools" align="center" gap="3" wrap="wrap">
        <TextField.Root
          type="search"
          aria-label={resource === 'slo-alerts' ? 'Filter alerts on this page' : 'Search records'}
          size="2"
          placeholder={resource === 'slo-alerts' ? 'Filter this alert page' : 'Search by name'}
          value={search}
          disabled={!!rejected}
          onChange={(e) => update('search', e.target.value)}
        >
          <TextField.Slot>
            <Search size={14} aria-hidden="true" />
          </TextField.Slot>
        </TextField.Root>
        {supportedStatuses.length > 0 && (
          <Select.Root
            size="2"
            disabled={!!rejected}
            value={status || '__all__'}
            onValueChange={(v) => update('status', v === '__all__' ? '' : v)}
          >
            <Select.Trigger aria-label="Status" />
            <Select.Content>
              <Select.Item value="__all__">All statuses</Select.Item>
              {supportedStatuses.map((s) => (
                <Select.Item key={s} value={s}>
                  {s[0].toUpperCase() + s.slice(1)}
                </Select.Item>
              ))}
            </Select.Content>
          </Select.Root>
        )}
        {resource === 'slo-alerts' && search && query.data && !query.error && !rejected && (
          <Text size="1" color="gray" className="loaded-count">
            {items.length} of {query.data.length} match on this page
          </Text>
        )}
      </Flex>
      {rejected ? (
        <ErrorState error={rejected} />
      ) : query.isPending ? (
        <Loading />
      ) : query.error ? (
        <ErrorState error={query.error} retry={() => void query.refetch()} />
      ) : !items.length ? (
        <Empty
          title={
            search
              ? resource === 'slo-alerts'
                ? 'No matches on this alert page'
                : 'No matching records'
              : 'No records on this page'
          }
        >
          {search
            ? resource === 'slo-alerts'
              ? 'Clear the search or visit another page.'
              : 'Clear or change the search.'
            : status
              ? 'Choose All statuses to clear the filter.'
              : offset
                ? 'Return to an earlier page.'
                : resource === 'slo-alerts'
                  ? 'Alerts appear when an objective exceeds its burn threshold.'
                  : 'Use New to create the first record.'}
        </Empty>
      ) : (
        <Table.Root className="ledger-table monitoring-table" size="1">
          <Table.Header>
            <Table.Row>
              <Table.ColumnHeaderCell className="monitor-name">
                {resource === 'checks'
                  ? 'Check'
                  : resource === 'slos'
                    ? 'Objective'
                    : resource === 'slis'
                      ? 'Indicator'
                      : 'Alert'}
              </Table.ColumnHeaderCell>
              <Table.ColumnHeaderCell>
                {resource === 'checks' ? 'Scheduling' : resource === 'slos' ? 'Target' : 'Type'}
              </Table.ColumnHeaderCell>
              <Table.ColumnHeaderCell>
                {resource === 'checks'
                  ? 'Schedule type'
                  : resource === 'slis'
                    ? 'Aggregation'
                    : 'State'}
              </Table.ColumnHeaderCell>
              <Table.ColumnHeaderCell className="hide-mobile">
                {resource === 'checks'
                  ? 'Next scheduled'
                  : resource === 'slos'
                    ? 'Window'
                    : resource === 'slis'
                      ? 'Source'
                      : 'Triggered'}
              </Table.ColumnHeaderCell>
              {resource === 'slo-alerts' && (
                <Table.ColumnHeaderCell>Burn at trigger</Table.ColumnHeaderCell>
              )}
              {(resource === 'slo-alerts' || resource === 'slos') && (
                <Table.ColumnHeaderCell className="hide-mobile">
                  {resource === 'slo-alerts' ? 'Objective' : 'Indicator'}
                </Table.ColumnHeaderCell>
              )}
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {items.map((i) => (
              <Table.Row key={String(i.id)}>
                <Table.RowHeaderCell>
                  <Link
                    className="record-link"
                    to={`/${resource}/${i.id}`}
                    state={forwardState(location)}
                  >
                    {i.name}
                  </Link>
                </Table.RowHeaderCell>
                <Table.Cell>
                  {resource === 'checks' ? <State value={i.state} /> : i.type}
                </Table.Cell>
                <Table.Cell>
                  {resource === 'checks' ? i.extra : i.state ? <State value={i.state} /> : i.extra}
                </Table.Cell>
                <Table.Cell className="hide-mobile">{i.detail}</Table.Cell>
                {resource === 'slo-alerts' && <Table.Cell>{i.extra}</Table.Cell>}
                {i.reference && (
                  <Table.Cell className="hide-mobile">
                    <Link
                      className="record-link"
                      to={i.reference.to}
                      state={forwardState(location)}
                    >
                      {i.reference.name}
                    </Link>
                  </Table.Cell>
                )}
              </Table.Row>
            ))}
          </Table.Body>
        </Table.Root>
      )}
      {query.data && !query.error && !rejected && (
        <Paging
          limit={limit}
          setLimit={(n) => setParams(pageLimitParams(params, n), { state: location.state })}
          offset={offset}
          count={query.data.length}
          setOffset={(n) => setParams(pageParams(params, n))}
        />
      )}
    </>
  );
}
export function CheckView() {
  const { id = '' } = useParams();
  const { client, generation } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const [params, setParams] = useSearchParams();
  const location = useLocation();
  const back = useBack('/checks');
  const offset = pageOffset(params);
  const limit = pageLimit(params);
  const tab = params.get('tab') === 'configuration' ? 'configuration' : 'results';
  const status = ['pending', 'running', 'success', 'failed'].includes(params.get('status') ?? '')
    ? params.get('status')!
    : '';
  const query = useQuery({
    queryKey: ['monitoring', generation, 'check', id],
    queryFn: () => withCredential(client!, () => client!.check(id)),
    enabled: !rejected,
  });
  const results = useQuery({
    queryKey: ['monitoring', generation, 'check-results', id, limit, offset, status],
    queryFn: () =>
      withCredential(client!, () => client!.checkResults({ check_id: id, limit, offset, status })),
    enabled: !rejected && Boolean(query.data) && tab === 'results',
  });
  if (rejected)
    return (
      <>
        <MonitoringReceipts resource="checks" id={id} />
        <Back to={back}>Back to Checks</Back>
        <ErrorState error={rejected} />
      </>
    );
  if (query.isPending) return <Loading />;
  if (query.error)
    return (
      <>
        <MonitoringReceipts resource="checks" id={id} />
        <Back to={back}>Back to Checks</Back>
        <ErrorState error={query.error} retry={() => void query.refetch()} />
      </>
    );
  const item = query.data;
  return (
    <>
      <MonitoringReceipts resource="checks" id={id} />
      <Back to={back}>Back to Checks</Back>
      <PageHeading
        title={item.name}
        action={
          <Flex align="center" gap="3" wrap="wrap">
            <State value={item.status} />
            <Link
              className="text-link monitoring-next-action"
              to={`/slis?create=1&source_uid=check-${item.id}`}
              state={forwardState(location)}
            >
              Create indicator
            </Link>
            <Refresh
              run={() => {
                void query.refetch();
                if (tab === 'results') void results.refetch();
              }}
            />
            <ResourceActions
              resource="checks"
              placement="primary"
              item={item}
              refresh={() => query.refetch()}
              back={back}
            />
          </Flex>
        }
      />
      <SourceControlPanel kind="checks" id={String(item.id)} />
      <Tabs.Root
        className="job-tabs"
        value={tab}
        onValueChange={(v) => {
          const p = new URLSearchParams(params);
          p.set('tab', v);
          setParams(p, { state: location.state });
        }}
      >
        <Tabs.List size="1">
          <Tabs.Trigger value="results">Results</Tabs.Trigger>
          <Tabs.Trigger value="configuration">Configuration</Tabs.Trigger>
        </Tabs.List>
        <Tabs.Content className="tab-content" value="results">
          <Flex align="center" mb="3">
            <Select.Root
              size="1"
              value={status || '__all__'}
              onValueChange={(v) => {
                const p = new URLSearchParams(params);
                p.set('status', v === '__all__' ? '' : v);
                p.delete('page');
                setParams(p, { state: location.state });
              }}
            >
              <Select.Trigger aria-label="Result status" />
              <Select.Content>
                <Select.Item value="__all__">All results</Select.Item>
                {['pending', 'running', 'success', 'failed'].map((s) => (
                  <Select.Item value={s} key={s}>
                    {s[0].toUpperCase() + s.slice(1)}
                  </Select.Item>
                ))}
              </Select.Content>
            </Select.Root>
          </Flex>
          {results.isPending ? (
            <Loading />
          ) : results.error ? (
            <ErrorState error={results.error} retry={() => void results.refetch()} />
          ) : (
            <>
              {!results.data.length ? (
                <Empty title="No recorded results">
                  Wait for a scheduled execution, then refresh.
                </Empty>
              ) : (
                <Table.Root className="ledger-table" size="1">
                  <Table.Header>
                    <Table.Row>
                      <Table.ColumnHeaderCell>Result</Table.ColumnHeaderCell>
                      <Table.ColumnHeaderCell>State</Table.ColumnHeaderCell>
                      <Table.ColumnHeaderCell>Duration</Table.ColumnHeaderCell>
                      <Table.ColumnHeaderCell className="hide-mobile">
                        Recorded
                      </Table.ColumnHeaderCell>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {results.data.map((r) => (
                      <Table.Row key={String(r.id)}>
                        <Table.RowHeaderCell>
                          <Link
                            className="record-link"
                            to={`/check-results/${r.id}`}
                            state={forwardState(location)}
                          >
                            #{r.id}
                          </Link>
                        </Table.RowHeaderCell>
                        <Table.Cell>
                          <State value={r.status} />
                        </Table.Cell>
                        <Table.Cell>
                          {r.duration_ms == null ? 'Not recorded' : `${r.duration_ms} ms`}
                        </Table.Cell>
                        <Table.Cell className="hide-mobile">{when(r.created_at)}</Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </Table.Root>
              )}
              <Paging
                limit={limit}
                setLimit={(n) => setParams(pageLimitParams(params, n), { state: location.state })}
                offset={offset}
                count={results.data.length}
                setOffset={(n) => setParams(pageParams(params, n), { state: location.state })}
              />
            </>
          )}
        </Tabs.Content>
        <Tabs.Content className="tab-content" value="configuration">
          <section className="monitoring-details" aria-label="Check configuration">
            <Facts
              rows={[
                ...(item.description
                  ? ([['Description', item.description]] as [string, string][])
                  : []),
                ['Check type', item.check_type.replaceAll('_', ' ')],
                ['URL', String(item.check_config.url ?? 'Not recorded')],
                ['Method', String(item.check_config.method ?? 'GET')],
                ['Expected status', String(item.check_config.expected_status ?? 200)],
                [
                  'Schedule',
                  item.schedule_type === 'cron'
                    ? item.cron_expr
                    : `Every ${item.interval_sec} seconds`,
                ],
                ['Timezone', item.timezone],
                [
                  'Next scheduled',
                  item.status === 'active' && item.next_run_at
                    ? when(item.next_run_at)
                    : 'Not scheduled',
                ],
                ['Timeout', `${item.timeout_sec} seconds`],
                ['Retry limit', item.retry_limit],
              ]}
            />
            <details className="advanced-details">
              <summary>Check details</summary>
              <Facts
                rows={[
                  ['Version', item.version],
                  ['Source ID', <code>check-{item.id}</code>],
                  ['Created', when(item.created_at)],
                ]}
              />
              <Raw value={item} label="Raw definition" />
            </details>
          </section>
        </Tabs.Content>
      </Tabs.Root>
      <details className="monitoring-manage">
        <summary>Manage Check</summary>
        <ResourceActions
          resource="checks"
          placement="manage"
          item={item}
          refresh={() => query.refetch()}
          back={back}
        />
      </details>
    </>
  );
}
export function CheckResultView() {
  const location = useLocation();
  const { id = '' } = useParams();
  const { client, generation } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const query = useQuery({
    queryKey: ['monitoring', generation, 'result', id],
    queryFn: () => withCredential(client!, () => client!.checkResult(id)),
    enabled: !rejected,
  });
  const back = useBack(query.data ? `/checks/${query.data.check_id}` : '/checks');
  if (rejected)
    return (
      <>
        <Back to={back}>Back to Check results</Back>
        <ErrorState error={rejected} />
      </>
    );
  if (query.isPending) return <Loading />;
  if (query.error)
    return (
      <>
        <Back to={back}>Back to Check results</Back>
        <ErrorState error={query.error} retry={() => void query.refetch()} />
      </>
    );
  const item = query.data;
  return (
    <>
      <Back to={back}>Back to Check results</Back>
      <PageHeading
        title={`Result #${item.id}`}
        action={
          <Flex align="center" gap="3">
            <State value={item.status} />
            <Refresh run={() => query.refetch()} busy={query.isFetching} />
          </Flex>
        }
      />
      <div className="monitoring-details">
        <Facts
          rows={[
            [
              'Check',
              <Link
                className="record-link"
                to={
                  back.split('?')[0] === `/checks/${item.check_id}`
                    ? back
                    : `/checks/${item.check_id}`
                }
                state={
                  back.split('?')[0] === `/checks/${item.check_id}`
                    ? returnState(location.state, back)
                    : forwardState(location)
                }
              >
                Check #{item.check_id}
              </Link>,
            ],
            ['Scheduled', when(item.scheduled_at)],
            ['Started', item.started_at ? when(item.started_at) : 'Not recorded'],
            ['Finished', item.finished_at ? when(item.finished_at) : 'Not recorded'],
            ['Duration', item.duration_ms == null ? 'Not recorded' : `${item.duration_ms} ms`],
            ['Severity', item.severity || 'Not recorded'],
          ]}
        />
        <section className="attempt-evidence">
          <Text size="2" weight="medium">
            Response and evaluation
          </Text>
          {Object.keys(item.output).length ? (
            <Raw value={item.output} label="Response payload" />
          ) : (
            <Text as="p" size="2" color="gray" mt="2">
              No response was recorded.
            </Text>
          )}
          {Object.keys(item.evaluation_result).length ? (
            <Raw value={item.evaluation_result} label="Evaluation details" />
          ) : (
            <Text as="p" size="2" color="gray" mt="2">
              No evaluation details recorded.
            </Text>
          )}
        </section>
        <Raw value={item} label="Result details" />
      </div>
    </>
  );
}
export function SLOView() {
  const { id = '' } = useParams();
  const { client, generation } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const [params, setParams] = useSearchParams();
  const location = useLocation();
  const back = useBack('/slos');
  const offset = pageOffset(params);
  const limit = pageLimit(params);
  const tab = ['history', 'alerts'].includes(params.get('tab') ?? '')
    ? params.get('tab')!
    : 'budget';
  const query = useQuery({
    queryKey: ['monitoring', generation, 'slo', id],
    queryFn: () => withCredential(client!, () => client!.slo(id)),
    enabled: !rejected,
  });
  const budget = useQuery({
    queryKey: ['monitoring', generation, 'budget', id],
    queryFn: () => withCredential(client!, () => client!.budget(id)),
    enabled: !rejected && Boolean(query.data) && tab === 'budget',
  });
  const history = useQuery({
    queryKey: ['monitoring', generation, 'budgets', id, limit, offset],
    queryFn: () => withCredential(client!, () => client!.budgets(id, { limit, offset })),
    enabled: !rejected && Boolean(query.data) && tab === 'history',
  });
  const alerts = useQuery({
    queryKey: ['monitoring', generation, 'alerts', id, limit, offset],
    queryFn: () => withCredential(client!, () => client!.alerts({ slo_id: id, limit, offset })),
    enabled: !rejected && Boolean(query.data) && tab === 'alerts',
  });
  if (rejected)
    return (
      <>
        <MonitoringReceipts resource="slos" id={id} />
        <Back to={back}>Back to Objectives</Back>
        <ErrorState error={rejected} />
      </>
    );
  if (query.isPending) return <Loading />;
  if (query.error)
    return (
      <>
        <MonitoringReceipts resource="slos" id={id} />
        <Back to={back}>Back to Objectives</Back>
        <ErrorState error={query.error} retry={() => void query.refetch()} />
      </>
    );
  const item = query.data;
  return (
    <>
      <MonitoringReceipts resource="slos" id={id} />
      <Back to={back}>Back to Objectives</Back>
      <PageHeading
        title={item.name}
        action={
          <Flex align="center" gap="3">
            <State value={item.status} />
            <Link
              className="record-link monitoring-next-action"
              to={`/slis/${item.sli_id}`}
              state={forwardState(location)}
            >
              SLI #{item.sli_id}
            </Link>
            <Refresh
              run={() => {
                void query.refetch();
                if (tab === 'budget') void budget.refetch();
                if (tab === 'history') void history.refetch();
                if (tab === 'alerts') void alerts.refetch();
              }}
            />
            <ResourceActions
              resource="slos"
              placement="primary"
              item={item}
              refresh={() => query.refetch()}
              back={back}
            />
          </Flex>
        }
      />
      <Tabs.Root
        className="job-tabs"
        value={tab}
        onValueChange={(v) => {
          const p = new URLSearchParams(params);
          p.delete('page');
          p.delete('offset');
          p.set('tab', v);
          setParams(p, { state: location.state });
        }}
      >
        <Tabs.List size="1">
          {['budget', 'history', 'alerts'].map((v) => (
            <Tabs.Trigger key={v} value={v}>
              {v[0].toUpperCase() + v.slice(1)}
            </Tabs.Trigger>
          ))}
        </Tabs.List>
        <Tabs.Content value="budget" className="tab-content">
          {budget.isPending ? (
            <Loading />
          ) : budget.error ? (
            <ErrorState error={budget.error} retry={() => void budget.refetch()} />
          ) : (
            <BudgetView budget={budget.data} />
          )}
          <section className="budget-context monitoring-details" aria-label="Objective settings">
            <Heading as="h2" size="3" weight="medium">
              Objective settings
            </Heading>
            <Facts
              rows={[
                ...(item.description
                  ? ([['Description', item.description]] as [string, string][])
                  : []),
                ['Target', `${Number((item.target * 100).toFixed(6))}%`],
                ['Evaluation window', windowLabel(item.window_type, item.window_duration)],
                ['Fast burn threshold', `${item.alert_fast_burn_rate}×`],
                ['Slow burn threshold', `${item.alert_slow_burn_rate}×`],
              ]}
            />
            <Raw value={item} label="Objective details" />
          </section>
        </Tabs.Content>
        <Tabs.Content value="history" className="tab-content">
          {history.isPending ? (
            <Loading />
          ) : history.error ? (
            <ErrorState error={history.error} retry={() => void history.refetch()} />
          ) : (
            <>
              {!history.data.length ? (
                <Empty title="No budget history">
                  Recorded windows will appear after evaluation.
                </Empty>
              ) : (
                <Table.Root className="ledger-table budget-history" size="1">
                  <Table.Header>
                    <Table.Row>
                      {['Window', 'Allowed', 'Consumed', 'Remaining', 'State'].map((h) => (
                        <Table.ColumnHeaderCell key={h}>{h}</Table.ColumnHeaderCell>
                      ))}
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {history.data.map((b, index) => (
                      <Table.Row key={`${b.window_start}:${index}`}>
                        <Table.RowHeaderCell data-label="Window">
                          <Text size="1">
                            {when(b.window_start)}
                            <br />
                            to {when(b.window_end)}
                          </Text>
                        </Table.RowHeaderCell>
                        <Table.Cell data-label="Allowed">{b.budget_total}</Table.Cell>
                        <Table.Cell data-label="Consumed">{b.budget_consumed}</Table.Cell>
                        <Table.Cell
                          data-label="Remaining"
                          className={b.budget_remaining < 0 ? 'failure' : ''}
                        >
                          {b.budget_remaining}
                        </Table.Cell>
                        <Table.Cell data-label="State">
                          {b.budget_total > 0 ? <State value={b.status} /> : 'Not comparable'}
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
                count={history.data.length}
                setOffset={(n) => setParams(pageParams(params, n), { state: location.state })}
              />
            </>
          )}
        </Tabs.Content>
        <Tabs.Content value="alerts" className="tab-content">
          {alerts.isPending ? (
            <Loading />
          ) : alerts.error ? (
            <ErrorState error={alerts.error} retry={() => void alerts.refetch()} />
          ) : (
            <>
              {!alerts.data.length ? (
                <Empty title="No recorded alerts">
                  Alerts are created and resolved by evaluation.
                </Empty>
              ) : (
                <Table.Root className="ledger-table" size="1">
                  <Table.Header>
                    <Table.Row>
                      {['Alert', 'State', 'Burn at trigger', 'Triggered'].map((h) => (
                        <Table.ColumnHeaderCell key={h}>{h}</Table.ColumnHeaderCell>
                      ))}
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {alerts.data.map((a) => (
                      <Table.Row key={String(a.id)}>
                        <Table.RowHeaderCell>
                          <Link
                            className="record-link"
                            to={`/slo-alerts/${a.id}`}
                            state={forwardState(location)}
                          >
                            {a.alert_type}
                          </Link>
                        </Table.RowHeaderCell>
                        <Table.Cell>
                          <State value={a.status} />
                        </Table.Cell>
                        <Table.Cell>{a.burn_rate}×</Table.Cell>
                        <Table.Cell>{when(a.triggered_at)}</Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </Table.Root>
              )}
              <Paging
                limit={limit}
                setLimit={(n) => setParams(pageLimitParams(params, n), { state: location.state })}
                offset={offset}
                count={alerts.data.length}
                setOffset={(n) => setParams(pageParams(params, n), { state: location.state })}
              />
            </>
          )}
        </Tabs.Content>
      </Tabs.Root>
      <details className="monitoring-manage">
        <summary>Manage SLO</summary>
        <ResourceActions
          resource="slos"
          placement="manage"
          item={item}
          refresh={() => query.refetch()}
          back={back}
        />
      </details>
    </>
  );
}
export function SLIView() {
  const { id = '' } = useParams();
  const { client, generation } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const back = useBack('/slis');
  const location = useLocation();
  const query = useQuery({
    queryKey: ['monitoring', generation, 'sli', id],
    queryFn: () => withCredential(client!, () => client!.sli(id)),
    enabled: !rejected,
  });
  if (rejected)
    return (
      <>
        <MonitoringReceipts resource="slis" id={id} />
        <Back to={back}>Back to SLIs</Back>
        <ErrorState error={rejected} />
      </>
    );
  if (query.isPending) return <Loading />;
  if (query.error)
    return (
      <>
        <MonitoringReceipts resource="slis" id={id} />
        <Back to={back}>Back</Back>
        <ErrorState error={query.error} retry={() => void query.refetch()} />
      </>
    );
  const item = query.data;
  const source = item.source_config.source_uid;
  return (
    <>
      <MonitoringReceipts resource="slis" id={id} />
      <Back to={back}>Back</Back>
      <PageHeading
        title={item.name}
        action={
          <Flex align="center" gap="3" wrap="wrap">
            <Link
              className="text-link monitoring-next-action"
              to={`/slos?create=1&sli_id=${item.id}`}
              state={forwardState(location)}
            >
              Create objective
            </Link>
            <Refresh run={() => query.refetch()} busy={query.isFetching} />
            <ResourceActions
              resource="slis"
              item={item}
              refresh={() => query.refetch()}
              back={back}
            />
          </Flex>
        }
      />
      <section className="monitoring-details" aria-label="Indicator settings">
        <Facts
          rows={[
            ...(item.description
              ? ([['Description', item.description]] as [string, string][])
              : []),
            ['Type', item.sli_type],
            ['Aggregation', item.aggregation],
            ['Monitoring source', <MonitoringSourceLink value={source} />],
          ]}
        />
        <Raw value={item.good_event_criteria} label="Good-event criteria" />
        <details className="advanced-details">
          <summary>Indicator details</summary>
          <Facts
            rows={[
              ['Version', item.version],
              ['Source ID', <code>{typeof source === 'string' ? source : 'Not recorded'}</code>],
            ]}
          />
          <Raw value={item.source_config} label="Source configuration" />
        </details>
      </section>
    </>
  );
}
export function AlertView() {
  const { id = '' } = useParams();
  const { client, generation } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const back = useBack('/slo-alerts');
  const location = useLocation();
  const query = useQuery({
    queryKey: ['monitoring', generation, 'alert', id],
    queryFn: () => withCredential(client!, () => client!.alert(id)),
    enabled: !rejected,
  });
  if (rejected)
    return (
      <>
        <Back to={back}>Back to Alerts</Back>
        <ErrorState error={rejected} />
      </>
    );
  if (query.isPending) return <Loading />;
  if (query.error)
    return (
      <>
        <Back to={back}>Back</Back>
        <ErrorState error={query.error} retry={() => void query.refetch()} />
      </>
    );
  const item = query.data;
  return (
    <>
      <Back to={back}>Back to Alerts</Back>
      <PageHeading
        title={`Alert #${item.id}`}
        action={
          <Flex align="center" gap="3">
            <State value={item.status} />
            <Refresh run={() => query.refetch()} busy={query.isFetching} />
          </Flex>
        }
      />
      <section className="monitoring-details" aria-label="Alert details">
        <Facts
          rows={[
            ['Type', item.alert_type.replaceAll('_', ' ')],
            [
              'Objective',
              <Link
                className="record-link"
                to={`/slos/${item.slo_id}`}
                state={forwardState(location)}
              >
                SLO #{item.slo_id}
              </Link>,
            ],
            ['Burn at trigger', `${item.burn_rate}×`],
            ['Triggered', when(item.triggered_at)],
            ['Resolved', item.resolved_at ? when(item.resolved_at) : 'Not resolved'],
          ]}
        />
        <Raw value={item} label="Alert details" />
      </section>
    </>
  );
}
