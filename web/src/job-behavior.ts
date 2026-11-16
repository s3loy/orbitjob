import { LosslessNumber, parse } from 'lossless-json';

export type JobBehaviorSection = 'general' | 'container' | 'schedule' | 'policies' | 'resources';
type ObjectValue = Record<string, unknown>;
type DraftIssue = { section: JobBehaviorSection; message: string };
type Tokens = { state: 'inherited' | 'explicit' | 'invalid'; values: string[] };
type ResourceValue = { name: string; group: string; value: string };

export type JobBehavior = {
  readable: boolean;
  save: string;
  trigger: string;
  schedule?: string;
  suspended: boolean;
  image?: string;
  command: Tokens;
  args: Tokens;
  deadline: string;
  deadlineDetail?: string;
  attempts: string;
  backoff: string;
  showBackoff: boolean;
  repeat?: string;
  repeatWarning: boolean;
  overlap: string[];
  misfire?: string;
  resources: ResourceValue[];
  resourceNote: string;
  issues: DraftIssue[];
};

function object(value: unknown): ObjectValue | undefined {
  return value !== null &&
    typeof value === 'object' &&
    !Array.isArray(value) &&
    !(value instanceof LosslessNumber)
    ? (value as ObjectValue)
    : undefined;
}

function tokens(value: unknown): Tokens {
  if (value === undefined || (Array.isArray(value) && value.length === 0))
    return { state: 'inherited', values: [] };
  if (!Array.isArray(value) || value.some((entry) => typeof entry !== 'string'))
    return { state: 'invalid', values: [] };
  return { state: 'explicit', values: value as string[] };
}

function integer(value: unknown, fallback: string, minimum = 0): string | undefined {
  if (value === undefined) return fallback;
  const text = value instanceof LosslessNumber ? value.toString() : undefined;
  if (!text || !/^\d+$/.test(text)) return undefined;
  const number = BigInt(text);
  return number >= BigInt(minimum) && number <= 2147483647n ? text : undefined;
}

function overlap(policy: unknown): string[] {
  switch (policy) {
    // sourcePolicy resolves an omitted ScheduledJob policy to Allow.
    case undefined:
    case '':
    case 'Allow':
      return ['Scheduled, manual and workflow-task starts may overlap.'];
    case 'Forbid':
      return [
        'Scheduled, manual and workflow-task starts share one admission rule: runs cannot overlap.',
        'Scheduled starts are skipped while a prior run is open. Accepted manual starts wait for earlier runs to finish; workflow tasks also wait for admission.',
      ];
    case 'Replace':
      return [
        'A scheduled start requests cancellation of prior runs and waits for them to finish.',
        'Manual and workflow-task starts leave running work in place and may overlap. Replace is a legacy setting; choose Allow or Forbid for a new configuration.',
      ];
    default:
      return ['Choose a concurrency policy to explain what happens to overlapping starts.'];
  }
}

