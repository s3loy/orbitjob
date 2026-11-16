import { useEffect, useRef } from 'react';
import { useLocation } from 'react-router-dom';
type NavigationLocation = { pathname: string; search: string; state: unknown };
const monitoringPath =
  /^\/(jobs|checks|check-results|slos|slis|slo-alerts|workflows|functions|runs)([/?]|$)/;
export function consumedCreateSearch(search: string) {
  const params = new URLSearchParams(search);
  params.delete('create');
  const query = params.toString();
  return query ? `?${query}` : '';
}
function trail(state: unknown): string[] {
  if (!state || typeof state !== 'object') return [];
  const navigation = state as { trail?: unknown; back?: unknown; parentBack?: unknown };
  const values = Array.isArray(navigation.trail)
    ? navigation.trail
    : [navigation.back, navigation.parentBack];
  return values
    .filter((path): path is string => typeof path === 'string' && monitoringPath.test(path))
    .slice(0, 32);
}
// Keep each ancestor's query string when following related workload records.
export function forwardState(location: NavigationLocation) {
  return {
    trail: [
      location.pathname + consumedCreateSearch(location.search),
      ...trail(location.state),
    ].slice(0, 32),
  };
}
export function returnState(state: unknown, to: string) {
  const paths = trail(state);
  const index = paths.indexOf(to);
  return { trail: index < 0 ? [] : paths.slice(index + 1) };
}
export function collectionPath(
  location: NavigationLocation,
  section: 'checks' | 'slos' | 'slis' | 'jobs' | 'workflows' | 'functions' | 'runs' | 'slo-alerts',
) {
  const collection = new RegExp(`^/${section}(?:\\?|$)`);
  return (
    [location.pathname + location.search, ...trail(location.state)].find((path) =>
      collection.test(path),
    ) ?? `/${section}`
  );
}
export function useBack(fallback: string) {
  const { state } = useLocation();
  return trail(state)[0] ?? fallback;
}

export function useAlive() {
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  return alive;
}
