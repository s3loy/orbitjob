import { LosslessNumber } from 'lossless-json';
import { z } from 'zod';
import type { Request } from './monitoring';
import { APIError } from './transport';
import type { Identity } from './identity';

export type SourceControlKind = 'jobs' | 'workflows' | 'functions' | 'checks';
const version = z.union([
  z.number().int().nonnegative().refine(Number.isSafeInteger),
  z.string().regex(/^(0|[1-9]\d*)$/),
]);
export const sourceControlSchema = z.object({
  source_mode: z.enum(['kubernetes', 'workflow', 'function', 'check']),
  source_uid: z.string().min(1),
  version,
  schedule_paused: z.boolean(),
  execution_paused: z.boolean(),
  tombstoned: z.boolean(),
  can_manage: z.boolean(),
  actor: z.string().optional(),
  updated_at: z.string().optional(),
});
export type SourceControl = z.infer<typeof sourceControlSchema>;
export type SourceControlChange = Pick<
  SourceControl,
  'version' | 'schedule_paused' | 'execution_paused'
>;
export interface SourceControlClient {
  sourceControls?(kind: SourceControlKind, id: string): Promise<SourceControl>;
  updateSourceControls?(
    kind: SourceControlKind,
    id: string,
    change: SourceControlChange,
  ): Promise<SourceControl>;
}
export function sourceControlClient(request: Request): SourceControlClient {
  const path = (kind: SourceControlKind, id: string) =>
    `/api/v1/${kind}/${encodeURIComponent(id)}/execution-controls`;
  return {
    sourceControls: (kind, id) => request(path(kind, id), sourceControlSchema),
    updateSourceControls: (kind, id, change) =>
      request(path(kind, id), sourceControlSchema, 'PATCH', undefined, {
        ...change,
        version:
          typeof change.version === 'string' ? new LosslessNumber(change.version) : change.version,
      }),
  };
}

export function sourceControlWriteUnknown(error: unknown): boolean {
  return (
    !(error instanceof APIError) ||
    error.status === 0 ||
    error.status >= 500 ||
    error.code === 'INVALID_RESPONSE'
  );
}

export function sourceControlValuesEqual(
  current: SourceControlChange,
  requested: SourceControlChange,
): boolean {
  return (
    current.schedule_paused === requested.schedule_paused &&
    current.execution_paused === requested.execution_paused
  );
}

const intentSchema = z
  .object({
    source_uid: z.string().min(1),
    source_mode: sourceControlSchema.shape.source_mode,
    version,
    schedule_paused: z.boolean(),
    execution_paused: z.boolean(),
  })
  .strict();
export type SourceControlIntent = z.infer<typeof intentSchema>;
export function sourceControlIntentKey(
  identity: Pick<Identity, 'subject_type' | 'user_id' | 'key_id'> | undefined,
  tenant: string,
  kind: SourceControlKind,
  id: string,
): string | undefined {
  const subject = identity?.subject_type || 'api_key';
  const principal = subject === 'user' ? identity?.user_id : identity?.key_id;
  if (!principal) return undefined;
  return `orbitjob.source-controls:${JSON.stringify([subject, principal, tenant, kind, id])}`;
}
type IntentStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>;
function storage(): IntentStorage | undefined {
  try {
    return typeof window === 'undefined' ? undefined : window.sessionStorage;
  } catch {
    return undefined;
  }
}
export function readSourceControlIntent(
  key: string | undefined,
  store = storage(),
): SourceControlIntent | undefined {
  try {
    const raw = key ? store?.getItem(key) : undefined;
    if (!raw) return undefined;
    const parsed = intentSchema.safeParse(JSON.parse(raw));
    return parsed.success ? parsed.data : undefined;
  } catch {
    return undefined;
  }
}
export function keepSourceControlIntent(
  key: string | undefined,
  intent?: SourceControlIntent,
  store = storage(),
): void {
  try {
    if (!key) return;
    if (intent) store?.setItem(key, JSON.stringify(intent));
    else store?.removeItem(key);
  } catch {
    /* Storage failure does not grant a retry or change a write outcome. */
  }
}
