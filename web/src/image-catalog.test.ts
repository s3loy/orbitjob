import { describe, expect, it, vi } from 'vitest';
import { APIError } from './api/transport';
import type { Client } from './api/transport';
import {
  imageCatalogProblem,
  loadImageSources,
  publicImageSearch,
  resolveSourceImage,
} from './image-catalog';

describe('authorized image reuse', () => {
  it('pages by original Jobs page and reads an image only when chosen', async () => {
    const job = vi.fn().mockResolvedValue({
      job_template: { image: 'registry.private:5000/team/task@sha256:abc123' },
    });
    const jobs = vi.fn().mockResolvedValue(
      Array.from({ length: 20 }, (_, index) => ({
        id: index + 1,
        name: `job-${index}`,
        namespace: 'team',
        source_kind: index === 19 ? 'ScheduledJob' : 'workflow',
      })),
    );
    const client = { jobs, job } as unknown as Client;
    const page = await loadImageSources(client, 'jobs', 40, 'billing');
    expect(jobs).toHaveBeenCalledWith(40, 'billing');
    expect(page.hasMore).toBe(true);
    expect(page.items).toEqual([{ id: '20', name: 'job-19', namespace: 'team' }]);
    expect(job).not.toHaveBeenCalled();
    expect(await resolveSourceImage(client, page.items[0])).toBe(
      'registry.private:5000/team/task@sha256:abc123',
    );
    expect(job).toHaveBeenCalledWith('20');
  });
  it('preserves Function tags without looking up unrelated Jobs', async () => {
    const functions = vi
      .fn()
      .mockResolvedValue([
        { id: '9007199254740999', name: 'export', image: 'private.example/team/export:release-42' },
      ]);
    const job = vi.fn();
    const client = { functions, job } as unknown as Client;
    const page = await loadImageSources(client, 'functions', 20, 'export');
    expect(functions).toHaveBeenCalledWith({ offset: 20, search: 'export' });
    expect(await resolveSourceImage(client, page.items[0])).toBe(
      'private.example/team/export:release-42',
    );
    expect(job).not.toHaveBeenCalled();
    expect(page.hasMore).toBe(false);
  });
  it('keeps authorization failures distinct from an empty catalog', async () => {
    const denied = new APIError('Forbidden', 403);
    const client = { jobs: vi.fn().mockRejectedValue(denied) } as unknown as Client;
    await expect(loadImageSources(client, 'jobs', 0, '')).rejects.toBe(denied);
    expect(imageCatalogProblem(denied)).toContain('cannot read this source');
    expect(imageCatalogProblem(new APIError('limited', 429))).toContain('rate limited');
  });
  it('encodes only the explicitly provided public search text', () => {
    expect(publicImageSearch(' python & alpine ')).toBe(
      'https://hub.docker.com/search?q=python%20%26%20alpine',
    );
  });
});
