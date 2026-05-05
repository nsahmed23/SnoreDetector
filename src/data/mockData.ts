import type { SleepDatum, SnoreEvent, TrendDatum } from '../types';

export const DAILY_SLEEP_DATA: SleepDatum[] = [
  { time: '11:00 PM', stage: 0 },
  { time: '11:30 PM', stage: 1 },
  { time: '12:00 AM', stage: 2 },
  { time: '12:30 AM', stage: 2 },
  { time: '1:00 AM', stage: 1 },
  { time: '1:30 AM', stage: 3 },
  { time: '2:00 AM', stage: 2 },
  { time: '2:30 AM', stage: 2 },
  { time: '3:00 AM', stage: 1 },
  { time: '3:30 AM', stage: 3 },
  { time: '4:00 AM', stage: 1 },
  { time: '4:30 AM', stage: 2 },
  { time: '5:00 AM', stage: 1 },
  { time: '5:30 AM', stage: 3 },
  { time: '6:00 AM', stage: 1 },
  { time: '6:30 AM', stage: 0 },
];

export const DAILY_SNORE_EVENTS: SnoreEvent[] = [
  { time: '12:15 AM', intensity: 75, duration: 45, stage: 'Deep' },
  { time: '1:45 AM', intensity: 65, duration: 30, stage: 'REM' },
  { time: '4:10 AM', intensity: 82, duration: 120, stage: 'Light' },
  { time: '4:20 AM', intensity: 61, duration: 20, stage: 'Light' },
];

export const WEEKLY_DATA: TrendDatum[] = [
  { name: 'Mon', freq: 14, intensity: 68, durationMin: 12 },
  { name: 'Tue', freq: 8, intensity: 62, durationMin: 5 },
  { name: 'Wed', freq: 22, intensity: 75, durationMin: 28 },
  { name: 'Thu', freq: 4, intensity: 58, durationMin: 2 },
  { name: 'Fri', freq: 19, intensity: 71, durationMin: 21 },
  { name: 'Sat', freq: 25, intensity: 78, durationMin: 35 },
  { name: 'Sun', freq: 11, intensity: 65, durationMin: 9 },
];

export const MONTHLY_DATA: TrendDatum[] = [
  { name: 'Week 1', freq: 85, intensity: 65, durationMin: 120 },
  { name: 'Week 2', freq: 112, intensity: 71, durationMin: 165 },
  { name: 'Week 3', freq: 64, intensity: 62, durationMin: 85 },
  { name: 'Week 4', freq: 42, intensity: 59, durationMin: 50 },
];
