import { createContext, useContext } from 'react';
import type { Client } from './api/transport';
import type { AccountClient, AccountSnapshot } from './api/accounts';
import type { Identity } from './api/identity';

export type Mode = 'demo' | 'live' | 'disconnected';
export type Session = {
  generation: number;
  restoring?: boolean;
  restoreError?: Error;
  account?: AccountSnapshot;
  accounts?: AccountClient;
  restore?(): Promise<void>;
  authenticated?(account: AccountSnapshot): Promise<void>;
  targetTenant?: string;
  identity?: Identity;
  selectTenant?(tenant: string): Promise<Identity | undefined> | void;
  mode: Mode;
  client: Client | null;
  connect(mode: 'demo'): Promise<Identity | undefined> | void;
  disconnect(): Promise<void> | void;
};
export const SessionContext = createContext<Session | null>(null);
export function useSession() {
  const session = useContext(SessionContext);
  if (!session) throw new Error('Session provider missing');
  return session;
}
