import { describe, expect, it } from 'vitest';
import { workloadBrief } from './workload-brief';

describe('portable workload brief', () => {
  it('carries chosen metadata and installation-specific schema links without claiming remote access', () => {
    const brief = workloadBrief(
      {
        kind: 'WorkflowJob',
        name: 'nightly-flow',
        namespace: 'tenant-ops',
        request: 'Run the two existing export jobs in sequence.',
      },
      'http://127.0.0.1:18082',
      '/console/',
    );
    expect(brief).toContain('Resource name: "nightly-flow"');
    expect(brief).toContain('namespace: "tenant-ops"');
    expect(brief).toContain(
      'http://127.0.0.1:18082/console/ai/orbitjob-workloads/orbitjob-workloads.zip',
    );
    expect(brief).toContain('http://127.0.0.1:18082/openapi.json');
    expect(brief).toContain('ask me to attach the downloaded ZIP');
    expect(brief).toContain('Do not apply or run anything automatically');
  });
  it('asks for unknown operational choices instead of inventing a namespace or image', () => {
    const brief = workloadBrief({ kind: 'ScheduledJob' }, 'https://ops.example', '/console/');
    expect(brief).toContain('Ask me to confirm the namespace');
    expect(brief).toContain('Ask me for the image');
    expect(brief).not.toContain('busybox');
  });
  it('links a public Demo to its static reference rather than inventing an installation API', () => {
    const brief = workloadBrief(
      { kind: 'ScheduledJob' },
      'https://docs.example',
      '/orbitjob/demo/',
      true,
    );
    expect(brief).toContain(
      'https://docs.example/orbitjob/demo/ai/orbitjob-workloads/references/openapi.yaml',
    );
    expect(brief).not.toContain('https://docs.example/openapi.json');
  });
});
