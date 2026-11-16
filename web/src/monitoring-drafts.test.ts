import { describe, expect, it } from 'vitest';
import type { Client } from './api/transport';
import {
  clearMonitoringSession,
  monitoringDraft,
  monitoringDraftScope,
  resetMonitoringDraft,
  updateMonitoringDraft,
} from './monitoring-creations';

describe('monitoring creation drafts', () => {
  it('isolates connections, resource kinds and linked source/indicator contexts', () => {
    const client = {} as Client;
    const other = {} as Client;
    const first = monitoringDraftScope('slis', '?create=1&source_uid=check-1');
    const second = monitoringDraftScope('slis', '?source_uid=check-2');
    const generic = monitoringDraftScope('slis', '');
    const objective = monitoringDraftScope('slos', '?sli_id=11');
    const draft = monitoringDraft(client, first);
    const selectedSource = { value: 'check-1', kind: 'checks' as const, label: 'Health', to: '' };
    updateMonitoringDraft(client, first, {
      values: { ...draft.values, name: 'Availability', criteria: '{"status":' },
      sourceKind: 'checks',
      selectedSource,
    });
    expect(monitoringDraftScope('slis', '?source_uid=check-1&page=2')).toBe(first);
    expect(monitoringDraft(client, first)).toMatchObject({
      values: { name: 'Availability', source: 'check-1', criteria: '{"status":' },
      sourceKind: 'checks',
      selectedSource,
    });
    expect(monitoringDraft(client, second).values).toMatchObject({ name: '', source: 'check-2' });
    expect(monitoringDraft(client, generic).values.source).toBe('');
    expect(monitoringDraft(client, objective).values.sliId).toBe('11');
    expect(monitoringDraft(other, first).values.name).toBe('');
  });

  it('retains uncertain writes and resets only the explicitly completed or discarded scope', () => {
    const client = {} as Client;
    const check = monitoringDraftScope('checks', '');
    const sli = monitoringDraftScope('slis', '');
    const failure = new Error('The request outcome is unknown');
    updateMonitoringDraft(client, check, { uncertain: true, error: failure });
    updateMonitoringDraft(client, sli, {
      values: { ...monitoringDraft(client, sli).values, name: 'Keep this draft' },
    });
    expect(monitoringDraft(client, check)).toMatchObject({ uncertain: true, error: failure });
    resetMonitoringDraft(client, check);
    expect(monitoringDraft(client, check)).toMatchObject({ uncertain: false, error: null });
    expect(monitoringDraft(client, sli).values.name).toBe('Keep this draft');
  });

  it('clears on disconnect and ignores delayed completions from that connection', () => {
    const client = {} as Client;
    const scope = monitoringDraftScope('checks', '');
    updateMonitoringDraft(client, scope, {
      values: { ...monitoringDraft(client, scope).values, name: 'Old connection' },
      pending: true,
    });
    clearMonitoringSession(client);
    updateMonitoringDraft(client, scope, {
      pending: false,
      uncertain: true,
      error: new Error('Late'),
    });
    resetMonitoringDraft(client, scope);
    expect(monitoringDraft(client, scope)).toMatchObject({
      values: { name: '' },
      pending: false,
      uncertain: false,
      error: null,
    });
  });
});
