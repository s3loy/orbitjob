import type { ManifestImportPreview } from './manifest-import';
import type { ManifestKind } from './manifests';

export function previewManifestImportAsync(
  input: string,
  expectedKind: ManifestKind,
  signal?: AbortSignal,
): Promise<ManifestImportPreview> {
  if (new TextEncoder().encode(input).byteLength > 262144)
    return Promise.reject(new Error('Keep one workload document under 256 KiB.'));
  if (signal?.aborted) return Promise.reject(new DOMException('Import canceled.', 'AbortError'));
  return new Promise((resolve, reject) => {
    const worker = new Worker(new URL('./manifest-import.worker.ts', import.meta.url), {
      type: 'module',
    });
    const stop = () => {
      clearTimeout(timer);
      worker.terminate();
      signal?.removeEventListener('abort', abort);
    };
    const abort = () => {
      stop();
      reject(new DOMException('Import canceled.', 'AbortError'));
    };
    const timer = setTimeout(() => {
      stop();
      reject(new Error('YAML parsing exceeded its time budget. Reduce the document structure.'));
    }, 2000);
    signal?.addEventListener('abort', abort, { once: true });
    worker.onmessage = (
      event: MessageEvent<{ result?: ManifestImportPreview; error?: string }>,
    ) => {
      stop();
      if (event.data.result) resolve(event.data.result);
      else reject(new Error(event.data.error ?? 'Unable to read this YAML.'));
    };
    worker.onerror = () => {
      stop();
      reject(new Error('The YAML parser could not start. Reload this page before importing.'));
    };
    worker.postMessage({ input, expectedKind });
  });
}
