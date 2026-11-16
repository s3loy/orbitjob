import { Button, Flex, Text } from '@radix-ui/themes';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { correlate } from './api/contracts';
import { OBSERVATION_MS, observationDelay } from './api/transport';
import { ErrorState } from './components';
import { useOperations, type TriggerIntent } from './operation-state';
import { useSession } from './session';
import { useAdministrationAuthenticationError } from './administration-state';
import { withCredential } from './credential-requests';

export function TriggerReceipts() {
  const { intents, receipts } = useOperations();
  const [historyOpen, setHistoryOpen] = useState(false);
  const accepted = Object.values(receipts);
  const latestKey = accepted.at(-1)?.key;
  const visible = [
    ...Object.values(receipts),
    ...Object.values(intents).filter(
      (intent) => !intent.receipt && (intent.pending || intent.error),
    ),
  ].sort(
    (a, b) =>
      Number(Boolean(a.receipt) && a.key !== latestKey) -
      Number(Boolean(b.receipt) && b.key !== latestKey),
  );
  return (
    <div className="trigger-receipts" data-history-open={historyOpen}>
      <div id="trigger-receipt-list" className="trigger-receipt-list">
        {visible.map((intent) => (
          <ReceiptObservation
            key={intent.key}
            intent={intent}
            latest={!intent.receipt || intent.key === latestKey}
          />
        ))}
      </div>
      {accepted.length > 1 && (
        <Button
          size="1"
          variant="ghost"
          color="gray"
          mb="4"
          aria-expanded={historyOpen}
          aria-controls="trigger-receipt-list"
          onClick={() => setHistoryOpen((value) => !value)}
        >
          {historyOpen ? 'Hide' : 'Show'} earlier receipts ({accepted.length - 1})
        </Button>
      )}
    </div>
  );
}
function ReceiptObservation({ intent, latest }: { intent: TriggerIntent; latest: boolean }) {
  const { client, generation } = useSession();
  const queries = useQueryClient();
  const rejected = useAdministrationAuthenticationError(client);
  const { job, receipt } = intent;
  const [manualSince, setSince] = useState(() => Date.now());
  const since = Math.max(manualSince, intent.acceptedAt ?? 0);
  const [expansion, setExpansion] = useState({ latest, open: true });
  if (expansion.latest !== latest) setExpansion({ latest, open: latest });
  const deadline = useRef(since + OBSERVATION_MS);
  const [expiredSince, setExpiredSince] = useState(0);
  const paused = expiredSince === since;
  const support = useQuery({
    queryKey: ['correlation-support', generation],
    queryFn: () => withCredential(client!, () => client!.supportsCorrelation()),
    enabled: Boolean(receipt) && !paused && !rejected,
    networkMode: 'always',
    refetchOnReconnect: false,
  });
  const lookup = useQuery({
    queryKey: ['correlation', generation, job.source_uid, receipt?.occurrence_key],
    enabled: Boolean(receipt) && support.data === true && !paused && !rejected,
    networkMode: 'always',
    refetchOnReconnect: false,
    queryFn: () =>
      withCredential(client!, async () => {
        if (Date.now() >= Math.max(deadline.current, since + OBSERVATION_MS)) return null;
        return (
          correlate(
            await client!.runs({
              source_uid: job.source_uid,
              occurrence_key: receipt!.occurrence_key,
              limit: 2,
            }),
            job.source_uid,
            receipt!.occurrence_key,
          ) ?? null
        );
      }),
    refetchInterval: (query) =>
      query.state.data || paused || rejected ? false : observationDelay(query.state.error, since),
    refetchIntervalInBackground: false,
  });
  useEffect(() => {
    const timer = setTimeout(
      () => setExpiredSince(since),
      Math.max(0, since + OBSERVATION_MS - Date.now()),
    );
    return () => clearTimeout(timer);
  }, [since]);
  const observed = rejected ? undefined : lookup.data;
  const observedID = observed?.id;
  const refreshedHistory = useRef<string>('');
  useEffect(() => {
    if (!observedID || rejected) return;
    const confirmation = `${generation}/${job.source_uid}/${observedID}`;
    if (refreshedHistory.current === confirmation) return;
    refreshedHistory.current = confirmation;
    // Exact receipt correlation makes a cached empty history obsolete. A denied
    // history read retains its error and requires its own recovery action.
    void queries.invalidateQueries({
      queryKey: ['job-runs', generation, job.source_uid],
      predicate: (query) => !query.state.error,
    });
  }, [generation, job.source_uid, observedID, queries, rejected]);
  const error = rejected ?? lookup.error ?? support.error;
  return (
    <section
      className="operation-notice"
      data-history={!latest}
      aria-label={`Trigger receipt for ${job.name}`}
    >
      <details
        className="receipt-disclosure"
        open={expansion.open}
        onToggle={(event) => {
          const open = event.currentTarget.open;
          setExpansion((current) => (current.open === open ? current : { ...current, open }));
        }}
      >
        <summary id={`receipt-${intent.key}`}>
          <Text size="2" role="status">
            {receipt
              ? 'Trigger accepted for '
              : intent.pending
                ? 'Submitting trigger for '
                : 'Trigger not confirmed for '}
            <strong>{job.name}</strong>
          </Text>
        </summary>
        <Flex justify="end" mt="2">
          <Button asChild size="1" variant="ghost" color="gray">
            <Link to={`/jobs/${job.id}`}>View this job’s runs</Link>
          </Button>
        </Flex>
        {receipt ? (
          <>
            {!observed && (
              <Text as="p" size="1" color="gray" mt="2">
                This request is accepted and may be waiting for another run to finish. A start has
                not been confirmed. Keep observing this request.
              </Text>
            )}
            {observed ? (
              <Flex mt="2" gap="3" align="center" wrap="wrap">
                <Text size="2">
                  Run #{observed.id} recorded. Acceptance does not establish execution success.
                </Text>
                <Button asChild size="1" variant="outline" color="gray">
                  <Link to={`/runs/${observed.id}`}>View run</Link>
                </Button>
              </Flex>
            ) : (
              <Text as="p" size="2" color="gray" mt="1">
                {rejected
                  ? 'Observation unavailable. The receipt remains accepted.'
                  : support.data === false
                    ? 'This connection cannot link the accepted request to a run. Open Runs to check progress.'
                    : paused
                      ? 'Observation paused. No matching Run record has been confirmed.'
                      : 'Waiting for a matching Run record.'}
              </Text>
            )}
            {error && <ErrorState error={error} />}
            {support.data === false && (
              <Button asChild size="1" mt="2" variant="outline" color="gray">
                <Link to="/runs">Open Runs</Link>
              </Button>
            )}
            {!observed && (
              <Button
                size="1"
                mt="2"
                variant="ghost"
                color="gray"
                disabled={lookup.isFetching || support.isFetching || Boolean(rejected)}
                onClick={() => {
                  deadline.current = Date.now() + OBSERVATION_MS;
                  setSince(Date.now());
                  if (support.data === true) void lookup.refetch();
                  else void support.refetch();
                }}
              >
                Refresh observation
              </Button>
            )}
            <details>
              <summary>Request details</summary>
              <dl className="definition-list">
                <dt>Resource</dt>
                <dd className="mono">
                  {receipt.namespace}/{receipt.name}
                </dd>
                <dt>Occurrence</dt>
                <dd className="mono">{receipt.occurrence_key}</dd>
                <dt>Intent</dt>
                <dd className="mono">{intent.key}</dd>
              </dl>
            </details>
          </>
        ) : (
          <>
            {intent.error && <ErrorState error={intent.error} />}
            <Text as="p" size="1" color="gray" mt="1">
              Reopen this job’s trigger to retry the same request. Changing pages does not send
              another trigger.
            </Text>
          </>
        )}
      </details>
    </section>
  );
}
