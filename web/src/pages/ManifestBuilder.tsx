import { useEffect, useRef, useState } from 'react';
import {
  Button,
  Checkbox,
  Dialog,
  Flex,
  Heading,
  Select,
  Table,
  Tabs,
  Text,
  TextArea,
  TextField,
} from '@radix-ui/themes';
import { Link, useLocation, useParams } from 'react-router-dom';
import { Back } from '../monitoring-ui';
import { collectionPath } from '../monitoring-state';
import { ErrorState, PageHeading } from '../components';
import {
  getDefinitionDraft,
  setDefinitionDraft,
  unresolved,
  useDefinitionDraft,
} from '../definition-state';
import { DefinitionEditor } from './DefinitionEditor';
import { manifestSpecJSON } from '../api/manifests';
import { useSession } from '../session';
import { WorkflowSchedulePreview } from '../WorkflowSchedulePreview';
import { ContainerImageField } from '../ContainerImageField';
import { WorkloadAssistant } from '../WorkloadAssistant';
import { NamespaceField } from '../NamespaceField';
import { WorkflowJobPicker } from '../WorkflowJobPicker';
import {
  administrationAuthenticationError,
  useAdministrationAuthenticationError,
} from '../administration-state';
import { copyText, selectTextArea } from '../clipboard';
import { importDefinitionSpec, type ManifestImportPreview } from '../api/manifest-import';
import { previewManifestImportAsync } from '../api/manifest-import-async';
import {
  checkDraft,
  initialDraft,
  manifestYAML,
  type Draft,
  type ManifestKind,
  type Task,
} from '../api/manifests';
import '../manifest.css';
import { ExecutionResourcesFields } from '../ExecutionResourcesFields';
import { ExecutionCommandFields } from '../ExecutionCommandFields';
import { RuntimePresetField } from '../RuntimePresetField';
import { runtimePresetDraft } from '../runtime-presets';
import { ScheduleGenerator } from '../ScheduleGenerator';
import { executionCommandInputError, type ExecutionCommandInput } from '../execution-command';
import { JobStartingPoint } from '../JobStartingPoint';
import { JobBehaviorPreview } from '../JobBehaviorPreview';
import { focusJobSection, type JobSection } from '../job-authoring-focus';
import { namespaceCatalogKnown } from '../workspace';
import '../job-journey.css';

type BuilderDraft = {
  draft: Draft;
  reviewedSpec?: string;
  reviewedFormSpec?: string;
  sourceLabel?: string;
  startingNew?: boolean;
  commandInput?: ExecutionCommandInput;
};
const savedDrafts = new WeakMap<object, Map<ManifestKind, BuilderDraft>>();
const pendingYAML = new WeakMap<object, Map<ManifestKind, string>>();

function newBuilderDraft(kind: ManifestKind, sample: boolean, namespace = ''): Draft {
  const initial = initialDraft(kind);
  if (kind === 'ScheduledJob') {
    return {
      ...initial,
      ...runtimePresetDraft(),
      name: sample ? 'hello-orbitjob' : '',
      namespace: sample ? 'platform' : namespace,
      schedule: '',
    };
  }
  return sample
    ? { ...initial, namespace: kind === 'WorkflowJob' ? 'finance-prod' : 'platform' }
    : {
        ...initial,
        name: '',
        namespace,
        image: '',
        command: [],
        args: [],
        schedule: '',
        tasks: [{ name: '', job: '', dependencies: [], combination: '', rules: [] }],
      };
}

