import './job-run-workspace.css';
import { AttemptLogs } from './AttemptLogs';
import { when } from './format';
import { Box, Button, Flex, Heading, Text, Tooltip } from '@radix-ui/themes';
import { useState } from 'react';
import type { RunDetail } from './api/contracts';
import { PhaseBadge } from './components';
import { attemptTiming, duration, timelineModel } from './timeline';
const clock = (value: number) => new Date(value).toLocaleTimeString(undefined, { hour12: false });

export function AttemptTimeline({ run }: { run: RunDetail }) {
  const model = timelineModel(run);
  const [chosen, setChosen] = useState<number | null>(null);
  const selected =
    model.attempts.find((attempt) => attempt.attempt_number === chosen) ?? model.attempts.at(-1);
  if (!selected)
    return (
      <Box id="attempt-diagnostics" className="empty-state" tabIndex={-1}>
        <Heading as="h2" size="3" weight="medium">
          No attempt recorded yet
        </Heading>
        <Text as="p" size="2" color="gray" mt="2">
          Refresh to check whether this run has started an attempt.
        </Text>
      </Box>
    );
  const timing = attemptTiming(selected, run.updated_at);
  return (
    <>
      <section
        id="attempt-diagnostics"
        tabIndex={-1}
        className="attempt-evidence run-attempt-evidence"
        aria-label="Selected attempt evidence"
      >
        <Flex justify="between" align="center" gap="3">
          <Heading as="h2" size="3" weight="medium">
            Attempt {selected.attempt_number}
          </Heading>
          <PhaseBadge phase={selected.phase} />
        </Flex>
        {model.attempts.length > 1 && (
          <label className="run-attempt-picker">
            <span>Inspect attempt</span>
            <select
              value={selected.attempt_number}
              onChange={(event) => setChosen(Number(event.target.value))}
            >
              {model.attempts.map((attempt) => (
                <option key={String(attempt.id)} value={attempt.attempt_number}>
                  Attempt {attempt.attempt_number} ({attempt.phase})
                </option>
              ))}
            </select>
          </label>
        )}
        {selected.execution_unknown_since && (
          <Text as="p" size="2" color="gray" mt="2" role="status">
            Execution uncertain since {when(selected.execution_unknown_since)}.
            {selected.execution_unknown_reason
              ? ` Reason: ${selected.execution_unknown_reason}.`
              : ''}{' '}
            Missing workload evidence does not confirm termination.
          </Text>
        )}
        <div className="attempt-facts">
          <div>
            <label>Started</label>
            {when(selected.started_at)}
          </div>
          <div>
            <label>Completed</label>
            {when(selected.completed_at)}
          </div>
          <div>
            <label>{timing?.active ? 'Elapsed at observation' : 'Duration'}</label>
            {timing ? duration(timing.duration) : 'Not recorded'}
          </div>
        </div>
        <details>
          <summary>Resource identity</summary>
          <dl className="definition-list">
            <dt>Kubernetes Job</dt>
            <dd className="mono">{selected.kubernetes_job_name || 'Not recorded'}</dd>
            <dt>Job UID</dt>
            <dd className="mono">{selected.kubernetes_job_uid || 'Not recorded'}</dd>
            <dt>Resource version</dt>
            <dd className="mono">{selected.observed_resource_version || 'Not recorded'}</dd>
            <dt>Original Run UID</dt>
            <dd className="mono">{selected.owner_run_uid || 'Not recorded'}</dd>
          </dl>
        </details>
      </section>
      <AttemptLogs
        key={String(selected.id)}
        parent={{ kind: 'instance', run: String(run.id) }}
        attempt={selected.attempt_number}
      />
      {model.attempts.length > 1 && (
        <div className="run-attempt-timing">
          <section aria-label="Attempt timeline">
            <Flex justify="between" align="center" wrap="wrap" gap="3">
              <Heading as="h2" size="3" weight="medium">
                Attempts
              </Heading>
              {model.start !== null && model.end !== null && (
                <Text size="1" color="gray">
                  Recorded span: {duration(model.end - model.start)}
                </Text>
              )}
            </Flex>
            <div className="attempt-timeline">
              {model.attempts.map((attempt) => {
                const span = model.spans.find((span) => span.attempt === attempt);
                return (
                  <div className="timeline-lane" key={String(attempt.id)}>
                    <Button
                      className="lane-label"
                      variant="ghost"
                      color="gray"
                      aria-pressed={selected.attempt_number === attempt.attempt_number}
                      onClick={() => setChosen(attempt.attempt_number)}
                    >
                      <span>
                        Attempt {attempt.attempt_number}
                        <span className="lane-phase">{attempt.phase}</span>
                      </span>
                    </Button>
                    {span ? (
                      <div className="timeline-track">
                        <Tooltip
                          content={`${when(attempt.started_at)} to ${when(attempt.completed_at)}${span.active ? ', active at last observation' : ''}`}
                        >
                          <Button
                            className={`timeline-span ${span.active ? 'ongoing' : ''} ${attempt.phase === 'Failed' ? 'is-failed' : ''}`}
                            size="1"
                            color={
                              attempt.phase === 'Failed'
                                ? 'red'
                                : attempt.phase === 'Succeeded'
                                  ? 'green'
                                  : 'gray'
                            }
                            variant={
                              selected.attempt_number === attempt.attempt_number ? 'solid' : 'soft'
                            }
                            style={{ left: `${span.left}%`, width: `${span.width}%` }}
                            aria-label={`Attempt ${attempt.attempt_number}, ${attempt.phase}, ${duration(span.duration)}${span.active ? ' observed' : ''}`}
                            aria-pressed={selected.attempt_number === attempt.attempt_number}
                            onClick={() => setChosen(attempt.attempt_number)}
                          >
                            {span.width > 9 ? (
                              duration(span.duration)
                            ) : (
                              <span className="sr-only">{duration(span.duration)}</span>
                            )}
                          </Button>
                        </Tooltip>
                      </div>
                    ) : (
                      <Text size="1" color="gray">
                        Timing not recorded
                      </Text>
                    )}
                  </div>
                );
              })}
              {model.start !== null && model.end !== null && (
                <div className="timeline-axis">
                  <span>{clock(model.start)}</span>
                  <span>{clock(model.start + (model.end - model.start) / 2)}</span>
                  <span>{clock(model.end)}</span>
                </div>
              )}
              {model.gaps.map((gap) => (
                <Text as="p" size="1" color="gray" key={gap.to} className="timeline-gap">
                  {duration(Math.abs(gap.milliseconds))} {gap.milliseconds < 0 ? 'overlap' : 'gap'}{' '}
                  between attempts {gap.from} and {gap.to}.
                </Text>
              ))}
            </div>
          </section>
        </div>
      )}
    </>
  );
}
