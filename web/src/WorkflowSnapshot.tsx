import { Table, Text } from '@radix-ui/themes';
import type { WorkflowRunDetail } from './api/m4';

export function WorkflowSnapshot({ run }: { run: WorkflowRunDetail }) {
  return (
    <details
      className="m4-section m4-detail-disclosure"
      aria-label="Task versions"
      open={run.snapshot_version !== 1 || !Object.keys(run.task_snapshots ?? {}).length}
    >
      <summary>Task versions</summary>
      {run.snapshot_version !== 1 ? (
        <Text as="p" size="2" color="gray">
          {run.snapshot_version === 0
            ? 'Versions were selected as each step started.'
            : 'Task version snapshot unavailable.'}
        </Text>
      ) : !run.task_snapshots || !Object.keys(run.task_snapshots).length ? (
        <Text as="p" size="2" color="red">
          The accepted task snapshot is incomplete.
        </Text>
      ) : (
        <Table.Root className="ledger-table" size="1">
          <Table.Header>
            <Table.Row>
              {['Task', 'Namespace', 'Job', 'Revision', 'Image'].map((label) => (
                <Table.ColumnHeaderCell key={label}>{label}</Table.ColumnHeaderCell>
              ))}
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {Object.entries(run.task_snapshots).map(([task, snapshot]) => (
              <Table.Row key={task}>
                <Table.RowHeaderCell>{task}</Table.RowHeaderCell>
                <Table.Cell>{snapshot.namespace}</Table.Cell>
                <Table.Cell>{snapshot.name}</Table.Cell>
                <Table.Cell>#{snapshot.revision_id}</Table.Cell>
                <Table.Cell>
                  <code style={{ overflowWrap: 'anywhere', whiteSpace: 'normal' }}>
                    {snapshot.image}
                  </code>
                </Table.Cell>
              </Table.Row>
            ))}
          </Table.Body>
        </Table.Root>
      )}
    </details>
  );
}
