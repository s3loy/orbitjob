import { useId, useState, type ReactNode } from 'react';
import { Button, Dialog } from '@radix-ui/themes';
import type { Draft } from './api/manifests';
import { readExecutionScript } from './execution-command';
import {
  jobTaskTemplates,
  templateDefaults,
  compileJobTaskTemplate,
  type JobTaskTemplateId,
  type JobTaskTemplateValues,
} from './job-task-templates';
import './job-task-templates.css';

export type JobTaskTemplatesProps = {
  namespace: string;
  disabled?: boolean;
  onOpen?(): void;
  onApply(draft: Draft, sourceLabel: string): void;
};

export function JobTaskTemplates({
  namespace,
  disabled = false,
  onOpen,
  onApply,
}: JobTaskTemplatesProps) {
  const id = useId();
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState<JobTaskTemplateId>('message');
  const [values, setValues] = useState<Record<JobTaskTemplateId, JobTaskTemplateValues>>(() => ({
    message: templateDefaults('message'),
    http: templateDefaults('http'),
    json: templateDefaults('json'),
  }));
  const current = values[active];
  const template = jobTaskTemplates.find((item) => item.id === active)!;
  const compiled = compileJobTaskTemplate(active, current, namespace);
  const canApply = Boolean(compiled.draft) && Object.keys(compiled.errors).length === 0;
  const keys = objectKeys(current.input ?? '');

  if (disabled && open) setOpen(false);

  function changeOpen(next: boolean) {
    if (next && disabled) return;
    if (next) onOpen?.();
    setOpen(next);
  }

  function update(name: string, value: string) {
    if (disabled) return;
    setValues((previous) => ({
      ...previous,
      [active]: { ...previous[active], [name]: value },
    }));
  }

  function fieldProps(name: string) {
    return {
      id: `${id}-${name}`,
      disabled,
      'aria-invalid': Boolean(compiled.errors[name]),
      'aria-describedby': `${id}-${name}-help${compiled.errors[name] ? ` ${id}-${name}-error` : ''}`,
    };
  }

  function field(name: string, label: string, help: string, control: ReactNode) {
    return (
      <div className="job-task-template-field">
        <label htmlFor={`${id}-${name}`}>{label}</label>
        {control}
        <p id={`${id}-${name}-help`} className="job-task-template-help">
          {help}
        </p>
        {compiled.errors[name] && (
          <p id={`${id}-${name}-error`} className="job-task-template-error" role="alert">
            {compiled.errors[name]}
          </p>
        )}
      </div>
    );
  }

  return (
    <Dialog.Root open={open && !disabled} onOpenChange={changeOpen}>
      <Dialog.Trigger>
        <Button type="button" size="2" variant="outline" disabled={disabled}>
          Choose a task template
        </Button>
      </Dialog.Trigger>
      <Dialog.Content maxWidth="960px" className="job-task-templates-dialog">
        <header className="job-task-templates-heading">
          <Dialog.Title>Choose a task template</Dialog.Title>
          <Dialog.Description size="2" className="job-task-templates-description">
            Fill in the task details and review the generated code. You can edit the code and Job
            settings after applying the template.
          </Dialog.Description>
        </header>

        <div className="job-task-templates-body">
          <div className="job-task-templates-layout">
            <section className="job-task-template-parameters" aria-label="Task template parameters">
              <div className="job-task-template-field">
                <label htmlFor={`${id}-template`}>Task template</label>
                <select
                  id={`${id}-template`}
                  value={active}
                  disabled={disabled}
                  aria-describedby={`${id}-template-description`}
                  onChange={(event) => setActive(event.target.value as JobTaskTemplateId)}
                >
                  {jobTaskTemplates.map((item) => (
                    <option key={item.id} value={item.id}>
                      {item.title}
                    </option>
                  ))}
                </select>
                <p id={`${id}-template-description`} className="job-task-template-help">
                  {template.description}
                </p>
              </div>

              {active === 'message' &&
                field(
                  'message',
                  'Message',
                  'Write the text to print to the run log. A newline is added after the message.',
                  <textarea
                    {...fieldProps('message')}
                    rows={4}
                    value={current.message}
                    onChange={(event) => update('message', event.target.value)}
                  />,
                )}

              {active === 'http' && (
                <>
                  {field(
                    'url',
                    'URL',
                    'The HTTP or HTTPS address to request when the Job runs.',
                    <input
                      {...fieldProps('url')}
                      type="text"
                      inputMode="url"
                      autoCapitalize="none"
                      spellCheck={false}
                      value={current.url}
                      onChange={(event) => update('url', event.target.value)}
                    />,
                  )}
                  {field(
                    'expectedStatus',
                    'Expected status',
                    'An exact HTTP status from 100 to 599. A different status fails the task.',
                    <input
                      {...fieldProps('expectedStatus')}
                      type="text"
                      inputMode="numeric"
                      value={current.expectedStatus}
                      onChange={(event) => update('expectedStatus', event.target.value)}
                    />,
                  )}
                  {field(
                    'requestTimeout',
                    'Request timeout (seconds)',
                    'Use a whole number from 1 to 120 for the HTTP request timeout.',
                    <input
                      {...fieldProps('requestTimeout')}
                      type="text"
                      inputMode="numeric"
                      value={current.requestTimeout}
                      onChange={(event) => update('requestTimeout', event.target.value)}
                    />,
                  )}
                </>
              )}

              {active === 'json' && (
                <>
                  {field(
                    'input',
                    'JSON input',
                    'Paste JSON to include in the Job. Read a field requires an object at the top level.',
                    <textarea
                      {...fieldProps('input')}
                      className="job-task-template-json"
                      rows={7}
                      spellCheck={false}
                      value={current.input}
                      onChange={(event) => update('input', event.target.value)}
                    />,
                  )}
                  {field(
                    'operation',
                    'Operation',
                    'Format prints the complete JSON. Read a field prints one top-level value.',
                    <select
                      {...fieldProps('operation')}
                      value={current.operation}
                      onChange={(event) => update('operation', event.target.value)}
                    >
                      <option value="format">Format JSON</option>
                      <option value="field">Read a field</option>
                    </select>,
                  )}
                  {current.operation === 'field' &&
                    field(
                      'field',
                      'Field',
                      keys === undefined
                        ? 'Enter a valid JSON object to choose a top-level field.'
                        : keys.length === 0
                          ? 'Add a field to the JSON object to select it here.'
                          : 'Choose an existing top-level field. Nested field paths are not supported.',
                      <select
                        {...fieldProps('field')}
                        disabled={disabled || !keys?.length}
                        value={current.field}
                        onChange={(event) => update('field', event.target.value)}
                      >
                        {!keys?.includes(current.field) && (
                          <option value={current.field} disabled>
                            {keys?.length ? 'Choose a field' : 'No field available'}
                          </option>
                        )}
                        {keys?.map((key) => (
                          <option key={key} value={key}>
                            {JSON.stringify(key)}
                          </option>
                        ))}
                      </select>,
                    )}
                </>
              )}
              <p className="job-task-template-code-note">
                These values become ordinary Job code and arguments. Change them in the Job editor
                after applying this template. Use non-secret inputs; values are saved in the Job
                definition.
              </p>
            </section>

            <section
              className="job-task-template-preview"
              aria-labelledby={`${id}-preview-title`}
              data-task-template-preview={canApply ? 'available' : 'unavailable'}
            >
              <h2 id={`${id}-preview-title`}>Generated Job preview</h2>
              {compiled.draft && canApply ? (
                <>
                  <dl className="job-task-template-facts">
                    <div>
                      <dt>Image</dt>
                      <dd>
                        <code>{compiled.draft.image}</code>
                      </dd>
                    </div>
                    <div>
                      <dt>Trigger</dt>
                      <dd>Manual</dd>
                    </div>
                    <div>
                      <dt>Retries</dt>
                      <dd>None</dd>
                    </div>
                  </dl>
                  <p className="job-task-template-save-note">
                    Saving the Job does not start a run.
                  </p>
                  <ul className="job-task-template-behavior">
                    {compiled.behavior.map((behavior, index) => (
                      <li key={index}>{behavior}</li>
                    ))}
                  </ul>
                  {active === 'message' && (
                    <pre className="job-task-template-message" aria-label="Message to print">
                      {current.message}
                      {'\n'}
                    </pre>
                  )}
                  <details className="job-task-template-exact">
                    <summary>View generated code and arguments</summary>
                    <ExactInvocation draft={compiled.draft} />
                  </details>
                </>
              ) : (
                <p className="job-task-template-unavailable" role="status">
                  Preview unavailable. Fix the highlighted task details to generate the Job code.
                </p>
              )}
              <p className="job-task-template-preview-note">
                This preview does not run the task or send a request.
              </p>
            </section>
          </div>
        </div>

        <footer className="job-task-templates-actions">
          <Dialog.Close>
            <Button type="button" variant="soft" color="gray">
              Cancel
            </Button>
          </Dialog.Close>
          <Button
            type="button"
            disabled={disabled || !canApply}
            onClick={() => {
              if (disabled || !canApply || !compiled.draft) return;
              onApply(compiled.draft, template.title);
              setOpen(false);
            }}
          >
            Use this template
          </Button>
        </footer>
      </Dialog.Content>
    </Dialog.Root>
  );
}

