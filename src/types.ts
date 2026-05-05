export type Sensitivity = 'low' | 'medium' | 'high';

export type Tab = 'record' | 'insights' | 'settings';

export type SleepStageName = 'Awake' | 'Light' | 'Deep' | 'REM';

export interface SleepDatum {
  time: string;
  stage: number;
  realtimeStage?: number;
}

export interface SnoreEvent {
  time: string;
  intensity: number;
  duration: number;
  stage: 'Light' | 'Deep' | 'REM';
}

export interface TrendDatum {
  name: string;
  freq: number;
  intensity: number;
  durationMin: number;
}

export type ExportRange = '7days' | '30days' | 'custom';
