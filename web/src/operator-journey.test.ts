import { describe, expect, it, vi } from 'vitest';
import type { Client } from './api/transport';
import { workloadOrigin } from './workload-origin';
import { collectionPath, consumedCreateSearch, forwardState } from './monitoring-state';
import {
  monitoringSourceKind,
  resolveMonitoringSource,
  validateMonitoringSource,
} from './monitoring-source';
import {
  dismissMonitoringCreation,
  clearMonitoringSession,
  monitoringCreations,
  recordMonitoringCreation,
} from './monitoring-creations';

describe('source-specific execution boundaries', () => {
  const source = { id: '9007199254740993', source_uid: 'function-9007199254740995' };
  it('routes Function and Workflow projections to their own operations without changing large IDs', () => {
    expect(workloadOrigin({ ...source, source_kind: 'function' })).toMatchObject({
      to: '/functions/9007199254740995',
      actionable: false,
    });
    expect(workloadOrigin({ ...source, source_kind: 'workflow' })).toMatchObject({
      to: '/workflows/9007199254740993',
      actionable: false,
    });
    expect(
      workloadOrigin({ ...source, source_kind: 'check', source_uid: 'check-12' }),
    ).toMatchObject({ to: '/checks/12', actionable: false });
  });
  it('fails closed for missing, unknown or contradictory execution sources', () => {
    expect(workloadOrigin(source).actionable).toBe(false);
    expect(workloadOrigin({ ...source, source_kind: 'future-source' }).actionable).toBe(false);
    expect(
      workloadOrigin({ ...source, source_kind: 'kubernetes', source_mode: 'function' }).actionable,
    ).toBe(false);
    expect(workloadOrigin({ ...source, source_kind: 'kubernetes' }).actionable).toBe(true);
  });
});
describe('monitoring source validation', () => {
  it('rejects a retained Job identity after changing to Check and malformed synthetic identities', () => {
    expect(() => validateMonitoringSource('job-uid', 'checks')).toThrow('selected source type');
    expect(() => validateMonitoringSource('function-3', 'checks')).toThrow();
    expect(() => validateMonitoringSource('check-0', 'checks')).toThrow();
    expect(monitoringSourceKind('check-12')).toBe('checks');
  });
  it('resolves exact sources without accepting an unrelated or Function projection as a Job', async () => {
    const jobs = vi
      .fn()
      .mockResolvedValue([{ id: 2, source_uid: 'different', source_kind: 'kubernetes' }]);
    const client = { jobs } as unknown as Client;
    await expect(resolveMonitoringSource(client, 'wanted-uid', 'jobs')).rejects.toThrow(
      'available scheduled job',
    );
    expect(jobs).toHaveBeenCalledWith(0, '', { source_uid: 'wanted-uid' });
    jobs.mockResolvedValue([{ id: 3, source_uid: 'wanted-uid', source_kind: 'function' }]);
    await expect(resolveMonitoringSource(client, 'wanted-uid', 'jobs')).rejects.toThrow();
    jobs.mockResolvedValue([
      {
        id: '9007199254740993',
        source_uid: 'wanted-uid',
        source_kind: 'kubernetes',
        name: 'daily',
        namespace: 'ops',
      },
    ]);
    await expect(resolveMonitoringSource(client, 'wanted-uid', 'jobs')).resolves.toMatchObject({
      label: 'daily (ops)',
      to: '/jobs/9007199254740993',
    });
  });
});
describe('completed creation and return context', () => {
  it('consumes the create command while retaining source and list filters in return URLs', () => {
    expect(consumedCreateSearch('?create=1&search=daily&offset=20&source_uid=check-1')).toBe(
      '?search=daily&offset=20&source_uid=check-1',
    );
    const initial = {
      pathname: '/slis',
      search: '?create=1&search=availability&offset=20',
      state: null,
    };
    const detail = { pathname: '/slis/5', search: '', state: forwardState(initial) };
    expect(collectionPath(detail, 'slis')).toBe('/slis?search=availability&offset=20');
    const job = { pathname: '/jobs', search: '?search=daily&namespace=ops&offset=20', state: null };
    expect(
      collectionPath({ pathname: '/jobs/2', search: '', state: forwardState(job) }, 'jobs'),
    ).toBe('/jobs?search=daily&namespace=ops&offset=20');
  });
  it('retains confirmed IDs independently of any mounted view and isolates connection lifetimes', () => {
    const first = {} as Client,
      second = {} as Client;
    recordMonitoringCreation(first, {
      resource: 'slis',
      id: '9007199254740993',
      name: 'daily availability',
    });
    expect(monitoringCreations(first)[0]).toMatchObject({
      id: '9007199254740993',
      key: 'slis/9007199254740993',
    });
    expect(monitoringCreations(second)).toEqual([]);
    recordMonitoringCreation(first, {
      resource: 'slis',
      id: '9007199254740993',
      name: 'same confirmed resource',
    });
    expect(monitoringCreations(first)).toHaveLength(1);
    dismissMonitoringCreation(first, 'slis/9007199254740993');
    expect(monitoringCreations(first)).toEqual([]);
    clearMonitoringSession(first);
    recordMonitoringCreation(first, {
      resource: 'slis',
      id: 'late',
      name: 'off-page response after disconnect',
    });
    expect(monitoringCreations(first)).toEqual([]);
  });
});
