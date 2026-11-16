import { parse, stringify, LosslessNumber } from 'lossless-json';
import { Button, Checkbox, Flex, Heading, Select, Text, TextField } from '@radix-ui/themes';
import type { DefinitionKind } from './api/definitions';
import { WorkflowSchedulePreview } from './WorkflowSchedulePreview';
import { ContainerImageField } from './ContainerImageField';
import { WorkflowJobPicker } from './WorkflowJobPicker';
import { ExecutionResourcesFields, type ExecutionResources } from './ExecutionResourcesFields';
import { ExecutionCommandFields } from './ExecutionCommandFields';
import { RuntimePresetField } from './RuntimePresetField';
import { ScheduleGenerator } from './ScheduleGenerator';
import type { ExecutionCommandInput } from './execution-command';

type ObjectValue = Record<string, unknown>;
function object(value: unknown): ObjectValue {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
    ? (value as ObjectValue)
    : {};
}
export function DefinitionFields({
  kind,
  namespace,
  spec,
  disabled,
  change,
  commandInput,
  changeCommand,
}: {
  kind: DefinitionKind;
  namespace: string;
  spec: string;
  disabled: boolean;
  change: (value: string) => void;
  commandInput?: ExecutionCommandInput;
  changeCommand: (value: string, input?: ExecutionCommandInput) => void;
}) {
  let value: ObjectValue;
  try {
    value = object(parse(spec));
  } catch {
    return (
      <Text as="p" size="2">
        Correct the JSON in Advanced spec before using the form.
      </Text>
    );
  }
  const workflow = kind === 'workflows';
  const template = object(value.jobTemplate);
  function set(path: string[], next: unknown) {
    const updated = object(parse(spec));
    let target = updated;
    for (const name of path.slice(0, -1)) {
      target[name] = { ...object(target[name]) };
      target = target[name] as ObjectValue;
    }
    target[path.at(-1)!] = next;
    change(stringify(updated, undefined, 2)!);
  }
  function field(label: string, path: string[], current: unknown, numeric = false) {
    return (
      <label className="form-field" key={label}>
        {label}
        <TextField.Root
          aria-label={label}
          disabled={disabled}
          value={String(current ?? '')}
          onChange={(event) =>
            set(
              path,
              numeric && /^\d+$/.test(event.target.value)
                ? new LosslessNumber(event.target.value)
                : event.target.value,
            )
          }
        />
      </label>
    );
  }
  function choice(label: string, path: string[], current: unknown, choices: string[]) {
    const labels: Record<string, string> = {
      Allow: 'Allow overlapping runs',
      Forbid: 'Prevent overlapping runs',
      Skip: 'Skip missed starts',
      FireOnce: 'Run once after a missed start',
      CatchUpBounded: 'Catch up within the limit',
      FailFast: 'Stop on failure',
      Continue: 'Continue other tasks',
    };
    return (
      <label className="form-field">
        {label}
        <Select.Root
          disabled={disabled}
          value={String(current ?? '')}
          onValueChange={(next) => set(path, next)}
        >
          <Select.Trigger aria-label={label} placeholder="Default" />
          <Select.Content>
            {choices.map((item) => (
              <Select.Item key={item} value={item} disabled={item === 'Replace'}>
                {labels[item] ?? item}
              </Select.Item>
            ))}
          </Select.Content>
        </Select.Root>
      </label>
    );
  }
  const schedule = String(value.schedule ?? '');
  const tasks = Array.isArray(value.tasks) ? (value.tasks as ObjectValue[]) : [];
  return (
    <fieldset className="workload-fieldset" disabled={disabled}>
      {!workflow && (
        <section className="authoring-settings-group" data-job-section="container">
          <Heading as="h2" size="3" weight="medium">
            Container
          </Heading>
          <div>
            <RuntimePresetField
              image={String(template.image ?? '')}
              command={Array.isArray(template.command) ? template.command.map(String) : []}
              args={Array.isArray(template.args) ? template.args.map(String) : []}
              disabled={disabled}
              onChange={(image, command, args) => {
                const updated = object(parse(spec));
                updated.jobTemplate = { ...object(updated.jobTemplate), image, command, args };
                if (changeCommand) changeCommand(stringify(updated, undefined, 2)!, undefined);
                else change(stringify(updated, undefined, 2)!);
              }}
            />
            <ContainerImageField
              value={String(template.image ?? '')}
              onChange={(value) => set(['jobTemplate', 'image'], value)}
              disabled={disabled}
              required
            />
            <ExecutionCommandFields
              command={Array.isArray(template.command) ? template.command.map(String) : []}
              args={Array.isArray(template.args) ? template.args.map(String) : []}
              disabled={disabled}
              input={commandInput}
              onChange={(command, args, input) => {
                const updated = object(parse(spec));
                updated.jobTemplate = { ...object(updated.jobTemplate), command, args };
                changeCommand(stringify(updated, undefined, 2)!, input);
              }}
            />
          </div>
        </section>
      )}
      <section className="authoring-settings-group" data-job-section="schedule">
        <Heading as="h2" size="3" weight="medium">
          Schedule
        </Heading>
        <div>
          <div className={`manifest-fields${!workflow && schedule ? ' job-schedule-fields' : ''}`}>
            <label className="form-field">
              Trigger mode
              <Select.Root
                value={schedule ? 'scheduled' : 'manual'}
                disabled={disabled}
                onValueChange={(next) => set(['schedule'], next === 'manual' ? '' : '0 2 * * *')}
              >
                <Select.Trigger aria-label="Trigger mode" />
                <Select.Content>
                  <Select.Item value="manual">Manual only</Select.Item>
                  <Select.Item value="scheduled">Scheduled and manual</Select.Item>
                </Select.Content>
              </Select.Root>
            </label>
            {schedule && (
              <div>
                {field('Configured schedule', ['schedule'], schedule)}
                <ScheduleGenerator
                  value={schedule}
                  timeZone={workflow ? String(value.timeZone || 'UTC') : 'UTC'}
                  disabled={disabled}
                  onChange={(next) => set(['schedule'], next)}
                />
              </div>
            )}
            {workflow && schedule && field('Time zone', ['timeZone'], value.timeZone || 'UTC')}
            {field('Timeout in seconds', ['timeoutSeconds'], value.timeoutSeconds ?? 0, true)}
          </div>
          <Text as="label" size="2" className="manifest-suspend">
            <Flex align="center" gap="2">
              <Checkbox
                checked={Boolean(value.suspend)}
                disabled={disabled}
                onCheckedChange={(next) => set(['suspend'], next === true)}
              />
              {workflow
                ? 'Pause automatic starts; manual runs stay available'
                : 'Suspend automatic and manual starts'}
            </Flex>
          </Text>
          {workflow && schedule && (
            <WorkflowSchedulePreview schedule={schedule} zone={String(value.timeZone || 'UTC')} />
          )}
        </div>
      </section>
      <details className="advanced-run-settings" data-job-section="policies">
        <summary>Run policies</summary>
        <div className="manifest-fields">
          {choice(
            workflow ? 'Concurrent workflow runs' : 'Concurrency policy',
            ['concurrencyPolicy'],
            value.concurrencyPolicy,
            !workflow && value.concurrencyPolicy === 'Replace'
              ? ['Allow', 'Forbid', 'Replace']
              : ['Allow', 'Forbid'],
          )}
          {!workflow &&
            choice('Missed starts', ['misfirePolicy'], value.misfirePolicy, [
              'FireOnce',
              'Skip',
              'CatchUpBounded',
            ])}
          {workflow &&
            choice('Failure policy', ['failPolicy'], value.failPolicy, ['FailFast', 'Continue'])}
          {!workflow &&
            field(
              'Maximum attempts',
              ['retryPolicy', 'maxAttempts'],
              object(value.retryPolicy).maxAttempts ?? 1,
              true,
            )}
        </div>
        {value.concurrencyPolicy === 'Forbid' && (
          <Text as="p" size="1" color="gray" mb="3">
            {workflow
              ? 'Scheduled starts are skipped while another run is open. Accepted manual starts wait for it to finish.'
              : 'Scheduled, manual and workflow-task runs cannot overlap. Accepted manual starts wait for earlier runs to finish.'}
          </Text>
        )}
        {!workflow && value.concurrencyPolicy === 'Replace' && (
          <Text as="p" size="1" color="gray" mb="3">
            Replace requests cancellation for scheduled starts and waits for prior runs to finish.
            Manual and workflow-task starts do not replace running work. Choose Allow or Forbid for
            a new configuration.
          </Text>
        )}
      </details>
      {!workflow && (
        <details className="job-authoring-resources" data-job-section="resources">
          <summary>Resources and repeat safety</summary>
          <p>
            Optional overrides. Cluster checks confirm resource defaults and limits before saving.
          </p>
          <ExecutionResourcesFields
            version={Number(value.executionPolicyVersion ?? 0)}
            retrySafe={value.retrySafe === true}
            resources={template.resources as ExecutionResources | undefined}
            disabled={disabled}
            change={(settings) => {
              const updated = object(parse(spec));
              updated.executionPolicyVersion = settings.version;
              updated.retrySafe = settings.retrySafe;
              updated.jobTemplate = {
                ...object(updated.jobTemplate),
                resources: settings.resources,
              };
              change(stringify(updated, undefined, 2)!);
            }}
          />
        </details>
      )}
      {workflow && (
        <section className="form-section">
          <Heading as="h3" size="3" weight="medium" mb="3">
            Workflow tasks
          </Heading>

          {tasks.map((task, index) => (
            <div key={index} className="manifest-fields">
              <label className="form-field">
                Task name
                <TextField.Root
                  aria-label={`Task ${index + 1} name`}
                  disabled={disabled}
                  value={String(task.name ?? '')}
                  onChange={(event) =>
                    set(
                      ['tasks'],
                      tasks.map((item, n) =>
                        n === index ? { ...item, name: event.target.value } : item,
                      ),
                    )
                  }
                />
              </label>
              <div>
                <label className="form-field">
                  Job name
                  <TextField.Root
                    aria-label={`Task ${index + 1} job`}
                    disabled={disabled}
                    value={String(object(task.jobRef).name ?? '')}
                    onChange={(event) =>
                      set(
                        ['tasks'],
                        tasks.map((item, n) =>
                          n === index
                            ? {
                                ...item,
                                jobRef: { ...object(item.jobRef), name: event.target.value },
                              }
                            : item,
                        ),
                      )
                    }
                  />
                </label>
                <WorkflowJobPicker
                  namespace={namespace}
                  value={String(object(task.jobRef).name ?? '')}
                  disabled={disabled}
                  onChoose={(name) =>
                    set(
                      ['tasks'],
                      tasks.map((item, n) =>
                        n === index ? { ...item, jobRef: { ...object(item.jobRef), name } } : item,
                      ),
                    )
                  }
                />
              </div>
              <label className="form-field">
                Depends on
                <TextField.Root
                  aria-label={`Task ${index + 1} dependencies`}
                  disabled={disabled}
                  value={Array.isArray(task.dependsOn) ? task.dependsOn.join(', ') : ''}
                  onChange={(event) =>
                    set(
                      ['tasks'],
                      tasks.map((item, n) =>
                        n === index
                          ? {
                              ...item,
                              dependsOn: event.target.value
                                .split(',')
                                .map((name) => name.trim())
                                .filter(Boolean),
                            }
                          : item,
                      ),
                    )
                  }
                />
              </label>
              <Button
                size="1"
                variant="ghost"
                disabled={disabled}
                onClick={() =>
                  set(
                    ['tasks'],
                    tasks.filter((_, n) => n !== index),
                  )
                }
              >
                Remove task
              </Button>
            </div>
          ))}
          <Button
            size="1"
            variant="outline"
            disabled={disabled}
            onClick={() => set(['tasks'], [...tasks, { name: '', jobRef: { name: '' } }])}
          >
            Add task
          </Button>
        </section>
      )}
    </fieldset>
  );
}
