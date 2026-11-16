import { randomUUID } from './uuid';
import { useEffect, useRef, useState, type ReactNode } from 'react';
import type { Job } from './api/contracts';
import { useSession } from './session';
import { OperationsContext, type TriggerIntent } from './operation-state';
import {
  administrationAuthenticationError,
  rejectAdministrationCredential,
} from './administration-state';
type Cancellation = { pending: boolean; acceptedAt?: number; error?: Error };

// Mounted for one connection generation. Navigation never replaces an intent;
// connection changes unmount the provider and abort its client.
export function OperationsProvider({ children }: { children: ReactNode }) {
  const { client } = useSession();
  const [intents, setIntents] = useState<Record<string, TriggerIntent>>({});
  const current = useRef(intents);
  const [receipts, setReceipts] = useState<Record<string, TriggerIntent>>({});
  const [cancellations, setCancellations] = useState<Record<string, Cancellation>>({});
  const cancelCurrent = useRef(cancellations);
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  function put(intent: TriggerIntent) {
    if (!alive.current) return;
    current.current = { ...current.current, [String(intent.job.id)]: intent };
    setIntents(current.current);
    if (intent.receipt) setReceipts((prior) => ({ ...prior, [intent.key]: intent }));
  }
  function prepare(job: Job, fresh = false) {
    const prior = current.current[String(job.id)];
    if (prior && (!fresh || prior.pending)) return prior;
    const intent: TriggerIntent = { key: randomUUID(), job, pending: false };
    put(intent);
    return intent;
  }
  async function submit(job: Job) {
    const intent = prepare(job);
    if (
      !client ||
      administrationAuthenticationError(client) ||
      job.suspend ||
      intent.pending ||
      intent.receipt
    )
      return false;
    put({ ...intent, pending: true, error: undefined });
    try {
      const receipt = await client.trigger(job.id, intent.key);
      put({ ...intent, pending: false, receipt, acceptedAt: Date.now() });
      return alive.current;
    } catch (error) {
      if (error instanceof Error) rejectAdministrationCredential(client, error);
      if (alive.current && !(error instanceof DOMException && error.name === 'AbortError')) {
        put({
          ...intent,
          pending: false,
          error: error instanceof Error ? error : new Error('Trigger was not confirmed.'),
        });
      }
      return false;
    }
  }
  function putCancellation(id: string, value: Cancellation) {
    if (!alive.current) return;
    cancelCurrent.current = { ...cancelCurrent.current, [id]: value };
    setCancellations(cancelCurrent.current);
  }
  async function cancel(id: string) {
    const prior = cancelCurrent.current[id];
    if (!client || administrationAuthenticationError(client) || prior?.pending || prior?.acceptedAt)
      return false;
    putCancellation(id, { pending: true });
    try {
      await client.cancel(id);
      putCancellation(id, { pending: false, acceptedAt: Date.now() });
      return alive.current;
    } catch (error) {
      if (error instanceof Error) rejectAdministrationCredential(client, error);
      if (alive.current && !(error instanceof DOMException && error.name === 'AbortError'))
        putCancellation(id, {
          pending: false,
          error: error instanceof Error ? error : new Error('Cancellation not confirmed.'),
        });
      return false;
    }
  }
  return (
    <OperationsContext value={{ intents, receipts, cancellations, prepare, submit, cancel }}>
      {children}
    </OperationsContext>
  );
}
