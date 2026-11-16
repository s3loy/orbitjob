import { useRef, useState } from 'react';
import { Button, Flex, TextArea } from '@radix-ui/themes';
import { Copy, Download } from 'lucide-react';
import type { ManifestKind } from './api/manifests';
import { workloadBrief } from './workload-brief';
import { copyText } from './clipboard';
import { publicDemo } from './deployment';
import './workload-assistant.css';

export function WorkloadAssistant({
  kind,
  namespace,
  name,
  copy,
}: {
  kind: ManifestKind;
  namespace?: string;
  name?: string;
  copy?: (text: string) => Promise<unknown>;
}) {
  const [request, setRequest] = useState('');
  const [notice, setNotice] = useState('');
  const [manual, setManual] = useState(false);
  const briefInput = useRef<HTMLTextAreaElement>(null);
  const base = import.meta.env.BASE_URL;
  const assets = `${base}ai/orbitjob-workloads`;
  const brief = workloadBrief(
    { kind, namespace, name, request },
    window.location.origin,
    base,
    publicDemo,
  );
  async function copyBrief() {
    try {
      const copied = await (copy ? copy(brief) : copyText(brief));
      if (copied === false) throw new Error('Copy unavailable');
      const message = 'AI brief copied. Attach the downloaded skill to your assistant.';
      setNotice(message);
    } catch {
      const message = 'Copy is unavailable in this browser. Copy the selected brief manually.';
      setNotice(message);
      setManual(true);
      requestAnimationFrame(() => {
        briefInput.current?.focus();
        briefInput.current?.select();
      });
    }
  }
  return (
    <section className="workload-assistant" aria-label="AI YAML assistant">
      <label className="form-field">
        Workload brief (optional)
        <TextArea
          aria-label="AI workload request"
          value={request}
          rows={3}
          placeholder={
            kind === 'ScheduledJob'
              ? 'For example: run our export image every weekday at 02:00, with a 5-minute timeout.'
              : 'For example: run extract first, then publish only if extract succeeds.'
          }
          maxLength={4000}
          onChange={(event) => setRequest(event.target.value)}
        />
      </label>
      <Flex gap="3" wrap="wrap" mt="3" align="center">
        <Button type="button" size="2" variant="outline" onClick={() => void copyBrief()}>
          <Copy size={15} aria-hidden />
          Copy AI brief
        </Button>
        <a
          className="workload-skill-download"
          href={`${assets}/orbitjob-workloads.zip`}
          download="orbitjob-workloads.zip"
        >
          <Download size={15} aria-hidden />
          Download skill and references
        </a>
      </Flex>
      {notice && (
        <p role="status" className="assistant-notice">
          {notice}
        </p>
      )}
      <details
        className="assistant-brief"
        open={manual}
        onToggle={(event) => setManual(event.currentTarget.open)}
      >
        <summary>View brief</summary>
        <textarea
          ref={briefInput}
          aria-label="AI brief"
          value={brief}
          readOnly
          rows={10}
          onFocus={(event) => event.target.select()}
        />
      </details>
      <details className="assistant-reference-links">
        <summary>Schemas and API reference</summary>
        <ul>
          <li>
            <a href={`${assets}/SKILL.md`} target="_blank" rel="noreferrer">
              Skill instructions
            </a>
          </li>
          <li>
            <a
              href={publicDemo ? `${assets}/references/openapi.yaml` : '/openapi.json'}
              target="_blank"
              rel="noreferrer"
            >
              {publicDemo ? 'API reference snapshot' : 'Workspace API reference'}
            </a>
          </li>
          <li>
            <a href={`${assets}/references/api-index.md`} target="_blank" rel="noreferrer">
              Workload API index
            </a>
          </li>
          <li>
            <a
              href={`${assets}/references/${kind === 'ScheduledJob' ? 'scheduledjob' : 'workflowjob'}-crd.yaml`}
              target="_blank"
              rel="noreferrer"
            >
              {kind} CRD schema
            </a>
          </li>
          <li>
            <a href={`${assets}/references/usage.md`} target="_blank" rel="noreferrer">
              Console and workload usage
            </a>
          </li>
          <li>
            <a href={`${assets}/sources.json`} target="_blank" rel="noreferrer">
              Reference source hashes
            </a>
          </li>
        </ul>
      </details>
    </section>
  );
}
