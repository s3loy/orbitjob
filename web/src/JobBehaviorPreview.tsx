import { useId } from 'react';
import { jobBehavior, type JobBehaviorSection } from './job-behavior';
import './job-behavior.css';

export function JobBehaviorPreview({
  namespace,
  name,
  spec,
  context,
  onEdit,
}: {
  namespace: string;
  name: string;
  spec: string;
  context: 'create' | 'edit';
  onEdit?(section: JobBehaviorSection): void;
}) {
  const title = useId();
  const behavior = jobBehavior(spec, context);
  function edit(section: JobBehaviorSection, label: string) {
    return onEdit ? (
      <button type="button" className="job-behavior-edit" onClick={() => onEdit(section)}>
        {label}
      </button>
    ) : null;
  }
  return (
    <section className="job-behavior" aria-labelledby={title}>
      <header className="job-behavior-heading">
        <h2 id={title}>How this Job will run</h2>
        <span>Draft explanation</span>
      </header>
      {!behavior.readable ? (
        <div className="job-behavior-empty">
          <p>{behavior.issues[0].message}</p>
          {edit('general', 'Finish the Job configuration')}
        </div>
      ) : (
        <>
          <div className={`job-behavior-save${behavior.suspended ? ' is-suspended' : ''}`}>
            <p>{behavior.save}</p>
            <p className="job-behavior-secondary">{behavior.trigger}</p>
            {behavior.schedule && (
              <code className="job-behavior-schedule">{behavior.schedule}</code>
            )}
            {behavior.misfire && <p className="job-behavior-secondary">{behavior.misfire}</p>}
            {edit('schedule', 'Edit trigger')}
          </div>

          <div className="job-behavior-execution">
            {namespace.trim() && name.trim() ? (
              <p className="job-behavior-location">
                Runs of <code>{name}</code> use Kubernetes Job attempts in <code>{namespace}</code>.
              </p>
            ) : (
              <div className="job-behavior-empty">
                <p>Choose a name and namespace for this Job.</p>
                {edit('general', 'Choose where this runs')}
              </div>
            )}
            {behavior.image ? (
              <>
                <p className="job-behavior-image">
                  Run image <code>{behavior.image}</code>
                </p>
                <div className="job-behavior-invocation">
                  <div>
                    <h3>Command entries</h3>
                    {behavior.command.state === 'explicit' ? (
                      <Tokens values={behavior.command.values} label="Configured command entries" />
                    ) : (
                      <p className="job-behavior-secondary">
                        {behavior.command.state === 'invalid'
                          ? 'Use a text array for the command.'
                          : 'Uses the image entrypoint, which is unknown here.'}
                      </p>
                    )}
                  </div>
                  <div>
                    <h3>Argument entries</h3>
                    {behavior.args.state === 'explicit' ? (
                      <Tokens values={behavior.args.values} label="Configured argument entries" />
                    ) : (
                      <p className="job-behavior-secondary">
                        {behavior.args.state === 'invalid'
                          ? 'Use a text array for the arguments.'
                          : 'No argument override. The image may supply defaults, which are unknown here.'}
                      </p>
                    )}
                  </div>
                </div>
                <p className="job-behavior-literal">
                  Kubernetes and the image determine startup behavior from these configured entries.
                </p>
                {edit('container', 'Edit execution content')}
              </>
            ) : (
              <div className="job-behavior-empty">
                <p>Select an image to define the execution content.</p>
                {edit('container', 'Choose execution content')}
              </div>
            )}
          </div>

          <div className="job-behavior-attempt">
            <h3>For an accepted run</h3>
            <p>{behavior.deadline}</p>
            {behavior.deadlineDetail && (
              <p className="job-behavior-secondary">{behavior.deadlineDetail}</p>
            )}
            <p>{behavior.attempts}</p>
            {behavior.showBackoff && <p className="job-behavior-secondary">{behavior.backoff}</p>}
            {behavior.repeat && (
              <p
                className={
                  behavior.repeatWarning ? 'job-behavior-caution' : 'job-behavior-secondary'
                }
              >
                {behavior.repeat}
              </p>
            )}
            {edit('policies', 'Edit run policies')}
          </div>

          <details className="job-behavior-details">
            <summary>If a previous run is still active</summary>
            <div>
              {behavior.overlap.map((text) => (
                <p key={text}>{text}</p>
              ))}
              {edit('policies', 'Edit concurrency')}
            </div>
          </details>
          <details className="job-behavior-details">
            <summary>Resources for each attempt</summary>
            <div>
              {behavior.resources.length > 0 && (
                <ul className="job-behavior-resources">
                  {behavior.resources.map(({ name, group, value }) => (
                    <li key={`${group}/${name}`}>
                      <span>
                        {resourceName(name)} {group === 'requests' ? 'request' : 'limit'}
                      </span>
                      <code>{value}</code>
                    </li>
                  ))}
                </ul>
              )}
              <p className="job-behavior-secondary">{behavior.resourceNote}</p>
              {edit('resources', 'Edit resources')}
            </div>
          </details>

          {behavior.issues.length > 0 && (
            <div className="job-behavior-issues">
              <h3>Finish these settings</h3>
              <ul>
                {behavior.issues.map(({ section, message }, index) => (
                  <li key={index}>
                    {onEdit ? (
                      <button type="button" onClick={() => onEdit(section)}>
                        {message}
                      </button>
                    ) : (
                      message
                    )}
                  </li>
                ))}
              </ul>
            </div>
          )}
          <p className="job-behavior-output">
            A run can retain logs, exit status and timing when available.
          </p>
        </>
      )}
      <p className="job-behavior-boundary">
        This preview does not execute the Job or validate it against the cluster.
      </p>
    </section>
  );
}

function Tokens({ values, label }: { values: string[]; label: string }) {
  return (
    <ol className="job-behavior-tokens" aria-label={label}>
      {values.map((value, index) => (
        <li key={index}>
          <code>{JSON.stringify(value)}</code>
        </li>
      ))}
    </ol>
  );
}

function resourceName(name: string) {
  return resourceNames.get(name) ?? name;
}

const resourceNames = new Map([
  ['cpu', 'CPU'],
  ['memory', 'Memory'],
  ['ephemeral-storage', 'Temporary storage'],
]);
