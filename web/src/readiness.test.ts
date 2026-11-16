import { afterEach, expect, test, vi } from 'vitest';
import { renderToString } from 'react-dom/server';
import { createElement } from 'react';
import { MemoryRouter } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { DefinitionReadiness } from './DefinitionReadiness';
import { SessionContext } from './session';
import { APIError, type Client } from './api/transport';
import type { DefinitionResult } from './api/definitions';

afterEach(() => vi.useRealTimers());

test('a denied readiness refresh retains the confirmed save without claiming cached readiness', async () => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date('2026-10-25T12:00:00Z'));
  const client = {} as Client;
  const cache = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const receipt: DefinitionResult = {
    kind: 'WorkflowJob',
    namespace: 'runtime',
    name: 'reviewed-workflow',
    uid: 'workflow-uid',
    resource_version: '8',
    generation: 2,
    spec_json: '{}',
    manifest_digest: 'digest',
    last_request_id: 'request',
    last_manifest_digest: 'digest',
    request_id: 'request',
    state: 'accepted',
  };
  const key = [
    'definition-readiness',
    11,
    'workflows',
    receipt.uid,
    receipt.generation,
    Date.now(),
  ];
  cache.setQueryData(key, {
    item: { id: 42, source_uid: receipt.uid, generation: 2 },
    complete: true,
  });
  await expect(
    cache.fetchQuery({
      queryKey: key,
      queryFn: async () => {
        throw new APIError('Read denied', 403);
      },
    }),
  ).rejects.toMatchObject({ status: 403 });
  const html = renderToString(
    createElement(
      QueryClientProvider,
      { client: cache },
      createElement(
        SessionContext,
        { value: { client, mode: 'live', generation: 11, connect() {}, disconnect() {} } },
        createElement(
          MemoryRouter,
          null,
          createElement(DefinitionReadiness, { kind: 'workflows', receipt }),
        ),
      ),
    ),
  );
  expect(html).toContain('Saved. Readiness unavailable');
  expect(html).toContain('reviewed-workflow');
  expect(html).toContain('Read denied');
  expect(html).not.toContain('Saved version is ready');
  expect(html).not.toContain('Open workload and run');
  cache.clear();
});
