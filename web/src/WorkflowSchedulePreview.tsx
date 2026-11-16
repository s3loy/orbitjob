import { useEffect, useRef, useState } from 'react';
import { Button, Heading, Text } from '@radix-ui/themes';
import { useSession } from './session';
import { ErrorState } from './components';
import { useAdministrationAuthenticationError } from './administration-state';
import { withCredential } from './credential-requests';
import { M4Time } from './m4-ui';

type Preview = {
  schedule: string;
  zone: string;
  displayZone: string;
  upcoming: string[];
  generatedAt: number;
  token: number;
};
function localTime(at: string, zone: string) {
  try {
    return new Intl.DateTimeFormat(undefined, {
      dateStyle: 'medium',
      timeStyle: 'long',
      timeZone: zone,
    }).format(new Date(at));
  } catch {
    return `${at} (UTC)`;
  }
}

export function WorkflowSchedulePreview({ schedule, zone }: { schedule: string; zone: string }) {
  const { client } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const [result, setResult] = useState<Preview>();
  const [pending, setPending] = useState(false);
  const [failed, setFailed] = useState<{ schedule: string; zone: string; reason: Error }>();
  const [expiredToken, setExpiredToken] = useState<number>();
  const sequence = useRef(0);
  useEffect(
    () => () => {
      sequence.current++;
    },
    [],
  );
  useEffect(() => {
    if (!result?.upcoming.length) return;
    const boundary = Date.parse(result.upcoming[0]);
    if (!Number.isFinite(boundary)) return;
    let timer: ReturnType<typeof setTimeout>;
    function checkBoundary() {
      const remaining = boundary - Date.now();
      if (remaining <= 0) setExpiredToken(result!.token);
      else timer = setTimeout(checkBoundary, Math.min(remaining, 2_147_483_647));
    }
    timer = setTimeout(checkBoundary, Math.min(Math.max(0, boundary - Date.now()), 2_147_483_647));
    return () => clearTimeout(timer);
  }, [result]);
  const error =
    rejected ?? (failed?.schedule === schedule && failed.zone === zone ? failed.reason : undefined);
  const current =
    !error && result?.schedule === schedule && result.zone === zone ? result : undefined;
  const expired = current?.token === expiredToken && current !== undefined;
  return (
    <section className="schedule-preview" aria-label="Schedule preview">
      <Heading as="h3" size="2" mb="3">
        {expired ? 'Schedule preview expired' : 'Upcoming automatic starts'}
      </Heading>
      <Button
        size="2"
        type="button"
        variant="outline"
        disabled={pending || !schedule || !!rejected}
        onClick={async () => {
          const token = ++sequence.current;
          setPending(true);
          setFailed(undefined);
          setResult(undefined);
          try {
            const value = await withCredential(client!, () =>
              client!.previewWorkflowSchedule({ schedule, time_zone: zone, count: 5 }),
            );
            if (token === sequence.current)
              setResult({
                schedule,
                zone,
                displayZone: value.time_zone,
                upcoming: value.upcoming,
                generatedAt: Date.now(),
                token,
              });
          } catch (cause) {
            if (token === sequence.current)
              setFailed({
                schedule,
                zone,
                reason:
                  cause instanceof Error ? cause : new Error('Unable to preview this schedule.'),
              });
          } finally {
            if (token === sequence.current) setPending(false);
          }
        }}
      >
        {pending ? 'Calculating…' : 'Preview next starts'}
      </Button>
      {error && (
        <div className="form-feedback">
          <ErrorState error={error} />
        </div>
      )}
      {current && !expired && (
        <Text as="p" size="1" color="gray" mt="3">
          Times shown in {current.displayZone}
        </Text>
      )}
      {expired ? (
        <Text as="p" size="2" role="status" mt="3">
          The first start time has passed. Preview again to see upcoming starts. A preview does not
          confirm that a run took place.
        </Text>
      ) : (
        current && (
          <ol className="schedule-times">
            {current.upcoming.map((at) => (
              <li key={at}>
                <time dateTime={at}>{localTime(at, current.displayZone)}</time>
              </li>
            ))}
          </ol>
        )
      )}
      {current && (
        <details className="schedule-preview-details">
          <summary>Preview details</summary>
          <Text as="p" size="1" color="gray" mt="2">
            Calculated (UTC) <M4Time value={new Date(current.generatedAt).toISOString()} />
          </Text>
        </details>
      )}
    </section>
  );
}
