import { publicDemo } from './deployment';
export type AccountCapability = { kind: 'setup' | 'invite' | 'reset'; token: string };
let capability: AccountCapability | undefined;
export function takeAccountCapability() {
  if (publicDemo) return;
  const values = new URLSearchParams(location.hash.slice(1));
  for (const kind of ['setup', 'invite', 'reset'] as const) {
    const token = values.get(kind);
    if (token) {
      capability = { kind, token };
      history.replaceState(history.state, '', location.pathname + location.search);
      break;
    }
  }
  return capability;
}
export function clearAccountCapability() {
  capability = undefined;
}
