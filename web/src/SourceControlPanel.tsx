import { Button, Flex, Heading, Switch, Text } from '@radix-ui/themes';
import { useId, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { APIError } from './api/transport';
import {
  keepSourceControlIntent,
  readSourceControlIntent,
  sourceControlValuesEqual,
  sourceControlWriteUnknown,
  sourceControlIntentKey,
  type SourceControl,
  type SourceControlChange,
  type SourceControlIntent,
  type SourceControlKind,
} from './api/source-controls';
import { useSession } from './session';
import { withCredential } from './credential-requests';
import { useAdministrationAuthenticationError } from './administration-state';
import { ErrorState, Loading } from './components';
import './source-controls.css';

export function SourceControlPanel({ kind, id }: { kind: SourceControlKind; id: string }) {
  const { client, generation, identity, targetTenant } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const key = ['source-controls', generation, kind, id] as const;
  const query = useQuery({
    queryKey: key,
    queryFn: () => withCredential(client!, () => client!.sourceControls!(kind, id)),
    enabled: Boolean(client?.sourceControls) && !rejected,
    retry: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });
  if (!client?.sourceControls) return null;
  const storageKey = sourceControlIntentKey(
    identity,
    targetTenant || identity?.tenant_id || '',
    kind,
    id,
  );
  return (
    <section className="execution-controls" aria-label="Execution controls">
      <Heading as="h2" size="3" weight="medium">
        Execution controls
      </Heading>
      {rejected ? (
        <ErrorState error={rejected} />
      ) : query.isPending ? (
        <Loading />
      ) : query.error ? (
        <ErrorState error={query.error} retry={() => void query.refetch()} />
      ) : (
        <ControlForm
          key={`${generation}:${storageKey}:${kind}:${id}`}
          initial={query.data}
          kind={kind}
          id={id}
          storageKey={storageKey}
          queryKey={key}
          read={async () => {
            const result = await query.refetch();
            if (result.error) throw result.error;
            return result.data!;
          }}
        />
      )}
    </section>
  );
}

function ControlForm({
  initial,
  kind,
  id,
  storageKey,
  queryKey,
  read,
}: {
  initial: SourceControl;
  kind: SourceControlKind;
  id: string;
  storageKey: string | undefined;
  queryKey: readonly unknown[];
  read(): Promise<SourceControl>;
}) {
  const { client } = useSession();
  const cache = useQueryClient();
  const fields = useId();
  const [observed, setObserved] = useState(initial);
  const [intent, setIntent] = useState<SourceControlIntent | undefined>(() => {
    const stored = readSourceControlIntent(storageKey);
    return stored?.source_uid === initial.source_uid && stored.source_mode === initial.source_mode
      ? stored
      : undefined;
  });
  const [draft, setDraft] = useState<SourceControlChange>(() => intent ?? initial);
  const [outcome, setOutcome] = useState<'unknown' | 'conflict' | undefined>(
    intent ? 'unknown' : undefined,
  );
  const [error, setError] = useState<Error>();
  const [reading, setReading] = useState(false);
  const [checked, setChecked] = useState(false);
  const [saved, setSaved] = useState(false);
  const mutation = useMutation({
    mutationFn: (request: SourceControlIntent) =>
      withCredential(client!, () =>
        client!.updateSourceControls!(kind, id, {
          version: request.version,
          schedule_paused: request.schedule_paused,
          execution_paused: request.execution_paused,
        }),
      ),
    retry: false,
    onSuccess: (current) => {
      setObserved(current);
      setDraft(current);
      setIntent(undefined);
      setOutcome(undefined);
      setError(undefined);
      setSaved(true);
      keepSourceControlIntent(storageKey);
      cache.setQueryData(queryKey, current);
    },
    onError: (cause) => {
      const failure =
        cause instanceof Error ? cause : new Error('Could not save execution controls.');
      setError(failure);
      if (sourceControlWriteUnknown(cause)) setOutcome('unknown');
      else if (cause instanceof APIError && cause.status === 409) setOutcome('conflict');
      else {
        setIntent(undefined);
        keepSourceControlIntent(storageKey);
      }
    },
  });
  const busy = mutation.isPending || reading;
  const writable =
    observed.can_manage && !observed.tombstoned && Boolean(client?.updateSourceControls);
  const dirty = !sourceControlValuesEqual(observed, draft);
  function save() {
    if (busy || !writable || outcome || !dirty) return;
    const request: SourceControlIntent = {
      source_uid: observed.source_uid,
      source_mode: observed.source_mode,
      version: observed.version,
      schedule_paused: draft.schedule_paused,
      execution_paused: draft.execution_paused,
    };
    setIntent(request);
    setChecked(false);
    setSaved(false);
    setError(undefined);
    keepSourceControlIntent(storageKey, request);
    mutation.mutate(request);
  }
  async function refresh() {
    if (busy) return;
    setReading(true);
    setSaved(false);
    try {
      const current = await read();
      if (
        current.source_uid !== observed.source_uid ||
        current.source_mode !== observed.source_mode
      )
        throw new Error('The execution source changed. Reopen its current definition.');
      setObserved(current);
      setChecked(true);
      setError(undefined);
      if (!outcome) setDraft(current);
    } catch (cause) {
      setError(cause instanceof Error ? cause : new Error('Could not read execution controls.'));
    } finally {
      setReading(false);
    }
  }
  function useCurrent() {
    if (!checked || busy) return;
    setDraft(observed);
    setIntent(undefined);
    setOutcome(undefined);
    setError(undefined);
    setChecked(false);
    keepSourceControlIntent(storageKey);
  }
  const disabled = busy || !writable || Boolean(outcome);
  return (
    <div className="execution-control-form">
      {kind !== 'functions' && (
        <div className="execution-control-row">
          <div className="execution-control-label">
            <Text as="label" size="2" htmlFor={`${fields}-schedule`}>
              Pause schedule
            </Text>
            <Text as="p" size="1" color="gray">
              Manual starts and accepted runs remain available.
            </Text>
          </div>
          <Switch
            id={`${fields}-schedule`}
            checked={draft.schedule_paused}
            disabled={disabled}
            onCheckedChange={(value) => {
              setDraft({ ...draft, schedule_paused: value });
              setSaved(false);
            }}
          />
        </div>
      )}
      <div className="execution-control-row">
        <div className="execution-control-label">
          <Text as="label" size="2" htmlFor={`${fields}-execution`}>
            Pause tasks
          </Text>
          <Text as="p" size="1" color="gray">
            New starts and undispatched tasks pause. Running tasks continue.
          </Text>
        </div>
        <Switch
          id={`${fields}-execution`}
          checked={draft.execution_paused}
          disabled={disabled}
          onCheckedChange={(value) => {
            setDraft({ ...draft, execution_paused: value });
            setSaved(false);
          }}
        />
      </div>
      {outcome && intent && (
        <div className="execution-control-review" role="status">
          <Text as="p" size="2" weight="medium">
            {outcome === 'unknown' ? 'Save result not confirmed' : 'Controls changed before saving'}
          </Text>
          <Text as="p" size="1" color="gray">
            Check current controls before making another change.
          </Text>
          <details className="execution-control-request">
            <summary>Requested version</summary>
            <Text as="p" size="1" color="gray">
              Version {intent.version}
            </Text>
          </details>
          {checked && (
            <Text as="p" size="2">
              {sourceControlValuesEqual(observed, intent)
                ? 'Current controls match your changes.'
                : 'Current controls differ from your changes.'}
            </Text>
          )}
          <Flex gap="3" mt="3">
            <Button
              type="button"
              size="1"
              variant="outline"
              color="gray"
              disabled={busy}
              onClick={() => void refresh()}
            >
              Check current controls
            </Button>
            {checked && (
              <Button
                type="button"
                size="1"
                variant="soft"
                color="gray"
                disabled={busy}
                onClick={useCurrent}
              >
                Use current controls
              </Button>
            )}
          </Flex>
        </div>
      )}
      {error && <ErrorState error={error} />}
      {!outcome && (
        <Flex justify="between" align="center" gap="3" mt="3">
          <Text size="1" color="gray" role="status">
            {observed.tombstoned
              ? 'Source deleted'
              : saved
                ? 'Controls saved'
                : !observed.can_manage
                  ? 'Read access'
                  : ''}
          </Text>
          <Flex gap="3">
            <Button
              type="button"
              size="1"
              variant="ghost"
              color="gray"
              disabled={busy}
              onClick={() => void refresh()}
            >
              Refresh controls
            </Button>
            {writable && (
              <Button
                type="button"
                size="1"
                disabled={busy || !dirty}
                loading={mutation.isPending}
                onClick={save}
              >
                Save controls
              </Button>
            )}
          </Flex>
        </Flex>
      )}
    </div>
  );
}
