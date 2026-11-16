import { useQuery, type QueryClient } from '@tanstack/react-query';
import type { WorkspacePage } from './api/administration';
import { useSession } from './session';
import { useAdministrationAuthenticationError } from './administration-state';

export function workspaceDirectoryKey(generation: number, search = '', offset = 0) {
  return ['workspaces', generation, search.trim(), offset] as const;
}

export function cachedWorkspace(cache: QueryClient, generation: number, id: string) {
  for (const queryKey of [
    ['workspaces', generation],
    ['administration', generation, 'tenants'],
  ]) {
    for (const [key, page] of cache.getQueriesData<WorkspacePage>({ queryKey })) {
      if (cache.getQueryState(key)?.status === 'success') {
        const row = page?.items.find((workspace) => workspace.id === id);
        if (row) return row;
      }
    }
  }
}

export function useWorkspaceDirectory(search: string, offset: number, enabled = true) {
  const { client, generation } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const query = useQuery({
    queryKey: workspaceDirectoryKey(generation, search, offset),
    queryFn: () => client!.workspaceDirectory(offset, search),
    enabled: !!client && enabled && !rejected,
    retry: false,
  });
  return { ...query, error: rejected ?? query.error };
}
