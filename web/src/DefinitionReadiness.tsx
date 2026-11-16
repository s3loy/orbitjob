import { useQuery } from '@tanstack/react-query';
import { Button, Flex, Text } from '@radix-ui/themes';
import { Link } from 'react-router-dom';
import type { DefinitionKind, DefinitionResult } from './api/definitions';
import { observationDelay } from './api/transport';
import { ErrorState } from './components';
import { useSession } from './session';
import { useAdministrationAuthenticationError } from './administration-state';
import { withCredential } from './credential-requests';
import { useObservationWindow } from './observation-window';
import { M4Time } from './m4-ui';
import { Facts } from './monitoring-ui';
import { TriggerAction } from './pages/TriggerAction';

export function DefinitionReadiness({
  kind,
  receipt,
}: {
  kind: DefinitionKind;
  receipt: DefinitionResult;
}) {
  const { client, generation } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const { since, paused, restart } = useObservationWindow();
  const query = useQuery({
    queryKey: ['definition-readiness', generation, kind, receipt.uid, receipt.generation, since],
    enabled: !rejected,
    queryFn: () =>
      withCredential(client!, async () => {
        if (kind === 'jobs') {
          const items = await client!.jobs(0, '', {
            namespace: receipt.namespace,
            source_uid: receipt.uid,
          });
          return { item: items.find((item) => item.source_uid === receipt.uid), complete: true };
        }
        for (let offset = 0; offset < 1000; offset += 20) {
          const items = await client!.workflows({ offset, search: receipt.name });
          const item = items.find(
            (item) => item.source_uid === receipt.uid && item.namespace === receipt.namespace,
          );
          if (item) return { item, complete: true };
          if (items.length < 20) return { complete: true };
        }
        return { complete: false };
      }),
    refetchInterval: (state) =>
      paused ||
      rejected ||
      (state.state.data?.item &&
        BigInt(state.state.data.item.generation) >= BigInt(receipt.generation))
        ? false
        : observationDelay(state.state.error, since),
  });
  const error = rejected ?? query.error;
  const item = error ? undefined : query.data?.item;
  const ready = item && String(item.generation) === String(receipt.generation);
  const readyJob = ready && kind === 'jobs' && 'misfire_policy' in item ? item : undefined;
  const newer = item && BigInt(item.generation) > BigInt(receipt.generation);
  return (
    <section className="readiness" aria-label="Saved workload readiness">
      <Text as="p" size="2" weight="medium">
        {ready
          ? 'Saved version is ready'
          : newer
            ? 'A newer version is active'
            : error
              ? 'Saved. Readiness unavailable'
              : paused
                ? 'Saved. Readiness check paused'
                : 'Saved. Getting ready'}
      </Text>
      <Text as="p" size="2" color="gray" mt="2">
        {receipt.name} in {receipt.namespace}
      </Text>
      {error && <ErrorState error={error} />}
      {!error && !ready && !newer && paused && (
        <Text as="p" size="2" color="gray" mt="2">
          Automatic checks stopped after two minutes. Check again to continue.
        </Text>
      )}
      {!error && query.data && !query.data.complete && (
        <Text as="p" size="1" mt="2">
          The lookup reached its search limit. Open the workload list to continue.
        </Text>
      )}
      {readyJob && (
        <div className="job-first-run">
          <Text as="p" size="2" mb="3">
            {readyJob.suspend
              ? 'This saved definition is suspended. Resume it before starting a run.'
              : readyJob.schedule
                ? 'The schedule is active. You can also request one manual run now.'
                : 'The job is ready. Start one manual run, then follow its receipt to the recorded result.'}
          </Text>
          <TriggerAction job={readyJob} runOnce />
        </div>
      )}
      <Flex gap="3" wrap="wrap" align="center" mt="3">
        <Button asChild size="2" color="blue">
          <Link
            to={
              item ? `/${kind}/${item.id}` : `/${kind}?search=${encodeURIComponent(receipt.name)}`
            }
          >
            {readyJob ? 'Open job' : ready ? 'Open workload and run' : 'Open workload'}
          </Link>
        </Button>
        {!ready && !newer && (
          <Button variant="ghost" disabled={query.isFetching || !!rejected} onClick={restart}>
            Check readiness again
          </Button>
        )}
      </Flex>
      <details className="readiness-details">
        <summary>Readiness details</summary>
        <Facts
          rows={[
            ['Saved generation', receipt.generation],
            ['Observed generation', item?.generation ?? 'Not observed'],
            [
              'Last checked (UTC)',
              query.dataUpdatedAt ? (
                <M4Time value={new Date(query.dataUpdatedAt).toISOString()} />
              ) : (
                'Not checked'
              ),
            ],
          ]}
        />
      </details>
    </section>
  );
}
