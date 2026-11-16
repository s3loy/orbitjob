import { expect, test } from 'vitest';
import type { Identity } from './api/identity';
import { namespaceCatalogKnown, workspaceID, workspaceName } from './workspace';
const identity = {
  subject_type: 'api_key',
  installation_admin: false,
  key_id: 'key',
  kind: 'tenant',
  tenant_id: 'tenant-id-000001',
  target_tenant_id: 'tenant-id-000001',
  group_id: '',
  boundary_policy_id: '',
  capabilities: [],
  workload_namespaces: [],
} satisfies Identity;
test('workspace names use readable metadata and never manufacture a numbered tenant name', () => {
  expect(workspaceName({ ...identity, tenant_name: 'Operations', tenant_slug: 'ops' })).toBe(
    'Operations',
  );
  expect(workspaceName({ ...identity, tenant_name: '  ', tenant_slug: 'ops' })).toBe('ops');
  expect(workspaceName(identity)).toBe('Current workspace');
  expect(workspaceName({ ...identity, kind: 'platform' })).toBe('Selected workspace');
  expect(workspaceName()).toBe('Workspace');
  expect(workspaceID(identity)).toBe(identity.tenant_id);
});

test('platform namespace metadata without tenant read access remains unknown', () => {
  const reported = { ...identity, namespace_metadata_available: true };
  expect(namespaceCatalogKnown(reported)).toBe(true);
  expect(namespaceCatalogKnown({ ...reported, kind: 'platform' })).toBe(false);
  expect(namespaceCatalogKnown({ ...reported, kind: 'platform', tenant_name: 'Operations' })).toBe(
    true,
  );
  expect(
    namespaceCatalogKnown({ ...reported, kind: 'platform', workload_namespaces: ['runtime'] }),
  ).toBe(true);
  expect(namespaceCatalogKnown({ ...reported, namespace_metadata_available: false })).toBe(false);
});
