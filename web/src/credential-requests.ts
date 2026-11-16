import { APIError } from './api/transport';
import {
  administrationAuthenticationError,
  rejectAdministrationCredential,
} from './administration-state';

export async function withCredential<T>(client: object, request: () => Promise<T>): Promise<T> {
  const rejected = administrationAuthenticationError(client);
  if (rejected) throw rejected;
  try {
    return await request();
  } catch (error) {
    if (error instanceof APIError && error.status === 401)
      rejectAdministrationCredential(client, error);
    throw error;
  }
}
