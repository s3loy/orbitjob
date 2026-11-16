import { expect, test } from 'vitest';
import { demoClient } from './demo';
import { importDefinitionSpec } from './manifest-import';

test('seeded Workflow editing uses the same identity, schedule and tasks as the displayed workload', async () => {
  const client = demoClient();
  const workflow = await client.workflow('412');
  const definition = await client.definition('workflows', workflow.namespace, workflow.name);
  expect(definition.uid).toBe(workflow.source_uid);
  expect(definition.generation).toBe(workflow.generation);
  const spec = JSON.parse(definition.spec_json);
  expect(spec.schedule).toBe(workflow.schedule);
  expect(spec.timeZone).toBe(workflow.time_zone);
  expect(spec.tasks.map((task: { name: string }) => task.name)).toEqual(
    workflow.tasks.map((task) => task.name),
  );
  const form = importDefinitionSpec(
    definition.spec_json,
    'WorkflowJob',
    workflow.namespace,
    workflow.name,
  );
  expect(form.tasks.map((task) => task.job)).toEqual(workflow.tasks.map((task) => task.job_ref));
  await expect(
    client.definition('workflows', 'another-namespace', workflow.name),
  ).rejects.toMatchObject({ status: 404 });
  client.dispose();
});
