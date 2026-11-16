import { expect, test } from 'vitest';
import { demoClient } from './api/demo';
import type { Client } from './api/transport';
import { definitionOperation, getDefinitionDraft, setDefinitionDraft } from './definition-state';

test('a successful definition load clears earlier read errors and obsolete load instructions', async () => {
  const demo = demoClient();
  const resource = await demo.definition('jobs', 'finance-prod', 'billing-reconciliation');
  demo.dispose();
  const client = { definition: async () => resource } as unknown as Client;
  setDefinitionDraft(client, 'jobs', {
    namespace: resource.namespace,
    name: resource.name,
    spec: '{}',
    message: 'Load the existing definition before editing its spec.',
    error: new Error('An earlier read failed'),
  });
  await definitionOperation(client, 'jobs', 'load');
  const loaded = getDefinitionDraft(client, 'jobs');
  expect(loaded.loaded?.uid).toBe(resource.uid);
  expect(loaded.error).toBeUndefined();
  expect(loaded.message).toBeUndefined();
});

test('a successful live refresh clears a prior read error while retaining edits', async () => {
  const demo = demoClient();
  const resource = await demo.definition('jobs', 'finance-prod', 'billing-reconciliation');
  demo.dispose();
  const client = { definition: async () => resource } as unknown as Client;
  setDefinitionDraft(client, 'jobs', {
    namespace: resource.namespace,
    name: resource.name,
    spec: '{"reviewed":"edits"}',
    loaded: resource,
    error: new Error('Earlier read unavailable'),
  });
  await definitionOperation(client, 'jobs', 'refresh');
  expect(getDefinitionDraft(client, 'jobs').error).toBeUndefined();
  expect(getDefinitionDraft(client, 'jobs').spec).toBe('{"reviewed":"edits"}');
});

for (const matches of [true, false])
  test(`reconciliation clears old errors only when the original write is confirmed: ${matches}`, async () => {
    const demo = demoClient();
    const resource = await demo.definition('jobs', 'finance-prod', 'billing-reconciliation');
    demo.dispose();
    const requestID = crypto.randomUUID();
    const checked = {
      ...resource,
      manifest_digest: 'checked-digest',
      request_id: requestID,
      state: 'validated' as const,
    };
    const stored = {
      ...resource,
      manifest_digest: checked.manifest_digest,
      last_manifest_digest: checked.manifest_digest,
      last_request_id: matches ? requestID : crypto.randomUUID(),
    };
    const client = { definition: async () => stored } as unknown as Client;
    const error = new Error('Original response was lost');
    setDefinitionDraft(client, 'jobs', {
      namespace: resource.namespace,
      name: resource.name,
      spec: resource.spec_json,
      loaded: resource,
      checked,
      requestID,
      unknown: true,
      error,
    });
    await definitionOperation(client, 'jobs', 'reconcile');
    const result = getDefinitionDraft(client, 'jobs');
    expect(result.unknown).toBe(!matches);
    expect(result.error).toBe(matches ? undefined : error);
    expect(result.receipt?.state).toBe(matches ? 'reconciled' : undefined);
  });
