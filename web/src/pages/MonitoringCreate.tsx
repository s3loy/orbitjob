import { consumedCreateSearch, forwardState, useAlive } from '../monitoring-state';
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { Button, Dialog, Flex, Select, Text, TextArea, TextField } from '@radix-ui/themes';
import { useQueryClient } from '@tanstack/react-query';
import { useLocation, useNavigate } from 'react-router-dom';
import { useSession } from '../session';
import { APIError } from '../api/transport';
import type { Resource, CreateInputs } from '../api/monitoring';
import { ErrorState } from '../components';
import { ResourcePicker } from '../ResourcePicker';
import {
  recordMonitoringCreation,
  monitoringDraftScope,
  monitoringDraft,
  updateMonitoringDraft,
  resetMonitoringDraft,
  useMonitoringDraft,
} from '../monitoring-creations';
import { useAdministrationAuthenticationError } from '../administration-state';
import { withCredential } from '../credential-requests';
import {
  monitoringSourceKind,
  resolveMonitoringSource,
  validateMonitoringSource,
  type MonitoringSource,
  type MonitoringSourceKind,
} from '../monitoring-source';

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="form-field">
      {label}
      {children}
    </label>
  );
}
function Choice({
  label,
  value,
  values,
  change,
}: {
  label: string;
  value: string;
  values: string[];
  change: (v: string) => void;
}) {
  return (
    <Field label={label}>
      <Select.Root value={value} onValueChange={change} size="2">
        <Select.Trigger aria-label={label} />
        <Select.Content>
          {values.map((v) => (
            <Select.Item key={v} value={v}>
              {v === 'success'
                ? 'Successful runs'
                : v === 'duration'
                  ? 'Duration limit'
                  : v[0].toUpperCase() + v.slice(1)}
            </Select.Item>
          ))}
        </Select.Content>
      </Select.Root>
    </Field>
  );
}
export function MonitoringCreate({
  resource,
  authorizationError,
}: {
  resource: Resource;
  authorizationError?: Error;
}) {
  const { client } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const denied = rejected ?? authorizationError;
  const cache = useQueryClient();
  const navigate = useNavigate();
  const location = useLocation();
  const singular = resource === 'checks' ? 'check' : resource === 'slis' ? 'SLI' : 'SLO';
  const [open, setOpen] = useState(
    () => new URLSearchParams(location.search).get('create') === '1',
  );
  const alive = useAlive();
  useEffect(() => {
    if (new URLSearchParams(location.search).get('create') === '1')
      navigate(
        { pathname: location.pathname, search: consumedCreateSearch(location.search) },
        { replace: true, state: location.state },
      );
  }, [location.pathname, location.search, location.state, navigate]);
  return (
    <Dialog.Root open={open} onOpenChange={setOpen}>
      <Dialog.Trigger>
        <Button size="1" disabled={Boolean(denied)}>
          New {singular}
        </Button>
      </Dialog.Trigger>
      {open && (
        <CreateForm
          resource={resource}
          client={client!}
          authorizationError={denied}
          close={() => setOpen(false)}
          created={async (id) => {
            await cache.invalidateQueries({ queryKey: ['monitoring'] });
            if (!alive.current) return;
            setOpen(false);
            navigate(`/${resource}/${id}`, {
              state: forwardState(location),
            });
          }}
        />
      )}
    </Dialog.Root>
  );
}
function CreateForm({
  resource,
  client,
  close,
  created,
  authorizationError,
}: {
  resource: Resource;
  client: NonNullable<ReturnType<typeof useSession>['client']>;
  close: () => void;
  created: (id: number | string) => Promise<void>;
  authorizationError?: Error;
}) {
  const location = useLocation();
  const singular = resource === 'checks' ? 'check' : resource === 'slis' ? 'SLI' : 'SLO';
  const scope = monitoringDraftScope(resource, location.search);
  const { values, pending, sourceKind, selectedSource, uncertain, error } = useMonitoringDraft(
    client,
    scope,
  );
  const [discard, setDiscard] = useState(false);
  const setSourceKind = (sourceKind: MonitoringSourceKind) =>
    updateMonitoringDraft(client, scope, { sourceKind });
  const setSelectedSource = (selectedSource: MonitoringSource | undefined) =>
    updateMonitoringDraft(client, scope, { selectedSource });
  const inFlight = useRef(false);
  const authorization = useRef(authorizationError);
  useLayoutEffect(() => {
    authorization.current = authorizationError;
  }, [authorizationError]);
  const alive = useAlive();
  function set(key: keyof typeof values, value: string) {
    updateMonitoringDraft(client, scope, {
      values: { ...monitoringDraft(client, scope).values, [key]: value },
      ...(key === 'source' ? { selectedSource: undefined } : {}),
    });
  }
  function discardDraft() {
    resetMonitoringDraft(client, scope);
    close();
  }
  function input(
    key: keyof typeof values,
    label: string,
    props: {
      type?: 'number' | 'url' | 'text';
      min?: number;
      max?: number;
      step?: string;
      required?: boolean;
      maxLength?: number;
      placeholder?: string;
    } = {},
  ) {
    return (
      <Field label={label}>
        <TextField.Root
          aria-label={label}
          value={values[key]}
          onChange={(e) => set(key, e.target.value)}
          disabled={pending}
          {...props}
        />
      </Field>
    );
  }
  async function submit() {
    const current = monitoringDraft(client, scope);
    if (authorizationError || inFlight.current || current.pending || current.uncertain) return;
    inFlight.current = true;
    updateMonitoringDraft(client, scope, { pending: true, error: null });
    let writeAttempted = false;
    let completed = false;
    try {
      let payload: CreateInputs[Resource];
      const shared = {
        name: values.name.trim(),
        ...(values.description ? { description: values.description } : {}),
      };
      if (!shared.name) throw new Error('Enter a name.');
      if (resource === 'checks') {
        const url = new URL(values.url);
        if (!['http:', 'https:'].includes(url.protocol))
          throw new Error('Use an absolute HTTP or HTTPS URL.');
        payload = {
          ...shared,
          check_type: 'http_health',
          check_config: {
            url: values.url.trim(),
            method: values.method,
            expected_status: Number(values.expected),
          },
          schedule_type: values.schedule as 'interval' | 'cron',
          ...(values.schedule === 'interval'
            ? { interval_sec: Number(values.interval) }
            : { cron_expr: values.cron }),
          timezone: values.timezone,
          timeout_sec: Number(values.timeout),
          retry_limit: Number(values.retry),
        };
      } else if (resource === 'slis') {
        const criteria: unknown = JSON.parse(values.criteria);
        if (!criteria || typeof criteria !== 'object' || Array.isArray(criteria))
          throw new Error('Good-event criteria must be a JSON object.');
        if (
          !Object.keys(criteria).length &&
          ['availability', 'quality', 'latency'].includes(values.sliType)
        )
          throw new Error('Enter good-event criteria for this SLI type.');
        for (const key of Object.keys(criteria))
          if (!['status', 'duration_ms'].includes(key))
            throw new Error(
              `Criteria field ${key} has no run-outcome producer. Use status or duration_ms.`,
            );
        const c = criteria as Record<string, unknown>;
        if (
          c.status !== undefined &&
          (typeof c.status !== 'string' || !['success', 'failed'].includes(c.status))
        )
          throw new Error('Status criteria must be success or failed.');
        if (c.duration_ms !== undefined) {
          const d = c.duration_ms;
          const numeric =
            typeof d === 'number'
              ? d
              : typeof d === 'object' &&
                  d !== null &&
                  'value' in d &&
                  'op' in d &&
                  typeof d.op === 'string' &&
                  ['<', '<=', '>', '>=', '==', '!='].includes(d.op)
                ? d.value
                : null;
          if (typeof numeric !== 'number' || !Number.isFinite(numeric) || numeric < 0)
            throw new Error(
              'Duration criteria must contain a finite non-negative number and a supported operator.',
            );
        }
        if (!values.source.trim()) throw new Error('Choose a monitoring source.');
        validateMonitoringSource(values.source.trim(), sourceKind);
        if (
          !selectedSource ||
          selectedSource.value !== values.source.trim() ||
          selectedSource.kind !== sourceKind
        )
          await resolveMonitoringSource(client, values.source.trim(), sourceKind);
        if (!alive.current) return;
        payload = {
          ...shared,
          sli_type: values.sliType as CreateInputs['slis']['sli_type'],
          source_type: 'job_run',
          source_config: { source_uid: values.source.trim() },
          aggregation: values.aggregation as 'ratio' | 'count',
          good_event_criteria: criteria as Record<string, unknown>,
        };
      } else {
        if (!/^[1-9]\d*$/.test(values.sliId)) throw new Error('Enter a positive SLI ID.');
        if (!(
          Number.isInteger(Number(values.hours)) &&
          Number(values.hours) >= 1 &&
          Number(values.hours) <= 8760
        ))
          throw new Error('Rolling window must be a whole number from 1 to 8760 hours.');
        if (Number(values.slow) >= Number(values.fast))
          throw new Error('Slow burn threshold must be less than fast burn threshold.');
        await withCredential(client, () => client.sli(values.sliId));
        if (!alive.current) return;
        payload = {
          ...shared,
          sli_id: values.sliId,
          target: Number(values.target) / 100,
          window_type: 'rolling',
          window_duration: `${values.hours}h`,
          alert_fast_burn_rate: Number(values.fast),
          alert_slow_burn_rate: Number(values.slow),
        };
      }
      if (authorization.current) return;
      writeAttempted = true;
      const result = await withCredential(client, () => client.create(resource, payload));
      recordMonitoringCreation(client, { resource, id: String(result.id), name: shared.name });
      completed = true;
      resetMonitoringDraft(client, scope);
      if (alive.current) await created(result.id);
    } catch (cause) {
      if (!completed) {
        updateMonitoringDraft(client, scope, {
          error: cause instanceof Error ? cause : new Error('Creation failed'),
          uncertain:
            writeAttempted &&
            cause instanceof APIError &&
            (cause.status === 0 || cause.status >= 500 || cause.code === 'INVALID_RESPONSE'),
        });
      }
    } finally {
      inFlight.current = false;
      if (!completed) updateMonitoringDraft(client, scope, { pending: false });
    }
  }
  return (
    <Dialog.Content
      maxWidth="600px"
      className="console-dialog monitoring-sheet"
      aria-describedby={undefined}
      onEscapeKeyDown={(e) => {
        if (pending) e.preventDefault();
      }}
      onPointerDownOutside={(e) => {
        if (pending) e.preventDefault();
      }}
    >
      <Dialog.Title size="4" className="monitoring-sheet-title">
        New {singular}
      </Dialog.Title>
      <form
        className="monitoring-sheet-form"
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        <div className="monitoring-sheet-body">
          <fieldset
            disabled={pending || uncertain || Boolean(authorizationError)}
            className="monitoring-form"
          >
            <fieldset className="monitoring-field-group">
              <legend>Details</legend>
              {input('name', 'Name', { required: true, maxLength: 128 })}
              <Field label="Description">
                <TextArea
                  aria-label="Description"
                  value={values.description}
                  maxLength={512}
                  rows={2}
                  onChange={(e) => set('description', e.target.value)}
                />
              </Field>
            </fieldset>
            {resource === 'checks' ? (
              <>
                <fieldset className="monitoring-field-group">
                  <legend>Request</legend>
                  {input('url', 'URL', {
                    type: 'url',
                    required: true,
                    maxLength: 2048,
                    placeholder: 'https://service.example/health',
                  })}
                  <div className="form-columns">
                    <Choice
                      label="Method"
                      value={values.method}
                      values={['GET', 'HEAD']}
                      change={(v) => set('method', v)}
                    />
                    {input('expected', 'Expected status', {
                      type: 'number',
                      min: 100,
                      max: 599,
                      required: true,
                    })}
                  </div>
                </fieldset>
                <fieldset className="monitoring-field-group">
                  <legend>Timing</legend>
                  <Choice
                    label="Schedule"
                    value={values.schedule}
                    values={['interval', 'cron']}
                    change={(v) => set('schedule', v)}
                  />
                  {values.schedule === 'interval'
                    ? input('interval', 'Interval (seconds)', {
                        type: 'number',
                        min: 30,
                        required: true,
                      })
                    : input('cron', 'Cron expression', {
                        required: true,
                        placeholder: '*/5 * * * *',
                      })}
                  <div className="form-columns">
                    {input('timeout', 'Timeout (seconds)', {
                      type: 'number',
                      min: 1,
                      required: true,
                    })}
                    {input('retry', 'Retry limit', {
                      type: 'number',
                      min: 0,
                      max: 10,
                      required: true,
                    })}
                  </div>
                  <details className="advanced-details">
                    <summary>Time zone</summary>
                    {input('timezone', 'Timezone', { required: true, maxLength: 64 })}
                  </details>
                </fieldset>
              </>
            ) : resource === 'slis' ? (
              <>
                <fieldset className="monitoring-field-group">
                  <legend>Source</legend>
                  <Choice
                    label="SLI type"
                    value={values.sliType}
                    values={['availability', 'latency', 'quality', 'custom']}
                    change={(v) => {
                      set('sliType', v);
                      if (v === 'latency') {
                        set('criteria', '{"duration_ms":{"op":"<=","value":1000}}');
                        set('criterionMode', 'duration');
                      }
                    }}
                  />
                  <ResourcePicker
                    label="Monitoring source"
                    value={values.source}
                    change={(value) => set('source', value)}
                    sourceKind={sourceKind}
                    changeKind={setSourceKind}
                    selected={setSelectedSource}
                  />
                  <details className="advanced-details">
                    <summary>Enter source ID manually</summary>
                    <Field label="Source UID">
                      <TextField.Root
                        aria-label="Source UID"
                        value={values.source}
                        placeholder="check-123 or an existing Job source UID"
                        onChange={(event) => {
                          set('source', event.target.value);
                          setSourceKind(monitoringSourceKind(event.target.value));
                        }}
                      />
                    </Field>
                    <Text size="1" color="gray">
                      Use <code>check-ID</code> for checks or <code>function-ID</code> for
                      functions.
                    </Text>
                  </details>
                </fieldset>
                <fieldset className="monitoring-field-group">
                  <legend>Measurement</legend>
                  <Choice
                    label="Aggregation"
                    value={values.aggregation}
                    values={['ratio', 'count']}
                    change={(v) => set('aggregation', v)}
                  />
                  <Choice
                    label="Good execution rule"
                    value={values.criterionMode}
                    values={['success', 'duration', 'custom']}
                    change={(value) => {
                      set('criterionMode', value);
                      if (value === 'success') set('criteria', '{"status":"success"}');
                      if (value === 'duration')
                        set(
                          'criteria',
                          JSON.stringify({
                            duration_ms: { op: '<=', value: Number(values.duration) },
                          }),
                        );
                    }}
                  />
                  {values.criterionMode === 'duration' && (
                    <Field label="Maximum duration in milliseconds">
                      <TextField.Root
                        aria-label="Maximum duration in milliseconds"
                        type="number"
                        min={0}
                        required
                        value={values.duration}
                        onChange={(event) => {
                          set('duration', event.target.value);
                          set(
                            'criteria',
                            JSON.stringify({
                              duration_ms: { op: '<=', value: Number(event.target.value) },
                            }),
                          );
                        }}
                      />
                    </Field>
                  )}
                  <details className="advanced-details" open={values.criterionMode === 'custom'}>
                    <summary>Custom criteria</summary>
                    <Field label="Good-event criteria (JSON)">
                      <TextArea
                        className="mono"
                        aria-label="Good-event criteria (JSON)"
                        value={values.criteria}
                        required
                        rows={4}
                        onChange={(e) => {
                          set('criteria', e.target.value);
                          set('criterionMode', 'custom');
                        }}
                      />
                    </Field>
                  </details>
                </fieldset>
              </>
            ) : (
              <>
                <fieldset className="monitoring-field-group">
                  <legend>Target</legend>
                  <ResourcePicker
                    label="Service indicator"
                    indicatorOnly
                    value={values.sliId}
                    change={(value) => set('sliId', value)}
                  />
                  <details className="advanced-details">
                    <summary>Enter indicator ID manually</summary>
                    {input('sliId', 'SLI ID', { placeholder: 'Existing SLI ID' })}
                  </details>
                  <div className="form-columns">
                    {input('target', 'Target (%)', {
                      type: 'number',
                      min: 0.01,
                      max: 100,
                      step: '0.01',
                      required: true,
                    })}
                    {input('hours', 'Rolling window (hours)', {
                      type: 'number',
                      min: 1,
                      max: 8760,
                      step: '1',
                      required: true,
                    })}
                  </div>
                </fieldset>
                <fieldset className="monitoring-field-group">
                  <legend>Alert thresholds</legend>
                  <div className="form-columns">
                    {input('fast', 'Fast burn threshold', {
                      type: 'number',
                      min: 0.1,
                      step: '0.01',
                      required: true,
                    })}
                    {input('slow', 'Slow burn threshold', {
                      type: 'number',
                      min: 0.1,
                      step: '0.01',
                      required: true,
                    })}
                  </div>
                </fieldset>
              </>
            )}
          </fieldset>
          {authorizationError ? (
            <ErrorState error={authorizationError} />
          ) : (
            error && <ErrorState error={error} />
          )}
          {uncertain && (
            <Text as="p" size="1" color="gray">
              Creation was not confirmed. Close and refresh the list to check whether it was saved.
              This draft stays locked when reopened to prevent a duplicate creation.
            </Text>
          )}
          {discard && (
            <section className="operation-notice" aria-label="Discard unconfirmed monitoring draft">
              <Text as="p" size="2">
                The server may have created this {singular}. Check the list before discarding this
                draft. Discarding does not undo a saved resource; creating again may duplicate it.
              </Text>
              <Flex gap="3" mt="3" wrap="wrap">
                <Button type="button" size="1" variant="soft" onClick={() => setDiscard(false)}>
                  Keep draft
                </Button>
                <Button type="button" size="1" color="red" onClick={discardDraft}>
                  Discard unconfirmed draft
                </Button>
              </Flex>
            </section>
          )}
        </div>
        <Flex gap="3" justify="end" className="monitoring-sheet-actions" wrap="wrap">
          <Button
            type="button"
            size="1"
            color="gray"
            variant="ghost"
            disabled={pending}
            onClick={() => (uncertain ? setDiscard(true) : discardDraft())}
          >
            Discard draft
          </Button>
          <Button
            type="button"
            size="1"
            color="gray"
            variant="soft"
            disabled={pending}
            onClick={close}
          >
            Cancel
          </Button>
          <Button
            type="submit"
            size="1"
            disabled={pending || uncertain || Boolean(authorizationError)}
          >
            {pending ? 'Creating…' : `Create ${singular}`}
          </Button>
        </Flex>
      </form>
    </Dialog.Content>
  );
}
