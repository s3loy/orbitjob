import { expect, test } from 'vitest';
import type { Identity } from './api/identity';
import { administrationCreateAllowed, administrationCreateProbes } from './administration-access';

test('management probes match the actual collection authorization targets', () => {
  expect(administrationCreateProbes('tenant-b')).toEqual([
    { action: 'tenant:Create', resource: 'orbitjob:*:*:tenant/*' },
    { action: 'apikey:Create', resource: 'orbitjob:tenant-b:*:apikey/*' },
    { action: 'policy:Create', resource: 'orbitjob:tenant-b:*:policy/*' },
    { action: 'group:Create', resource: 'orbitjob:tenant-b:*:group/*' },
  ]);
});
test('only an exact capability result decides whether a creation form is available', () => {
  const identity = {
    capabilities: [
      { action: 'policy:Create', resource: 'orbitjob:tenant-a:*:policy/*', allowed: true },
      { action: 'policy:Create', resource: 'orbitjob:tenant-b:*:policy/*', allowed: false },
      { action: 'policy:List', resource: 'orbitjob:tenant-b:*:policy/*', allowed: false },
    ],
  } as Identity;
  expect(administrationCreateAllowed(identity, 'policies', 'tenant-a')).toBe(true);
  expect(administrationCreateAllowed(identity, 'policies', 'tenant-b')).toBe(false);
  expect(administrationCreateAllowed(identity, 'keys', 'tenant-b')).toBeUndefined();
  expect(administrationCreateAllowed(undefined, 'policies', 'tenant-b')).toBeUndefined();
});
test('an unscoped account probes only workspace creation without implying scoped grants', () => {
  expect(administrationCreateProbes('')).toEqual([
    { action: 'tenant:Create', resource: 'orbitjob:*:*:tenant/*' },
  ]);
  expect(administrationCreateAllowed(undefined, 'keys', '')).toBe(false);
  expect(administrationCreateAllowed(undefined, 'policies', '')).toBe(false);
  expect(administrationCreateAllowed(undefined, 'groups', '')).toBe(false);
});
