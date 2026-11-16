import { useId, useState } from 'react';
import { Button, Checkbox, Dialog, Select, TextField } from '@radix-ui/themes';
import { CalendarClock } from 'lucide-react';
import {
  generateSchedule,
  MINUTE_INTERVALS,
  scheduleDraftFromValue,
  WEEKDAYS,
  type ScheduleDraft,
  type ScheduleFrequency,
} from './schedule-generator';
import './schedule-generator.css';

export type ScheduleGeneratorProps = {
  value: string;
  timeZone?: string;
  disabled?: boolean;
  onChange: (value: string) => void;
};

export function ScheduleGenerator({
  value,
  timeZone = 'UTC',
  disabled = false,
  onChange,
}: ScheduleGeneratorProps) {
  const id = useId();
  const [open, setOpen] = useState(false);
  const [initial, setInitial] = useState(() => scheduleDraftFromValue(value));
  const [draft, setDraft] = useState<ScheduleDraft>(initial.draft);
  const generated = generateSchedule(draft);
  const error = 'error' in generated ? generated.error : undefined;
  const zone = timeZone || 'UTC';
  function update(next: Partial<ScheduleDraft>) {
    setDraft((current) => ({ ...current, ...next }));
  }
  function changeOpen(next: boolean) {
    if (next) {
      const current = scheduleDraftFromValue(value);
      setInitial(current);
      setDraft(current.draft);
    }
    setOpen(next);
  }
  return (
    <Dialog.Root open={open} onOpenChange={changeOpen}>
      <Dialog.Trigger>
        <Button type="button" size="1" variant="soft" disabled={disabled}>
          <CalendarClock size={14} aria-hidden="true" />
          Generate schedule
        </Button>
      </Dialog.Trigger>
      <Dialog.Content maxWidth="440px" className="schedule-generator-dialog">
        <Dialog.Title>Generate a schedule</Dialog.Title>
        <Dialog.Description size="2" className="schedule-generator-description">
          Choose a frequency and review the cron expression. Time zone: {zone}.
        </Dialog.Description>
        {initial.replacingCustom && (
          <p className="schedule-generator-custom">
            Your current expression uses a custom pattern. These controls start at daily 02:00 and
            replace it only when you choose Use this schedule.
          </p>
        )}
        <div className="schedule-generator-controls">
          <label className="schedule-generator-field">
            <span id={`${id}-frequency-label`}>Frequency</span>
            <Select.Root
              value={draft.frequency}
              disabled={disabled}
              onValueChange={(frequency) => update({ frequency: frequency as ScheduleFrequency })}
            >
              <Select.Trigger aria-labelledby={`${id}-frequency-label`} />
              <Select.Content position="popper">
                <Select.Item value="minutes">Every few minutes</Select.Item>
                <Select.Item value="hourly">Hourly</Select.Item>
                <Select.Item value="daily">Daily</Select.Item>
                <Select.Item value="weekly">Weekly</Select.Item>
              </Select.Content>
            </Select.Root>
          </label>
          {draft.frequency === 'minutes' && (
            <label className="schedule-generator-field">
              <span id={`${id}-interval-label`}>Minute interval</span>
              <Select.Root
                value={draft.interval}
                disabled={disabled}
                onValueChange={(interval) => update({ interval })}
              >
                <Select.Trigger aria-labelledby={`${id}-interval-label`} />
                <Select.Content position="popper">
                  {MINUTE_INTERVALS.map((interval) => (
                    <Select.Item key={interval} value={String(interval)}>
                      {interval === 1 ? 'Every minute' : `Every ${interval} minutes`}
                    </Select.Item>
                  ))}
                </Select.Content>
              </Select.Root>
            </label>
          )}
          {draft.frequency === 'hourly' && (
            <label className="schedule-generator-field" htmlFor={`${id}-minute`}>
              Minute of hour
              <TextField.Root
                id={`${id}-minute`}
                type="number"
                min="0"
                max="59"
                step="1"
                value={draft.minute}
                disabled={disabled}
                aria-invalid={!!error}
                aria-describedby={error ? `${id}-error` : undefined}
                onChange={(event) => update({ minute: event.target.value })}
              />
            </label>
          )}
          {(draft.frequency === 'daily' || draft.frequency === 'weekly') && (
            <label className="schedule-generator-field" htmlFor={`${id}-time`}>
              Time
              <input
                className="schedule-generator-time"
                id={`${id}-time`}
                type="time"
                value={draft.time}
                disabled={disabled}
                aria-invalid={!!error && !draft.time}
                aria-describedby={error ? `${id}-error` : undefined}
                onChange={(event) => update({ time: event.target.value })}
              />
            </label>
          )}
          {draft.frequency === 'weekly' && (
            <fieldset className="schedule-generator-weekdays">
              <legend>Days of the week</legend>
              <div>
                {WEEKDAYS.map((day, index) => (
                  <label key={day}>
                    <Checkbox
                      checked={draft.weekdays.includes(index)}
                      disabled={disabled}
                      onCheckedChange={(checked) =>
                        update({
                          weekdays: checked
                            ? [...draft.weekdays, index]
                            : draft.weekdays.filter((current) => current !== index),
                        })
                      }
                    />
                    {day}
                  </label>
                ))}
              </div>
            </fieldset>
          )}
        </div>
        <div className="schedule-generator-result" aria-live="polite" aria-atomic="true">
          {'error' in generated ? (
            <p id={`${id}-error`} role="alert" className="schedule-generator-error">
              {generated.error}
            </p>
          ) : (
            <>
              <p>
                {generated.summary} ({zone})
              </p>
              <code aria-label="Generated cron expression">{generated.cron}</code>
            </>
          )}
        </div>
        <div className="schedule-generator-actions">
          <Dialog.Close>
            <Button type="button" variant="soft" color="gray">
              Cancel
            </Button>
          </Dialog.Close>
          <Button
            type="button"
            disabled={disabled || !!error}
            onClick={() => {
              if (disabled || 'error' in generated) return;
              onChange(generated.cron);
              setOpen(false);
            }}
          >
            Use this schedule
          </Button>
        </div>
      </Dialog.Content>
    </Dialog.Root>
  );
}
