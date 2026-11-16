import { expect, test, vi } from 'vitest';
import { APIError } from './api/transport';
import { administrationAuthenticationError } from './administration-state';
import { withCredential } from './credential-requests';

test('a known 401 prevents later reads and writes while another credential remains independent', async () => {
  const client = {};
  const rejected = new APIError('Key revoked', 401);
  await expect(
    withCredential(client, async () => {
      throw rejected;
    }),
  ).rejects.toBe(rejected);
  const request = vi.fn(async () => 'should not be sent');
  await expect(withCredential(client, request)).rejects.toBe(rejected);
  expect(request).not.toHaveBeenCalled();
  await expect(withCredential({}, request)).resolves.toBe('should not be sent');
});

test('a denied read does not reject the credential or imply that a separate write is denied', async () => {
  const client = {};
  await expect(
    withCredential(client, async () => {
      throw new APIError('Read denied', 403);
    }),
  ).rejects.toMatchObject({ status: 403 });
  expect(administrationAuthenticationError(client)).toBeUndefined();
  await expect(withCredential(client, async () => ({ saved: true }))).resolves.toEqual({
    saved: true,
  });
});
