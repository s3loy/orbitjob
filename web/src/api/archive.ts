import { z } from 'zod';
import type { Request } from './monitoring';
import type { LogParent } from './definitions';

export const archiveRecordSchema = z.object({
  id: z.string(),
  sequence: z.number().int().nonnegative().refine(Number.isSafeInteger),
  time: z.string(),
  observed_at: z.string(),
  time_source: z.string().optional(),
  kind: z.enum(['stdout', 'stderr', 'event']),
  stage: z.string(),
  attempt: z.number().int().nonnegative(),
  pod_uid: z.string(),
  container: z.string(),
  restart: z.number().int(),
  text: z.string(),
  exit_code: z.number().int().optional(),
  signal: z.number().int().optional(),
});
export const archivePageSchema = z.object({
  state: z.enum([
    'collecting',
    'sealed',
    'partial',
    'capacity',
    'expired',
    'deleted',
    'unavailable',
  ]),
  reason: z.string(),
  outcome: z.string(),
  records: z.array(archiveRecordSchema),
  gaps: z.array(z.object({ from: z.number(), through: z.number(), reason: z.string() })),
  next: z.number().int().nonnegative().refine(Number.isSafeInteger),
  has_more: z.boolean(),
  output_bytes: z.number().nonnegative(),
  retained_bytes: z.number().nonnegative(),
  limit_bytes: z.number().nonnegative(),
  protected_until: z.string().optional(),
});
export type ArchiveRecord = z.infer<typeof archiveRecordSchema>;
export type ArchivePage = z.infer<typeof archivePageSchema>;
export type ArchiveQuery = { after?: number; limit?: number; attempt?: number; search?: string };
export interface ArchiveClient {
  archiveLogs?(parent: LogParent, query?: ArchiveQuery): Promise<ArchivePage>;
  deleteArchiveLogs?(parent: LogParent): Promise<{ state: string }>;
}
export function archiveClient(request: Request): ArchiveClient {
  const path = (parent: LogParent) =>
    parent.kind === 'instance'
      ? `/api/v1/instances/${encodeURIComponent(parent.run)}/logs/archive`
      : `/api/v1/functions/${encodeURIComponent(parent.id)}/runs/${encodeURIComponent(parent.run)}/logs/archive`;
  return {
    archiveLogs: (parent, query = {}) => {
      const params = new URLSearchParams();
      Object.entries(query).forEach(([key, value]) => {
        if (value !== undefined && value !== '') params.set(key, String(value));
      });
      return request(`${path(parent)}?${params}`, archivePageSchema);
    },
    deleteArchiveLogs: (parent) => request(path(parent), z.object({ state: z.string() }), 'DELETE'),
  };
}
