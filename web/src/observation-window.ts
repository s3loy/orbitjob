import { useCallback, useEffect, useState } from 'react';
import { OBSERVATION_MS } from './api/transport';

export function useObservationWindow() {
  const [since, setSince] = useState(Date.now);
  const [expired, setExpired] = useState(0);
  useEffect(() => {
    const timer = setTimeout(
      () => setExpired(since),
      Math.max(0, since + OBSERVATION_MS - Date.now()),
    );
    return () => clearTimeout(timer);
  }, [since]);
  const restart = useCallback(() => setSince(Date.now()), []);
  return { since, paused: expired === since, restart };
}
