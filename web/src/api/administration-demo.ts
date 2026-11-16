import { randomUUID } from '../uuid';
import { ulid } from 'ulid';
import type { AdministrationClient, Tenant, Key, Policy, Group } from './administration';
import { APIError } from './transport';
import { policyDocument, policyTemplates } from '../administration-policy';
import { sampleWorkspace } from '../sample-workspace';

export const sampleSearchGroup = {
  id: '00000000000000000000000031',
  name: 'Search infrastructure',
  slug: 'search-infra',
} as const satisfies Group;

export function administrationDemo(answer: <T>(value: T) => Promise<T>): AdministrationClient {
  const tenants: Tenant[] = [{ ...sampleWorkspace, status: 'active' }];
  const keys = new Map<string, Key[]>();
  const policies: Policy[] = [
    {
      id: '00000000000000000000000021',
      name: 'Workload operator',
      description: 'Run commerce workloads and maintain the payments and checkout checks.',
      platform: false,
      document: JSON.parse(policyDocument()),
    },
    {
      id: '00000000000000000000000022',
      name: 'On-call diagnostics',
      description: 'Read execution logs, check results and error budgets during incidents.',
      platform: false,
      document: JSON.parse(policyDocument(policyTemplates.readonly.actions)),
    },
  ];
  const groups: Group[] = [{ ...sampleSearchGroup }];
  const id = ulid;
  const find = <T extends { id: string }>(rows: T[], value: string): T => {
    const row = rows.find((r) => r.id === value);
    if (!row) throw new APIError('Record not found.', 404, 'NOT_FOUND');
    return row;
  };
  return {
    workspaceDirectory: (offset = 0, search = '', limit = 20) => {
      const needle = search.trim().toLowerCase();
      const visible = tenants
        .filter(
          (row) =>
            row.name.toLowerCase().includes(needle) || row.slug.toLowerCase().includes(needle),
        )
        .sort(
          (a, b) =>
            a.name.toLowerCase().localeCompare(b.name.toLowerCase()) || a.id.localeCompare(b.id),
        );
      return answer({
        items: structuredClone(visible.slice(offset, offset + limit)),
        has_more: visible.length > offset + limit,
      });
    },
    adminTenants: (offset = 0) => answer(structuredClone(tenants.slice(offset, offset + 20))),
    adminTenant: (value) => answer(structuredClone(find(tenants, value))),
    adminCreateTenant: (input) => {
      const row = { ...input, id: id(), status: input.status ?? 'active' };
      tenants.push(row);
      return answer(row);
    },
    adminKeys: (tenant) => answer(structuredClone(keys.get(tenant) ?? [])),
    adminCreateKey: (tenant, input) => {
      find(tenants, tenant);
      for (const p of input.policies ?? []) find(policies, p);
      if (input.boundary_policy_id) find(policies, input.boundary_policy_id);
      if (input.resource_group_id) find(groups, input.resource_group_id);
      const row = {
        id: id(),
        name: input.name ?? '',
        key_prefix: 'demo_key',
        created_at: new Date().toISOString(),
      };
      keys.set(tenant, [...(keys.get(tenant) ?? []), row]);
      return answer({ ...row, key: `demo-only-${randomUUID()}` });
    },
    adminRevokeKey: async (value, tenantID) => {
      const rows = tenantID ? (keys.get(tenantID) ?? []) : [...keys.values()].flat();
      find(rows, value).revoked_at = new Date().toISOString();
      await answer(undefined);
    },
    adminRenameKey: async (value, name, tenantID) => {
      const rows = tenantID ? (keys.get(tenantID) ?? []) : [...keys.values()].flat();
      const row = find(rows, value);
      row.name = name;
      return answer({ id: row.id, name });
    },
    adminPolicies: () => answer(structuredClone(policies)),
    adminPolicy: (value) => answer(structuredClone(find(policies, value))),
    adminCreatePolicy: (input) => {
      const row = {
        ...structuredClone(input),
        description: input.description ?? '',
        id: id(),
        platform: false,
      };
      policies.push(row);
      return answer({ id: row.id, name: row.name, description: row.description });
    },
    adminDeletePolicy: async (value) => {
      const row = find(policies, value);
      if (row.platform) throw new APIError('Platform preset.', 403, 'FORBIDDEN');
      policies.splice(policies.indexOf(row), 1);
      await answer(undefined);
    },
    adminGroups: () => answer(structuredClone(groups)),
    adminCreateGroup: (input) => {
      const row = { ...input, id: id() };
      groups.push(row);
      return answer(row);
    },
  };
}
