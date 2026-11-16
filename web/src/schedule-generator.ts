export const MINUTE_INTERVALS = [1, 5, 10, 15, 20, 30] as const;

export const WEEKDAYS = [
  'Sunday',
  'Monday',
  'Tuesday',
  'Wednesday',
  'Thursday',
  'Friday',
  'Saturday',
] as const;

export type ScheduleFrequency = 'minutes' | 'hourly' | 'daily' | 'weekly';

export type ScheduleDraft = {
  frequency: ScheduleFrequency;
  interval: string;
  minute: string;
  time: string;
  weekdays: number[];
};

export type GeneratedSchedule = { cron: string; summary: string } | { error: string };

function defaultDraft(): ScheduleDraft {
  return { frequency: 'daily', interval: '5', minute: '0', time: '02:00', weekdays: [1] };
}

function integer(value: string, min: number, max: number): number | null {
  if (!/^\d+$/.test(value)) return null;
  const result = Number(value);
  return Number.isSafeInteger(result) && result >= min && result <= max ? result : null;
}

function clockTime(value: string): { hour: number; minute: number } | null {
  if (!/^\d{2}:\d{2}$/.test(value)) return null;
  const [hour, minute] = value.split(':').map(Number);
  return hour <= 23 && minute <= 59 ? { hour, minute } : null;
}

export function generateSchedule(draft: ScheduleDraft): GeneratedSchedule {
  if (draft.frequency === 'minutes') {
    const interval = integer(draft.interval, 1, 30);
    if (interval === null || !MINUTE_INTERVALS.some((allowed) => allowed === interval)) {
      return { error: 'Choose a minute interval of 1, 5, 10, 15, 20, or 30.' };
    }
    return {
      cron: interval === 1 ? '* * * * *' : `*/${interval} * * * *`,
      summary: interval === 1 ? 'Every minute' : `Every ${interval} minutes`,
    };
  }
  if (draft.frequency === 'hourly') {
    const minute = integer(draft.minute, 0, 59);
    if (minute === null) return { error: 'Enter a minute from 0 to 59.' };
    return {
      cron: `${minute} * * * *`,
      summary: `Every hour at minute ${String(minute).padStart(2, '0')}`,
    };
  }
  if (draft.frequency !== 'daily' && draft.frequency !== 'weekly') {
    return { error: 'Choose a frequency.' };
  }
  const time = clockTime(draft.time);
  if (!time) return { error: 'Enter a valid time from 00:00 to 23:59.' };
  if (draft.frequency === 'daily') {
    return { cron: `${time.minute} ${time.hour} * * *`, summary: `Daily at ${draft.time}` };
  }
  if (
    draft.weekdays.length === 0 ||
    draft.weekdays.some((day) => !Number.isInteger(day) || day < 0 || day > 6)
  ) {
    return { error: 'Select at least one day from Sunday to Saturday.' };
  }
  const weekdays = [...new Set(draft.weekdays)].sort((left, right) => left - right);
  return {
    cron: `${time.minute} ${time.hour} * * ${weekdays.join(',')}`,
    summary: `Every ${weekdays.map((day) => WEEKDAYS[day]).join(', ')} at ${draft.time}`,
  };
}

// Recognize only the recurring shapes this generator can represent exactly.
export function parseSchedule(value: string): ScheduleDraft | null {
  const fields = value.trim().split(/\s+/);
  if (fields.length !== 5) return null;
  const [minuteField, hourField, dayOfMonth, month, dayOfWeek] = fields;
  if (dayOfMonth !== '*' || month !== '*') return null;
  const base = defaultDraft();
  if (hourField === '*' && dayOfWeek === '*') {
    if (minuteField === '*') return { ...base, frequency: 'minutes', interval: '1' };
    if (minuteField.startsWith('*/')) {
      const interval = integer(minuteField.slice(2), 1, 30);
      return interval !== null && MINUTE_INTERVALS.some((allowed) => allowed === interval)
        ? { ...base, frequency: 'minutes', interval: String(interval) }
        : null;
    }
    const minute = integer(minuteField, 0, 59);
    return minute === null ? null : { ...base, frequency: 'hourly', minute: String(minute) };
  }
  const minute = integer(minuteField, 0, 59);
  const hour = integer(hourField, 0, 23);
  if (minute === null || hour === null) return null;
  const time = `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`;
  if (dayOfWeek === '*') return { ...base, frequency: 'daily', time };
  const weekdays = dayOfWeek.split(',').map((day) => integer(day, 0, 6));
  if (weekdays.some((day) => day === null)) return null;
  return {
    ...base,
    frequency: 'weekly',
    time,
    weekdays: [...new Set(weekdays as number[])].sort((left, right) => left - right),
  };
}

export function scheduleDraftFromValue(value: string): {
  draft: ScheduleDraft;
  replacingCustom: boolean;
} {
  const parsed = parseSchedule(value);
  return { draft: parsed ?? defaultDraft(), replacingCustom: !parsed && value.trim() !== '' };
}
