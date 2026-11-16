import type { Client } from './api/transport';
import { workloadOrigin } from './workload-origin';
import { withCredential } from './credential-requests';

export type MonitoringSourceKind = 'jobs' | 'checks' | 'functions';
export type MonitoringSource = {
  value: string;
  label: string;
  kind: MonitoringSourceKind;
  to: string;
};
export function monitoringSourceKind(value: string): MonitoringSourceKind {
  return value.startsWith('check-')
    ? 'checks'
    : value.startsWith('function-')
      ? 'functions'
      : 'jobs';
}
export function validateMonitoringSource(value: string, kind: MonitoringSourceKind) {
  if (
    !value ||
    monitoringSourceKind(value) !== kind ||
    (kind !== 'jobs' && !/^(check|function)-[1-9]\d*$/.test(value))
  )
    throw new Error('Choose a resource matching the selected source type.');
}
export async function resolveMonitoringSource(
  client: Client,
  value: string,
  kind = monitoringSourceKind(value),
): Promise<MonitoringSource> {
  validateMonitoringSource(value, kind);
  if (kind === 'checks') {
    const item = await withCredential(client, () => client.check(value.slice(6)));
    return { value, kind, label: item.name, to: `/checks/${item.id}` };
  }
  if (kind === 'functions') {
    const item = await withCredential(client, () => client.functionDefinition(value.slice(9)));
    return { value, kind, label: item.name, to: `/functions/${item.id}` };
  }
  const rows = await withCredential(client, () => client.jobs(0, '', { source_uid: value }));
  const item = rows.find((row) => row.source_uid === value && workloadOrigin(row).actionable);
  if (!item)
    throw new Error(
      'This source is not an available scheduled job. Choose an authorized resource.',
    );
  return { value, kind, label: `${item.name} (${item.namespace})`, to: `/jobs/${item.id}` };
}
