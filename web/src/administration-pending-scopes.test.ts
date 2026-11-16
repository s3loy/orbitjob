import { expect, test } from 'vitest';
import { APIError } from './api/transport';
import {
  administrationPendingMutations,
  clearAdministrationSession,
  reconcileAdministrationMutation,
  runAdministrationMutation,
} from './administration-state';

test('all unresolved mutation scopes retain their identity until individually reconciled or disconnected', async () => {
  const client = {};
  const operation = async () => {
    throw new APIError('Response unavailable', 503);
  };
  await runAdministrationMutation(client, 'policies:workspace-b', operation);
  await runAdministrationMutation(client, 'groups:workspace-b', operation);
  const snapshot = administrationPendingMutations(client);
  expect(snapshot).toEqual([
    { scope: 'policies:workspace-b', stage: 'unknown' },
    { scope: 'groups:workspace-b', stage: 'unknown' },
  ]);
  expect(administrationPendingMutations(client)).toBe(snapshot);
  reconcileAdministrationMutation(client, 'groups:workspace-b');
  expect(administrationPendingMutations(client)).toEqual([
    { scope: 'policies:workspace-b', stage: 'unknown' },
  ]);
  clearAdministrationSession(client);
  expect(administrationPendingMutations(client)).toEqual([]);
});
