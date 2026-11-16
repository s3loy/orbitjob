import { randomUUID } from '../uuid';
import { useEffect, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Flex, Heading, Select, Text, TextArea, TextField } from '@radix-ui/themes';
import { Link, useLocation, useParams } from 'react-router-dom';
import { useSession } from '../session';
import { ErrorState, Loading, PageHeading } from '../components';
import { Back, Facts } from '../monitoring-ui';
import { returnState, useBack } from '../monitoring-state';
import { M4Action } from '../m4-actions';
import {
  functionInput,
  reconcileFunctionDraft,
  saveFunctionDraft,
  setFunctionDraft,
  useFunctionDraft,
} from '../function-management-state';
import type { FunctionInput } from '../api/m4';
import { ContainerImageField } from '../ContainerImageField';
import { observationDelay } from '../api/transport';
import { useAdministrationAuthenticationError } from '../administration-state';
import { withCredential } from '../credential-requests';
import { useObservationWindow } from '../observation-window';
import { M4Time, ContainerEntries } from '../m4-ui';
import '../manifest.css';
import { ExecutionResourcesFields } from '../ExecutionResourcesFields';

export function FunctionEditor() {
  const { id = 'new' } = useParams();
  const location = useLocation();
  const back = useBack(id === 'new' ? '/functions' : `/functions/${id}`);
  const { client, generation } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const observation = useObservationWindow();
  const cache = useQueryClient();
  const draft = useFunctionDraft(client!, id);
  const [localError, setLocalError] = useState<Error>();
  const [labelsError, setLabelsError] = useState(false);
  const [review, setReview] = useState(false);
  const nameField = useRef<HTMLInputElement>(null);
  const advancedSettings = useRef<HTMLDetailsElement>(null);
  const labelsField = useRef<HTMLTextAreaElement>(null);
  const reviewHeading = useRef<HTMLHeadingElement>(null);
  const saveHeading = useRef<HTMLHeadingElement>(null);
  const saveOutcome = draft.saved ? 'saved' : draft.unknown ? 'unknown' : undefined;
  useEffect(() => {
    if (saveOutcome) saveHeading.current?.focus();
    else if (review) reviewHeading.current?.focus();
  }, [review, saveOutcome]);
  const input = draft.input;
  const locked = Boolean(rejected || draft.pending || draft.unknown || draft.saved);
  const existing = useQuery({
    queryKey: ['function-edit', generation, id],
    queryFn: () => withCredential(client!, () => client!.functionDefinition(id)),
    enabled: !rejected && id !== 'new' && !draft.expectedVersion,
  });
  useEffect(() => {
    if (existing.data && !draft.expectedVersion && !draft.pending && !draft.unknown)
      setFunctionDraft(client!, id, {
        ...draft,
        input: functionInput(existing.data),
        expectedVersion: existing.data.version,
      });
  }, [client, id, existing.data, draft]);
  const readiness = useQuery({
    queryKey: ['function-saved', generation, draft.saved?.id, draft.saved?.version],
    queryFn: () =>
      withCredential(client!, () => client!.functionDefinition(String(draft.saved!.id))),
    enabled: Boolean(draft.saved && !rejected),
    refetchInterval: (state) =>
      observation.paused || state.state.data?.ready || draft.saved?.status === 'paused' || rejected
        ? false
        : observationDelay(state.state.error, observation.since),
  });
  const saved = readiness.data ?? draft.saved;
  const observationError = rejected ?? readiness.error;
  const ready = Boolean(
    !observationError &&
    saved?.ready &&
    saved.version === draft.saved?.version &&
    saved.active_revision_version === draft.saved?.version,
  );
  async function reloadCurrent() {
    try {
      const current = await withCredential(client!, () => client!.functionDefinition(id));
      setFunctionDraft(client!, id, {
        input: functionInput(current),
        expectedVersion: current.version,
        requestID: randomUUID(),
      });
      setReview(false);
      setLocalError(undefined);
      setLabelsError(false);
    } catch (cause) {
      setLocalError(
        cause instanceof Error ? cause : new Error('Unable to reload the current definition.'),
      );
    }
  }
  function change<K extends keyof FunctionInput>(key: K, value: FunctionInput[K]) {
    if (locked) return;
    setFunctionDraft(client!, id, {
      ...draft,
      input: { ...input, [key]: value },
      error: undefined,
    });
    setReview(false);
    setLocalError(undefined);
  }
  function arrayField(key: 'command' | 'args', label: string) {
    return (
      <section className="function-editor-entries">
        <Text as="p" size="2" weight="medium" mb="2">
          {label}
        </Text>
        {input[key].map((value, index) => (
          <Flex key={index} gap="2" mb="2">
            <TextArea
              aria-label={`${label} entry ${index + 1}`}
              value={value}
              rows={1}
              disabled={locked}
              onChange={(event) =>
                change(
                  key,
                  input[key].map((value, n) => (n === index ? event.target.value : value)),
                )
              }
            />
            <Button
              type="button"
              size="1"
              variant="ghost"
              disabled={locked}
              aria-label={`Remove ${label.toLowerCase()} entry ${index + 1}`}
              onClick={() =>
                change(
                  key,
                  input[key].filter((_, n) => n !== index),
                )
              }
            >
              Remove
            </Button>
          </Flex>
        ))}
        <Button
          type="button"
          size="1"
          variant="outline"
          disabled={locked}
          onClick={() => change(key, [...input[key], ''])}
        >
          {key === 'command' ? 'Add command' : 'Add argument'}
        </Button>
      </section>
    );
  }
  if (id !== 'new' && !draft.expectedVersion)
    return (
      <>
        <Back to={back}>Back to Function</Back>
        {rejected || existing.error ? (
          <ErrorState error={rejected ?? existing.error!} retry={() => void existing.refetch()} />
        ) : (
          <Loading />
        )}
      </>
    );
  return (
    <>
      <Back to={back}>Back to Functions</Back>
      <PageHeading title={id === 'new' ? 'Create a function' : `Edit ${input.name}`} />
      {localError && <ErrorState error={localError} />}
      {rejected && !draft.saved && <ErrorState error={rejected} />}
      {draft.saved ? (
        <>
          <section
            className="readiness function-save-status"
            aria-label="Function version readiness"
          >
            <Heading as="h2" size="3" weight="medium" ref={saveHeading} tabIndex={-1}>
              {observationError
                ? 'Saved. Readiness unavailable'
                : saved && saved.version !== draft.saved.version
                  ? 'A different version is now saved'
                  : draft.saved.status === 'paused'
                    ? 'Saved and paused'
                    : ready
                      ? 'Saved version is ready'
                      : observation.paused
                        ? 'Saved. Readiness check paused'
                        : 'Saved. Getting ready'}
            </Heading>
            <Text as="p" size="2" color="gray" mt="2">
              {draft.saved.name}
            </Text>
            {!observationError &&
              (saved?.version !== draft.saved.version ||
                draft.saved.status === 'paused' ||
                (!ready && observation.paused)) && (
                <Text as="p" size="2" color="gray" mt="2">
                  {saved?.version !== draft.saved.version
                    ? 'Open the function to review the current configuration before invoking it.'
                    : draft.saved.status === 'paused'
                      ? 'New invocations are paused. Existing executions continue.'
                      : 'Automatic checks stopped after two minutes. Check again to continue.'}
                </Text>
              )}
            {observationError && <ErrorState error={observationError} />}
            <Flex gap="3" wrap="wrap" mt="4">
              {ready && saved && (
                <M4Action kind="function" item={{ ...saved, version: draft.saved.version }} />
              )}
              <Button
                size="2"
                variant="outline"
                disabled={readiness.isFetching || !!rejected}
                onClick={() => {
                  observation.restart();
                  void readiness.refetch();
                }}
              >
                Check readiness again
              </Button>
              <Button asChild size="2" variant="outline">
                <Link to={`/functions/${draft.saved.id}`}>Open function</Link>
              </Button>
              {id !== 'new' && (
                <Button
                  size="2"
                  variant="ghost"
                  disabled={!!rejected}
                  onClick={() => void reloadCurrent()}
                >
                  Continue editing current version
                </Button>
              )}
            </Flex>
            <details className="readiness-details">
              <summary>Version details</summary>
              <Facts
                rows={[
                  ['Saved version', draft.saved.version],
                  ['Current version', observationError ? 'Unavailable' : saved?.version],
                  [
                    'Active execution version',
                    observationError
                      ? 'Unavailable'
                      : (saved?.active_revision_version ?? 'Not observed'),
                  ],
                  [
                    'Last checked (UTC)',
                    readiness.dataUpdatedAt ? (
                      <M4Time value={new Date(readiness.dataUpdatedAt).toISOString()} />
                    ) : (
                      'Not checked'
                    ),
                  ],
                ]}
              />
              <Text as="p" size="1" color="gray">
                Invocations from this page require the saved version to be ready.
              </Text>
            </details>
          </section>
          {id === 'new' && (
            <Button
              size="2"
              variant="ghost"
              disabled={!!rejected}
              onClick={() => {
                setFunctionDraft(client!, id, {
                  input: functionInput(),
                  requestID: randomUUID(),
                });
                setReview(false);
              }}
            >
              Create another function
            </Button>
          )}
        </>
      ) : (
        <form
          className="function-editor"
          onSubmit={(event) => {
            event.preventDefault();
            if (review || locked) return;
            setLocalError(undefined);
            setLabelsError(false);
            try {
              if (draft.labelsText !== undefined) {
                const value: unknown = JSON.parse(draft.labelsText);
                if (!value || Array.isArray(value) || typeof value !== 'object')
                  throw new Error('Labels must be a JSON object.');
                setFunctionDraft(client!, id, {
                  ...draft,
                  input: { ...input, labels: value as Record<string, unknown> },
                });
              }
              setReview(true);
            } catch {
              setLabelsError(true);
              if (advancedSettings.current) advancedSettings.current.open = true;
              labelsField.current?.focus();
            }
          }}
        >
          {!review && !draft.unknown && (
            <fieldset disabled={locked} className="workload-fieldset function-editor-fields">
              <section className="authoring-settings-group">
                <Heading as="h2" size="3" weight="medium">
                  General
                </Heading>
                <div className="manifest-fields">
                  <label className="form-field">
                    Name
                    <TextField.Root
                      ref={nameField}
                      aria-label="Function name"
                      required
                      maxLength={128}
                      value={input.name}
                      onChange={(event) => change('name', event.target.value)}
                    />
                  </label>
                  <label className="form-field">
                    Description
                    <TextArea
                      aria-label="Function description"
                      rows={2}
                      value={input.description}
                      onChange={(event) => change('description', event.target.value)}
                    />
                  </label>
                </div>
              </section>
              <section className="authoring-settings-group">
                <Heading as="h2" size="3" weight="medium">
                  Container
                </Heading>
                <div>
                  <ContainerImageField
                    value={input.image}
                    onChange={(value) => change('image', value)}
                    disabled={locked}
                    required
                  />
                  <div className="manifest-fields">
                    {arrayField('command', 'Command')}
                    {arrayField('args', 'Arguments')}
                  </div>
                  {(input.command.length > 0 || input.args.length > 0) && (
                    <Text as="p" size="1" color="gray">
                      Each entry is passed to the container exactly as entered.
                    </Text>
                  )}
                </div>
              </section>
              <ExecutionResourcesFields
                version={input.execution_policy_version ?? 0}
                retrySafe={input.retry_safe ?? false}
                resources={input.resources ?? undefined}
                disabled={locked}
                change={(settings) => {
                  if (locked) return;
                  setFunctionDraft(client!, id, {
                    ...draft,
                    requestID: randomUUID(),
                    input: {
                      ...input,
                      execution_policy_version: settings.version,
                      retry_safe: settings.retrySafe,
                      resources: settings.resources,
                    },
                    error: undefined,
                  });
                  setReview(false);
                  setLocalError(undefined);
                }}
              />
              <section className="authoring-settings-group">
                <Heading as="h2" size="3" weight="medium">
                  Execution
                </Heading>
                <div>
                  <div className="manifest-fields">
                    <label className="form-field">
                      Timeout in seconds
                      <TextField.Root
                        aria-label="Timeout in seconds"
                        type="number"
                        min={1}
                        max={2147483647}
                        required
                        value={String(input.timeout_seconds)}
                        onChange={(event) => change('timeout_seconds', Number(event.target.value))}
                      />
                    </label>
                    <label className="form-field">
                      Retries after the first attempt
                      <TextField.Root
                        aria-label="Retries"
                        type="number"
                        min={0}
                        max={2147483646}
                        required
                        value={String(input.retry_limit)}
                        onChange={(event) => change('retry_limit', Number(event.target.value))}
                      />
                    </label>
                    <label className="form-field">
                      Invocation state
                      <Select.Root
                        disabled={locked}
                        value={input.status}
                        onValueChange={(value) => change('status', value)}
                      >
                        <Select.Trigger aria-label="Invocation state" />
                        <Select.Content>
                          <Select.Item value="active">Active</Select.Item>
                          <Select.Item value="paused">Paused</Select.Item>
                        </Select.Content>
                      </Select.Root>
                    </label>
                  </div>
                  {input.status === 'paused' && (
                    <Text as="p" size="1" color="gray">
                      New invocations are paused. Existing invocations continue.
                    </Text>
                  )}
                </div>
              </section>
              <details className="advanced-details" ref={advancedSettings}>
                <summary>Advanced settings</summary>
                <div className="manifest-fields">
                  {(['history_success', 'history_failed'] as const).map((key) => (
                    <label key={key} className="form-field">
                      {key === 'history_success'
                        ? 'Retain successful invocations'
                        : 'Retain failed invocations'}
                      <TextField.Root
                        type="number"
                        min={0}
                        required
                        value={String(input[key])}
                        onChange={(event) => change(key, Number(event.target.value))}
                      />
                    </label>
                  ))}
                </div>
                <label className="form-field">
                  Labels JSON
                  <TextArea
                    ref={labelsField}
                    aria-label="Labels JSON"
                    aria-invalid={labelsError || undefined}
                    aria-describedby={labelsError ? 'function-labels-error' : undefined}
                    rows={4}
                    value={draft.labelsText ?? JSON.stringify(input.labels, null, 2)}
                    onChange={(event) => {
                      setFunctionDraft(client!, id, {
                        ...draft,
                        labelsText: event.target.value,
                        error: undefined,
                      });
                      setLocalError(undefined);
                      setLabelsError(false);
                      setReview(false);
                    }}
                  />
                  {labelsError && (
                    <Text id="function-labels-error" size="1" color="red" role="alert">
                      Labels must be a valid JSON object.
                    </Text>
                  )}
                </label>
              </details>
            </fieldset>
          )}
          {draft.error && <ErrorState error={draft.error} />}
          {draft.unknown && (
            <section className="readiness" role="status">
              <Heading as="h2" size="3" ref={saveHeading} tabIndex={-1}>
                Save outcome unknown
              </Heading>
              <Text as="p" size="2" mt="2">
                The server may have saved this configuration. Check the result before making another
                change.
              </Text>
              <Text as="p" size="1" mt="2">
                Request {draft.requestID}
              </Text>
              <Button
                type="button"
                size="2"
                mt="3"
                disabled={draft.pending || !!rejected}
                onClick={async () => {
                  const result = await (id === 'new'
                    ? saveFunctionDraft(client!, id)
                    : reconcileFunctionDraft(client!, id));
                  if (result) observation.restart();
                }}
              >
                {id === 'new' ? 'Retry the same creation request' : 'Check stored definition'}
              </Button>
              {id !== 'new' && (
                <>
                  <Text as="p" size="1" mt="3">
                    If the saved result cannot be confirmed, load the current configuration and
                    review it before making another change. Reloading does not undo an earlier save.
                  </Text>
                  <Button
                    type="button"
                    size="2"
                    variant="outline"
                    mt="3"
                    disabled={draft.pending || !!rejected}
                    onClick={() => void reloadCurrent()}
                  >
                    Load current configuration for review
                  </Button>
                </>
              )}
            </section>
          )}
          {review && !draft.unknown && (
            <section
              className="function-editor-group function-editor-review"
              aria-label="Review Function save"
            >
              <Heading as="h2" size="3" weight="medium" ref={reviewHeading} tabIndex={-1}>
                Review before saving
              </Heading>
              <Facts
                rows={[
                  ['Function', input.name],
                  ...(input.description
                    ? [['Description', input.description] as [string, string]]
                    : []),
                  ['Image', <code>{input.image}</code>],
                  ['Command', <ContainerEntries entries={input.command} empty="Image default" />],
                  ['Arguments', <ContainerEntries entries={input.args} />],
                  ['Timeout', `${input.timeout_seconds} seconds`],
                  ['Retries', input.retry_limit],
                  ['Invocations', input.status === 'paused' ? 'Paused' : 'Enabled'],
                ]}
              />
              <details className="advanced-details function-review-details">
                <summary>Resources, history and version</summary>
                <Facts
                  rows={[
                    ['Safe to repeat', input.retry_safe ? 'Yes' : 'No'],
                    ...Object.entries(input.resources ?? {}).flatMap(([group, values]) =>
                      Object.entries(values ?? {}).map(
                        ([name, quantity]) =>
                          [
                            `${name === 'cpu' ? 'CPU' : name === 'memory' ? 'Memory' : 'Temporary storage'} ${group === 'requests' ? 'request' : 'limit'}`,
                            quantity,
                          ] as [string, string],
                      ),
                    ),
                    ['Expected version', draft.expectedVersion ?? 'New definition'],
                    ['Successful invocations to retain', input.history_success],
                    ['Failed invocations to retain', input.history_failed],
                    [
                      'Labels',
                      Object.keys(input.labels).length ? (
                        <code>{JSON.stringify(input.labels)}</code>
                      ) : (
                        'None'
                      ),
                    ],
                  ]}
                />
              </details>
              <Text as="p" size="2" color="gray">
                {input.status === 'paused'
                  ? 'Saving creates a new version with new invocations paused.'
                  : 'Saving creates a new version. It becomes callable when ready.'}
              </Text>
              <Flex gap="3" mt="4" wrap="wrap" justify="end">
                <Button
                  type="button"
                  size="2"
                  variant="outline"
                  disabled={draft.pending}
                  onClick={() => {
                    setReview(false);
                    requestAnimationFrame(() => nameField.current?.focus());
                  }}
                >
                  Keep editing
                </Button>
                <Button
                  type="button"
                  size="2"
                  disabled={draft.pending || !!rejected}
                  onClick={async () => {
                    const result = await saveFunctionDraft(client!, id);
                    if (result) {
                      observation.restart();
                      void cache.invalidateQueries({ queryKey: ['m4'] });
                    }
                  }}
                >
                  {draft.pending ? 'Saving…' : 'Confirm save'}
                </Button>
              </Flex>
            </section>
          )}
          {!review && !draft.unknown && (
            <Flex gap="3" className="form-actions" justify="end">
              <Button asChild size="2" variant="outline">
                <Link to={back} state={returnState(location.state, back)}>
                  Cancel
                </Link>
              </Button>
              <Button type="submit" size="2" disabled={locked}>
                Review function
              </Button>
            </Flex>
          )}
          {id !== 'new' && !review && !draft.pending && !draft.unknown && (
            <Button
              type="button"
              size="1"
              variant="ghost"
              mt="4"
              disabled={!!rejected}
              onClick={() => void reloadCurrent()}
            >
              Discard edits and reload current version
            </Button>
          )}
        </form>
      )}
    </>
  );
}
