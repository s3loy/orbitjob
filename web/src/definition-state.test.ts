import { describe, expect, it, vi } from 'vitest';
import { demoClient } from './api/demo';
import { APIError } from './api/transport';
import { initialDraft, manifestSpecJSON } from './api/manifests';
import {
  clearDefinitionSession,
  acceptDefinitionMerge,
  chooseDefinitionMerge,
  definitionOperation,
  getDefinitionDraft,
  keepDraftAgainstLive,
  seedDefinitionDraft,
  setDefinitionDraft,
} from './definition-state';

describe('definition write lifecycle', () => {
  it.each(['unfinished input', 'empty program'])(
    'refuses direct check and apply with a previous valid check and %s',
    async (condition) => {
      const client = demoClient();
      try {
        const spec = manifestSpecJSON(initialDraft('ScheduledJob'));
        seedDefinitionDraft(client, 'jobs', {
          namespace: 'orbitjob',
          name: 'guarded-command',
          spec,
        });
        await definitionOperation(client, 'jobs', 'check');
        const checked = getDefinitionDraft(client, 'jobs');
        expect(checked.checked?.state).toBe('validated');
        const value = JSON.parse(spec);
        if (condition === 'empty program') value.jobTemplate.command = [''];
        setDefinitionDraft(client, 'jobs', {
          ...checked,
          spec: condition === 'empty program' ? JSON.stringify(value) : spec,
          commandInput:
            condition === 'unfinished input'
              ? {
                  text: "printf 'unfinished",
                  error: 'Close the single quote to finish this command.',
                  value: {
                    command: value.jobTemplate.command,
                    args: value.jobTemplate.args,
                  },
                }
              : undefined,
        });
        client.applyDefinition = vi.fn();
        await definitionOperation(client, 'jobs', 'check');
        await definitionOperation(client, 'jobs', 'apply');
        expect(client.applyDefinition).not.toHaveBeenCalled();
      } finally {
        client.dispose();
      }
    },
  );

  it('retains exact int64 spec and uses one checked request for apply', async () => {
    const client = demoClient();
    const draft = initialDraft('WorkflowJob');
    draft.tasks[1].rules[0] = {
      task: 'extract',
      field: 'duration_ms',
      operator: 'ge',
      values: [],
      number: '9223372036854775807',
    };
    const spec = manifestSpecJSON(draft);
    expect(spec).toContain('9223372036854775807');
    expect(spec).not.toContain('"9223372036854775807"');
    seedDefinitionDraft(client, 'workflows', { namespace: 'orbitjob', name: 'review-test', spec });
    await definitionOperation(client, 'workflows', 'check');
    const checked = getDefinitionDraft(client, 'workflows');
    expect(checked.checked?.state).toBe('validated');
    expect(checked.loaded).toBeUndefined();
    await definitionOperation(client, 'workflows', 'apply');
    expect(getDefinitionDraft(client, 'workflows').receipt?.request_id).toBe(checked.requestID);
    expect(getDefinitionDraft(client, 'workflows').receipt?.spec_json).toContain(
      '9223372036854775807',
    );
    client.dispose();
  });
  it('keeps uncertain writes, refuses replacement and reconciles without another write', async () => {
    const client = demoClient();
    const apply = client.applyDefinition.bind(client);
    const write = vi.fn(async (...args: Parameters<typeof apply>) => {
      const result = await apply(...args);
      if (!args[3].dry_run) throw new APIError('Connection lost');
      return result;
    });
    client.applyDefinition = write;
    seedDefinitionDraft(client, 'jobs', {
      namespace: 'finance-prod',
      name: 'billing-reconciliation',
      spec: manifestSpecJSON(initialDraft('ScheduledJob')),
    });
    await definitionOperation(client, 'jobs', 'load');
    await definitionOperation(client, 'jobs', 'check');
    await definitionOperation(client, 'jobs', 'apply');
    expect(getDefinitionDraft(client, 'jobs').unknown).toBe(true);
    seedDefinitionDraft(client, 'jobs', { namespace: 'foreign', name: 'replacement', spec: '{}' });
    expect(getDefinitionDraft(client, 'jobs').name).toBe('billing-reconciliation');
    await definitionOperation(client, 'jobs', 'apply');
    expect(write).toHaveBeenCalledTimes(2);
    const read = client.definition.bind(client);
    client.definition = async (...args) => ({ ...(await read(...args)), uid: 'replacement-uid' });
    await definitionOperation(client, 'jobs', 'reconcile');
    expect(getDefinitionDraft(client, 'jobs').unknown).toBe(true);
    expect(getDefinitionDraft(client, 'jobs').receipt).toBeUndefined();
    client.definition = read;
    await definitionOperation(client, 'jobs', 'reconcile');
    expect(getDefinitionDraft(client, 'jobs').receipt?.state).toBe('reconciled');
    expect(getDefinitionDraft(client, 'jobs').unknown).toBe(false);
    expect(write).toHaveBeenCalledTimes(2);
    client.dispose();
  });
  it('does not restore a closed connection from a late response', async () => {
    const client = demoClient();
    setDefinitionDraft(client, 'jobs', {
      namespace: 'orbitjob',
      name: 'late',
      spec: manifestSpecJSON(initialDraft('ScheduledJob')),
    });
    const pending = definitionOperation(client, 'jobs', 'check');
    clearDefinitionSession(client);
    await pending;
    expect(getDefinitionDraft(client, 'jobs').name).toBe('');
    expect(getDefinitionDraft(client, 'jobs').checked).toBeUndefined();
    client.dispose();
  });
  it('refreshes a status-only version conflict without discarding edited exact integers', async () => {
    const client = demoClient();
    const live = await client.definition('jobs', 'finance-prod', 'billing-reconciliation');
    const edited = live.spec_json.replace(/"timeoutSeconds"\s*:\s*\d+/, '"timeoutSeconds":120');
    setDefinitionDraft(client, 'jobs', {
      namespace: live.namespace,
      name: live.name,
      loaded: live,
      spec: edited,
    });
    client.definition = vi.fn(async () => ({ ...live, resource_version: 'new-status-version' }));
    client.applyDefinition = vi.fn();
    await definitionOperation(client, 'jobs', 'refresh');
    const refreshed = getDefinitionDraft(client, 'jobs');
    expect(refreshed.spec).toBe(edited);
    expect(refreshed.loaded?.resource_version).toBe('new-status-version');
    expect(refreshed.checked).toBeUndefined();
    expect(refreshed.conflict).toBeUndefined();
    expect(client.applyDefinition).not.toHaveBeenCalled();
    client.dispose();
  });
  it('retains both specs after a real conflict and requires explicit adoption and recheck', async () => {
    const client = demoClient();
    const live = await client.definition('jobs', 'finance-prod', 'billing-reconciliation');
    const edited = '{"timeoutSeconds":90,"customExactInteger":9223372036854775807}';
    const changed = {
      ...live,
      spec_json: '{"timeoutSeconds":30}',
      manifest_digest: 'changed',
      resource_version: 'changed-version',
    };
    setDefinitionDraft(client, 'jobs', {
      namespace: live.namespace,
      name: live.name,
      loaded: live,
      spec: edited,
    });
    client.definition = vi.fn(async () => changed);
    client.applyDefinition = vi.fn();
    await definitionOperation(client, 'jobs', 'refresh');
    expect(getDefinitionDraft(client, 'jobs').loaded).toBe(live);
    expect(getDefinitionDraft(client, 'jobs').spec).toBe(edited);
    expect(getDefinitionDraft(client, 'jobs').conflict).toEqual(changed);
    await definitionOperation(client, 'jobs', 'check');
    expect(client.applyDefinition).not.toHaveBeenCalled();
    keepDraftAgainstLive(client, 'jobs');
    const merge = getDefinitionDraft(client, 'jobs').merge!;
    expect(merge).toBeDefined();
    expect(getDefinitionDraft(client, 'jobs').loaded).toEqual(live);
    for (const field of merge.conflicts) chooseDefinitionMerge(client, 'jobs', field.path, 'local');
    acceptDefinitionMerge(client, 'jobs');
    expect(getDefinitionDraft(client, 'jobs').loaded).toEqual(changed);
    expect(getDefinitionDraft(client, 'jobs').spec).toContain('9223372036854775807');
    expect(getDefinitionDraft(client, 'jobs').checked).toBeUndefined();
    client.dispose();
  });
  it('refuses adopting a replaced UID and keeps unknown-write reconciliation separate', async () => {
    const client = demoClient();
    const live = await client.definition('jobs', 'finance-prod', 'billing-reconciliation');
    const replaced = { ...live, uid: 'replacement-uid' };
    setDefinitionDraft(client, 'jobs', {
      namespace: live.namespace,
      name: live.name,
      loaded: live,
      spec: live.spec_json,
    });
    client.definition = vi.fn(async () => replaced);
    await definitionOperation(client, 'jobs', 'refresh');
    keepDraftAgainstLive(client, 'jobs');
    expect(getDefinitionDraft(client, 'jobs').loaded?.uid).toBe(live.uid);
    expect(getDefinitionDraft(client, 'jobs').conflict?.uid).toBe('replacement-uid');
    setDefinitionDraft(client, 'jobs', { ...getDefinitionDraft(client, 'jobs'), unknown: true });
    vi.mocked(client.definition).mockClear();
    await definitionOperation(client, 'jobs', 'refresh');
    expect(client.definition).not.toHaveBeenCalled();
    expect(getDefinitionDraft(client, 'jobs').unknown).toBe(true);
    client.dispose();
  });
});
