import { describe, expect, it, vi } from 'vitest';
import { demoClient } from './api/demo';
import { APIError } from './api/transport';
import {
  administrationAuthenticationError,
  rejectAdministrationCredential,
} from './administration-state';
import { definitionOperation, getDefinitionDraft, setDefinitionDraft } from './definition-state';

async function fixture() {
  const client = demoClient();
  const live = await client.definition('jobs', 'finance-prod', 'billing-reconciliation');
  setDefinitionDraft(client, 'jobs', {
    namespace: live.namespace,
    name: live.name,
    loaded: live,
    spec: live.spec_json,
  });
  return { client, live };
}

describe('definition credential recovery', () => {
  for (const action of ['load', 'refresh', 'check', 'apply', 'reconcile'] as const)
    it(`broadcasts a ${action} 401 and retains definition and request identity`, async () => {
      const { client, live } = await fixture();
      await definitionOperation(client, 'jobs', 'check');
      if (action === 'reconcile')
        setDefinitionDraft(client, 'jobs', {
          ...getDefinitionDraft(client, 'jobs'),
          unknown: true,
        });
      const before = getDefinitionDraft(client, 'jobs');
      const failure = new APIError('Credential rejected', 401, 'UNAUTHORIZED');
      client.definition = vi.fn(async () => {
        throw failure;
      });
      client.applyDefinition = vi.fn(async () => {
        throw failure;
      });
      await definitionOperation(client, 'jobs', action);
      const after = getDefinitionDraft(client, 'jobs');
      expect(administrationAuthenticationError(client)).toBe(failure);
      expect(after.loaded).toBe(live);
      expect(after.spec).toBe(before.spec);
      expect(after.requestID).toBe(before.requestID);
      expect(after.checked).toBe(before.checked);
      expect(Boolean(after.unknown)).toBe(action === 'reconcile');
      expect(after.error).toBe(failure);
      expect(after.busy).toBeUndefined();
      const readCount = vi.mocked(client.definition).mock.calls.length;
      const writeCount = vi.mocked(client.applyDefinition).mock.calls.length;
      await definitionOperation(client, 'jobs', 'reconcile');
      await definitionOperation(client, 'jobs', 'check');
      expect(client.definition).toHaveBeenCalledTimes(readCount);
      expect(client.applyDefinition).toHaveBeenCalledTimes(writeCount);
      expect(getDefinitionDraft(client, 'jobs')).toBe(after);
      client.dispose();
    });

  for (const retained of ['receipt', 'unknown', 'pending'] as const)
    it(`does not alter retained ${retained} state when the connection was already rejected`, async () => {
      const { client } = await fixture();
      await definitionOperation(client, 'jobs', 'check');
      if (retained === 'receipt') await definitionOperation(client, 'jobs', 'apply');
      else
        setDefinitionDraft(client, 'jobs', {
          ...getDefinitionDraft(client, 'jobs'),
          ...(retained === 'unknown' ? { unknown: true } : { busy: 'apply' }),
        });
      const before = getDefinitionDraft(client, 'jobs');
      client.definition = vi.fn();
      client.applyDefinition = vi.fn();
      rejectAdministrationCredential(client, new APIError('Credential rejected', 401));
      for (const action of ['load', 'refresh', 'check', 'apply', 'reconcile'] as const)
        await definitionOperation(client, 'jobs', action);
      expect(getDefinitionDraft(client, 'jobs')).toBe(before);
      expect(client.definition).not.toHaveBeenCalled();
      expect(client.applyDefinition).not.toHaveBeenCalled();
      client.dispose();
    });

  it('keeps a denied definition read separate from an independently authorized check', async () => {
    const { client } = await fixture();
    client.definition = vi.fn(async () => {
      throw new APIError('Read forbidden', 403);
    });
    await definitionOperation(client, 'jobs', 'load');
    expect(administrationAuthenticationError(client)).toBeUndefined();
    await definitionOperation(client, 'jobs', 'check');
    expect(getDefinitionDraft(client, 'jobs').checked?.state).toBe('validated');
    client.dispose();
  });
});
