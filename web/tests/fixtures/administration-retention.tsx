import { useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { Theme } from '@radix-ui/themes';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Link, Route, Routes } from 'react-router-dom';
import { Administration } from '../../src/pages/Administration';
import { AdministrationReceipt } from '../../src/AdministrationReceipt';
import { clearAdministrationSession } from '../../src/administration-state';
import { administrationDemo } from '../../src/api/administration-demo';
import type { AdministrationClient } from '../../src/api/administration';
import { demoClient } from '../../src/api/demo';
import { APIError, type Client } from '../../src/api/transport';
import { SessionContext } from '../../src/session';
import '@radix-ui/themes/styles.css';
import '../../src/styles.css';
const query = new QueryClient({ defaultOptions: { queries: { retry: false } } });
function Fixture() {
  const [requests, setRequests] = useState(0);
  const resolve = useRef<() => void>(() => {});
  const reject = useRef<() => void>(() => {});
  const [client, setClient] = useState<Client | null>(() => {
    const base = { ...demoClient(), ...administrationDemo(async (value) => value) };
    return {
      ...base,
      adminCreateKey: async (
        tenant: string,
        input: Parameters<AdministrationClient['adminCreateKey']>[1],
      ) => {
        setRequests((count) => count + 1);
        const result = await base.adminCreateKey(tenant, input);
        await new Promise<void>((done, fail) => {
          resolve.current = done;
          reject.current = () => fail(new APIError('Connection interrupted.', 0, 'NETWORK'));
        });
        return result;
      },
    } as Client;
  });
  function disconnect() {
    clearAdministrationSession(client);
    setClient(null);
    query.clear();
  }
  return (
    <Theme
      accentColor="gray"
      appearance={window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'}
    >
      <QueryClientProvider client={query}>
        <SessionContext
          value={{
            generation: client ? 1 : 2,
            mode: client ? 'demo' : 'disconnected',
            client,
            connect: () => {},
            disconnect,
          }}
        >
          <MemoryRouter initialEntries={['/administration']}>
            <nav>
              <Link to="/administration">Administration</Link>{' '}
              <Link to="/elsewhere">Elsewhere</Link>
            </nav>
            <p>Create requests: {requests}</p>
            <button onClick={() => resolve.current()}>Complete create response</button>
            <button onClick={() => reject.current()}>Fail create response</button>
            <button onClick={disconnect}>Disconnect fixture</button>
            <AdministrationReceipt />
            {client ? (
              <Routes>
                <Route path="/administration" element={<Administration />} />
                <Route path="/elsewhere" element={<h1>Elsewhere</h1>} />
              </Routes>
            ) : (
              <p>Fixture disconnected</p>
            )}
          </MemoryRouter>
        </SessionContext>
      </QueryClientProvider>
    </Theme>
  );
}
createRoot(document.getElementById('root')!).render(<Fixture />);
