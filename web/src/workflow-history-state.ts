import type { WorkflowDetail, WorkflowRunDetail } from './api/m4';
type Task = WorkflowDetail['tasks'][number];
export function workflowDecisionReason(reason?: string) {
  if (reason === 'missing_job_reference') return 'Job reference missing';
  if (reason === 'deadline') return 'Workflow deadline exceeded';
  return reason || 'Reason not recorded';
}
export function workflowOutcomeConflict(run: WorkflowRunDetail) {
  return (
    run.phase === 'Succeeded' &&
    Object.values(run.task_decisions ?? {}).some(
      (decision) =>
        decision.skipped && ['deadline', 'missing_job_reference'].includes(decision.reason ?? ''),
    )
  );
}
export function pinnedTaskLayers(tasks: Task[]): Task[][] {
  const remaining = new Set(tasks.map((task) => task.name));
  const done = new Set<string>();
  const layers: Task[][] = [];
  while (remaining.size) {
    const layer = tasks.filter(
      (task) => remaining.has(task.name) && task.depends_on.every((name) => done.has(name)),
    );
    if (!layer.length) {
      layers.push(tasks.filter((task) => remaining.has(task.name)));
      break;
    }
    layers.push(layer);
    for (const task of layer) {
      remaining.delete(task.name);
      done.add(task.name);
    }
  }
  return layers;
}
export function stepsForTask(run: WorkflowRunDetail, taskName: string) {
  return run.steps.filter((step) => step.task_name === taskName);
}
export function hasPinnedGraph(run: WorkflowRunDetail) {
  return run.pinned_revision_available === true && Array.isArray(run.tasks);
}
