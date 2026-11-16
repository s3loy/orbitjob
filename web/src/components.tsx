import { runPhaseLabel, when } from './format';
import { Box, Button, Callout, Flex, Heading, Skeleton, Table, Text } from '@radix-ui/themes';
import { ArrowLeft, ArrowRight, CircleAlert, RefreshCw } from 'lucide-react';
import { Link, useLocation } from 'react-router-dom';
import type { ReactNode } from 'react';
import type { Job, Run } from './api/contracts';
import { APIError, PAGE_SIZE } from './api/transport';
import { forwardState } from './monitoring-state';

export function PageHeading({ title, action }: { title: string; action?: ReactNode }) {
  return (
    <Flex className="page-heading" justify="between" align="center" gap="4" wrap="wrap">
      <Box>
        <Heading as="h1" size="5" weight="medium" className="page-title" tabIndex={-1}>
          {title}
        </Heading>
      </Box>
      {action}
    </Flex>
  );
}
export function PhaseBadge({ phase }: { phase: string }) {
  const color =
    phase === 'Succeeded'
      ? 'success'
      : phase === 'Failed' || phase === 'Rejected'
        ? 'failure'
        : ['RetryWaiting', 'CancelUnknown', 'CancelRequested', 'ExecutionUnknown'].includes(phase)
          ? 'warning'
          : ['Running', 'CreatingAttempt'].includes(phase)
            ? 'active'
            : 'neutral';
  return (
    <span className={`phase-label ${color}`}>
      <span className="status-dot" />
      {phase ? runPhaseLabel(phase) : 'Awaiting observation'}
    </span>
  );
}
export function ErrorState({ error, retry }: { error: Error; retry?: () => void }) {
  const location = useLocation();
  if (error.name === 'AbortError') return null;
  const authentication = error instanceof APIError && error.status === 401;
  const forbidden = error instanceof APIError && error.status === 403;
  const title =
    error instanceof APIError && error.status === 401
      ? 'Sign in required'
      : error instanceof APIError && error.status === 403
        ? 'Permission required'
        : error instanceof APIError && error.status === 429
          ? 'Too many requests'
          : 'Unable to complete this request';
  return (
    <Callout.Root color="red" role="alert" className="error-state" size="1">
      <Callout.Icon>
        <CircleAlert size={16} />
      </Callout.Icon>
      <div className="error-content">
        <strong>{title}</strong>
        <br />
        {error.message}
        {error instanceof APIError && error.field && (
          <Text as="span" size="1" style={{ display: 'block' }}>
            Field: {error.field}
          </Text>
        )}
        {error instanceof APIError && error.retryAfter > 0 && (
          <Text as="span" size="1" style={{ display: 'block' }}>
            Retry after at least {Math.ceil(error.retryAfter / 1000)} seconds.
          </Text>
        )}
        {error instanceof APIError && error.trace && (
          <details className="error-details">
            <summary>Error details</summary>
            <span>
              Request reference: <code>{error.trace}</code>
            </span>
          </details>
        )}
        {(authentication || forbidden) && (
          <Text as="span" size="1" style={{ display: 'block', marginTop: 8 }}>
            {authentication
              ? 'Sign in again to continue.'
              : 'Ask your workspace administrator for access to this action.'}
          </Text>
        )}
        {authentication && location.pathname !== '/connection' && (
          <Button asChild mt="3" size="1" variant="soft" color="red">
            <Link
              to="/connection"
              state={{ reconnect: true, returnTo: location.pathname + location.search }}
            >
              Sign in
            </Link>
          </Button>
        )}
        {retry && !authentication && !forbidden && (
          <Button mt="3" size="1" variant="soft" color="red" onClick={retry}>
            <RefreshCw size={13} />
            Try again
          </Button>
        )}
      </div>
    </Callout.Root>
  );
}
export function Loading() {
  return (
    <Box role="status" aria-label="Loading records">
      <Skeleton height="35px" mb="3" />
      <Skeleton height="160px" />
      <Text className="sr-only" size="1" color="gray">
        Loading records…
      </Text>
    </Box>
  );
}
export function Empty({ title, children }: { title: string; children: ReactNode }) {
  return (
    <Box className="empty-state" role="status">
      <Heading as="h2" size="3" mb="2" weight="medium">
        {title}
      </Heading>
      <Text color="gray" size="2">
        {children}
      </Text>
    </Box>
  );
}
export function Paging({
  offset,
  count,
  setOffset,
  limit = PAGE_SIZE,
  setLimit,
  hasMore,
  disabled = false,
}: {
  offset: number;
  count: number;
  setOffset: (n: number) => void;
  limit?: number;
  setLimit?: (n: number) => void;
  hasMore?: boolean;
  disabled?: boolean;
}) {
  return (
    <Flex className="paging" align="center" justify="between" wrap="wrap" gap="3">
      <Text size="1" color="gray">
        {count ? `Rows ${offset + 1}–${offset + count}` : 'No records on this page'}
      </Text>
      <Flex gap="3" align="center" wrap="wrap">
        {setLimit && (
          <label className="page-size-control">
            <Text size="1" color="gray">
              Rows per page
            </Text>
            <select
              className="page-size-select"
              aria-label="Rows per page"
              value={limit}
              disabled={disabled}
              onChange={(event) => setLimit(Number(event.target.value))}
            >
              {[10, 20, 50, 100].map((size) => (
                <option key={size} value={size}>
                  {size}
                </option>
              ))}
            </select>
          </label>
        )}
        <Text size="1" color="gray">
          Page {Math.floor(offset / limit) + 1}
        </Text>
        <Button
          size="1"
          variant="ghost"
          color="gray"
          disabled={disabled || offset === 0}
          aria-label="Previous page"
          onClick={() => setOffset(Math.max(0, offset - limit))}
        >
          <ArrowLeft size={13} />
        </Button>
        <Button
          size="1"
          variant="ghost"
          color="gray"
          disabled={disabled || !(hasMore ?? count >= limit)}
          aria-label="Next page"
          onClick={() => setOffset(offset + limit)}
        >
          <ArrowRight size={13} />
        </Button>
      </Flex>
    </Flex>
  );
}
export function RunTable({
  runs,
  jobs = [],
  scoped = false,
}: {
  runs: Run[];
  jobs?: Job[];
  scoped?: boolean;
}) {
  const location = useLocation();
  if (!runs.length) return <Empty title="No runs to show">No runs match the current view.</Empty>;
  return (
    <Table.Root className="ledger-table runs-table" size="1">
      <Table.Header>
        <Table.Row>
          <Table.ColumnHeaderCell className="run-id-col">Run</Table.ColumnHeaderCell>
          <Table.ColumnHeaderCell className="run-phase-col">State</Table.ColumnHeaderCell>
          <Table.ColumnHeaderCell className="hide-mobile">Started from</Table.ColumnHeaderCell>
          <Table.ColumnHeaderCell>Created</Table.ColumnHeaderCell>
        </Table.Row>
      </Table.Header>
      <Table.Body>
        {runs.map((run) => {
          const job = jobs.find((job) => job.source_uid === run.source_uid);
          return (
            <Table.Row key={String(run.id)}>
              <Table.RowHeaderCell>
                <Link
                  className="record-link"
                  to={`/runs/${run.id}`}
                  state={forwardState(location)}
                  aria-label={`View run ${run.id}`}
                >
                  #{run.id}
                </Link>
                {!scoped && (
                  <Text as="p" size="1" color="gray" className="wrap-code">
                    {run.source_name ?? job?.name ?? run.source_uid}
                    {run.source_namespace ? ` (${run.source_namespace})` : ''}
                  </Text>
                )}
              </Table.RowHeaderCell>
              <Table.Cell>
                <PhaseBadge phase={run.phase} />
              </Table.Cell>
              <Table.Cell className="hide-mobile">{run.trigger}</Table.Cell>
              <Table.Cell>
                <Text size="2" className="tabular">
                  {when(run.created_at)}
                </Text>
              </Table.Cell>
            </Table.Row>
          );
        })}
      </Table.Body>
    </Table.Root>
  );
}
