import { useEffect, useRef, useState } from 'react';
import { Button, Flex, Heading, Text, TextArea, TextField } from '@radix-ui/themes';
import { ArrowLeft, Check, Copy, KeyRound, Terminal } from 'lucide-react';

type Path = 'request' | 'install' | null;
export function ConnectionHelp() {
  const [path, setPath] = useState<Path>(null);
  const [namespace, setNamespace] = useState('');
  const [purpose, setPurpose] = useState('Run and manage workloads');
  const [copiedValue, setCopied] = useState('');
  const [failedValue, setCopyError] = useState('');
  const instructions = useRef<HTMLHeadingElement>(null);
  const requestButton = useRef<HTMLButtonElement>(null);
  const installButton = useRef<HTMLButtonElement>(null);
  const returnPath = useRef<Path>(null);
  useEffect(() => {
    if (path) {
      instructions.current?.focus();
    } else if (returnPath.current) {
      const button = returnPath.current === 'request' ? requestButton : installButton;
      button.current?.focus();
      returnPath.current = null;
    }
  }, [path]);
  const validNamespace = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(namespace);
  const command = `kubectl -n ${namespace} get secret bootstrap-api-key -o jsonpath='{.data.api-key}' | base64 --decode`;
  const request = `Please issue a dedicated OrbitJob tenant API key for me.\nInstallation: ${window.location.origin}\nRequired access: ${purpose}\nPlease confirm the tenant workspace and include the permissions needed for these tasks. Send the key through our approved secret-sharing channel.`;
  const activeValue = path === 'request' ? request : path === 'install' ? command : '';
  const copied = Boolean(activeValue && copiedValue === activeValue);
  const copyError = Boolean(activeValue && failedValue === activeValue);
  async function copy(value: string) {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(value);
      setCopyError('');
    } catch {
      setCopyError(value);
    }
  }
  function choose(value: Path) {
    if (value === null) returnPath.current = path;
    setPath(value);
    setCopied('');
    setCopyError('');
  }
  return (
    <aside className="connection-access-help" aria-label="Get an API key">
      {path ? (
        <>
          <Button type="button" variant="ghost" size="1" onClick={() => choose(null)}>
            <ArrowLeft size={13} />
            Other ways to get access
          </Button>
          <Heading as="h2" ref={instructions} tabIndex={-1} size="3" weight="medium" mt="4">
            {path === 'request'
              ? 'Ask your administrator for access'
              : 'Use your installation’s initial key'}
          </Heading>
          {path === 'request' ? (
            <>
              <Text as="p" size="2" color="gray" mt="3">
                Ask your administrator to issue a tenant API key for the work you need to do.
              </Text>
              <label className="form-field">
                Required access
                <select
                  aria-label="Required access"
                  value={purpose}
                  onChange={(event) => {
                    setPurpose(event.target.value);
                    setCopied('');
                  }}
                >
                  <option>Run and manage workloads</option>
                  <option>View executions and diagnostic logs</option>
                  <option>Configure checks and service objectives</option>
                </select>
              </label>
              <TextArea aria-label="Access request" value={request} readOnly rows={7} />
              <Button
                type="button"
                variant="outline"
                size="2"
                mt="3"
                onClick={() => void copy(request)}
              >
                {copied ? <Check size={14} /> : <Copy size={14} />}
                {copied ? 'Request copied' : 'Copy access request'}
              </Button>
              <Text as="p" size="1" color="gray" mt="3">
                A key is shown only when it is issued. If you lost an existing key, ask for a
                replacement.
              </Text>
            </>
          ) : (
            <>
              <Text as="p" size="2" color="gray" mt="3">
                For a Helm installation you administer, read the initial key from its Kubernetes
                Secret, then paste it into the API key field.
              </Text>
              <ol className="access-instructions">
                <li>
                  <Text as="p" size="2" weight="medium">
                    Choose the installation namespace
                  </Text>
                  <TextField.Root
                    aria-label="Installation namespace"
                    placeholder="e.g. orbitjob-system"
                    value={namespace}
                    maxLength={63}
                    onChange={(event) => {
                      setNamespace(event.target.value);
                      setCopied('');
                    }}
                    mt="2"
                  />
                  <Text as="p" size="1" color="gray" mt="2">
                    Use the namespace passed to Helm. Find it in the NAMESPACE column of{' '}
                    <code>helm list --all-namespaces</code>.
                  </Text>
                </li>
                <li>
                  <Text as="p" size="2" weight="medium">
                    Run this command in your terminal
                  </Text>
                  {validNamespace ? (
                    <pre className="access-command" tabIndex={0}>
                      {command}
                    </pre>
                  ) : (
                    <Text as="p" size="1" color="gray" mt="2">
                      Enter a valid namespace above to generate the command.
                    </Text>
                  )}
                  <Button
                    type="button"
                    variant="outline"
                    size="1"
                    mt="2"
                    disabled={!validNamespace}
                    onClick={() => void copy(command)}
                  >
                    {copied ? <Check size={13} /> : <Copy size={13} />}
                    {copied ? 'Command copied' : 'Copy command'}
                  </Button>
                </li>
                <li>
                  <Text as="p" size="2" weight="medium">
                    Connect and create a key for daily work
                  </Text>
                  <Text as="p" size="1" color="gray" mt="2">
                    Issue a dedicated key in API keys under Administration. Keep the initial key for
                    setup and access management.
                  </Text>
                </li>
              </ol>
              <details className="access-troubleshooting">
                <summary>Secret not found or access denied</summary>
                <Text as="p" size="1" color="gray" mt="2">
                  Check your Kubernetes context and installation namespace. Your Kubernetes account
                  needs permission to read this Secret. If the original key is no longer available,
                  contact the installation administrator; its plaintext cannot be recovered from the
                  database.
                </Text>
              </details>
            </>
          )}
          {copyError && (
            <Text as="p" size="1" color="red" role="status" mt="2">
              Clipboard access is unavailable. Select and copy the text above.
            </Text>
          )}
        </>
      ) : (
        <>
          <Heading as="h2" size="3" weight="medium">
            Need a key?
          </Heading>
          <Flex direction="column" gap="3" mt="4">
            <button
              ref={requestButton}
              className="access-path"
              type="button"
              onClick={() => choose('request')}
            >
              <KeyRound size={18} />
              <strong>Request access from my administrator</strong>
            </button>
            <button
              ref={installButton}
              className="access-path"
              type="button"
              onClick={() => choose('install')}
            >
              <Terminal size={18} />
              <strong>I installed OrbitJob</strong>
            </button>
          </Flex>
        </>
      )}
    </aside>
  );
}
