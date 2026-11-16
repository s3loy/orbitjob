import { documentSchema } from './api/administration';

// Task templates contain explicit actions implemented in policy/KnownActions.
const execution = [
  'job:Apply',
  'job:Get',
  'job:List',
  'job:Trigger',
  'instance:Cancel',
  'instance:Get',
  'instance:ReadLogs',
  'instance:List',
  'function:Create',
  'function:Update',
  'function:Get',
  'function:List',
  'function:ReadLogs',
  'function:Invoke',
  'workflow:Apply',
  'workflow:Get',
  'workflow:List',
  'workflow:Trigger',
];
const monitoring = [
  'check:Create',
  'check:Delete',
  'check:Get',
  'check:List',
  'check:Pause',
  'check:Resume',
  'checkrun:Get',
  'checkrun:List',
  'sli:Create',
  'sli:Delete',
  'sli:Get',
  'sli:List',
  'slo:Create',
  'slo:Delete',
  'slo:Get',
  'slo:List',
  'slo:Pause',
  'slo:Resume',
  'slo:GetBudget',
  'slo:ListBudgets',
  'sloalert:Get',
  'sloalert:List',
];
export const policyTemplates = {
  workloads: {
    name: 'Operate workloads and monitoring',
    description:
      'Create and run workloads, investigate logs, configure checks and objectives. Excludes deletion and access administration.',
    actions: [...execution, ...monitoring.filter((action) => !action.endsWith(':Delete'))],
  },
  readonly: {
    name: 'View workloads and diagnostics',
    description:
      'Read definitions, execution history, logs and monitoring without starting or changing workloads.',
    actions: [...execution, ...monitoring].filter((action) =>
      /:(Get|List|ReadLogs|GetBudget|ListBudgets)$/.test(action),
    ),
  },
  monitoring: {
    name: 'Manage checks and service objectives',
    description: 'Configure checks, indicators and objectives; inspect results and error budgets.',
    actions: monitoring,
  },
};
export const guidedActions = [...new Set([...execution, ...monitoring])];
export type PolicyScope = { group: string; type: string; id: string };
export function policyDocument(
  actions = policyTemplates.workloads.actions,
  scope: PolicyScope = { group: '*', type: '*', id: '*' },
) {
  const types =
    scope.type === '*' ? [...new Set(actions.map((action) => action.split(':')[0]))] : [scope.type];
  return JSON.stringify(
    {
      version: '1',
      statement: [
        {
          effect: 'Allow',
          action: actions,
          resource: types.map((type) => `orbitjob:self:${scope.group}:${type}/${scope.id}`),
        },
      ],
    },
    null,
    2,
  );
}
function policyJSON(value: string): unknown {
  const parsed: unknown = JSON.parse(value);
  const containers: (Set<string> | null)[] = [];
  for (const token of value.matchAll(/"(?:\\.|[^"\\])*"|[{}[\]]/g)) {
    const text = token[0];
    if (text === '{' || text === '[') containers.push(text === '{' ? new Set() : null);
    else if (text === '}' || text === ']') containers.pop();
    else {
      let next = token.index + text.length;
      while (next < value.length && /[\t\r\n ]/.test(value[next])) next++;
      const keys = containers[containers.length - 1];
      if (value[next] !== ':' || !keys) continue;
      const key = JSON.parse(text) as string;
      if (keys.has(key))
        throw new Error(
          `Duplicate policy field "${key}". Keep one explicit value before formatting or creating this policy.`,
        );
      keys.add(key);
    }
  }
  return parsed;
}
export function formatPolicyDocument(value: string) {
  return JSON.stringify(parsePolicyDocument(value), null, 2);
}
function supportedFields(value: unknown, fields: string[], path: string) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return;
  for (const key of Object.keys(value))
    if (!fields.includes(key))
      throw new Error(
        `Unsupported policy field ${path}.${key}. This API cannot enforce that rule. Keep the draft and revise the policy explicitly before creating it.`,
      );
}
export function parsePolicyDocument(value: string) {
  const raw = policyJSON(value);
  supportedFields(raw, ['version', 'statement'], 'document');
  if (raw && typeof raw === 'object' && 'statement' in raw && Array.isArray(raw.statement))
    raw.statement.forEach((statement: unknown, index: number) =>
      supportedFields(statement, ['effect', 'action', 'resource'], `statement[${index}]`),
    );
  const parsed = documentSchema.parse(raw);
  if (!parsed.statement.length) throw new Error('Add at least one statement.');
  for (const [index, statement] of parsed.statement.entries()) {
    if (!statement.action.length || !statement.resource.length)
      throw new Error(`Statement ${index + 1} needs actions and resources.`);
    if (
      statement.action.some((action) => action.includes('*') || !/^[a-z]+:[A-Za-z]+$/.test(action))
    )
      throw new Error(`Statement ${index + 1} must use explicit supported actions.`);
    if (statement.resource.some((resource) => !/^orbitjob:self:[^:]*:[^:/]+\/.+$/.test(resource)))
      throw new Error(
        `Statement ${index + 1} must use resources in this workspace: orbitjob:self:GROUP:TYPE/ID.`,
      );
  }
  return parsed;
}
export function guidedPolicy(value: string) {
  try {
    const document = parsePolicyDocument(value);
    if (
      Object.keys(document).some((key) => !['version', 'statement'].includes(key)) ||
      document.version !== '1' ||
      document.statement.length !== 1
    )
      return;
    const statement = document.statement[0];
    if (
      statement.effect !== 'Allow' ||
      Object.keys(statement).some((key) => !['effect', 'action', 'resource'].includes(key)) ||
      statement.action.some((action) => !guidedActions.includes(action))
    )
      return;
    const [, , group, tail] = statement.resource[0].split(':');
    const split = tail.indexOf('/');
    const scope = {
      group,
      type: statement.resource.length > 1 ? '*' : tail.slice(0, split),
      id: tail.slice(split + 1),
    };
    const generated = JSON.parse(policyDocument(statement.action, scope)) as {
      statement: { resource: string[] }[];
    };
    if (
      JSON.stringify([...generated.statement[0].resource].sort()) !==
      JSON.stringify([...statement.resource].sort())
    )
      return;
    return { actions: statement.action, scope };
  } catch {
    return;
  }
}
export function policySummary(document: {
  statement: { effect: string; action: string[]; resource: string[] }[];
}) {
  const allowed = [
    ...new Set(document.statement.filter((s) => s.effect === 'Allow').flatMap((s) => s.action)),
  ];
  const denied = document.statement.filter((s) => s.effect === 'Deny').length;
  return `${allowed.includes('*') ? 'All actions' : `${allowed.length} allowed actions`}${denied ? `, ${denied} deny ${denied === 1 ? 'rule' : 'rules'}` : ''}`;
}
