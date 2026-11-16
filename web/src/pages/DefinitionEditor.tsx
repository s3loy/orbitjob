import { useEffect, useRef, useState } from 'react';
import { Button, Dialog, Flex, Heading, Text, TextArea, TextField } from '@radix-ui/themes';
import { useLocation, useParams, useSearchParams } from 'react-router-dom';
import { PageHeading, ErrorState } from '../components';
import { Back, Facts } from '../monitoring-ui';
import { collectionPath } from '../monitoring-state';
import { useSession } from '../session';
import {
  definitionOperation,
  definitionCommandInputError,
  acceptDefinitionMerge,
  chooseDefinitionMerge,
  keepDraftAgainstLive,
  setDefinitionDraft,
  unresolved,
  useDefinitionDraft,
} from '../definition-state';
import { initialDraft, manifestSpecJSON } from '../api/manifests';
import type { DefinitionKind } from '../api/definitions';
import '../manifest.css';
import { mergeResolved } from '../definition-merge';
import { DefinitionReadiness } from '../DefinitionReadiness';
import { DefinitionFields } from '../DefinitionFields';
import { DefinitionReview } from '../DefinitionReview';
import { APIError, OBSERVATION_MS } from '../api/transport';
import { useAdministrationAuthenticationError } from '../administration-state';
import { JobBehaviorPreview } from '../JobBehaviorPreview';
import { focusJobSection, type JobSection } from '../job-authoring-focus';
import '../job-journey.css';
import type { ExecutionCommandInput } from '../execution-command';

