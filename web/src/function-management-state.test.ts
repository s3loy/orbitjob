import { describe, expect, it } from 'vitest';
import { APIError, type Client } from './api/transport';
import { getFunctionDraft, saveFunctionDraft, setFunctionDraft } from './function-management-state';

describe('Function management recovery', () => {
  it('retains the create identity and exact input after a lost response', async () => {
    const requests: string[] = [];
    const client = {
      createFunction: async (_input: unknown, key: string) => {
        requests.push(key);
        throw new APIError('Response lost');
      },
    } as unknown as Client;
    const original = getFunctionDraft(client, 'new');
    setFunctionDraft(client, 'new', {
      ...original,
      input: { ...original.input, name: 'report', image: 'busybox:1.37' },
    });
    await saveFunctionDraft(client, 'new');
    await saveFunctionDraft(client, 'new');
    expect(requests).toEqual([original.requestID, original.requestID]);
    expect(getFunctionDraft(client, 'new').unknown).toBe(true);
    expect(getFunctionDraft(client, 'new').input.name).toBe('report');
  });
  it('rejects double submission while a save is pending', async () => {
    let count = 0;
    let resolve!: (value: never) => void;
    const client = {
      createFunction: () => {
        count++;
        return new Promise((done) => {
          resolve = done;
        });
      },
    } as unknown as Client;
    const pending = saveFunctionDraft(client, 'new');
    await saveFunctionDraft(client, 'new');
    expect(count).toBe(1);
    resolve({ id: 1 } as never);
    await pending;
    expect(getFunctionDraft(client, 'new').saved?.id).toBe(1);
  });
});
