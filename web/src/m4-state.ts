import { createContext, useContext } from 'react';
import type { M4Receipt, CancelReceipt, WorkflowRunDetail } from './api/m4';
export type Target = {
  id: number | string;
  name: string;
  source_uid?: string;
  suspend?: boolean;
  status?: string;
  version?: number;
};
export type Kind = 'workflow' | 'function';
export function sameTarget(kind: Kind, left: Target, right: Target) {
  return kind === 'workflow' && left.source_uid && right.source_uid
    ? left.source_uid === right.source_uid
    : String(left.id) === String(right.id);
}
export type Intent = {
  key: string;
  kind: Kind;
  target: Target;
  pending: boolean;
  receipt?: M4Receipt;
  error?: Error;
  unknown?: boolean;
};
export type Cancellation = {
  workflowId?: string;
  runId?: string;
  pending: boolean;
  receipt?: CancelReceipt;
  error?: Error;
  unknown?: boolean;
};
export function cancellationKey(run: WorkflowRunDetail) {
  return `${run.workflow_source_uid}/${run.id}`;
}
export const M4Context = createContext<{
  intents: Record<string, Intent>;
  cancellations: Record<string, Cancellation>;
  limitedUntil: number;
  prepare: (kind: Kind, target: Target) => Intent;
  submit: (key: string) => Promise<boolean>;
  dismiss: (key: string) => void;
  cancel: (id: string, run: WorkflowRunDetail) => Promise<boolean>;
} | null>(null);
export function useM4Operations() {
  const value = useContext(M4Context);
  if (!value) throw new Error('M4 provider required');
  return value;
}
export function uncertain(error: Error) {
  return (
    !(error instanceof DOMException && error.name === 'AbortError') &&
    (!('status' in error) ||
      error.status === 0 ||
      Number(error.status) >= 500 ||
      ('code' in error && error.code === 'INVALID_RESPONSE'))
  );
}
