import type { ManifestKind } from './api/manifests';

export type WorkloadBrief = {
  kind: ManifestKind;
  namespace?: string;
  name?: string;
  request?: string;
};
export function workloadBrief(
  { kind, namespace, name, request }: WorkloadBrief,
  origin: string,
  base: string,
  preview = false,
): string {
  return [
    'Use the attached orbitjob-workloads skill and its bundled CRD, API and usage references to create one OrbitJob YAML manifest.',
    `Requested kind: ${kind}`,
    `Resource name: ${name ? JSON.stringify(name) : 'Ask me for the name.'}`,
    `Tenant-mapped Kubernetes namespace: ${namespace ? JSON.stringify(namespace) : 'Ask me to confirm the namespace with my administrator.'}`,
    `What the workload should do: ${request?.trim() || 'Ask me for the image, command, literal arguments, timing, timeout and retry requirements.'}`,
    `Skill and complete references: ${origin}${base}ai/orbitjob-workloads/orbitjob-workloads.zip`,
    'If this installation URL is local or unreachable from your environment, ask me to attach the downloaded ZIP. Do not substitute an unrelated OrbitJob version.',
    preview
      ? `API reference snapshot: ${origin}${base}ai/orbitjob-workloads/references/openapi.yaml`
      : `Current installation API schema: ${origin}/openapi.json`,
    'Return editable YAML and explain execution timing and any setup still required. Preserve tags, digests and literal command argument boundaries. An empty schedule means manual only.',
    'For a WorkflowJob, reference existing ScheduledJobs in the same namespace, with acyclic dependencies; ask me to confirm their names.',
    'I will import YAML into the Console, review the form, run cluster validation and explicitly confirm the save. Do not apply or run anything automatically.',
    'Keep API keys, registry passwords and other credentials outside this conversation and manifest.',
  ].join('\n\n');
}