function ExactInvocation({ draft }: { draft: Draft }) {
  const script = readExecutionScript(draft);
  return (
    <div className="job-task-template-invocation">
      <div>
        <h3>Command</h3>
        <pre aria-label="Generated command">
          <code>{JSON.stringify(draft.command)}</code>
        </pre>
      </div>
      <div>
        <h3>Arguments</h3>
        <ol className="job-task-template-arguments" aria-label="Generated arguments">
          {draft.args.map((argument, index) => {
            const isScript = script?.location === 'args' && script.index === index;
            return (
              <li key={index}>
                <span>
                  args[{index}]{isScript ? ' (Python script)' : ''}
                </span>
                <pre
                  className={isScript ? 'job-task-template-script' : undefined}
                  aria-label={isScript ? 'Generated Python script' : `Argument ${index}`}
                >
                  <code>{isScript ? argument : JSON.stringify(argument)}</code>
                </pre>
              </li>
            );
          })}
        </ol>
        {draft.args.some((argument) => argument.includes('$$')) && (
          <p className="job-task-template-help job-task-template-argument-note">
            Dollar signs in input values are escaped as $$ for Kubernetes; the program receives the
            original text.
          </p>
        )}
      </div>
    </div>
  );
}

function objectKeys(input: string): string[] | undefined {
  if (input.length > 32768) return undefined;
  try {
    const parsed: unknown = JSON.parse(input);
    return parsed !== null && typeof parsed === 'object' && !Array.isArray(parsed)
      ? Object.keys(parsed)
      : undefined;
  } catch {
    return undefined;
  }
}
