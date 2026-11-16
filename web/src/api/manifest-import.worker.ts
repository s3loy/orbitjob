import { previewManifestImport } from './manifest-import';
import type { ManifestKind } from './manifests';

self.onmessage = (event: MessageEvent<{ input: string; expectedKind: ManifestKind }>) => {
  try {
    self.postMessage({ result: previewManifestImport(event.data.input, event.data.expectedKind) });
  } catch (error) {
    self.postMessage({
      error: error instanceof Error ? error.message : 'Unable to read this YAML.',
    });
  }
};
