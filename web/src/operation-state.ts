import { createContext, useContext } from 'react';
import type { Job, Receipt } from './api/contracts';
export type TriggerIntent = {
  key: string;
  job: Job;
  pending: boolean;
  receipt?: Receipt;
  error?: Error;
  acceptedAt?: number;
};
type Cancellation = { pending: boolean; acceptedAt?: number; error?: Error };
type Operations = {
  intents: Record<string, TriggerIntent>;
  receipts: Record<string, TriggerIntent>;
  cancellations: Record<string, Cancellation>;
  cancel(id: string): Promise<boolean>;
  prepare(job: Job, fresh?: boolean): TriggerIntent;
  submit(job: Job): Promise<boolean>;
};
export const OperationsContext = createContext<Operations | null>(null);

export function useOperations() {
  const value = useContext(OperationsContext);
  if (!value) throw new Error('Operation provider missing');
  return value;
}
