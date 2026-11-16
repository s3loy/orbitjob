import { demoClient } from '../api/demo';
import { cookieClient } from '../api/transport';
import type { Mode } from '../session';

// Explicit test-server composition; excluded from both release profiles.
export const allowDemo = true;
export function createClient(mode: Mode, csrf = '', tenant = '') {
  return mode === 'demo' ? demoClient() : mode === 'live' ? cookieClient(csrf, tenant) : null;
}
