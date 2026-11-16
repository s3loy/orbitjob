import { parse } from 'lossless-json';
import type { DefinitionKind } from './api/definitions';

type ObjectValue = Record<string, unknown>;
const object = (value: unknown): ObjectValue =>
  value !== null && typeof value === 'object' && !Array.isArray(value)
    ? (value as ObjectValue)
    : {};
const display = (value: unknown, fallback = 'Not configured') =>
  value == null || value === '' ? fallback : String(value);
const entries = (value: unknown) => (Array.isArray(value) ? value.map(String) : []);

export function definitionSummary(kind: DefinitionKind, spec: string) {
  const value = object(parse(spec));
  const template = object(value.jobTemplate);
  const workflow = kind === 'workflows';
  const schedule = display(value.schedule, 'Manual only');
  const tasks = Array.isArray(value.tasks) ? value.tasks.map(object) : [];
  const history = object(value.history);
  return {
    schedule,
    timeZone: workflow && value.schedule ? display(value.timeZone, 'UTC') : undefined,
    starts: value.suspend
      ? workflow
        ? 'Automatic starts paused. Manual runs available.'
        : 'Automatic and manual starts suspended'
      : value.schedule
        ? 'Automatic and manual starts enabled'
        : 'Runs only when manually requested',
    timeout: display(value.timeoutSeconds, '0'),
    concurrency: display(value.concurrencyPolicy, 'Installation default'),
    misfire: display(value.misfirePolicy, 'Installation default'),
    failure: display(value.failPolicy, 'Installation default'),
    successfulHistory: display(history.successfulRuns, '0'),
    failedHistory: display(history.failedRuns, '0'),
    backoff: display(template.backoffLimit, 'Installation default'),
    attempts:
      display(object(value.retryPolicy).maxAttempts, '1') === '0'
        ? '1'
        : display(object(value.retryPolicy).maxAttempts, '1'),
    image: display(template.image),
    resourceFields: Object.entries(object(template.resources)).flatMap(([group, values]) =>
      Object.entries(object(values)).map(([name, quantity]): [string, string] => [
        `${name === 'cpu' ? 'CPU' : name === 'memory' ? 'Memory' : 'Temporary storage'} ${group === 'requests' ? 'request' : 'limit'}`,
        String(quantity),
      ]),
    ),
    retrySafe: value.retrySafe === true,
    command: entries(template.command),
    args: entries(template.args),
    tasks: tasks.map((task) => ({
      name: display(task.name),
      job: display(object(task.jobRef).name),
      dependencies: entries(task.dependsOn),
      conditional: Boolean(task.condition),
    })),
  };
}