export function ManifestBuilder() {
  const { kind = 'job' } = useParams();
  const { generation } = useSession();
  return (
    <Builder
      key={`${kind}/${generation}`}
      kind={kind === 'workflow' ? 'WorkflowJob' : 'ScheduledJob'}
    />
  );
}
function Builder({ kind }: { kind: ManifestKind }) {
  const location = useLocation();
  const { client, mode, identity } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const definitionKind = kind === 'ScheduledJob' ? 'jobs' : 'workflows';
  const online = useDefinitionDraft(client!, definitionKind);
  const [builder, setBuilder] = useState<BuilderDraft>(
    () =>
      savedDrafts.get(client!)?.get(kind) ?? {
        startingNew: kind === 'ScheduledJob',
        draft: newBuilderDraft(
          kind,
          mode === 'demo',
          kind === 'ScheduledJob' &&
            namespaceCatalogKnown(identity) &&
            identity!.workload_namespaces.length === 1
            ? identity!.workload_namespaces[0]
            : '',
        ),
      },
  );
  const draft = builder.draft;
  const [tab, setTab] = useState('configuration');
  const [checked, setChecked] = useState(false);
  const [failedChecks, setFailedChecks] = useState(0);
  const [notice, setNotice] = useState('');
  const [conflictingDraft, setConflictingDraft] = useState(false);
  const [selected, setSelected] = useState(0);
  const [commandEpoch, setCommandEpoch] = useState(0);
  const [leaving, setLeaving] = useState(false);
  const [yamlInput, setYAMLInput] = useState<string | null>(() =>
    client ? (pendingYAML.get(client)?.get(kind) ?? null) : null,
  );
  const [yamlError, setYAMLError] = useState('');
  const [importing, setImporting] = useState(false);
  const [cleanupPreview, setCleanupPreview] = useState<ManifestImportPreview | null>(null);
  const importController = useRef<AbortController | null>(null);
  useEffect(() => () => importController.current?.abort(), []);
  const validation = useRef<HTMLDivElement>(null);
  const authoring = useRef<HTMLDivElement>(null);
  const issues = checked || failedChecks ? checkDraft(draft) : [];
  const commandError =
    kind === 'ScheduledJob' ? executionCommandInputError(builder.commandInput, draft) : '';
  const valid = checked && !issues.length && !commandError;
  const yaml = manifestYAML(draft);
  const editorYAML = yamlInput ?? yaml;
  const yamlChanged = yamlInput !== null && yamlInput !== yaml;
  const sameTarget = online.name === draft.name && online.namespace === draft.namespace;
  const onlineChanged =
    sameTarget && builder.reviewedSpec !== undefined && online.spec !== builder.reviewedSpec;
  let onlineIssue = '';
  if (onlineChanged && !unresolved(online)) {
    try {
      const projected = importDefinitionSpec(online.spec, kind, draft.namespace, draft.name);
      // A new external spec replaces its form projection before React commits this
      // render. The full source travels with it, including admission defaults.
      remember(projected, online.spec, manifestSpecJSON(projected), builder.startingNew, null);
      setCommandEpoch((epoch) => epoch + 1);
      setSelected(0);
    } catch (error) {
      onlineIssue = error instanceof Error ? error.message : 'The advanced spec cannot be read.';
    }
  }
  const onlineLocked = onlineChanged || (sameTarget && unresolved(online));
  const configurationLocked = Boolean(rejected) || yamlChanged || onlineLocked || importing;
  const workflow = kind === 'WorkflowJob';
  const hasStartingContent =
    Boolean(builder.commandInput) ||
    Boolean(draft.name) ||
    manifestSpecJSON(draft) !== manifestSpecJSON(newBuilderDraft(kind, false));
  const collection = collectionPath(location, definitionKind);
  useEffect(() => {
    if (failedChecks) validation.current?.focus();
  }, [failedChecks]);
  function remember(
    next: Draft,
    reviewedSpec = builder.reviewedSpec,
    reviewedFormSpec = builder.reviewedFormSpec,
    startingNew = builder.startingNew,
    commandInput: ExecutionCommandInput | null = builder.commandInput ?? null,
  ) {
    const value = {
      draft: next,
      reviewedSpec,
      reviewedFormSpec,
      sourceLabel: builder.sourceLabel,
      startingNew,
      commandInput: commandInput ?? undefined,
    };
    setBuilder(value);
    if (client) {
      const drafts = savedDrafts.get(client) ?? new Map<ManifestKind, BuilderDraft>();
      drafts.set(kind, value);
      savedDrafts.set(client, drafts);
    }
  }
  function startFrom(next: Draft, sourceLabel: string) {
    if (configurationLocked || unresolved(online) || administrationAuthenticationError(client))
      return;
    const value = { draft: next, sourceLabel, startingNew: true };
    setBuilder(value);
    setCommandEpoch((epoch) => epoch + 1);
    if (client) {
      const drafts = savedDrafts.get(client) ?? new Map<ManifestKind, BuilderDraft>();
      drafts.set(kind, value);
      savedDrafts.set(client, drafts);
      pendingYAML.get(client)?.delete(kind);
    }
    setYAMLInput(null);
    setYAMLError('');
    setChecked(false);
    setFailedChecks(0);
    setNotice('');
    setSelected(0);
  }
  function editSection(section: JobSection) {
    setTab('configuration');
    requestAnimationFrame(() => {
      const command =
        section === 'container' && commandError
          ? authoring.current?.querySelector<HTMLTextAreaElement>('textarea[aria-label="Command"]')
          : undefined;
      if (command) {
        command.focus();
        return;
      }
      focusJobSection(authoring.current, section);
    });
  }
  function applyImported(imported: Draft) {
    if (administrationAuthenticationError(client) || importController.current?.signal.aborted)
      return;
    remember(imported, builder.reviewedSpec, builder.reviewedFormSpec, builder.startingNew, null);
    setCommandEpoch((epoch) => epoch + 1);
    setYAMLInput(null);
    setSelected(0);
    if (client) pendingYAML.get(client)?.delete(kind);
    setYAMLError('');
    setChecked(true);
    setFailedChecks((count) => (checkDraft(imported).length ? count + 1 : 0));
    setTab('configuration');
    setNotice('YAML imported.');
    setCleanupPreview(null);
  }
  async function importYAML() {
    if (importing || onlineLocked || administrationAuthenticationError(client)) return;
    importController.current?.abort();
    const controller = new AbortController();
    importController.current = controller;
    setImporting(true);
    try {
      const preview = await previewManifestImportAsync(editorYAML, kind, controller.signal);
      if (controller.signal.aborted) return;
      if (preview.removedFields.length) setCleanupPreview(preview);
      else applyImported(preview.draft);
    } catch (error) {
      if (controller.signal.aborted) return;
      setYAMLError(error instanceof Error ? error.message : 'Unable to read this YAML.');
      setTab('manifest');
    } finally {
      if (!controller.signal.aborted) setImporting(false);
    }
  }
  function change<K extends keyof Draft>(key: K, value: Draft[K]) {
    if (configurationLocked) return;
    setBuilder((current) => {
      const next = { ...current, draft: { ...current.draft, [key]: value } };
      if (client) {
        const drafts = savedDrafts.get(client) ?? new Map<ManifestKind, BuilderDraft>();
        drafts.set(kind, next);
        savedDrafts.set(client, drafts);
      }
      return next;
    });
    setChecked(false);
    setNotice('');
  }
  function taskChange(patch: Partial<Task>) {
    change(
      'tasks',
      draft.tasks.map((t, i) => (i === selected ? { ...t, ...patch } : t)),
    );
  }
  function unfinishedCommand() {
    if (!commandError) return false;
    setNotice('Finish the Command field before reviewing, checking, or exporting this draft.');
    editSection('container');
    return true;
  }
  function publish() {
    if (administrationAuthenticationError(client)) return;
    if (unfinishedCommand()) return;
    if (onlineLocked) {
      setTab('publish');
      return;
    }
    if (yamlChanged) {
      setNotice(
        'Choose whether to import your YAML edits or keep the configuration form before reviewing.',
      );
      return;
    }
    const candidate = draft;
    setChecked(true);
    setNotice('');
    if (checkDraft(candidate).length) {
      setFailedChecks((count) => count + 1);
      setTab('configuration');
      return;
    }
    setFailedChecks(0);
    const existing = getDefinitionDraft(client!, definitionKind);
    const spec = manifestSpecJSON(candidate);
    if (unresolved(existing)) {
      setNotice(
        'A pending or uncertain save must be resolved before opening this draft. Open the retained editor below.',
      );
      setConflictingDraft(true);
      setTab('configuration');
      return;
    }
    if (
      existing.name &&
      (existing.name !== candidate.name ||
        existing.namespace !== candidate.namespace ||
        (builder.reviewedSpec === undefined && existing.spec !== spec))
    ) {
      setNotice(
        `Another draft for ${existing.namespace}/${existing.name} is retained. Choose which draft to review.`,
      );
      setConflictingDraft(true);
      setTab('configuration');
      return;
    } else {
      const unchanged =
        !builder.startingNew &&
        (existing.spec === spec ||
          (existing.spec === builder.reviewedSpec && spec === builder.reviewedFormSpec));
      remember(candidate, unchanged ? existing.spec : spec, spec, false);
      setDefinitionDraft(client!, definitionKind, {
        namespace: candidate.namespace,
        name: candidate.name,
        spec,
        ...(!builder.startingNew && existing.loaded ? { loaded: existing.loaded } : {}),
        ...(unchanged ? existing : {}),
        commandInput: builder.commandInput,
      });
    }
    setConflictingDraft(false);
    setTab('publish');
  }
  function check() {
    if (onlineLocked || administrationAuthenticationError(client)) return;
    if (unfinishedCommand()) return;
    if (yamlChanged) {
      setNotice(
        'Choose whether to import your YAML edits or keep the configuration form before checking.',
      );
      return;
    }
    setChecked(true);
    if (checkDraft(draft).length) {
      setFailedChecks((count) => count + 1);
      setNotice('');
    } else {
      setFailedChecks(0);
      setNotice('Draft checks passed.');
    }
  }
  function discardYAMLEdits() {
    setYAMLInput(null);
    if (client) pendingYAML.get(client)?.delete(kind);
    setYAMLError('');
    setNotice('YAML edits discarded.');
  }
  async function copy() {
    if (unfinishedCommand()) return;
    if (await copyText(editorYAML)) {
      setNotice('YAML copied.');
    } else {
      selectTextArea('manifest-yaml');
      setNotice('Clipboard unavailable. YAML is selected below; press ⌘C or Ctrl+C to copy it.');
    }
  }
  function download() {
    if (unfinishedCommand()) return;
    const url = URL.createObjectURL(
      new Blob([editorYAML], { type: 'application/yaml;charset=utf-8' }),
    );
    const a = document.createElement('a');
    a.href = url;
    a.download = `${draft.name || 'workload-draft'}.yaml`;
    a.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
    setNotice('YAML downloaded.');
  }
  const issueAt = (path: string) => issues.find((issue) => issue.path === path)?.message;
  function field(label: string, key: keyof Draft, help?: string, path?: string) {
    const error = path ? issueAt(path) : undefined;
    const detail = error || help;
    const detailID = `manifest-${key}-detail`;
    return (
      <label className="manifest-field" key={key}>
        <Text size="2" weight="medium">
          {label}
        </Text>
        <TextField.Root
          aria-label={label}
          aria-invalid={Boolean(error) || undefined}
          aria-describedby={detail ? detailID : undefined}
          value={String(draft[key])}
          placeholder={
            key === 'name'
              ? 'e.g. daily-report'
              : draft.sourceSpec && String(draft[key]) === ''
                ? 'Use default'
                : undefined
          }
          onChange={(e) => change(key, e.target.value as never)}
        />
        {detail && (
          <Text id={detailID} size="1" color={error ? 'red' : 'gray'}>
            {detail}
          </Text>
        )}
      </label>
    );
  }
  const policyLabels: Record<string, string> = {
    Allow: 'Allow overlapping runs',
    Forbid: 'Prevent overlapping runs',
    Skip: 'Skip missed starts',
    FireOnce: 'Run once after a missed start',
    CatchUpBounded: 'Catch up within the limit',
    FailFast: 'Stop on failure',
    Continue: 'Continue other tasks',
  };
  const ruleLabels: Record<string, string> = {
    phase: 'Result',
    attempts: 'Attempts',
    duration_ms: 'Duration (ms)',
    eq: 'Is',
    ne: 'Is not',
    in: 'Is one of',
    not_in: 'Is not one of',
    lt: 'Less than',
    le: 'At most',
    gt: 'Greater than',
    ge: 'At least',
  };
  const select = (label: string, key: keyof Draft, values: string[]) => (
    <label className="manifest-field" key={key}>
      <Text size="2" weight="medium">
        {label}
      </Text>
      <Select.Root
        disabled={configurationLocked}
        value={String(draft[key])}
        onValueChange={(v) => change(key, v as never)}
      >
        <Select.Trigger aria-label={label} placeholder="Default" />
        <Select.Content>
          {values.map((v) => (
            <Select.Item key={v} value={v} disabled={v === 'Replace'}>
              {policyLabels[v] ?? v}
            </Select.Item>
          ))}
        </Select.Content>
      </Select.Root>
    </label>
  );
  const task = draft.tasks[selected];
  return (
    <div className={`workload-builder${workflow ? '' : ' job-authoring'}`} ref={authoring}>
      <Back to={collection}>Back to {kind === 'ScheduledJob' ? 'Jobs' : 'Workflows'}</Back>
      <PageHeading
        title={workflow ? 'Create a workflow' : 'Create a job'}
        action={
          tab !== 'publish' && (
            <Flex gap="3" align="center" wrap="wrap">
              <Button size="1" color="gray" variant="ghost" onClick={() => setLeaving(true)}>
                Discard draft
              </Button>
              <Button
                size="1"
                color="gray"
                variant="outline"
                disabled={configurationLocked}
                onClick={check}
              >
                Check draft
              </Button>
              {tab !== 'manifest' && (
                <Button size="2" disabled={yamlChanged || !!rejected} onClick={publish}>
                  Review and save
                </Button>
              )}
            </Flex>
          )
        }
      />
      {rejected && <ErrorState error={rejected} />}
      {commandError && (
        <section className="configuration-errors" role="alert" aria-label="Command needs attention">
          <Text as="p" size="2">
            {commandError} Finish the Command field before reviewing, checking, or exporting.
          </Text>
          <Button size="1" variant="outline" mt="3" onClick={() => editSection('container')}>
            Edit command
          </Button>
        </section>
      )}
      {onlineIssue && (
        <section className="configuration-errors" aria-label="Advanced JSON needs review">
          <Heading as="h2" size="3">
            Keep your advanced JSON changes
          </Heading>
          <Text as="p" size="2" mt="2">
            The form cannot display these edits. Your complete draft is available in Advanced spec
            JSON. {onlineIssue}
          </Text>
          <Flex gap="3" mt="3" wrap="wrap">
            <Button
              onClick={() => {
                if (!unfinishedCommand()) setTab('publish');
              }}
            >
              Return to advanced JSON
            </Button>
            <Button
              variant="outline"
              disabled={!!rejected}
              onClick={() => {
                if (administrationAuthenticationError(client)) return;
                if (unfinishedCommand()) return;
                const retained = getDefinitionDraft(client!, definitionKind);
                if (unresolved(retained)) return;
                const spec = manifestSpecJSON(draft);
                remember(draft, spec, spec);
                setDefinitionDraft(client!, definitionKind, {
                  ...retained,
                  spec,
                  checked: undefined,
                  receipt: undefined,
                  requestID: undefined,
                  error: undefined,
                  message: 'Advanced JSON edits discarded. The configuration form was kept.',
                });
              }}
            >
              Discard advanced JSON edits and keep form
            </Button>
          </Flex>
        </section>
      )}
      {yamlChanged && tab !== 'manifest' && (
        <section className="configuration-errors" aria-label="Choose which draft to use">
          <Heading as="h2" size="3">
            Choose which draft to use
          </Heading>
          <Text as="p" size="2" mt="2">
            Import your YAML changes or discard them to unlock the form.
          </Text>
          <Flex gap="2" mt="3" wrap="wrap">
            <Button disabled={!!rejected || importing} onClick={() => void importYAML()}>
              Import YAML edits
            </Button>
            <Button variant="outline" onClick={discardYAMLEdits}>
              Discard YAML edits and keep form
            </Button>
          </Flex>
        </section>
      )}
      {issues.length > 0 && tab !== 'manifest' && tab !== 'publish' && (
        <div className="configuration-errors" role="alert" tabIndex={-1} ref={validation}>
          <Heading as="h2" size="3">
            Complete the configuration
          </Heading>
          <Text as="p" size="2" mt="2">
            Correct these fields to continue.
          </Text>
          <ul>
            {issues.map((issue, index) => (
              <li key={index}>
                <strong>
                  {issue.path === 'metadata.name'
                    ? 'Name'
                    : issue.path === 'metadata.namespace'
                      ? 'Execution namespace'
                      : issue.path === 'spec.jobTemplate.image'
                        ? 'Container image'
                        : issue.path}
                </strong>
                : {issue.message}
              </li>
            ))}
          </ul>
        </div>
      )}
      {notice && tab !== 'manifest' && (
        <section className="operation-notice" role="status">
          <Text as="p" size="2">
            {notice}
          </Text>
          {conflictingDraft && (
            <Flex gap="3" mt="3" wrap="wrap">
              <Button asChild variant="outline">
                <Link to={`/definitions/${definitionKind}`} state={location.state}>
                  Open retained editor
                </Link>
              </Button>
              <Button
                variant="outline"
                disabled={
                  !!rejected ||
                  Boolean(commandError) ||
                  yamlChanged ||
                  onlineLocked ||
                  unresolved(
                    getDefinitionDraft(client!, kind === 'ScheduledJob' ? 'jobs' : 'workflows'),
                  )
                }
                onClick={() => {
                  if (
                    administrationAuthenticationError(client) ||
                    unfinishedCommand() ||
                    yamlChanged ||
                    onlineLocked ||
                    unresolved(
                      getDefinitionDraft(client!, kind === 'ScheduledJob' ? 'jobs' : 'workflows'),
                    )
                  )
                    return;
                  const retained = getDefinitionDraft(client!, definitionKind);
                  const spec = manifestSpecJSON(draft);
                  remember(draft, spec, spec, false);
                  setDefinitionDraft(client!, definitionKind, {
                    namespace: draft.namespace,
                    name: draft.name,
                    spec,
                    commandInput: builder.commandInput,
                    ...(!builder.startingNew &&
                    retained.name === draft.name &&
                    retained.namespace === draft.namespace &&
                    retained.loaded
                      ? { loaded: retained.loaded }
                      : {}),
                  });
                  setConflictingDraft(false);
                  setNotice('');
                  setTab('publish');
                }}
              >
                Discard retained draft and review this one
              </Button>
            </Flex>
          )}
        </section>
      )}
      <Tabs.Root
        className="job-tabs"
        value={tab}
        onValueChange={(value) => (value === 'publish' ? publish() : setTab(value))}
      >
        <Tabs.List size="1">
          <Tabs.Trigger value="configuration">Configuration</Tabs.Trigger>
          {kind === 'WorkflowJob' && <Tabs.Trigger value="tasks">Tasks</Tabs.Trigger>}
          <Tabs.Trigger value="manifest">Advanced YAML</Tabs.Trigger>
          {tab === 'publish' && <Tabs.Trigger value="publish">Review and save</Tabs.Trigger>}
        </Tabs.List>
        <Tabs.Content value="configuration" className="tab-content">
          {!workflow && (
            <a className="job-preview-jump" href="#job-live-preview">
              Preview how this job will run
            </a>
          )}
          <div className={workflow ? undefined : 'job-authoring-layout'}>
            <div className="job-authoring-form">
              <fieldset className="workload-configuration" disabled={configurationLocked}>
                {!workflow && (
                  <JobStartingPoint
                    namespace={draft.namespace}
                    disabled={configurationLocked || unresolved(online)}
                    hasContent={hasStartingContent}
                    onApply={startFrom}
                    onCustom={() => editSection('container')}
                  />
                )}
                {builder.sourceLabel && !workflow && (
                  <p className="job-draft-source">
                    Started from {builder.sourceLabel}. Review your name and execution location.
                  </p>
                )}
                <section className="authoring-settings-group" data-job-section="general">
                  <Heading as="h2" size="3" weight="medium">
                    General
                  </Heading>
                  <div className="manifest-fields">
                    {field('Name', 'name', undefined, 'metadata.name')}
                    <NamespaceField
                      value={draft.namespace}
                      onChange={(value) => change('namespace', value)}
                      error={issueAt('metadata.namespace')}
                    />
                  </div>
                </section>
                {!workflow && (
                  <section className="authoring-settings-group" data-job-section="container">
                    <Heading as="h2" size="3" weight="medium">
                      Container
                    </Heading>
                    <div>
                      <RuntimePresetField
                        key={`runtime-${commandEpoch}`}
                        image={draft.image}
                        command={draft.command}
                        args={draft.args}
                        disabled={configurationLocked}
                        onChange={(image, command, args) => {
                          if (configurationLocked) return;
                          remember(
                            { ...draft, image, command, args },
                            builder.reviewedSpec,
                            builder.reviewedFormSpec,
                            builder.startingNew,
                            null,
                          );
                          setCommandEpoch((epoch) => epoch + 1);
                          setChecked(false);
                          setNotice('');
                        }}
                      />
                      <ContainerImageField
                        disabled={configurationLocked}
                        value={draft.image}
                        onChange={(value) => change('image', value)}
                        error={issueAt('spec.jobTemplate.image')}
                        required
                      />
                      <ExecutionCommandFields
                        key={`command-${commandEpoch}`}
                        command={draft.command}
                        args={draft.args}
                        input={builder.commandInput}
                        disabled={configurationLocked}
                        onChange={(command, args, input) => {
                          if (configurationLocked) return;
                          remember(
                            { ...draft, command, args },
                            builder.reviewedSpec,
                            builder.reviewedFormSpec,
                            builder.startingNew,
                            input ?? null,
                          );
                          setChecked(false);
                          setNotice('');
                        }}
                      />
                      <details className="authoring-examples">
                        <summary>Command examples</summary>
                        <Button
                          size="1"
                          variant="outline"
                          mt="3"
                          onClick={() => {
                            remember(
                              { ...draft, command: ['sh', '-c'], args: ['df -P /'] },
                              builder.reviewedSpec,
                              builder.reviewedFormSpec,
                              builder.startingNew,
                              null,
                            );
                            setChecked(false);
                            setNotice('');
                          }}
                        >
                          Insert disk usage command
                        </Button>
                      </details>
                    </div>
                  </section>
                )}
                <section className="authoring-settings-group" data-job-section="schedule">
                  <Heading as="h2" size="3" weight="medium">
                    Schedule
                  </Heading>
                  <div>
                    <div
                      className={`manifest-fields${!workflow && draft.schedule ? ' job-schedule-fields' : ''}`}
                    >
                      <label className="manifest-field">
                        <Text size="2" weight="medium">
                          Trigger mode
                        </Text>
                        <Select.Root
                          disabled={configurationLocked}
                          value={draft.schedule ? 'scheduled' : 'manual'}
                          onValueChange={(value) =>
                            change('schedule', value === 'manual' ? '' : '0 2 * * *')
                          }
                        >
                          <Select.Trigger aria-label="Trigger mode" />
                          <Select.Content>
                            <Select.Item value="manual">Manual only</Select.Item>
                            <Select.Item value="scheduled">Scheduled and manual</Select.Item>
                          </Select.Content>
                        </Select.Root>
                      </label>
                      {draft.schedule && (
                        <div>
                          {field(
                            'Configured schedule',
                            'schedule',
                            workflow
                              ? 'Five cron fields: minute, hour, day, month, weekday.'
                              : 'Five cron fields. Job schedules use UTC.',
                            'spec.schedule',
                          )}
                          <ScheduleGenerator
                            value={draft.schedule}
                            timeZone={workflow ? draft.timeZone || 'UTC' : 'UTC'}
                            disabled={configurationLocked}
                            onChange={(value) => change('schedule', value)}
                          />
                        </div>
                      )}
                      {kind === 'WorkflowJob' &&
                        draft.schedule &&
                        field(
                          'Time zone',
                          'timeZone',
                          'IANA zone, for example UTC or Asia/Shanghai.',
                        )}

                      {field(
                        'Timeout in seconds',
                        'timeout',
                        'Enter 0 for no deadline.',
                        'spec.timeoutSeconds',
                      )}
                    </div>
                    <Text as="label" size="2" className="manifest-suspend">
                      <Flex align="center" gap="2">
                        <Checkbox
                          checked={draft.suspend}
                          onCheckedChange={(v) => change('suspend', v === true)}
                        />
                        {kind === 'WorkflowJob'
                          ? 'Pause automatic starts; manual runs stay available'
                          : 'Suspend scheduled and manual starts'}
                      </Flex>
                    </Text>
                    {workflow && draft.schedule && (
                      <WorkflowSchedulePreview
                        schedule={draft.schedule}
                        zone={draft.timeZone || 'UTC'}
                      />
                    )}
                  </div>
                </section>
                {!workflow && (
                  <details className="job-authoring-resources" data-job-section="resources">
                    <summary>Resources and repeat safety</summary>
                    <p>
                      Optional overrides. Cluster checks confirm resource defaults and limits before
                      saving.
                    </p>
                    <ExecutionResourcesFields
                      version={draft.executionPolicyVersion ?? 0}
                      retrySafe={draft.retrySafe ?? false}
                      resources={draft.resources}
                      disabled={configurationLocked}
                      change={(settings) => {
                        if (configurationLocked) return;
                        remember({
                          ...draft,
                          executionPolicyVersion: settings.version,
                          retrySafe: settings.retrySafe,
                          resources: settings.resources,
                        });
                        setChecked(false);
                        setNotice('');
                      }}
                    />
                  </details>
                )}
                <details className="advanced-run-settings" data-job-section="policies">
                  <summary>Run policies</summary>
                  <div className="manifest-fields">
                    {select(
                      'Concurrency policy',
                      'concurrency',
                      kind === 'ScheduledJob' && draft.concurrency === 'Replace'
                        ? ['Allow', 'Forbid', 'Replace']
                        : ['Allow', 'Forbid'],
                    )}
                    {kind === 'ScheduledJob'
                      ? select('Missed starts', 'misfire', ['Skip', 'FireOnce', 'CatchUpBounded'])
                      : select('Failure policy', 'failPolicy', ['FailFast', 'Continue'])}
                    {kind === 'ScheduledJob' &&
                      field('Maximum attempts', 'attempts', 'Includes the first attempt.')}
                    {kind === 'ScheduledJob' &&
                      field(
                        'Kubernetes Job backoff limit',
                        'backoff',
                        'Independent from platform attempts.',
                      )}
                    {field('Retain successful runs', 'success')}
                    {field('Retain failed runs', 'failed')}
                  </div>
                  <Text as="p" size="1" color="gray" mt="3">
                    Retained history uses the default when set to 0.
                  </Text>
                  {draft.concurrency === 'Forbid' && (
                    <Text as="p" size="1" color="gray" mt="3">
                      {workflow
                        ? 'Scheduled starts are skipped while another run is open. Accepted manual starts wait for it to finish.'
                        : 'Scheduled, manual and workflow-task runs cannot overlap. Accepted manual starts wait for earlier runs to finish.'}
                    </Text>
                  )}
                  {!workflow && draft.concurrency === 'Replace' && (
                    <Text as="p" size="1" color="gray" mt="3">
                      Replace requests cancellation for scheduled starts and waits for prior runs to
                      finish. Manual and workflow-task starts do not replace running work. Choose
                      Allow or Forbid for a new configuration.
                    </Text>
                  )}
                </details>
              </fieldset>
            </div>
            {!workflow && !rejected && (
              <aside className="job-authoring-preview" id="job-live-preview">
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
                    spec={manifestSpecJSON(draft)}
                    context={
                      sameTarget && online.loaded && !builder.startingNew ? 'edit' : 'create'
                    }
                    onEdit={editSection}
                  />
                )}
              </aside>
            )}
          </div>
        </Tabs.Content>
        {kind === 'WorkflowJob' && (
          <Tabs.Content value="tasks" className="tab-content">
            <fieldset className="workload-configuration" disabled={configurationLocked}>
              <Flex justify="between" align="center" mb="4" gap="3">
                <Heading size="3" weight="medium">
                  Tasks
                </Heading>
                <Button
                  size="1"
                  color="gray"
                  variant="outline"
                  onClick={() => {
                    change('tasks', [
                      ...draft.tasks,
                      {
                        name: `task-${draft.tasks.length + 1}`,
                        job: '',
                        dependencies: [],
                        combination: '',
                        rules: [],
                      },
                    ]);
                    setSelected(draft.tasks.length);
                  }}
                >
                  Add task
                </Button>
              </Flex>
              <Text as="p" size="1" color="gray" mb="3">
                Select a job from the execution namespace for each task.
              </Text>
              <Table.Root size="1" className="ledger-table">
                <Table.Header>
                  <Table.Row>
                    {['Task', 'Referenced Job', 'Dependencies'].map((h) => (
                      <Table.ColumnHeaderCell key={h}>{h}</Table.ColumnHeaderCell>
                    ))}
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {draft.tasks.map((t, i) => (
                    <Table.Row key={i}>
                      <Table.RowHeaderCell>
                        <button
                          type="button"
                          className="manifest-task-link"
                          aria-pressed={selected === i}
                          onClick={() => setSelected(i)}
                        >
                          {t.name || 'Unnamed task'}
                        </button>
                      </Table.RowHeaderCell>
                      <Table.Cell>{t.job || 'Not set'}</Table.Cell>
                      <Table.Cell>{t.dependencies.join(', ') || 'None'}</Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table.Root>
              {task && (
                <section className="m4-section">
                  <Flex justify="between" align="center" mb="4">
                    <Heading size="3" weight="medium">
                      Selected task
                    </Heading>
                    <Button
                      size="1"
                      color="red"
                      variant="ghost"
                      onClick={() => {
                        change(
                          'tasks',
                          draft.tasks.filter((_, i) => i !== selected),
                        );
                        setSelected(Math.max(0, selected - 1));
                      }}
                    >
                      Remove task
                    </Button>
                  </Flex>
                  <div className="manifest-fields">
                    <label className="manifest-field">
                      <Text size="1">Task name</Text>
                      <TextField.Root
                        aria-label="Task name"
                        value={task.name}
                        onChange={(e) => taskChange({ name: e.target.value })}
                      />
                    </label>
                    <div>
                      <label className="manifest-field">
                        <Text size="1">Referenced Job name</Text>
                        <TextField.Root
                          aria-label="Referenced Job name"
                          value={task.job}
                          onChange={(e) => taskChange({ job: e.target.value })}
                        />
                      </label>
                      <WorkflowJobPicker
                        namespace={draft.namespace}
                        value={task.job}
                        disabled={configurationLocked}
                        onChoose={(job) => taskChange({ job })}
                      />
                    </div>
                    <label className="manifest-field">
                      <Text size="1">Dependencies (comma separated)</Text>
                      <TextField.Root
                        aria-label="Dependencies"
                        value={task.dependencies.join(',')}
                        onChange={(e) =>
                          taskChange({
                            dependencies: e.target.value
                              ? e.target.value.split(',').map((s) => s.trim())
                              : [],
                          })
                        }
                      />
                    </label>
                    <label className="manifest-field">
                      <Text size="1">Rule combination</Text>
                      <Select.Root
                        disabled={configurationLocked}
                        value={task.combination || 'none'}
                        onValueChange={(v) =>
                          taskChange({
                            combination: v === 'none' ? '' : (v as 'all' | 'any'),
                            rules:
                              v === 'none'
                                ? []
                                : task.rules.length
                                  ? task.rules
                                  : [
                                      {
                                        task: task.dependencies[0] || '',
                                        field: 'phase',
                                        operator: 'eq',
                                        values: ['Succeeded'],
                                        number: '0',
                                      },
                                    ],
                          })
                        }
                      >
                        <Select.Trigger aria-label="Rule combination" />
                        <Select.Content>
                          <Select.Item value="none">Unconditional</Select.Item>
                          <Select.Item value="all">All rules</Select.Item>
                          <Select.Item value="any">At least one rule</Select.Item>
                        </Select.Content>
                      </Select.Root>
                    </label>
                  </div>
                  {task.combination && (
                    <>
                      <Heading size="2" weight="medium" mb="3">
                        Condition rules
                      </Heading>
                      {task.rules.map((rule, index) => (
                        <div className="manifest-rule" key={index}>
                          {(['task', 'field', 'operator'] as const).map((key) => (
                            <label className="manifest-field" key={key}>
                              <Text size="1">
                                {key === 'task'
                                  ? 'Dependency'
                                  : key === 'field'
                                    ? 'Measure'
                                    : 'Condition'}
                              </Text>
                              <Select.Root
                                disabled={configurationLocked}
                                value={rule[key] || '__unset'}
                                onValueChange={(v) => {
                                  const next = { ...rule, [key]: v };
                                  if (key === 'field') {
                                    next.operator = v === 'phase' ? 'eq' : 'ge';
                                    next.values = v === 'phase' ? ['Succeeded'] : [];
                                  }
                                  taskChange({
                                    rules: task.rules.map((r, i) => (i === index ? next : r)),
                                  });
                                }}
                              >
                                <Select.Trigger aria-label={`Rule ${index + 1} ${key}`} />
                                <Select.Content>
                                  {(key === 'task'
                                    ? ['__unset', ...task.dependencies]
                                    : key === 'field'
                                      ? ['phase', 'attempts', 'duration_ms']
                                      : rule.field === 'phase'
                                        ? ['eq', 'ne', 'in', 'not_in']
                                        : ['lt', 'le', 'gt', 'ge']
                                  ).map((v) => (
                                    <Select.Item key={v} value={v}>
                                      {v === '__unset' ? 'Choose dependency' : (ruleLabels[v] ?? v)}
                                    </Select.Item>
                                  ))}
                                </Select.Content>
                              </Select.Root>
                            </label>
                          ))}
                          <label className="manifest-field">
                            <Text size="1">
                              {rule.field === 'phase'
                                ? 'Phases (comma separated)'
                                : rule.field === 'duration_ms'
                                  ? 'Duration in milliseconds'
                                  : 'Number of attempts'}
                            </Text>
                            <TextField.Root
                              aria-label={`Rule ${index + 1} value`}
                              value={rule.field === 'phase' ? rule.values.join(',') : rule.number}
                              onChange={(e) =>
                                taskChange({
                                  rules: task.rules.map((r, i) =>
                                    i === index
                                      ? {
                                          ...r,
                                          ...(rule.field === 'phase'
                                            ? {
                                                values: e.target.value
                                                  .split(',')
                                                  .map((v) => v.trim()),
                                              }
                                            : { number: e.target.value }),
                                        }
                                      : r,
                                  ),
                                })
                              }
                            />
                          </label>
                          <Button
                            size="1"
                            color="gray"
                            variant="ghost"
                            onClick={() =>
                              taskChange({ rules: task.rules.filter((_, i) => i !== index) })
                            }
                          >
                            Remove rule
                          </Button>
                        </div>
                      ))}
                      <Button
                        size="1"
                        color="gray"
                        variant="outline"
                        onClick={() =>
                          taskChange({
                            rules: [
                              ...task.rules,
                              {
                                task: task.dependencies[0] || '',
                                field: 'phase',
                                operator: 'eq',
                                values: ['Succeeded'],
                                number: '0',
                              },
                            ],
                          })
                        }
                      >
                        Add rule
                      </Button>
                    </>
                  )}
                </section>
              )}
            </fieldset>
          </Tabs.Content>
        )}
        <Tabs.Content value="publish" className="tab-content">
          <DefinitionEditor
            embedded
            definitionKind={kind === 'ScheduledJob' ? 'jobs' : 'workflows'}
          />
        </Tabs.Content>
        <Tabs.Content value="manifest" className="tab-content">
          {commandError ? (
            <section className="m4-notice" aria-label="YAML waiting for command">
              <Heading as="h2" size="3">
                Finish the command before generating YAML
              </Heading>
              <Text as="p" size="2" mt="2">
                Your unfinished Command text is retained in Configuration.
              </Text>
              <Button size="1" variant="outline" mt="3" onClick={() => editSection('container')}>
                Edit command
              </Button>
            </section>
          ) : (
            <>
              <Heading as="h2" size="3" mb="3">
                YAML editor
              </Heading>
              {yamlError && (
                <section role="alert" className="configuration-errors">
                  {yamlError}
                </section>
              )}
              {(yamlChanged || issues.length > 0) && (
                <section
                  className="m4-notice"
                  role={issues.length && !yamlChanged ? 'alert' : 'status'}
                >
                  <Text as="p" size="2" weight="medium">
                    {yamlChanged ? 'YAML changes need importing' : `${issues.length} draft issues`}
                  </Text>

                  {issues.length > 0 && !yamlChanged && (
                    <ul>
                      {issues.map((e, i) => (
                        <li key={i}>
                          <code>{e.path}</code>: {e.message}
                        </li>
                      ))}
                    </ul>
                  )}
                </section>
              )}
              <Flex className="yaml-editor-tools" gap="2" mb="3" wrap="wrap">
                <Button
                  size="2"
                  disabled={onlineLocked || !!rejected || importing}
                  onClick={() => void importYAML()}
                >
                  Import YAML into form
                </Button>
                {yamlChanged && (
                  <Button size="1" variant="outline" color="gray" onClick={discardYAMLEdits}>
                    Discard YAML edits and keep form
                  </Button>
                )}
                <Button
                  size="1"
                  color="gray"
                  variant="outline"
                  disabled={!editorYAML.trim()}
                  onClick={() => void copy()}
                >
                  Copy YAML
                </Button>
                <Button
                  size="1"
                  color="gray"
                  variant="outline"
                  onClick={() => {
                    if (unfinishedCommand()) return;
                    selectTextArea('manifest-yaml');
                    setNotice('YAML selected. Press ⌘C or Ctrl+C to copy.');
                  }}
                >
                  Select YAML
                </Button>
                <Button
                  size="1"
                  color="gray"
                  variant="outline"
                  disabled={!valid || yamlChanged || !!rejected}
                  onClick={publish}
                >
                  Review and save
                </Button>
                <Button
                  size="1"
                  color="gray"
                  variant="outline"
                  disabled={!editorYAML.trim()}
                  onClick={download}
                >
                  Download YAML
                </Button>
              </Flex>
              {notice && (
                <Text as="p" size="2" role="status" mb="3">
                  {notice}
                </Text>
              )}
              <TextArea
                id="manifest-yaml"
                aria-label="Generated YAML"
                disabled={onlineLocked || !!rejected || importing}
                value={editorYAML}
                onChange={(event) => {
                  const value = event.target.value;
                  setYAMLInput(value);
                  if (client) {
                    const edits = pendingYAML.get(client) ?? new Map<ManifestKind, string>();
                    edits.set(kind, value);
                    pendingYAML.set(client, edits);
                  }
                  setChecked(false);
                  setYAMLError('');
                }}
                className="manifest-yaml"
                rows={Math.min(32, yaml.split('\n').length)}
              />
              <details className="advanced-details yaml-assistance">
                <summary>YAML resources</summary>
                <WorkloadAssistant kind={kind} namespace={draft.namespace} name={draft.name} />
              </details>
            </>
          )}
        </Tabs.Content>
      </Tabs.Root>
      <Dialog.Root
        open={cleanupPreview !== null}
        onOpenChange={(open) => {
          if (!open) setCleanupPreview(null);
        }}
      >
        <Dialog.Content maxWidth="720px">
          <Dialog.Title>Review import cleanup</Dialog.Title>
          <Dialog.Description size="2">
            These server-managed fields will be removed. The configuration below will become your
            local draft.
          </Dialog.Description>
          <ul>
            {cleanupPreview?.removedFields.map((field) => (
              <li key={field}>
                <code>{field}</code>
              </li>
            ))}
          </ul>
          <TextArea
            aria-label="Cleaned import preview"
            readOnly
            rows={16}
            value={cleanupPreview?.cleanedYAML ?? ''}
          />
          <Flex justify="end" gap="3" mt="4">
            <Dialog.Close>
              <Button variant="soft">Keep YAML</Button>
            </Dialog.Close>
            <Button
              disabled={!!rejected}
              onClick={() => {
                if (cleanupPreview) applyImported(cleanupPreview.draft);
              }}
            >
              Confirm import
            </Button>
          </Flex>
        </Dialog.Content>
      </Dialog.Root>
      <Dialog.Root open={leaving} onOpenChange={setLeaving}>
        <Dialog.Content maxWidth="440px">
          <Dialog.Title>Discard this local draft?</Dialog.Title>
          <Dialog.Description size="2">
            Your form and YAML edits in this builder will be cleared.
          </Dialog.Description>
          <Flex gap="3" justify="end" mt="4">
            <Dialog.Close>
              <Button size="1" variant="soft" color="gray">
                Keep editing
              </Button>
            </Dialog.Close>
            <Button size="1" color="red" asChild>
              <Link
                to={collection}
                state={location.state}
                onClick={() => {
                  if (client) {
                    savedDrafts.get(client)?.delete(kind);
                    pendingYAML.get(client)?.delete(kind);
                  }
                }}
              >
                Discard and return
              </Link>
            </Button>
          </Flex>
        </Dialog.Content>
      </Dialog.Root>
    </div>
  );
}
