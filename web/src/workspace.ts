import type { Identity } from './api/identity';

export function workspaceID(identity?: Identity | null, fallback = '') {
  return identity?.target_tenant_id || identity?.tenant_id || fallback;
}

export function workspaceName(identity?: Identity | null, fallback = '') {
  const id = workspaceID(identity, fallback);
  return (
    identity?.tenant_name?.trim() ||
    identity?.tenant_slug?.trim() ||
    (id
      ? identity?.kind === 'platform'
        ? 'Selected workspace'
        : 'Current workspace'
      : 'Workspace')
  );
}

export function shortID(value: string) {
  return value.length > 12 ? `${value.slice(0, 4)}…${value.slice(-6)}` : value;
}

export function namespaceCatalogKnown(identity?: Identity | null) {
  if (!identity?.namespace_metadata_available) return false;
  // Platform metadata uses tenant:Get. Without disclosed target metadata an
  // empty array also means "not authorized to inspect", not "no mapping".
  return (
    identity.kind !== 'platform' ||
    Boolean(identity.tenant_name || identity.tenant_slug || identity.workload_namespaces.length)
  );
}
