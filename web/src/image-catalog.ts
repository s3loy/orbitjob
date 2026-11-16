import type { Client } from './api/transport';
import { APIError, PAGE_SIZE } from './api/transport';
import { withCredential } from './credential-requests';

export type ImageSourceKind = 'jobs' | 'functions';
export type ImageSource = {
  id: string;
  name: string;
  namespace?: string;
  image?: string;
};
export async function loadImageSources(
  client: Pick<Client, 'jobs' | 'functions'>,
  kind: ImageSourceKind,
  offset: number,
  search: string,
) {
  if (kind === 'functions') {
    const rows = await withCredential(client, () => client.functions({ offset, search }));
    return {
      items: rows.map(({ id, name, image }) => ({ id: String(id), name, image })),
      hasMore: rows.length === PAGE_SIZE,
    };
  }
  const rows = await withCredential(client, () => client.jobs(offset, search));
  return {
    items: rows
      .filter(
        (row) =>
          !['WorkflowJob', 'workflow', 'Function', 'function'].includes(row.source_kind ?? ''),
      )
      .map(({ id, name, namespace }) => ({ id: String(id), name, namespace })),
    // Pagination follows the unfiltered API page, including pages with no reusable Jobs.
    hasMore: rows.length === PAGE_SIZE,
  };
}
export async function resolveSourceImage(
  client: Pick<Client, 'job'>,
  source: ImageSource,
): Promise<string> {
  return withCredential(client, async () => {
    const image = source.image ?? (await client.job(source.id)).job_template.image;
    if (!image.trim())
      throw new Error('The selected workload does not contain an image reference.');
    return image;
  });
}
export function imageCatalogProblem(error: unknown): string {
  if (error instanceof APIError && error.status === 403)
    return 'Your key cannot read this source. Try the other source or enter an image reference directly.';
  if (error instanceof APIError && error.status === 429)
    return 'Image sources are temporarily rate limited. Wait a moment, then try again.';
  return 'Image sources could not be loaded. Try again or enter an image reference directly.';
}
export function publicImageSearch(query: string): string {
  return `https://hub.docker.com/search?q=${encodeURIComponent(query.trim())}`;
}
