import { z } from 'zod';
import type { components } from './schema';
import type { Request } from './monitoring';
type S = components['schemas'];
export type Tenant = S['TenantListItem'];
export type Workspace = S['WorkspaceListItem'];
export type WorkspacePage = S['WorkspaceListResult'];
export type Key = S['APIKeyListItem'];
export type Policy = S['PolicyListItem'];
export type Group = S['ResourcegroupListItem'];
export type PolicyInput = Omit<S['CreatePolicyRequest'], 'document'> & {
  document: Policy['document'];
};
export const documentSchema = z
  .object({
    version: z.string().min(1),
    statement: z.array(
      z
        .object({
          effect: z.enum(['Allow', 'Deny']),
          action: z.array(z.string()),
          resource: z.array(z.string()),
        })
        .passthrough(),
    ),
  })
  .passthrough();
const tenant = z.object({ id: z.string(), name: z.string(), slug: z.string(), status: z.string() });
const key = z.object({
  id: z.string(),
  name: z.string().default(''),
  key_prefix: z.string(),
  created_at: z.string(),
  revoked_at: z.string().nullable().optional(),
});
const policy = z.object({
  id: z.string(),
  name: z.string(),
  description: z.string(),
  platform: z.boolean(),
  document: documentSchema,
});
const group = z.object({ id: z.string(), name: z.string(), slug: z.string() });
const keyResult = z.object({
  id: z.string(),
  name: z.string().default(''),
  key: z.string(),
  key_prefix: z.string(),
  created_at: z.string(),
});
export interface AdministrationClient {
  workspaceDirectory(offset?: number, search?: string, limit?: number): Promise<WorkspacePage>;
  adminTenants(offset?: number): Promise<Tenant[]>;
  adminTenant(id: string): Promise<Tenant>;
  adminCreateTenant(input: S['CreateTenantRequest']): Promise<Tenant>;
  adminKeys(tenantID: string): Promise<Key[]>;
  adminCreateKey(
    tenantID: string,
    input: S['CreateAPIKeyRequest'],
  ): Promise<S['APIKeyCreateResult']>;
  adminRevokeKey(id: string, tenantID?: string): Promise<void>;
  adminRenameKey(
    id: string,
    name: string,
    tenantID?: string,
  ): Promise<{ id: string; name: string }>;
  adminPolicies(tenantID?: string): Promise<Policy[]>;
  adminPolicy(id: string): Promise<Policy>;
  adminCreatePolicy(input: PolicyInput, tenantID?: string): Promise<S['PolicyCreateResult']>;
  adminDeletePolicy(id: string): Promise<void>;
  adminGroups(tenantID?: string): Promise<Group[]>;
  adminCreateGroup(input: S['CreateResourceGroupRequest'], tenantID?: string): Promise<Group>;
}
export function administrationClient(request: Request, pageSize: number): AdministrationClient {
  const segment = encodeURIComponent;
  const scoped = (path: string, tenantID?: string) =>
    tenantID ? `/api/v1/tenants/${segment(tenantID)}/${path}` : `/api/v1/${path}`;
  const keyTarget = (id: string, suffix: string, tenantID?: string) => {
    const path = `/api/v1/api_keys/${segment(id)}${suffix}`;
    return tenantID ? `${path}?${new URLSearchParams({ tenant_id: tenantID })}` : path;
  };
  const list = async <T>(path: string, schema: z.ZodType<T>) =>
    (await request(path, z.object({ items: z.array(schema) }))).items;
  return {
    workspaceDirectory: (offset = 0, search = '', limit = pageSize) => {
      const query = new URLSearchParams({ limit: String(limit), offset: String(offset) });
      if (search.trim()) query.set('search', search.trim());
      return request(
        `/api/v1/workspaces?${query}`,
        z.object({ items: z.array(tenant), has_more: z.boolean() }),
      );
    },
    adminTenants: (offset = 0) =>
      list(`/api/v1/tenants?limit=${pageSize}&offset=${offset}`, tenant),
    adminTenant: (id) => request(`/api/v1/tenants/${segment(id)}`, tenant),
    adminCreateTenant: (input) => request('/api/v1/tenants', tenant, 'POST', undefined, input),
    adminKeys: (id) => list(`/api/v1/tenants/${segment(id)}/api_keys`, key),
    adminCreateKey: (id, input) =>
      request(`/api/v1/tenants/${segment(id)}/api_keys`, keyResult, 'POST', undefined, input),
    adminRevokeKey: async (id, tenantID) => {
      await request(keyTarget(id, '/revoke', tenantID), z.object({ revoked: z.boolean() }), 'POST');
    },
    adminRenameKey: (id, name, tenantID) =>
      request(
        keyTarget(id, '', tenantID),
        z.object({ id: z.string(), name: z.string() }),
        'PATCH',
        undefined,
        { name },
      ),
    adminPolicies: (tenantID) => list(scoped('policies', tenantID), policy),
    adminPolicy: (id) => request(`/api/v1/policies/${segment(id)}`, policy),
    adminCreatePolicy: (input, tenantID) =>
      request(
        scoped('policies', tenantID),
        policy.omit({ platform: true, document: true }),
        'POST',
        undefined,
        input,
      ),
    adminDeletePolicy: async (id) => {
      await request(`/api/v1/policies/${segment(id)}`, z.unknown(), 'DELETE');
    },
    adminGroups: (tenantID) => list(scoped('resource_groups', tenantID), group),
    adminCreateGroup: (input, tenantID) =>
      request(scoped('resource_groups', tenantID), group, 'POST', undefined, input),
  };
}
