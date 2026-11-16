import { Text, TextField } from '@radix-ui/themes';
import { useId } from 'react';
import { Link } from 'react-router-dom';
import { CircleHelp, Network } from 'lucide-react';
import { useSession } from './session';
import { namespaceCatalogKnown } from './workspace';

export function NamespaceField({
  value,
  onChange,
  disabled = false,
  error,
}: {
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
  error?: string;
}) {
  const { identity } = useSession();
  const id = useId();
  const namespaces = [...new Set(identity?.workload_namespaces ?? [])];
  const known = namespaceCatalogKnown(identity);
  const missing = known && !namespaces.length;
  const outsideMapping = known && !!value && !!namespaces.length && !namespaces.includes(value);
  const hasGuidance = missing || outsideMapping || !known;
  const descriptionID =
    [error && `${id}-error`, hasGuidance && `${id}-help`].filter(Boolean).join(' ') || undefined;
  return (
    <div
      className={`manifest-field namespace-field${missing || outsideMapping ? ' namespace-field-warning' : ''}`}
    >
      <label htmlFor={id}>
        <Text size="2" weight="medium">
          Execution namespace
        </Text>
      </label>
      {namespaces.length ? (
        <select
          id={id}
          aria-label="Namespace"
          value={value}
          onChange={(event) => onChange(event.target.value)}
          disabled={disabled}
          aria-describedby={descriptionID}
          aria-invalid={Boolean(error) || outsideMapping || undefined}
        >
          <option value="">Choose an execution namespace</option>
          {value && !namespaces.includes(value) && (
            <option value={value}>{value} (outside workspace)</option>
          )}
          {namespaces.map((namespace) => (
            <option key={namespace} value={namespace}>
              {namespace}
            </option>
          ))}
        </select>
      ) : (
        <TextField.Root
          id={id}
          aria-label="Namespace"
          value={value}
          disabled={disabled || missing}
          placeholder={
            missing
              ? 'No execution namespace connected'
              : 'Namespace supplied by your administrator'
          }
          onChange={(event) => onChange(event.target.value)}
          aria-describedby={descriptionID}
          aria-invalid={Boolean(error) || undefined}
        />
      )}
      {error && (
        <Text id={`${id}-error`} size="1" color="red">
          {error}
        </Text>
      )}
      {hasGuidance && (
        <div id={`${id}-help`} className="namespace-guidance">
          {missing ? (
            <>
              <Network size={15} />
              <span>
                <strong>No execution namespace is connected</strong>Ask your administrator to
                connect a Kubernetes namespace to this workspace, then reconnect.{' '}
                <Link to="/connection">Review connection</Link>
              </span>
            </>
          ) : outsideMapping ? (
            <>
              <Network size={15} />
              <span>
                <strong>Choose a namespace in this workspace</strong>Your draft still contains{' '}
                {value}. Select one of the available namespaces to continue.
              </span>
            </>
          ) : (
            <>
              <CircleHelp size={15} />
              <span>
                The workspace has not provided its namespace list. Enter the namespace assigned by
                your administrator.
              </span>
            </>
          )}
        </div>
      )}
    </div>
  );
}