export function jobBehavior(spec: string, context: 'create' | 'edit'): JobBehavior {
  const result: JobBehavior = {
    readable: false,
    save: '',
    trigger: '',
    suspended: false,
    command: { state: 'inherited', values: [] },
    args: { state: 'inherited', values: [] },
    deadline: '',
    attempts: '',
    backoff: '',
    showBackoff: false,
    repeatWarning: false,
    overlap: [],
    resources: [],
    resourceNote: '',
    issues: [],
  };
  let value: ObjectValue | undefined;
  try {
    value = object(parse(spec));
  } catch {
    // A partial JSON edit cannot establish execution behavior.
  }
  if (!value) {
    result.issues.push({
      section: 'general',
      message: 'Finish the advanced JSON object to see how this Job will run.',
    });
    return result;
  }
  result.readable = true;
  const parsedTemplate = object(value.jobTemplate);
  const invalidTemplate = value.jobTemplate !== undefined && !parsedTemplate;
  const template = parsedTemplate ?? {};
  const issue = (section: JobBehaviorSection, message: string) =>
    result.issues.push({ section, message });
  if (invalidTemplate) issue('container', 'Use an object for the execution content.');
  const schedule = value.schedule;
  const validSchedule = schedule === undefined || typeof schedule === 'string';
  const suspended = value.suspend === true;
  const validSuspend = value.suspend === undefined || typeof value.suspend === 'boolean';
  result.suspended = suspended;
  if (!validSchedule || (typeof schedule === 'string' && schedule && !schedule.trim()))
    issue('schedule', 'Use a schedule string, or clear it for manual starts only.');
  else if (typeof schedule === 'string' && schedule) result.schedule = schedule;
  if (!validSuspend)
    issue('schedule', 'Set suspension to true or false to establish start behavior.');
  const saveAction =
    context === 'create'
      ? 'Saving creates this Job definition.'
      : 'Saving updates this Job definition.';
  result.save = suspended
    ? `${saveAction} Automatic and manual starts stay suspended.`
    : result.schedule && validSuspend
      ? `${saveAction} Once the saved version is active, its schedule can request future runs.`
      : !validSchedule || !validSuspend || result.issues.length
        ? `${saveAction} Correct the start settings before relying on its trigger behavior.`
        : `${saveAction} Saving does not request a run.`;
  result.trigger = !validSuspend
    ? 'Start behavior is incomplete.'
    : suspended
      ? 'Resume starts before requesting an automatic or manual run.'
      : result.schedule
        ? 'You can also request a manual run or use this Job in a workflow task.'
        : validSchedule && validSuspend && !result.issues.length
          ? 'Request a manual run, or reference this Job from a workflow task.'
          : 'Start behavior is incomplete.';

  if (typeof template.image === 'string' && template.image.trim()) result.image = template.image;
  result.command = tokens(template.command);
  result.args = tokens(template.args);
  if (result.command.state === 'invalid' || result.args.state === 'invalid')
    issue('container', 'Command and arguments must be arrays of literal text entries.');
  if (result.command.state === 'explicit' && result.command.values[0] === '')
    issue('container', 'Set the first command entry to the executable to run.');

  // The typed spec's omitted int32 is zero; BuildJob omits the deadline at zero.
  const timeout = integer(value.timeoutSeconds, '0');
  result.deadline =
    timeout === undefined
      ? 'Set a nonnegative whole-number attempt deadline.'
      : timeout === '0'
        ? 'No deadline is set for each Kubernetes Job attempt.'
        : `Each attempt has a ${timeout}-second Kubernetes Job deadline.`;
  if (timeout !== undefined && timeout !== '0')
    result.deadlineDetail =
      'The deadline includes startup and Pod retries within that Kubernetes Job. It is not a total Run deadline or a process-duration estimate.';
  if (timeout === undefined)
    issue('schedule', 'Correct the timeout before relying on an attempt deadline.');
  const retry = object(value.retryPolicy);
  const invalidRetry = value.retryPolicy !== undefined && !retry;
  const attempts = invalidRetry ? undefined : integer(retry?.maxAttempts, '1');
  const effectiveAttempts = attempts === '0' ? '1' : attempts;
  result.attempts =
    effectiveAttempts === undefined
      ? 'Set a whole-number platform attempt budget.'
      : effectiveAttempts === '1'
        ? 'One platform attempt, including the first. No platform retry follows a failure.'
        : `Up to ${effectiveAttempts} platform attempts, including the first, if earlier attempts fail.`;
  if (effectiveAttempts === undefined)
    issue('policies', 'Correct maximum attempts to explain retries.');
  if (attempts === '0')
    issue(
      'policies',
      'Use at least 1 for maximum attempts. A zero budget resolves to one attempt at runtime.',
    );
  // BuildJob always sets this pointer, including zero; Kubernetes adds no default.
  const backoff = invalidTemplate ? undefined : integer(template.backoffLimit, '0');
  result.backoff =
    backoff === undefined
      ? 'Set a nonnegative Kubernetes Job backoff limit.'
      : backoff === '0'
        ? 'Kubernetes Job backoff is 0: no Pod retry within an attempt.'
        : `Kubernetes Job backoff is ${backoff}: Pod retries happen within one attempt, separately from the platform attempt budget.`;
  if (backoff === undefined)
    issue('policies', 'Correct Kubernetes Job backoff to explain Pod retries.');
  const repeated =
    (effectiveAttempts !== undefined && BigInt(effectiveAttempts) > 1n) ||
    (backoff !== undefined && BigInt(backoff) > 0n);
  result.showBackoff = template.backoffLimit !== undefined || repeated || backoff === undefined;
  const version = integer(value.executionPolicyVersion, '0');
  const validVersion = version === '0' || version === '1';
  if (!validVersion) issue('resources', 'Choose execution policy version 0 or 1.');
  const validRetrySafe = value.retrySafe === undefined || typeof value.retrySafe === 'boolean';
  if (!validRetrySafe) issue('resources', 'Set safe to repeat to true or false.');
  const validRetrySettings =
    effectiveAttempts !== undefined && backoff !== undefined && validVersion && validRetrySafe;
  result.repeatWarning = !validRetrySettings || (repeated && value.retrySafe !== true);
  if (!validRetrySettings)
    result.repeat = 'Correct the retry settings before relying on repeat behavior.';
  else if (repeated)
    result.repeat =
      value.retrySafe === true
        ? 'Marked safe to repeat. If this configuration is accepted, retries can execute the task again.'
        : version === '1'
          ? 'Version 1 requires this task to be marked safe to repeat for these retry settings.'
          : 'Retries can repeat side effects. This task is not marked safe to repeat.';
  result.overlap = overlap(value.concurrencyPolicy);
  if (
    ![undefined, '', 'Allow', 'Forbid', 'Replace'].includes(
      value.concurrencyPolicy as string | undefined,
    )
  )
    issue('policies', 'Choose Allow or Forbid for concurrent runs.');
  if (result.schedule) {
    switch (value.misfirePolicy) {
      // Scheduler.selectOccurrences uses FireOnce for an omitted policy.
      case undefined:
      case '':
      case 'FireOnce':
        result.misfire = 'After missed schedule times, request only the newest eligible start.';
        break;
      case 'Skip':
        result.misfire = 'Skip missed schedule times instead of replaying them.';
        break;
      case 'CatchUpBounded':
        result.misfire =
          "Catch up eligible missed schedule times within the installation's catch-up limit.";
        break;
      default:
        issue('policies', 'Choose how to handle missed schedule times.');
    }
  }

  const resources = object(template.resources);
  if (template.resources !== undefined && !resources)
    issue('resources', 'Use resource requests and limits objects with quantity strings.');
  for (const group of ['requests', 'limits'] as const) {
    const values = object(resources?.[group]);
    if (resources?.[group] !== undefined && !values)
      issue('resources', `Use quantity strings in resource ${group}.`);
    for (const [name, quantity] of Object.entries(values ?? {})) {
      if (!['cpu', 'memory', 'ephemeral-storage'].includes(name))
        issue('resources', `${name} is not a supported resource dimension.`);
      if (typeof quantity === 'string' && quantity.trim())
        result.resources.push({ name, group, value: quantity });
      else issue('resources', `Set a quantity string for ${name} ${group}.`);
    }
  }
  result.resourceNote =
    version === '1'
      ? 'Unspecified requests and limits use installation policy defaults; their values are unknown here. Cluster capacity is not checked.'
      : 'Unspecified requests and limits are unknown here. Cluster capacity is not checked.';
  return result;
}
