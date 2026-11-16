import { z } from 'zod';
import type { Request } from './monitoring';

export type DefinitionKind = 'jobs' | 'workflows';
export const definitionSchema = z.object({
  kind: z.string(),
  name: z.string(),
  namespace: z.string(),
  uid: z.string(),
  resource_version: z.string(),
  generation: z.union([z.number(), z.string()]),
  spec_json: z.string(),
  manifest_digest: z.string(),
  last_request_id: z.string(),
  last_manifest_digest: z.string(),
});
const resultSchema = definitionSchema.extend({
  request_id: z.string(),
  state: z.enum(['validated', 'accepted', 'reconciled']),
});
export type Definition = z.infer<typeof definitionSchema>;
export type DefinitionResult = z.infer<typeof resultSchema>;
const operationSchema = z.object({
  request_id: z.string(),
  kind: z.string(),
  namespace: z.string(),
  name: z.string(),
  state: z.enum(['checked', 'dispatching', 'accepted', 'rejected', 'unknown', 'expired']),
  result: resultSchema.nullish(),
  admitted_uid: z.string().optional(),
});
export type DefinitionOperationResult = z.infer<typeof operationSchema>;
export type DefinitionInput = {
  request_id: string;
  expected_uid: string;
  expected_resource_version: string;
  spec_json: string;
  dry_run: boolean;
  manifest_digest?: string;
};
const logSchema = z.object({
  state: z.enum(['ready', 'starting', 'absent', 'unavailable']),
  text: z.string(),
  truncated: z.boolean(),
  tail_lines: z.number().int(),
  limit_bytes: z.number().int(),
  attempt: z.number().int(),
  container: z.string(),
  reason: z.string(),
});
export type LogSnapshot = z.infer<typeof logSchema>;
export type LogParent =
  { kind: 'instance'; run: string } | { kind: 'function'; id: string; run: string };
export interface DefinitionClient {
  definitionOperation?(
    kind: DefinitionKind,
    namespace: string,
    name: string,
    requestID: string,
  ): Promise<DefinitionOperationResult>;
  definition(kind: DefinitionKind, namespace: string, name: string): Promise<Definition>;
  applyDefinition(
    kind: DefinitionKind,
    namespace: string,
    name: string,
    input: DefinitionInput,
  ): Promise<DefinitionResult>;
  attemptLogs(parent: LogParent, attempt: number, tail: number): Promise<LogSnapshot>;
}
export function definitionClient(request: Request): DefinitionClient {
  const path = (kind: DefinitionKind, namespace: string, name: string) =>
    `/api/v1/definitions/${kind}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`;
  return {
    definitionOperation: (kind, namespace, name, requestID) =>
      request(
        `${path(kind, namespace, name)}/operations/${encodeURIComponent(requestID)}`,
        operationSchema,
      ),
    definition: (kind, namespace, name) => request(path(kind, namespace, name), definitionSchema),
    applyDefinition: (kind, namespace, name, input) =>
      request(path(kind, namespace, name), resultSchema, 'PUT', undefined, input),
    attemptLogs: (parent, attempt, tail) =>
      request(
        parent.kind === 'instance'
          ? `/api/v1/instances/${encodeURIComponent(parent.run)}/attempts/${attempt}/logs?tail_lines=${tail}&limit_bytes=65536`
          : `/api/v1/functions/${encodeURIComponent(parent.id)}/runs/${encodeURIComponent(parent.run)}/attempts/${attempt}/logs?tail_lines=${tail}&limit_bytes=65536`,
        logSchema,
      ),
  };
}
