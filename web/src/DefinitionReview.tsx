import { Heading, Text } from '@radix-ui/themes';
import { Facts } from './monitoring-ui';
import type { DefinitionKind } from './api/definitions';
import { definitionSummary } from './definition-summary';
import { useSession } from './session';

export function DefinitionReview({
  kind,
  namespace,
  name,
  spec,
  showSaveNotice = true,
}: {
  kind: DefinitionKind;
  namespace: string;
  name: string;
  spec: string;
  showSaveNotice?: boolean;
}) {
  const { mode } = useSession();
  let summary: ReturnType<typeof definitionSummary>;
  try {
    summary = definitionSummary(kind, spec);
  } catch {
    return (
      <Text as="p" size="2">
        Correct the advanced JSON to review the configuration.
      </Text>
    );
  }
  const retention = (count: string) => (count === '0' ? 'Default' : count);
  const policyLabels: Record<string, string> = {
    'Installation default': 'Default',
    Allow: 'Allow overlapping runs',
    Forbid: 'Prevent overlapping runs',
    Skip: 'Skip missed starts',
    FireOnce: 'Run once after a missed start',
    CatchUpBounded: 'Catch up within the limit',
    FailFast: 'Stop on failure',
    Continue: 'Continue other tasks',
  };
  const policy = (value: string) => policyLabels[value] ?? value;
  return (
    <section className="definition-summary" aria-label="Workload save summary">
      <Facts
        rows={[
          ['Name', name],
          ['Namespace', namespace],
          ['Schedule', summary.schedule],
          ...(summary.timeZone ? ([['Time zone', summary.timeZone]] as [string, string][]) : []),
          ['Starts', summary.starts],
        ]}
      />
      {kind === 'jobs' ? (
        <section>
          <Heading as="h3" size="2" weight="medium">
            Container
          </Heading>
          <Facts
            rows={[
              ['Image', <code>{summary.image}</code>],
              [
                'Command',
                summary.command.length ? (
                  <Entries entries={summary.command} label="Command entries" />
                ) : (
                  'Image default'
                ),
              ],
              [
                'Arguments',
                summary.args.length ? (
                  <Entries entries={summary.args} label="Argument entries" />
                ) : (
                  'None configured'
                ),
              ],
            ]}
          />
          {summary.resourceFields.length > 0 && <Facts rows={summary.resourceFields} />}
        </section>
      ) : (
        <section>
          <Heading as="h3" size="2" weight="medium">
            Tasks
          </Heading>
          <ol className="workflow-review-tasks">
            {summary.tasks.map((task, index) => (
              <li key={index}>
                <strong>{task.name}</strong>
                <span>
                  Job <code>{task.job}</code>
                </span>
                <span>
                  {task.dependencies.length
                    ? `Starts after ${task.dependencies.join(', ')}`
                    : 'Starts independently'}
                  {task.conditional ? ', if its conditions are met' : ''}
                </span>
              </li>
            ))}
          </ol>
        </section>
      )}
      <details className="advanced-details review-policies">
        <summary>Run policies</summary>
        <Facts
          rows={[
            ['Timeout', summary.timeout === '0' ? 'No deadline' : `${summary.timeout} seconds`],
            ['Concurrent runs', policy(summary.concurrency)],
            ...(kind === 'jobs'
              ? ([
                  ['Missed starts', policy(summary.misfire)],
                  ['Maximum attempts', summary.attempts],
                  ['Kubernetes Job backoff limit', policy(summary.backoff)],
                  ['Safe to repeat', summary.retrySafe ? 'Yes' : 'No'],
                ] as [string, string][])
              : ([['On task failure', policy(summary.failure)]] as [string, string][])),
            ['Successful runs retained', retention(summary.successfulHistory)],
            ['Failed runs retained', retention(summary.failedHistory)],
          ]}
        />
      </details>
      {showSaveNotice && mode !== 'demo' && (
        <Text as="p" size="2" color="gray">
          {summary.starts.includes('paused') || summary.starts.includes('suspended')
            ? 'Saving keeps these start restrictions.'
            : summary.schedule === 'Manual only'
              ? 'Saving a manual workload does not run it.'
              : 'The schedule can start future runs when the saved version is ready.'}
        </Text>
      )}
    </section>
  );
}

function Entries({ entries, label }: { entries: string[]; label: string }) {
  return (
    <ol className="review-entries" aria-label={label}>
      {entries.map((entry, index) => (
        <li key={index}>
          <code>{entry || '""'}</code>
        </li>
      ))}
    </ol>
  );
}
