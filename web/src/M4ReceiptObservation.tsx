import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Flex, Text } from '@radix-ui/themes';
import { useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { APIError, observationDelay } from './api/transport';
import { ErrorState, PhaseBadge } from './components';
import type { Intent } from './m4-state';
import { useSession } from './session';
import { when } from './format';
import {
  administrationAuthenticationError,
  useAdministrationAuthenticationError,
} from './administration-state';
import { withCredential } from './credential-requests';

const terminal = (phase: string) =>
  ['succeeded', 'success', 'failed', 'canceled', 'rejected'].includes(phase.toLowerCase());

export function M4ReceiptObservation({ intent }: { intent: Intent }) {
  const { client, generation } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const cache = useQueryClient();
  const refreshed = useRef<string | undefined>(undefined);
  const [since, setSince] = useState(Date.now);
  const query = useQuery({
    queryKey: ['m4-receipt-observation', generation, intent.key, since],
    enabled: Boolean(intent.receipt && !rejected),
    queryFn: () =>
      withCredential(client!, async () => {
        for (let offset = 0; offset < 1000; offset += 20) {
          if (intent.kind === 'workflow') {
            const rows = await client!.workflowRuns(String(intent.target.id), {
              offset,
              limit: 20,
            });
            const matches = rows.filter(
              (row) =>
                row.occurrence_key === intent.receipt!.occurrence_key &&
                (!intent.target.source_uid || row.workflow_source_uid === intent.target.source_uid),
            );
            if (matches.length > 1)
              throw new Error(
                'Multiple records match this request. Automatic linking is unavailable.',
              );
            if (matches[0])
              return {
                path: `/workflows/${intent.target.id}/runs/${matches[0].id}`,
                phase: matches[0].phase,
                reason: matches[0].reason,
              };
            if (rows.length < 20) return null;
          } else {
            const rows = await client!.functionRuns(String(intent.target.id), {
              offset,
              limit: 20,
            });
            const matches = rows.filter(
              (row) =>
                row.occurrence_key === intent.receipt!.occurrence_key &&
                String(row.function_id) === String(intent.target.id),
            );
            if (matches.length > 1)
              throw new Error(
                'Multiple records match this request. Automatic linking is unavailable.',
              );
            if (matches[0])
              return {
                path: `/functions/${intent.target.id}/runs/${encodeURIComponent(matches[0].run_id)}`,
                phase: matches[0].status,
                reason: '',
              };
            if (rows.length < 20) return null;
          }
        }
        throw new Error(
          'This request is outside the first 1,000 history records. Open history to continue.',
        );
      }),
    refetchInterval: (state) =>
      state.state.data && terminal(state.state.data.phase)
        ? false
        : observationDelay(state.state.error, since),
  });
  useEffect(() => {
    if (
      !client ||
      rejected ||
      administrationAuthenticationError(client) ||
      query.error ||
      !query.data ||
      !terminal(query.data.phase)
    )
      return;
    const completed = `${generation}:${intent.key}:${query.data.path}`;
    if (refreshed.current === completed) return;
    refreshed.current = completed;
    void cache.invalidateQueries({
      queryKey: [
        'm4',
        generation,
        intent.kind === 'workflow' ? 'workflow-runs' : 'function-runs',
        String(intent.target.id),
      ],
      // A completed receipt does not authorize retries of a denied or failed history read.
      predicate: (history) => !history.state.error,
      refetchType: 'active',
    });
  }, [
    cache,
    client,
    generation,
    intent.key,
    intent.kind,
    intent.target.id,
    query.data,
    query.error,
    rejected,
  ]);
  if (!intent.receipt) return null;
  if (rejected) return <ErrorState error={rejected} />;
  const unreadable = query.error instanceof APIError && [403, 404].includes(query.error.status);
  const observed = unreadable ? undefined : query.data;
  return (
    <section aria-label="Observed execution" className="receipt-observation">
      {query.error && observed && (
        <Text as="p" size="2" mt="3">
          The latest observation failed. This previously read snapshot is not confirmed current.
        </Text>
      )}
      {observed ? (
        <Flex gap="3" mt="3" align="center" wrap="wrap">
          <Text size="1" color="gray">
            Last observed
          </Text>
          <PhaseBadge
            phase={
              observed.phase === 'success'
                ? 'Succeeded'
                : observed.phase.charAt(0).toUpperCase() + observed.phase.slice(1)
            }
          />
          <Text size="1" color="gray">
            {when(new Date(query.dataUpdatedAt).toISOString())}
          </Text>
          <Button asChild variant="outline" size="1">
            <Link to={observed.path}>Open this execution</Link>
          </Button>
          {observed.phase === 'Rejected' && (
            <Text as="p" size="2">
              {observed.reason === 'concurrency_forbid'
                ? 'Another execution was still open. This request ended without starting steps.'
                : 'This execution was rejected. Open the record for details.'}
            </Text>
          )}
          {observed.phase === 'ExecutionUnknown' && (
            <Text as="p" size="2">
              Execution may still be running. No retry or completed outcome is confirmed. Open this
              execution to request cancellation or continue observing.
            </Text>
          )}
          {observed.phase === 'Pending' && observed.reason === 'ConcurrencyBlocked' && (
            <Text as="p" size="2">
              Accepted and waiting for another execution to stop. Keep observing the same request.
            </Text>
          )}
        </Flex>
      ) : (
        <Text as="p" size="1" color="gray" mt="3">
          {query.error
            ? 'Execution history is unavailable. The request is still accepted, but its outcome is not confirmed.'
            : 'Waiting for the execution record. This request may be waiting for another run to finish. Checks continue for up to two minutes; a missing record does not confirm failure.'}
        </Text>
      )}
      {query.error && <ErrorState error={query.error} />}
      {(!observed || !terminal(observed.phase)) && (
        <Button
          size="1"
          mt="2"
          variant="ghost"
          disabled={query.isFetching}
          onClick={() => setSince(Date.now())}
        >
          Check execution history again
        </Button>
      )}
    </section>
  );
}
