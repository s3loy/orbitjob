import { useState } from 'react';
import { Button, Flex, Heading, Table, Text } from '@radix-ui/themes';
import type { WorkflowRunDetail } from './api/m4';
import { Empty, PhaseBadge } from './components';
import { Facts, Raw } from './monitoring-ui';
import { M4Notice } from './m4-ui';

import {
  hasPinnedGraph,
  pinnedTaskLayers,
  stepsForTask,
  workflowDecisionReason,
  workflowOutcomeConflict,
} from './workflow-history-state';
export function PinnedWorkflowGraph({ run }: { run: WorkflowRunDetail }) {
  const [selected, setSelected] = useState('');
  const tasks = hasPinnedGraph(run) ? run.tasks! : [];
  const task = tasks.find((candidate) => candidate.name === selected) ?? tasks[0];
  const steps = task ? stepsForTask(run, task.name) : [];
  const decision = task ? run.task_decisions?.[task.name] : undefined;
  const decisions = Object.entries(run.task_decisions ?? {});
  return (
    <section aria-label="Pinned workflow history">
      <Flex gap="3" wrap="wrap" align="center">
        <Heading as="h2" size="3" weight="medium">
          Tasks in this run
        </Heading>
        <Text size="1" color="gray">
          Revision #{run.revision_id}
        </Text>
      </Flex>
      {workflowOutcomeConflict(run) && (
        <M4Notice title="Recorded outcome needs review">
          Task decisions conflict with the recorded success. Check the failed tasks to verify the
          outcome.
        </M4Notice>
      )}
      {decisions.some(([, decision]) => decision.reason === 'missing_job_reference') && (
        <M4Notice title="Job reference missing">
          A task’s ScheduledJob was unavailable. Check that the Job exists in the same namespace as
          this Workflow before starting a new run. This run did not complete successfully.
        </M4Notice>
      )}
      {!hasPinnedGraph(run) ? (
        <M4Notice title="Pinned task graph unavailable">
          {run.pinned_revision_available === false
            ? 'The pinned revision could not be loaded.'
            : 'A saved task graph is unavailable for this run.'}{' '}
          Recorded steps and decisions are available below.
        </M4Notice>
      ) : !task ? (
        <Empty title="No tasks in the pinned revision">
          The recorded revision returned an empty task list.
        </Empty>
      ) : (
        <div className="m4-task-workspace">
          <div className="m4-dag" role="group" aria-label="Pinned revision tasks">
            {pinnedTaskLayers(tasks).map((layer, index) => (
              <div className="m4-dag-stage" key={index}>
                {layer.map((node) => {
                  const recorded = stepsForTask(run, node.name);
                  const skipped = run.task_decisions?.[node.name];
                  return (
                    <Button
                      key={node.name}
                      color="gray"
                      variant="outline"
                      size="2"
                      className="m4-task"
                      aria-pressed={task.name === node.name}
                      onClick={() => setSelected(node.name)}
                    >
                      <span className="m4-task-name">{node.name}</span>
                      <span>{node.job_ref}</span>
                      <span>
                        {node.depends_on.length
                          ? `After ${node.depends_on.join(', ')}`
                          : 'No dependencies'}
                      </span>
                      <span>
                        {skipped?.skipped
                          ? `Skipped (${workflowDecisionReason(skipped.reason)})`
                          : recorded.length
                            ? recorded.map((step) => step.phase).join(', ')
                            : 'No recorded step'}
                      </span>
                    </Button>
                  );
                })}
              </div>
            ))}
          </div>
          <section className="m4-task-detail" aria-label="Selected pinned task">
            <Heading as="h3" size="3" weight="medium">
              {task.name}
            </Heading>
            <Facts
              rows={[
                ['Referenced Job', <code>{task.job_ref}</code>],
                ['Dependencies', task.depends_on.join(', ') || 'None'],
                [
                  'Recorded decision',
                  decision
                    ? decision.skipped
                      ? `Skipped (${workflowDecisionReason(decision.reason)})`
                      : 'Not skipped'
                    : 'No skip decision recorded',
                ],
                [
                  'Recorded step runs',
                  steps.length ? (
                    <ul className="m4-container-entries">
                      {steps.map((step) => (
                        <li key={String(step.id)}>
                          Step #{step.id} <PhaseBadge phase={step.phase} />
                        </li>
                      ))}
                    </ul>
                  ) : (
                    'No execution recorded for this task'
                  ),
                ],
                ['Rule combination', task.condition?.type ?? 'Unconditional'],
              ]}
            />
            {task.condition && <Raw value={task.condition} label="Pinned condition rules" />}
          </section>
        </div>
      )}
      <details className="m4-detail-disclosure" open={decisions.some(([, value]) => value.skipped)}>
        <summary>Task decisions</summary>
        {!decisions.length ? (
          <Text as="p" size="1" color="gray">
            {run.task_decisions === undefined
              ? 'Skip decisions are unavailable for this run.'
              : 'No skip decisions recorded.'}
          </Text>
        ) : (
          <Table.Root className="ledger-table m4-records" size="1">
            <Table.Header>
              <Table.Row>
                {['Task', 'Decision', 'Reason'].map((label) => (
                  <Table.ColumnHeaderCell key={label}>{label}</Table.ColumnHeaderCell>
                ))}
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {decisions.map(([name, value]) => (
                <Table.Row key={name}>
                  <Table.RowHeaderCell data-label="Task">{name}</Table.RowHeaderCell>
                  <Table.Cell data-label="Decision">
                    {value.skipped ? 'Skipped' : 'Not skipped'}
                  </Table.Cell>
                  <Table.Cell data-label="Reason">
                    {workflowDecisionReason(value.reason)}
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table.Root>
        )}
      </details>
    </section>
  );
}
