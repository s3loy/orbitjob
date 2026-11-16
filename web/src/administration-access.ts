import type { CapabilityProbe, Identity } from './api/identity';

export const administrationCreateActions = {
  tenants: 'tenant:Create',
  keys: 'apikey:Create',
  policies: 'policy:Create',
  groups: 'group:Create',
} as const;
export function administrationCreateProbes(tenant: string): CapabilityProbe[] {
  const workspace = { action: 'tenant:Create', resource: 'orbitjob:*:*:tenant/*' };
  if (!tenant) return [workspace];
  return [
    workspace,
    { action: 'apikey:Create', resource: `orbitjob:${tenant}:*:apikey/*` },
    { action: 'policy:Create', resource: `orbitjob:${tenant}:*:policy/*` },
    { action: 'group:Create', resource: `orbitjob:${tenant}:*:group/*` },
  ];
}
export function administrationCreateAllowed(
  identity: Identity | undefined,
  panel: keyof typeof administrationCreateActions,
  tenant: string,
): boolean | undefined {
  const probe = administrationCreateProbes(tenant).find(
    (value) => value.action === administrationCreateActions[panel],
  );
  if (!probe) return false;
  return identity?.capabilities.find(
    (value) => value.action === probe.action && value.resource === probe.resource,
  )?.allowed;
}
