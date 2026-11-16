import { demoClient } from '../api/demo';
import type { Mode } from '../session';

export const allowDemo = true;
export function createClient(mode: Mode) {
  if (mode === 'live') throw new Error('Live connections are unavailable on the public demo.');
  return mode === 'demo' ? demoClient() : null;
}
