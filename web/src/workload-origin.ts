type Source = {
  id: string | number;
  source_uid: string;
  source_mode?: string;
  source_kind?: string;
};

export function workloadOrigin(source: Source) {
  const kind = source.source_mode || source.source_kind || '';
  if (kind === 'kubernetes' || kind === 'ScheduledJob')
    return { kind: 'job', label: 'Scheduled job', to: `/jobs/${source.id}`, actionable: true };
  if (kind === 'workflow' || kind === 'WorkflowJob')
    return {
      kind: 'workflow',
      label: 'Workflow',
      to: `/workflows/${source.id}`,
      actionable: false,
    };
  const match = /^(function|check)-([1-9]\d*)$/.exec(source.source_uid);
  if ((kind === 'function' || kind === 'Function') && match?.[1] === 'function')
    return { kind: 'function', label: 'Function', to: `/functions/${match[2]}`, actionable: false };
  if ((kind === 'check' || kind === 'Check') && match?.[1] === 'check')
    return { kind: 'check', label: 'Check', to: `/checks/${match[2]}`, actionable: false };
  return { kind: 'unknown', label: kind || 'Unknown source', to: '', actionable: false };
}