export function DefinitionEditor({
  embedded = false,
  definitionKind,
}: {
  embedded?: boolean;
  definitionKind?: DefinitionKind;
}) {
  const { kind: routeKind } = useParams();
  const location = useLocation();
  const kind: DefinitionKind = definitionKind ?? (routeKind === 'workflows' ? 'workflows' : 'jobs');
  const { client, mode } = useSession();
  const sample = mode === 'demo';
  const rejected = useAdministrationAuthenticationError(client);
  const draft = useDefinitionDraft(client!, kind);
  const [params] = useSearchParams();
  const [confirm, setConfirm] = useState(false);
  const [discard, setDiscard] = useState(false);
  const [reload, setReload] = useState(false);
  const [resolve, setResolve] = useState(false);
  const keep = useRef<HTMLButtonElement>(null);
  const reviewHeading = useRef<HTMLHeadingElement>(null);
  const checkButton = useRef<HTMLButtonElement>(null);
  const saveButton = useRef<HTMLButtonElement>(null);
  const checkingFromButton = useRef(false);
  const recovery = useRef({ requestID: '', until: 0, attempts: 0 });
  const authoring = useRef<HTMLDivElement>(null);
  const pending = unresolved(draft);
  const blocked = pending || Boolean(rejected);
  const commandError = kind === 'jobs' ? definitionCommandInputError(draft) : '';
  const workload = kind === 'jobs' ? 'job' : 'workflow';
  const reviewed =
    !commandError && !draft.receipt && !draft.merge && (!draft.error || draft.unknown)
      ? draft.checked?.spec_json
      : undefined;
  const canSave = Boolean(reviewed && !blocked && !draft.conflict && !draft.error && !commandError);
  useEffect(() => {
    if (draft.busy || !checkingFromButton.current) return;
    checkingFromButton.current = false;
    if (
      document.activeElement === document.body ||
      document.activeElement === checkButton.current
    ) {
      (canSave ? saveButton.current : checkButton.current)?.focus();
    }
  }, [canSave, draft.busy]);
  useEffect(() => {
    if (embedded) reviewHeading.current?.focus();
  }, [embedded]);
  useEffect(() => {
    if (!draft.unknown || !draft.requestID || draft.busy || rejected) return;
    if (recovery.current.requestID !== draft.requestID)
      recovery.current = {
        requestID: draft.requestID,
        until: Date.now() + OBSERVATION_MS,
        attempts: 0,
      };
    const remaining = recovery.current.until - Date.now();
    if (remaining <= 0) return;
    const delay = Math.min(
      remaining,
      Math.max(
        draft.error instanceof APIError ? draft.error.retryAfter : 0,
        Math.min(15000, 2000 * 2 ** recovery.current.attempts),
      ),
    );
    const timer = setTimeout(() => {
      recovery.current.attempts++;
      void definitionOperation(client!, kind, 'reconcile');
    }, delay);
    return () => clearTimeout(timer);
  }, [client, kind, draft.unknown, draft.requestID, draft.busy, draft.error, rejected]);
  useEffect(() => {
    if (draft.namespace || draft.name || draft.spec !== '{}') return;
    const initial = initialDraft(kind === 'jobs' ? 'ScheduledJob' : 'WorkflowJob');
    setDefinitionDraft(client!, kind, {
      namespace:
        params.get('namespace') ??
        (sample ? (kind === 'jobs' ? 'platform' : 'finance-prod') : initial.namespace),
      name: params.get('name') ?? initial.name,
      spec: params.has('name') ? '{}' : manifestSpecJSON(initial),
      message: params.has('name')
        ? 'Load the existing definition before editing its spec.'
        : undefined,
    });
  }, [client, kind, params, draft.namespace, draft.name, draft.spec, sample]);
  const identity = Boolean(draft.namespace && draft.name);
  const requestedName = params.get('name');
  const waitingForLoad = !embedded && !!requestedName && !draft.loaded && draft.spec === '{}';
  useEffect(() => {
    if (
      !embedded &&
      !rejected &&
      requestedName &&
      identity &&
      !draft.loaded &&
      draft.spec === '{}' &&
      !pending &&
      !draft.error
    )
      void definitionOperation(client!, kind, 'load');
  }, [
    client,
    kind,
    embedded,
    requestedName,
    identity,
    draft.loaded,
    draft.spec,
    draft.error,
    pending,
    rejected,
  ]);
  const retainedTarget =
    requestedName && (requestedName !== draft.name || params.get('namespace') !== draft.namespace);
  function edit(
    key: 'namespace' | 'name' | 'spec',
    value: string,
    commandInput: ExecutionCommandInput | null = draft.commandInput ?? null,
  ) {
    if (blocked) return;
    setDefinitionDraft(client!, kind, {
      ...draft,
      [key]: value,
      commandInput: commandInput ?? undefined,
      merge: undefined,
      checked: undefined,
      receipt: undefined,
      requestID: undefined,
      error: undefined,
      message: undefined,
      ...(key !== 'spec' ? { loaded: undefined } : {}),
    });
  }
  function editSection(section: JobSection) {
    const command =
      section === 'container' && commandError
        ? authoring.current?.querySelector<HTMLTextAreaElement>('textarea[aria-label="Command"]')
        : undefined;
    if (command) {
      command.focus();
      return;
    }
    let validObject: boolean;
    try {
      const value: unknown = JSON.parse(draft.spec);
      validObject = value !== null && typeof value === 'object' && !Array.isArray(value);
    } catch {
      validObject = false;
    }
    if (!validObject || !focusJobSection(authoring.current, section)) {
      const details = authoring.current?.querySelector<HTMLDetailsElement>('.definition-raw');
      if (details) {
        details.open = true;
        details.querySelector('textarea')?.focus();
      }
      return;
    }
  }
  const rawSpec = (
    <details className="advanced-details definition-raw">
      <summary>Advanced spec JSON</summary>
      {commandError ? (
        <Text as="p" size="2">
          Finish the Command field before viewing or editing its generated JSON.
        </Text>
      ) : (
        <TextArea
          aria-label="Definition spec JSON"
          className="manifest-yaml"
          rows={22}
          value={draft.spec}
          disabled={blocked}
          onChange={(e) => edit('spec', e.target.value, null)}
        />
      )}
    </details>
  );
  return (
    <div
      className={`definition-editor${!embedded && kind === 'jobs' ? ' job-authoring' : ''}`}
      ref={authoring}
    >
      {!embedded && (
        <Back to={collectionPath(location, kind)}>
          Back to {kind === 'jobs' ? 'Jobs' : 'Workflows'}
        </Back>
      )}
      {!embedded && (
        <PageHeading
          title={kind === 'jobs' ? 'Job definition' : 'Workflow definition'}
          action={
            <Flex gap="3" align="center">
              <Button
                size="1"
                variant="ghost"
                color="gray"
                disabled={Boolean(draft.busy) || Boolean(rejected)}
                onClick={() => setDiscard(true)}
              >
                Discard draft
              </Button>
              <Button
                size="2"
                disabled={!identity || waitingForLoad || Boolean(commandError)}
                onClick={() => reviewHeading.current?.focus()}
              >
                Review changes
              </Button>
            </Flex>
          }
        />
      )}
      {retainedTarget && (
        <section className="m4-notice" role="status">
          <Text as="p" size="2">
            Your draft for {draft.name} in {draft.namespace} is still open.
          </Text>
          <Text as="p" size="1" color="gray">
            Discard this draft to open {requestedName} in {params.get('namespace')}.
          </Text>
        </section>
      )}
      {commandError && (
        <section className="configuration-errors" role="alert" aria-label="Command needs attention">
          <Text as="p" size="2">
            {commandError} Finish the Command field before reviewing, checking, or saving.
          </Text>
          <Button size="1" variant="outline" mt="3" onClick={() => editSection('container')}>
            Edit command
          </Button>
        </section>
      )}
      {!embedded && (
        <section className="authoring-settings-group" data-job-section="general">
          <Heading as="h2" size="3" weight="medium">
            General
          </Heading>
          <div>
            <div className="manifest-fields">
              {(['name', 'namespace'] as const).map((key) => (
                <label className="manifest-field" key={key}>
                  <Text size="1" weight="medium">
                    {key === 'name' ? 'Name' : 'Namespace'}
                  </Text>
                  <TextField.Root
                    aria-label={key === 'name' ? 'Definition name' : 'Definition namespace'}
                    value={draft[key]}
                    disabled={blocked}
                    onChange={(e) => edit(key, e.target.value)}
                  />
                </label>
              ))}
            </div>
            <Flex gap="3" wrap="wrap">
              <Button
                size="1"
                color="gray"
                variant="outline"
                disabled={!identity || blocked}
                onClick={() => setReload(true)}
              >
                {draft.loaded ? 'Reload live definition' : 'Load existing definition'}
              </Button>
            </Flex>
          </div>
        </section>
      )}
      {draft.loaded && (
        <details className="advanced-details">
          <summary>Resource identity and version</summary>
          <Facts
            rows={[
              ['Resource UID', <code>{draft.loaded.uid}</code>],
              ['Resource version', draft.loaded.resource_version],
              ['Generation', draft.loaded.generation],
            ]}
          />
        </details>
      )}
      {!embedded && (
        <>
          {kind === 'jobs' && (
            <a className="job-preview-jump" href="#job-edit-preview">
              Preview how this job will run
            </a>
          )}
          <div className={kind === 'jobs' ? 'job-authoring-layout' : undefined}>
            <div className="job-authoring-form">
              {draft.spec !== '{}' && (
                <DefinitionFields
                  key={`${draft.namespace}/${draft.name}/${draft.loaded?.uid ?? ''}/${draft.loaded?.resource_version ?? ''}`}
                  kind={kind}
                  namespace={draft.namespace}
                  spec={draft.spec}
                  disabled={blocked}
                  change={(value) => edit('spec', value)}
                  commandInput={draft.commandInput}
                  changeCommand={(value, input) => edit('spec', value, input ?? null)}
                />
              )}
              {rawSpec}
            </div>
            {kind === 'jobs' && !rejected && (
              <aside className="job-authoring-preview" id="job-edit-preview">
                {commandError ? (
                  <section className="m4-notice" aria-label="Unfinished command">
                    <Heading as="h2" size="3">
                      Command is unfinished
                    </Heading>
                    <Text as="p" size="2" mt="2">
                      Finish the Command field to preview how this Job will run.
                    </Text>
                    <Button
                      size="1"
                      variant="outline"
                      mt="3"
                      onClick={() => editSection('container')}
                    >
                      Edit command
                    </Button>
                  </section>
                ) : (
                  <JobBehaviorPreview
                    namespace={draft.namespace}
                    name={draft.name}
                    spec={draft.spec}
                    context={draft.loaded ? 'edit' : 'create'}
                    onEdit={editSection}
                  />
                )}
              </aside>
            )}
          </div>
        </>
      )}
      {!waitingForLoad && (
        <section
          className="authoring-group definition-review"
          aria-labelledby="definition-review-title"
        >
          <Heading
            as="h2"
            size="4"
            weight="medium"
            id="definition-review-title"
            ref={reviewHeading}
            tabIndex={-1}
          >
            {reviewed
              ? 'Checked configuration'
              : draft.receipt
                ? 'Saved configuration'
                : 'Review configuration'}
          </Heading>
          {draft.receipt && !sample && <DefinitionReadiness kind={kind} receipt={draft.receipt} />}
          {draft.receipt && sample && (
            <Text as="p" size="2" color="gray" mb="3">
              Saved for this session.
            </Text>
          )}
          {reviewed && !draft.receipt && (
            <Text as="p" size="2" color="gray" mb="3">
              {sample
                ? 'Sample configuration checked. Save to keep it for this session.'
                : 'Checked against the cluster. Your changes have not been saved.'}
            </Text>
          )}
          {!commandError && (
            <DefinitionReview
              kind={kind}
              namespace={draft.namespace}
              name={draft.name}
              spec={draft.receipt?.spec_json ?? reviewed ?? draft.spec}
              showSaveNotice={!draft.receipt}
            />
          )}
          <Flex className="definition-action-bar" gap="3" align="center" wrap="wrap">
            <Button
              size="2"
              ref={checkButton}
              color={reviewed || draft.receipt ? 'gray' : 'blue'}
              variant={reviewed || draft.receipt ? 'outline' : 'solid'}
              disabled={!identity || blocked || Boolean(draft.conflict) || Boolean(commandError)}
              loading={draft.busy === 'check'}
              onClick={() => {
                if (commandError) return;
                checkingFromButton.current = document.activeElement === checkButton.current;
                void definitionOperation(client!, kind, 'check');
              }}
            >
              {sample ? 'Check sample definition' : 'Check against cluster'}
            </Button>
            {!draft.receipt && (
              <Button
                size="2"
                ref={saveButton}
                variant={canSave ? 'solid' : 'outline'}
                color={canSave ? 'blue' : 'gray'}
                disabled={!canSave}
                onClick={() => {
                  if (canSave) setConfirm(true);
                }}
              >
                Save definition
              </Button>
            )}
            {draft.busy === 'apply' && (
              <Text size="2" color="gray" role="status">
                Saving…
              </Text>
            )}
          </Flex>
        </section>
      )}
      {embedded && rawSpec}
      {(rejected || draft.error) && <ErrorState error={(rejected || draft.error)!} />}
      {draft.loaded &&
        !draft.unknown &&
        draft.error instanceof APIError &&
        draft.error.status === 409 && (
          <section className="m4-notice" role="status">
            <Text as="p" size="2">
              The live resource version changed. Your edits are still here.
            </Text>
            <Text as="p" size="1" color="gray" mt="2">
              Compare the live spec first. If only status changed, this refresh keeps your edits and
              updates the version safely.
            </Text>
            <Button
              mt="3"
              size="1"
              variant="outline"
              disabled={blocked}
              onClick={() => void definitionOperation(client!, kind, 'refresh')}
            >
              Refresh version and keep edits
            </Button>
          </section>
        )}
      {draft.conflict && (
        <section className="m4-notice" aria-label="Live definition conflict">
          <Text as="p" size="2" weight="medium">
            Review the changed live definition
          </Text>
          <details>
            <summary>Current live spec (version {draft.conflict.resource_version})</summary>
            <TextArea
              aria-label="Conflicting live spec"
              readOnly
              rows={10}
              value={draft.conflict.spec_json}
            />
          </details>
          <Text as="p" size="1" color="gray" mt="2">
            Your edited spec remains in the editor. No write has been sent.
          </Text>
          <Flex gap="3" mt="3" wrap="wrap">
            {draft.loaded?.uid === draft.conflict.uid && (
              <Button
                size="1"
                variant="outline"
                disabled={blocked}
                onClick={() => setResolve(true)}
              >
                Merge changes
              </Button>
            )}
            <Button size="1" variant="ghost" disabled={blocked} onClick={() => setReload(true)}>
              Replace draft with live spec
            </Button>
          </Flex>
        </section>
      )}
      {draft.merge && (
        <section className="m4-notice definition-merge" aria-label="Merged definition draft">
          <Heading as="h2" size="3">
            Merged draft
          </Heading>
          {draft.merge.conflicts.map((field) => (
            <div key={field.path} className="definition-merge-field">
              <Heading as="h3" size="2">
                <code>{field.path}</code>
              </Heading>
              <details>
                <summary>Original value</summary>
                <pre>{field.base}</pre>
              </details>
              <Flex gap="3" wrap="wrap" mt="2">
                <div>
                  <Text as="p" size="2">
                    Your value
                  </Text>
                  <pre>{field.local}</pre>
                  <Button
                    size="1"
                    variant={draft.merge?.choices[field.path] === 'local' ? 'solid' : 'outline'}
                    disabled={blocked}
                    onClick={() => chooseDefinitionMerge(client!, kind, field.path, 'local')}
                  >
                    Use my value
                  </Button>
                </div>
                <div>
                  <Text as="p" size="2">
                    Live value
                  </Text>
                  <pre>{field.live}</pre>
                  <Button
                    size="1"
                    variant={draft.merge?.choices[field.path] === 'live' ? 'solid' : 'outline'}
                    disabled={blocked}
                    onClick={() => chooseDefinitionMerge(client!, kind, field.path, 'live')}
                  >
                    Use live value
                  </Button>
                </div>
              </Flex>
            </div>
          ))}
          <details open={!draft.merge.conflicts.length}>
            <summary>Combined configuration</summary>
            <TextArea
              aria-label="Merged definition spec"
              readOnly
              rows={12}
              value={draft.merge.spec}
            />
          </details>
          <Button
            mt="3"
            disabled={blocked || !mergeResolved(draft.merge)}
            onClick={() => acceptDefinitionMerge(client!, kind)}
          >
            Use merged draft
          </Button>
        </section>
      )}
      {draft.message &&
        draft.message !== 'Cluster dry-run passed. No definition has been saved.' &&
        draft.message !==
          'Definition write accepted. Operator projection and execution are observed separately.' && (
          <section className="m4-notice" role="status">
            <Text as="p" size="2">
              {draft.message}
            </Text>
          </section>
        )}
      {draft.unknown && (
        <section className="m4-notice" role="status">
          <Text as="p" size="2">
            Save outcome unknown
          </Text>
          <Text as="p" size="1" color="gray">
            The cluster may have saved this definition. Keep this draft and check the result before
            taking another action.
          </Text>
          <Button
            size="1"
            color="gray"
            variant="outline"
            mt="3"
            loading={draft.busy === 'reconcile'}
            disabled={Boolean(draft.busy) || Boolean(rejected)}
            onClick={() => void definitionOperation(client!, kind, 'reconcile')}
          >
            Check save outcome
          </Button>
        </section>
      )}
      {(draft.checked || draft.receipt) && (
        <details className="advanced-details definition-request">
          <summary>Request details</summary>
          <Facts
            rows={[
              ['Write state', draft.receipt?.state ?? 'Validated only'],
              ['Request ID', <code>{draft.receipt?.request_id ?? draft.requestID}</code>],
              [
                'Manifest digest',
                <code>{draft.receipt?.manifest_digest ?? draft.checked?.manifest_digest}</code>,
              ],
            ]}
          />
        </details>
      )}
      <Dialog.Root open={confirm} onOpenChange={setConfirm}>
        <Dialog.Content
          maxWidth="640px"
          className="definition-confirm"
          onCloseAutoFocus={(event) => {
            event.preventDefault();
            if (!saveButton.current?.disabled) saveButton.current?.focus();
            else reviewHeading.current?.focus();
          }}
          onOpenAutoFocus={(e) => {
            e.preventDefault();
            keep.current?.focus();
          }}
        >
          <Dialog.Title>{draft.loaded ? 'Save changes?' : `Create this ${workload}?`}</Dialog.Title>
          <Dialog.Description size="2">
            {sample
              ? `Save the sample definition in ${draft.namespace} for this session.`
              : `Save the cluster-checked configuration to ${draft.namespace}.`}
          </Dialog.Description>
          {rejected && <ErrorState error={rejected} />}
          <DefinitionReview
            kind={kind}
            namespace={draft.namespace}
            name={draft.name}
            spec={draft.checked?.spec_json ?? '{}'}
          />
          <details className="advanced-details">
            <summary>Checked JSON and version protection</summary>
            <Facts
              rows={[
                ['Expected UID', draft.loaded?.uid || 'Create only'],
                ['Expected version', draft.loaded?.resource_version || 'No existing resource'],
                ['Digest', <code>{draft.checked?.manifest_digest}</code>],
              ]}
            />
            <Text as="p" size="1" color="gray" mb="2">
              {sample
                ? 'The sample saves this checked JSON.'
                : 'The save request sends this checked JSON, including any defaults supplied by the cluster.'}
            </Text>
            <TextArea
              aria-label="Checked definition spec"
              className="manifest-yaml"
              readOnly
              rows={12}
              value={draft.checked?.spec_json ?? ''}
            />
          </details>
          <Flex gap="3" justify="end" wrap="wrap" mt="4">
            <Dialog.Close>
              <Button ref={keep} size="1" color="gray" variant="soft">
                Keep reviewing
              </Button>
            </Dialog.Close>
            <Button
              size="2"
              color="blue"
              disabled={!canSave}
              onClick={() => {
                if (!canSave) return;
                setConfirm(false);
                void definitionOperation(client!, kind, 'apply');
              }}
            >
              Confirm save
            </Button>
          </Flex>
        </Dialog.Content>
      </Dialog.Root>
      <Dialog.Root open={resolve} onOpenChange={setResolve}>
        <Dialog.Content maxWidth="540px">
          <Dialog.Title>Merge these changes?</Dialog.Title>
          <Dialog.Description size="2">
            Changes to different fields will be combined. You will choose a value for each
            conflicting field before checking the merged draft.
          </Dialog.Description>
          <Flex gap="3" justify="end" wrap="wrap" mt="4">
            <Dialog.Close>
              <Button size="1" variant="soft">
                Keep comparing
              </Button>
            </Dialog.Close>
            <Button
              size="1"
              disabled={blocked}
              onClick={() => {
                keepDraftAgainstLive(client!, kind);
                setResolve(false);
              }}
            >
              Prepare merged draft
            </Button>
          </Flex>
        </Dialog.Content>
      </Dialog.Root>
      <Dialog.Root open={discard} onOpenChange={setDiscard}>
        <Dialog.Content maxWidth="450px">
          <Dialog.Title>Discard this draft?</Dialog.Title>
          <Dialog.Description size="2">
            Your local draft and its receipt will be cleared.
            {draft.unknown
              ? ' The cluster may have saved this definition. Record the request ID before discarding the draft.'
              : ''}
          </Dialog.Description>
          {draft.unknown && (
            <Facts
              rows={[
                ['Unconfirmed request', <code>{draft.requestID}</code>],
                ['Digest', <code>{draft.checked?.manifest_digest}</code>],
              ]}
            />
          )}
          <Flex gap="3" justify="end" mt="4">
            <Dialog.Close>
              <Button size="1" color="gray" variant="soft">
                Keep draft
              </Button>
            </Dialog.Close>
            <Button
              size="1"
              color="gray"
              disabled={Boolean(draft.busy) || Boolean(rejected)}
              onClick={() => {
                setDefinitionDraft(client!, kind, { namespace: '', name: '', spec: '{}' });
                setDiscard(false);
              }}
            >
              Discard draft
            </Button>
          </Flex>
        </Dialog.Content>
      </Dialog.Root>
      <Dialog.Root open={reload} onOpenChange={setReload}>
        <Dialog.Content maxWidth="450px">
          <Dialog.Title>Load the live definition?</Dialog.Title>
          <Dialog.Description size="2">
            The live definition will replace your draft. Any unsaved edits will be lost.
          </Dialog.Description>
          {rejected && <ErrorState error={rejected} />}
          <Flex gap="3" justify="end" mt="4">
            <Dialog.Close>
              <Button size="1" color="gray" variant="soft">
                Keep editing
              </Button>
            </Dialog.Close>
            <Button
              size="1"
              color="gray"
              disabled={blocked}
              onClick={() => {
                setReload(false);
                void definitionOperation(client!, kind, 'load');
              }}
            >
              Load live spec
            </Button>
          </Flex>
        </Dialog.Content>
      </Dialog.Root>
    </div>
  );
}
