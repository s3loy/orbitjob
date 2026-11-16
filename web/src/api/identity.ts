import { z } from 'zod';
import type { components } from './schema';
import type { Request } from './monitoring';
export type Identity = components['schemas']['IdentityResponse'] & {
  namespace_metadata_available?: boolean;
  subject_type?: string;
  user_id?: string;
  username?: string;
  display_name?: string;
  installation_admin?: boolean;
  membership_role?: string;
};
export type CapabilityProbe = { action: string; resource: string };
const schema = z
  .object({
    key_id: z.string(),
    subject_type: z.string().default('api_key'),
    user_id: z.string().optional(),
    username: z.string().optional(),
    display_name: z.string().optional(),
    installation_admin: z.boolean().default(false),
    membership_role: z.string().optional(),
    kind: z.string(),
    tenant_id: z.string(),
    group_id: z.string(),
    boundary_policy_id: z.string(),
    target_tenant_id: z.string().optional(),
    tenant_name: z.string().optional(),
    tenant_slug: z.string().optional(),
    key_name: z.string().optional(),
    workload_namespaces: z.array(z.string()).optional(),
    capabilities: z.array(
      z.object({ action: z.string(), resource: z.string(), allowed: z.boolean() }),
    ),
  })
  .transform((value) => ({
    ...value,
    workload_namespaces: value.workload_namespaces ?? [],
    namespace_metadata_available: value.workload_namespaces !== undefined,
  })) satisfies z.ZodType<Identity>;
export interface IdentityClient {
  identity(probes?: CapabilityProbe[], tenant?: string): Promise<Identity>;
}
export function identityClient(request: Request): IdentityClient {
  return {
    identity: (probes = [], tenant = '') => {
      const query = new URLSearchParams();
      if (tenant) query.set('tenant_id', tenant);
      for (const probe of probes) {
        query.append('action', probe.action);
        query.append('resource', probe.resource);
      }
      return request(`/api/v1/identity${query.size ? `?${query}` : ''}`, schema);
    },
  };
}
