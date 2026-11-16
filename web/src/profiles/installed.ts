import { cookieClient } from '../api/transport';
import type { Mode } from '../session';

export const allowDemo = false;
export function createClient(mode: Mode, csrf = '', tenant = '') {
  if (mode === 'demo') throw new Error('This installation requires a live connection.');
  return mode === 'live' ? cookieClient(csrf, tenant) : null;
}
