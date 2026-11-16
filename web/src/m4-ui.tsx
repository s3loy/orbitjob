import { Button, Flex, Heading, Text, TextField } from '@radix-ui/themes';
import { useState } from 'react';
import { Link } from 'react-router-dom';
import { Empty, ErrorState } from './components';
import { APIError } from './api/transport';
import { Facts, Raw } from './monitoring-ui';
import type { WorkflowDetail } from './api/m4';
import { useSession } from './session';
import './m4.css';

export function M4ReadError({
  error,
  retry,
  kind,
}: {
  error: Error;
  retry: () => void;
  kind: 'workflow' | 'function';
}) {
  const { account, identity } = useSession();
  const accountAccess = Boolean(account?.authenticated || identity?.subject_type === 'user');
  return (
    <>
      <ErrorState error={error} retry={retry} />
      {error instanceof APIError && error.status === 404 && (
        <Text as="p" size="2" color="gray" mt="3">
          {kind === 'workflow'
            ? 'The revision may no longer be active, or this record is no longer visible.'
            : 'The definition or invocation is unavailable or you no longer have access.'}{' '}
          <Link className="record-link" to={kind === 'workflow' ? '/workflows' : '/functions'}>
            Return to {kind === 'workflow' ? 'Workflows' : 'Functions'}
          </Link>{' '}
          and refresh the definitions.
        </Text>
      )}
      {kind === 'workflow' && error instanceof APIError && error.status === 403 && (
        <Text as="p" size="2" color="gray" mt="3">
          {accountAccess
            ? 'Your account does not have access to this workflow. Ask a workspace administrator to review your permissions.'
            : 'This API key does not have workflow access. Workflows require a key without a resource-group restriction and with the required workflow permissions.'}
        </Text>
      )}
    </>
  );
}

export function M4Search({
  search,
  count,
  change,
}: {
  search: string;
  count: number;
  change: (value: string) => void;
}) {
  return (
    <Flex className="list-tools" align="center" gap="3" wrap="wrap">
      <TextField.Root
        size="2"
        type="search"
        aria-label="Search loaded records"
        placeholder="Search this page…"
        value={search}
        onChange={(e) => change(e.target.value)}
      />
      <Text size="1" color="gray" className="loaded-count">
        {count} on this page
      </Text>
    </Flex>
  );
}

export function M4Notice({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="m4-notice" role="status">
      <Text as="p" size="2" weight="medium">
        {title}
      </Text>
      <Text as="p" size="1" color="gray" mt="1">
        {children}
      </Text>
    </section>
  );
}

export function M4Time({ value }: { value?: string | null }) {
  if (!value) return <>Not recorded</>;
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return <>Not recorded</>;
  return (
    <time dateTime={value} title={value}>
      {date.toLocaleString(undefined, {
        month: 'short',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
        hour12: false,
        timeZone: 'UTC',
      })}
    </time>
  );
}

export function ContainerEntries({
  entries,
  empty = 'None',
}: {
  entries: string[];
  empty?: string;
}) {
  if (!entries.length) return <>{empty}</>;
  return (
    <ol className="m4-container-entries">
      {entries.map((entry, index) => (
        <li key={index}>
          <code>{JSON.stringify(entry)}</code>
        </li>
      ))}
    </ol>
  );
}

export function CurrentGraph({ workflow }: { workflow: WorkflowDetail }) {
  const tasks = workflow.tasks;
  const [selected, setSelected] = useState(tasks[0]?.name ?? '');
  const task = tasks.find((t) => t.name === selected) ?? tasks[0];
  // This is a read-only topological presentation, not an execution graph.
  // Explicit dependency labels remain the source of meaning at every viewport.
  const remaining = new Set(tasks.map((t) => t.name));
  const done = new Set<string>();
  const layers: (typeof tasks)[] = [];
  while (remaining.size) {
    const next = tasks.filter(
      (t) => remaining.has(t.name) && t.depends_on.every((dependency) => done.has(dependency)),
    );
    if (!next.length) {
      layers.push(tasks.filter((t) => remaining.has(t.name)));
      break;
    }
    layers.push(next);
    next.forEach((t) => {
      remaining.delete(t.name);
      done.add(t.name);
    });
  }
  if (!task)
    return (
      <Empty title="No tasks in this definition">
        Edit the workflow to add tasks, then refresh this page.
      </Empty>
    );
  const operators: Record<string, string> = {
    eq: '=',
    ne: '!=',
    in: 'in',
    not_in: 'not in',
    lt: '<',
    le: '<=',
    gt: '>',
    ge: '>=',
  };
  return (
    <>
      <Flex justify="between" gap="3" wrap="wrap" align="center">
        <Heading as="h2" size="3" weight="medium">
          Tasks
        </Heading>
      </Flex>
      <div className="m4-task-workspace">
        <div className="m4-dag" role="group" aria-label="Current definition tasks">
          {layers.map((layer, index) => (
            <div className="m4-dag-stage" key={index}>
              {layer.map((t) => (
                <Button
                  key={t.name}
                  color="gray"
                  variant="outline"
                  size="2"
                  className="m4-task"
                  aria-pressed={task.name === t.name}
                  onClick={() => setSelected(t.name)}
                >
                  <span className="m4-task-name">{t.name}</span>
                  <span>{t.job_ref}</span>
                  <span>
                    {t.depends_on.length ? `After ${t.depends_on.join(', ')}` : 'No dependencies'}
                  </span>
                </Button>
              ))}
            </div>
          ))}
        </div>
        <section className="m4-task-detail" aria-label="Selected task">
          <Heading as="h2" size="3" weight="medium">
            {task.name}
          </Heading>
          <Facts
            rows={[
              ['Referenced Job', <code>{task.job_ref}</code>],
              ['Dependencies', task.depends_on.join(', ') || 'None'],
              [
                'Condition',
                task.condition ? (
                  <div>
                    {task.condition.rules.map((rule, index) => (
                      <div key={index}>
                        <code>
                          {rule.task}.{rule.field} {operators[rule.operator] ?? rule.operator}{' '}
                          {rule.number != null ? rule.number : (rule.values ?? []).join(', ')}
                        </code>
                      </div>
                    ))}
                  </div>
                ) : (
                  'No additional condition'
                ),
              ],
              ...(task.condition
                ? [
                    [
                      'Rule combination',
                      task.condition.type === 'any'
                        ? 'At least one rule must pass'
                        : 'All rules must pass',
                    ] as [string, string],
                  ]
                : []),
            ]}
          />
          {task.condition && (
            <Text as="p" size="1" color="gray">
              A task is skipped when its condition is unmet.
            </Text>
          )}
          {task.condition && <Raw value={task.condition} label="Condition fields" />}
        </section>
      </div>
    </>
  );
}
