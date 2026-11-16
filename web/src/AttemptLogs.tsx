import { useEffect, useRef, useState } from 'react';
import { Button, Flex, Select, Text, TextArea, TextField } from '@radix-ui/themes';
import type { LogParent, LogSnapshot } from './api/definitions';
import { ErrorState } from './components';
import { useSession } from './session';
import './manifest.css';
import { defaultLogAttempt, type RecordedAttempt } from './attempt-choice';
import {
  administrationAuthenticationError,
  rejectAdministrationCredential,
  useAdministrationAuthenticationError,
} from './administration-state';

type AttemptLogsProps = {
  parent: LogParent;
  attempt?: number;
  attempts?: RecordedAttempt[] | null;
  lastAttempt?: number | null;
};
function LogView({ parent, attempt, attempts, lastAttempt }: AttemptLogsProps) {
  const { client } = useSession();
  const rejectedCredential = useAdministrationAuthenticationError(client);
  const [chosen, setChosen] = useState(
    attempt !== undefined
      ? String(attempt)
      : parent.kind === 'function'
        ? defaultLogAttempt(attempts, lastAttempt)
        : '1',
  );
  const [tail, setTail] = useState('100');
  const [storedSnapshot, setSnapshot] = useState<LogSnapshot>();
  const [error, setError] = useState<Error>();
  const [busy, setBusy] = useState(false);
  const [expanded, setExpanded] = useState(false);
  const sequence = useRef(0);
  useEffect(
    () => () => {
      sequence.current++;
    },
    [],
  );
  function clear() {
    sequence.current++;
    setSnapshot(undefined);
    setError(undefined);
    setBusy(false);
  }
  async function load() {
    if (administrationAuthenticationError(client)) return;
    const token = ++sequence.current;
    setBusy(true);
    setSnapshot(undefined);
    setError(undefined);
    try {
      const value = await client!.attemptLogs(parent, Number(chosen), Number(tail));
      if (token === sequence.current && !administrationAuthenticationError(client))
        setSnapshot(value);
    } catch (e) {
      const failure = e instanceof Error ? e : new Error('Log request failed.');
      rejectAdministrationCredential(client!, failure);
      if (token === sequence.current) {
        setError(failure);
      }
    } finally {
      if (token === sequence.current) setBusy(false);
    }
  }
  const valid =
    /^[1-9]\d*$/.test(chosen) &&
    Number.isSafeInteger(Number(chosen)) &&
    (!attempts || attempts.some((item) => item.attempt_number === Number(chosen)));
  const snapshot = rejectedCredential ? undefined : storedSnapshot;
  return (
    <details
      className="m4-section attempt-log-disclosure"
      aria-label="Attempt logs"
      open={expanded}
      onToggle={(event) => setExpanded(event.currentTarget.open)}
    >
      <summary>Container logs</summary>
      <Text as="p" size="1" color="gray" mb="3">
        Read the latest available container output for one attempt. Access requires log permission;
        output disappears when the container is removed and stays only in this open view.
      </Text>
      {parent.kind === 'function' && attempt === undefined && !attempts && (
        <Text as="p" size="1" color="gray" mb="3">
          Attempt details unavailable.{' '}
          {lastAttempt
            ? 'The log reader uses the recorded last attempt.'
            : 'No attempt is assumed. Enter a known attempt number only if you have independent execution evidence.'}
        </Text>
      )}
      {attempts?.length === 0 && (
        <Text as="p" size="1" color="gray" mb="3">
          No execution attempts were recorded for this invocation.
        </Text>
      )}
      <Flex gap="3" wrap="wrap" align="end" mb="3">
        {attempt === undefined && attempts?.length ? (
          <label className="manifest-field">
            <Text size="1">Recorded attempt</Text>
            <select
              aria-label="Log attempt"
              value={chosen}
              onChange={(event) => {
                clear();
                setChosen(event.target.value);
              }}
            >
              {[...attempts]
                .sort((a, b) => b.attempt_number - a.attempt_number)
                .map((item) => (
                  <option key={item.attempt_number} value={item.attempt_number}>
                    Attempt {item.attempt_number} ({item.phase})
                    {item.attempt_number === Number(defaultLogAttempt(attempts, lastAttempt))
                      ? ', latest'
                      : ''}
                  </option>
                ))}
            </select>
          </label>
        ) : attempt === undefined && attempts?.length !== 0 ? (
          <label className="manifest-field">
            <Text size="1">Attempt number</Text>
            <TextField.Root
              aria-label="Log attempt number"
              inputMode="numeric"
              value={chosen}
              onChange={(e) => {
                clear();
                setChosen(e.target.value);
              }}
            />
          </label>
        ) : attempt !== undefined ? (
          <Text size="2">Attempt {attempt}</Text>
        ) : null}
        <label className="manifest-field">
          <Text size="1">Tail lines</Text>
          <Select.Root
            value={tail}
            onValueChange={(value) => {
              clear();
              setTail(value);
            }}
          >
            <Select.Trigger aria-label="Log tail lines" />
            <Select.Content>
              {['50', '100', '500'].map((v) => (
                <Select.Item key={v} value={v}>
                  {v} lines
                </Select.Item>
              ))}
            </Select.Content>
          </Select.Root>
        </label>
        <Button
          size="1"
          color="gray"
          variant="outline"
          loading={busy}
          disabled={!valid || busy || Boolean(rejectedCredential)}
          onClick={() => void load()}
        >
          Read logs
        </Button>
        {snapshot && (
          <Button size="1" color="gray" variant="ghost" onClick={clear}>
            Clear log text
          </Button>
        )}
      </Flex>
      {!valid && attempts?.length !== 0 && (
        <Text as="p" color="red" size="1" role="alert">
          Enter a positive attempt number.
        </Text>
      )}
      {(rejectedCredential || error) && <ErrorState error={(rejectedCredential ?? error)!} />}
      {snapshot && (
        <>
          <Text as="p" size="1" color="gray" role="status" mb="3">
            {snapshot.state === 'ready'
              ? `Snapshot from ${snapshot.container}, attempt ${snapshot.attempt}.`
              : snapshot.state === 'starting'
                ? 'The task container has not started.'
                : snapshot.state === 'absent'
                  ? 'No retained log source is available for this attempt.'
                  : 'The log source is currently unavailable.'}{' '}
            {snapshot.reason}
          </Text>
          {snapshot.truncated && (
            <Text as="p" size="1" role="status" mb="3">
              Output was truncated at the {snapshot.limit_bytes.toLocaleString()} byte limit. This
              is not the complete log.
            </Text>
          )}
          {snapshot.state === 'ready' && (
            <TextArea
              aria-label="Container log snapshot"
              className="manifest-yaml"
              readOnly
              value={snapshot.text}
              rows={14}
            />
          )}
          {snapshot.state === 'ready' && !snapshot.text && (
            <Text as="p" size="1">
              This snapshot contains no log lines.
            </Text>
          )}
        </>
      )}
    </details>
  );
}

export function AttemptLogs(props: AttemptLogsProps) {
  const p = props.parent;
  return (
    <LogView
      key={`${p.kind}/${p.kind === 'function' ? p.id : ''}/${p.run}/${props.attempt ?? 'choose'}/${props.lastAttempt ?? 'unknown'}/${props.attempts?.map((item) => item.attempt_number).join(',') ?? 'unavailable'}`}
      {...props}
    />
  );
}
