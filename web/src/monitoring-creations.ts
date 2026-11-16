import { useSyncExternalStore } from 'react';
import type { Client } from './api/transport';
import type { Resource } from './api/monitoring';
import { monitoringSourceKind, type MonitoringSource } from './monitoring-source';

export type MonitoringCreation = {
  key: string;
  resource: Resource;
  id: string;
  name: string;
  createdAt: string;
};
function initialDraft(scope: string) {
  const [resource, target] = JSON.parse(scope) as [Resource, string];
  return {
    values: {
      name: '',
      description: '',
      url: '',
      method: 'GET',
      expected: '200',
      schedule: 'interval',
      interval: '60',
      cron: '',
      timezone: 'UTC',
      timeout: '10',
      retry: '2',
      sliType: 'availability',
      source: resource === 'slis' ? target : '',
      aggregation: 'ratio',
      criteria: '{"status":"success"}',
      criterionMode: 'success',
      duration: '1000',
      sliId: resource === 'slos' ? target : '',
      target: '99.9',
      hours: '720',
      fast: '14.4',
      slow: '6',
    },
    sourceKind: monitoringSourceKind(resource === 'slis' ? target : ''),
    selectedSource: undefined as MonitoringSource | undefined,
    pending: false,
    uncertain: false,
    error: null as Error | null,
  };
}
export type MonitoringDraft = ReturnType<typeof initialDraft>;
export function monitoringDraftScope(resource: Resource, search: string) {
  const params = new URLSearchParams(search);
  return JSON.stringify([
    resource,
    resource === 'slis'
      ? (params.get('source_uid') ?? '')
      : resource === 'slos'
        ? (params.get('sli_id') ?? '')
        : '',
  ]);
}
const stores = new WeakMap<
  Client,
  {
    items: MonitoringCreation[];
    drafts: Map<string, MonitoringDraft>;
    listeners: Set<() => void>;
  }
>();
const closed = new WeakSet<Client>();
const empty: MonitoringCreation[] = [];
function state(client: Client) {
  let value = stores.get(client);
  if (!value) {
    value = { items: [], drafts: new Map(), listeners: new Set() };
    stores.set(client, value);
  }
  return value;
}
export function recordMonitoringCreation(
  client: Client,
  receipt: Omit<MonitoringCreation, 'key' | 'createdAt'>,
) {
  if (closed.has(client)) return;
  const store = state(client);
  const key = `${receipt.resource}/${receipt.id}`;
  store.items = [
    { ...receipt, key, createdAt: new Date().toISOString() },
    ...store.items.filter((item) => item.key !== key),
  ];
  store.listeners.forEach((listener) => listener());
}
export function dismissMonitoringCreation(client: Client, key: string) {
  const store = state(client);
  store.items = store.items.filter((item) => item.key !== key);
  store.listeners.forEach((listener) => listener());
}
export function monitoringCreations(client: Client) {
  return state(client).items;
}
export function clearMonitoringSession(client: Client) {
  closed.add(client);
  const store = stores.get(client);
  if (!store) return;
  store.items = [];
  store.drafts.clear();
  store.listeners.forEach((listener) => listener());
}
export function monitoringDraft(client: Client, scope: string) {
  const store = state(client);
  let draft = store.drafts.get(scope);
  if (!draft) {
    draft = initialDraft(scope);
    store.drafts.set(scope, draft);
  }
  return draft;
}
export function updateMonitoringDraft(
  client: Client,
  scope: string,
  patch: Partial<MonitoringDraft>,
) {
  if (closed.has(client)) return;
  const store = state(client);
  store.drafts.set(scope, { ...monitoringDraft(client, scope), ...patch });
  store.listeners.forEach((listener) => listener());
}
export function resetMonitoringDraft(client: Client, scope: string) {
  if (closed.has(client)) return;
  const store = state(client);
  store.drafts.set(scope, initialDraft(scope));
  store.listeners.forEach((listener) => listener());
}
export function useMonitoringDraft(client: Client, scope: string) {
  return useSyncExternalStore(
    (listener) => {
      const store = state(client);
      store.listeners.add(listener);
      return () => {
        store.listeners.delete(listener);
      };
    },
    () => monitoringDraft(client, scope),
  );
}
export function useMonitoringCreations(client: Client | null) {
  return useSyncExternalStore(
    (listener) => {
      if (!client) return () => {};
      const store = state(client);
      store.listeners.add(listener);
      return () => {
        store.listeners.delete(listener);
      };
    },
    () => (client ? state(client).items : empty),
  );
}
