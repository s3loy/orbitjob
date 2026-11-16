import { describe, expect, it } from 'vitest';
import { ulid } from 'ulid';
import {
  administrationRecordDraft,
  updateAdministrationRecordDraft,
  resetAdministrationRecordDraft,
  clearAdministrationSession,
} from './administration-state';
import { getFunctionDraft, setFunctionDraft, saveFunctionDraft } from './function-management-state';
import type { Client } from './api/transport';

describe('connection-local nonsecret drafts', () => {
  it('keeps raw policy text separate by workspace, panel and connection', () => {
    const client = {},
      other = {},
      first = ulid(),
      second = ulid();
    const patch = { name: 'Operator access', document: '{"statement":', advanced: true };
    updateAdministrationRecordDraft(client, 'policies', first, patch);
    expect(administrationRecordDraft(client, 'policies', first)).toMatchObject(patch);
    expect(administrationRecordDraft(client, 'policies', second).name).toBe('');
    expect(administrationRecordDraft(client, 'groups', first).name).toBe('');
    expect(administrationRecordDraft(other, 'policies', first).name).toBe('');
  });

  it('resets only the completed creation scope and refuses late updates after disconnect', () => {
    const client = {},
      first = ulid(),
      second = ulid();
    updateAdministrationRecordDraft(client, 'policies', first, { name: 'First' });
    updateAdministrationRecordDraft(client, 'policies', second, { name: 'Second' });
    resetAdministrationRecordDraft(client, 'policies', first);
    expect(administrationRecordDraft(client, 'policies', first).name).toBe('');
    expect(administrationRecordDraft(client, 'policies', second).name).toBe('Second');
    clearAdministrationSession(client);
    updateAdministrationRecordDraft(client, 'policies', second, { name: 'Late response' });
    resetAdministrationRecordDraft(client, 'policies', first);
    expect(administrationRecordDraft(client, 'policies', second).name).toBe('');
  });

  it('does not forward raw Function labels text to the API', async () => {
    let payload: unknown;
    const client = {
      createFunction: async (input: unknown) => {
        payload = input;
        return { id: 1 };
      },
    } as unknown as Client;
    const draft = getFunctionDraft(client, 'new');
    setFunctionDraft(client, 'new', {
      ...draft,
      input: {
        ...draft.input,
        name: 'report',
        image: 'busybox:1.37',
        labels: { team: 'operations' },
      },
      labelsText: '{ "team": "operations" }',
    });
    await saveFunctionDraft(client, 'new');
    expect(payload).toMatchObject({ labels: { team: 'operations' } });
    expect(payload).not.toHaveProperty('labelsText');
  });
});
