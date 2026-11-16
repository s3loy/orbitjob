import { randomUUID } from './uuid';
import { useEffect, useRef, useState, type ReactNode } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useSession } from './session';
import { APIError } from './api/transport';
import {
  M4Context,
  cancellationKey,
  sameTarget,
  uncertain,
  type Intent,
  type Kind,
  type Target,
  type Cancellation,
} from './m4-state';
import type { WorkflowRunDetail } from './api/m4';
import {
  administrationAuthenticationError,
  rejectAdministrationCredential,
} from './administration-state';

export function M4Provider({ children }: { children: ReactNode }) {
  const { client, generation } = useSession();
  const cache = useQueryClient();
  const alive = useRef(true);
  const [intents, setIntents] = useState<Record<string, Intent>>({});
  const current = useRef(intents);
  const [cancellations, setCancellations] = useState<Record<string, Cancellation>>({});
  const stops = useRef(cancellations);
  const [limitedUntil, setLimitedUntil] = useState(0);
  const limit = useRef(0);
  function limited(error: Error, at = Date.now()) {
    if (error instanceof APIError && error.status === 429) {
      limit.current = Math.max(limit.current, at + error.retryAfter);
      setLimitedUntil(limit.current);
    }
  }
  useEffect(() => {
    alive.current = true;
    const unsubscribe = cache.getQueryCache().subscribe((event) => {
      const state = event.query.state;
      if (
        client &&
        event.query.queryKey[1] === generation &&
        state.error instanceof APIError &&
        state.error.status === 401
      )
        rejectAdministrationCredential(client, state.error);
      if (state.error instanceof APIError && state.error.status === 429) {
        const until = state.errorUpdatedAt + state.error.retryAfter;
        if (until > Date.now() && until > limit.current) {
          limit.current = until;
          setLimitedUntil(until);
        }
      }
    });
    return () => {
      alive.current = false;
      unsubscribe();
    };
  }, [cache, client, generation]);
  useEffect(() => {
    if (!limitedUntil) return;
    const timer = setTimeout(() => setLimitedUntil(0), Math.max(0, limitedUntil - Date.now()));
    return () => clearTimeout(timer);
  }, [limitedUntil]);
  function put(intent: Intent) {
    if (!alive.current) return;
    current.current = { ...current.current, [intent.key]: intent };
    setIntents(current.current);
  }
  function prepare(kind: Kind, target: Target) {
    const prior = Object.values(current.current).find(
      (i) => i.kind === kind && sameTarget(kind, i.target, target) && (i.pending || i.unknown),
    );
    if (prior) return prior;
    const intent: Intent = {
      key: randomUUID(),
      kind,
      target: structuredClone(target),
      pending: false,
    };
    put(intent);
    return intent;
  }
  async function submit(key: string) {
    const intent = current.current[key];
    if (
      !client ||
      administrationAuthenticationError(client) ||
      !intent ||
      intent.pending ||
      intent.receipt ||
      Date.now() < limit.current
    )
      return false;
    put({ ...intent, pending: true, error: undefined });
    try {
      const receipt = await (intent.kind === 'workflow'
        ? client.triggerWorkflow(String(intent.target.id), intent.key)
        : client.invokeFunction(String(intent.target.id), intent.key, intent.target.version));
      put({ ...intent, pending: false, receipt, unknown: false, error: undefined });
      if (alive.current) void cache.invalidateQueries({ queryKey: ['m4'], refetchType: 'none' });
      return alive.current;
    } catch (cause) {
      if (!alive.current || (cause instanceof DOMException && cause.name === 'AbortError'))
        return false;
      const error = cause instanceof Error ? cause : new Error('Request not confirmed');
      rejectAdministrationCredential(client, error);
      limited(error);
      // A rejected replay does not resolve a preceding unknown write.
      put({ ...intent, pending: false, error, unknown: intent.unknown || uncertain(error) });
      return false;
    }
  }
  function dismiss(key: string) {
    const intent = current.current[key];
    if (!intent || intent.pending || intent.unknown) return;
    const next = { ...current.current };
    delete next[key];
    current.current = next;
    setIntents(next);
  }
  function stop(id: string, value: Cancellation) {
    if (!alive.current) return;
    stops.current = { ...stops.current, [id]: value };
    setCancellations(stops.current);
  }
  async function cancel(id: string, run: WorkflowRunDetail) {
    const key = cancellationKey(run);
    const prior = stops.current[key];
    if (
      !client ||
      administrationAuthenticationError(client) ||
      prior?.pending ||
      prior?.receipt ||
      Date.now() < limit.current
    )
      return false;
    const identity = { workflowId: id, runId: String(run.id) };
    stop(key, { ...identity, pending: true, unknown: prior?.unknown });
    try {
      const receipt = await client.cancelWorkflow(id, String(run.id));
      stop(key, { ...identity, pending: false, receipt });
      return alive.current;
    } catch (cause) {
      if (!alive.current || (cause instanceof DOMException && cause.name === 'AbortError'))
        return false;
      const error = cause instanceof Error ? cause : new Error('Cancellation not confirmed');
      rejectAdministrationCredential(client, error);
      limited(error);
      stop(key, {
        ...identity,
        pending: false,
        error,
        unknown: prior?.unknown || uncertain(error),
      });
      return false;
    }
  }
  return (
    <M4Context value={{ intents, cancellations, limitedUntil, prepare, submit, dismiss, cancel }}>
      {children}
    </M4Context>
  );
}
