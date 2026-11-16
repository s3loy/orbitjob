import { describe, expect, it } from 'vitest';
import {
  generateSchedule,
  MINUTE_INTERVALS,
  parseSchedule,
  scheduleDraftFromValue,
  type ScheduleDraft,
} from './schedule-generator';

function draft(update: Partial<ScheduleDraft> = {}): ScheduleDraft {
  return { ...scheduleDraftFromValue('').draft, ...update };
}

describe('schedule generation', () => {
  it('generates daily 02:00 without a time-zone field', () => {
    expect(generateSchedule(draft())).toEqual({ cron: '0 2 * * *', summary: 'Daily at 02:00' });
  });

  it.each(MINUTE_INTERVALS)('uses an evenly spaced minute interval of %i', (interval) => {
    const generated = generateSchedule(draft({ frequency: 'minutes', interval: String(interval) }));
    expect(generated).toMatchObject({
      cron: interval === 1 ? '* * * * *' : `*/${interval} * * * *`,
    });
    expect(60 % interval).toBe(0);
  });

  it.each(['', '0', '2', '7', '45', '60', '-5', '1.5', '5x', 'Infinity', '1e1'])(
    'rejects unsupported interval %j without clamping',
    (interval) => {
      expect(generateSchedule(draft({ frequency: 'minutes', interval }))).toHaveProperty('error');
    },
  );

  it.each([
    ['0', '0 * * * *'],
    ['59', '59 * * * *'],
  ])('accepts hourly minute boundary %s', (minute, cron) => {
    expect(generateSchedule(draft({ frequency: 'hourly', minute }))).toMatchObject({ cron });
  });

  it.each(['', '-1', '60', '2.5', '1e1', '3x'])('rejects invalid hourly minute %j', (minute) => {
    expect(generateSchedule(draft({ frequency: 'hourly', minute }))).toHaveProperty('error');
  });

  it.each([
    ['00:00', '0 0 * * *'],
    ['23:59', '59 23 * * *'],
  ])('accepts daily time boundary %s', (time, cron) => {
    expect(generateSchedule(draft({ time }))).toMatchObject({ cron });
  });

  it.each([
    '',
    '24:00',
    '02:60',
    '2:00',
    '02:0',
    '-1:00',
    '02:00:01',
    '2026-11-16',
    '2026-11-16T02:00',
  ])('rejects an invalid daily clock time %j', (time) => {
    expect(generateSchedule(draft({ time }))).toHaveProperty('error');
  });

  it('generates weekdays in cron order and includes their full names', () => {
    expect(
      generateSchedule(draft({ frequency: 'weekly', time: '23:59', weekdays: [6, 0, 2, 2] })),
    ).toEqual({ cron: '59 23 * * 0,2,6', summary: 'Every Sunday, Tuesday, Saturday at 23:59' });
  });

  it.each([[], [-1], [7], [1.5], [NaN]].map((weekdays) => ({ weekdays })))(
    'rejects invalid weekday selection $weekdays',
    ({ weekdays }) => {
      expect(generateSchedule(draft({ frequency: 'weekly', weekdays }))).toHaveProperty('error');
    },
  );
});

describe('schedule draft initialization', () => {
  it.each([
    '* * * * *',
    '*/5 * * * *',
    '*/30 * * * *',
    '0 * * * *',
    '59 * * * *',
    '0 2 * * *',
    '59 23 * * *',
    '0 0 * * 0',
    '30 9 * * 1,3,5,6',
  ])('round-trips a recognized cron without changing its meaning: %s', (cron) => {
    const parsed = parseSchedule(cron);
    expect(parsed).not.toBeNull();
    expect(generateSchedule(parsed!)).toMatchObject({ cron });
    expect(scheduleDraftFromValue(cron).replacingCustom).toBe(false);
  });

  it('opens the current daily 02:00 value as an editable daily draft', () => {
    expect(scheduleDraftFromValue('0 2 * * *').draft).toMatchObject({
      frequency: 'daily',
      time: '02:00',
    });
  });

  it('recognizes whitespace and zero-padded numbers as the same daily schedule', () => {
    const cron = '  00  02\t* * *  ';
    expect(parseSchedule(cron)).toMatchObject({ frequency: 'daily', time: '02:00' });
  });

  it.each([
    '*/45 * * * *',
    '0 2 1 * *',
    '0 2 * 1 *',
    '0 2 1 * 1',
    '0 2 * * 1-5',
    '0 2 * * MON',
    '0 2 * * 7',
    '0 24 * * *',
    '60 2 * * *',
    '0 2 * * 1,',
    '0 2 * * -1',
    '0 0 2 * * *',
    '@daily',
    'CRON_TZ=UTC 0 2 * * *',
    'invalid custom expression',
  ])('marks unsupported expression %j as custom and offers a replacement draft', (cron) => {
    expect(parseSchedule(cron)).toBeNull();
    const initialized = scheduleDraftFromValue(cron);
    expect(initialized.replacingCustom).toBe(true);
    expect(initialized.draft).toMatchObject({ frequency: 'daily', time: '02:00' });
    expect(generateSchedule(initialized.draft)).toMatchObject({ cron: '0 2 * * *' });
  });

  it('returns a fresh default draft for each opening', () => {
    const first = scheduleDraftFromValue('');
    first.draft.weekdays.push(6);
    first.draft.time = '23:59';
    expect(scheduleDraftFromValue('')).toMatchObject({
      replacingCustom: false,
      draft: { time: '02:00', weekdays: [1] },
    });
  });
});
