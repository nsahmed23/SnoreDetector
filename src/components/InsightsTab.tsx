import { useState, useMemo } from 'react';
import {
  BarChart2,
  Activity,
  Moon,
  Clock,
  HeartPulse,
  Volume2,
  History,
  Download,
  Play,
  Square,
} from 'lucide-react';
import { cn } from '../lib/cn';
import { DAILY_SLEEP_DATA, DAILY_SNORE_EVENTS } from '../data/mockData';
import { DailyChart } from './charts/DailyChart';
import { TrendChart } from './charts/TrendChart';
import { ExportModal } from './ExportModal';
import type { ExportRange, SleepDatum } from '../types';

export function InsightsTab({
  healthKitEnabled,
  isTracking,
  liveSleepData,
}: {
  healthKitEnabled: boolean;
  isTracking: boolean;
  liveSleepData: SleepDatum[];
}) {
  const [timeRange, setTimeRange] = useState<'daily' | 'weekly' | 'monthly'>('daily');
  const [playingId, setPlayingId] = useState<number | null>(null);
  const [showExportModal, setShowExportModal] = useState(false);
  const [exportRange, setExportRange] = useState<ExportRange>('7days');

  const combinedData = useMemo<SleepDatum[]>(() => {
    const data: SleepDatum[] = [...DAILY_SLEEP_DATA];
    if (isTracking && liveSleepData.length > 0) {
      data[data.length - 1] = {
        ...data[data.length - 1],
        realtimeStage: data[data.length - 1].stage,
      };
      return [...data, ...liveSleepData.slice(1)];
    }
    return data;
  }, [isTracking, liveSleepData]);

  const togglePlay = (idx: number) => {
    if (playingId === idx) {
      setPlayingId(null);
    } else {
      setPlayingId(idx);
      setTimeout(() => {
        setPlayingId(current => (current === idx ? null : current));
      }, 3000);
    }
  };

  return (
    <div className="flex-1 p-6 overflow-y-auto pb-24 w-full relative z-10">
      {showExportModal && (
        <ExportModal
          exportRange={exportRange}
          setExportRange={setExportRange}
          onClose={() => setShowExportModal(false)}
        />
      )}

      <div className="flex items-center justify-between mb-6">
        <h2 className="text-2xl font-semibold flex items-center gap-2 text-indigo-50">
          <BarChart2 className="text-indigo-400" /> Sleep Analytics
        </h2>
        <button
          onClick={() => setShowExportModal(true)}
          className="text-indigo-400 flex items-center gap-1.5 text-xs font-semibold bg-indigo-500/10 hover:bg-indigo-500/20 px-3 py-1.5 rounded-full transition-colors"
        >
          <Download className="w-3.5 h-3.5" /> Export Data
        </button>
      </div>

      <div className="flex bg-slate-900/80 p-1 rounded-xl mb-6 shadow-inner ring-1 ring-white/10">
        {(['daily', 'weekly', 'monthly'] as const).map(rng => (
          <button
            key={rng}
            onClick={() => setTimeRange(rng)}
            className={cn(
              'flex-1 py-1.5 text-xs font-semibold rounded-lg capitalize transition-all',
              timeRange === rng
                ? 'bg-indigo-600 text-white shadow-md shadow-indigo-900/50'
                : 'text-slate-400 hover:text-slate-200',
            )}
          >
            {rng}
          </button>
        ))}
      </div>

      {timeRange === 'daily' && (
        <div className="grid grid-cols-2 gap-3 mb-6">
          <div className="bg-indigo-900/20 border border-indigo-500/20 rounded-2xl p-4">
            <div className="flex items-center gap-1.5 mb-2">
              <Moon className="w-4 h-4 text-indigo-400" />
              <h3 className="text-indigo-200 text-xs font-medium">Total Sleep</h3>
            </div>
            <div className="text-2xl font-light text-white">7h 24m</div>
          </div>
          <div className="bg-purple-900/20 border border-purple-500/20 rounded-2xl p-4">
            <div className="flex items-center gap-1.5 mb-2">
              <Activity className="w-4 h-4 text-purple-400" />
              <h3 className="text-purple-200 text-xs font-medium">Primary Stage</h3>
            </div>
            <div className="text-2xl font-light text-white">Light</div>
          </div>
          <div className="bg-rose-900/20 border border-rose-500/20 rounded-2xl p-4">
            <div className="flex items-center gap-1.5 mb-2">
              <Clock className="w-4 h-4 text-rose-400" />
              <h3 className="text-rose-200 text-xs font-medium">Snoring Time</h3>
            </div>
            <div className="text-2xl font-light text-white">3m 35s</div>
          </div>
          <div className="bg-orange-900/20 border border-orange-500/20 rounded-2xl p-4">
            <div className="flex items-center gap-1.5 mb-2">
              <Volume2 className="w-4 h-4 text-orange-400" />
              <h3 className="text-orange-200 text-xs font-medium">Avg Intensity</h3>
            </div>
            <div className="text-2xl font-light text-white">
              70.7 <span className="text-sm font-normal text-orange-200/60">dB</span>
            </div>
          </div>
        </div>
      )}

      <div className="bg-white/5 backdrop-blur-lg border border-white/10 rounded-3xl p-5 mb-6">
        {timeRange !== 'daily' && (
          <div className="mb-2 flex justify-between items-end">
            <div>
              <h3 className="text-indigo-200 text-sm mb-1">Avg Sleep</h3>
              <div className="text-3xl font-light text-white">
                {timeRange === 'weekly' ? '6h 50m' : '7h 05m'}
              </div>
            </div>
            <div className="text-right">
              <h3 className="text-indigo-200 text-sm mb-1">Total Snores</h3>
              <div className="text-3xl font-light text-rose-400">
                {timeRange === 'weekly' ? '83' : '303'}
              </div>
            </div>
          </div>
        )}

        {timeRange === 'daily' ? (
          <DailyChart combinedData={combinedData} isTracking={isTracking} />
        ) : (
          <TrendChart timeRange={timeRange} />
        )}
      </div>

      {healthKitEnabled && (
        <div className="bg-gradient-to-br from-emerald-950/40 to-slate-900/50 border border-emerald-500/20 rounded-2xl p-4 mb-6">
          <div className="flex items-center gap-2 mb-3 text-emerald-400">
            <HeartPulse className="w-5 h-5" />
            <h3 className="font-medium text-sm">HealthKit Daily Sync</h3>
          </div>
          <div className="space-y-2 text-xs text-slate-300">
            <div className="flex justify-between border-b border-emerald-500/10 pb-1">
              <span className="flex items-center gap-1.5">
                <History className="w-3.5 h-3.5 text-emerald-500" /> Sleep Stages Read
              </span>
              <span className="text-white font-medium">Synced at 7:00 AM</span>
            </div>
            <div className="flex justify-between border-b border-emerald-500/10 pb-1">
              <span className="flex items-center gap-1.5">
                <Activity className="w-3.5 h-3.5 text-rose-400" /> Snoring Duration Written
              </span>
              <span className="text-white font-medium">14 minutes</span>
            </div>
            <div className="flex justify-between">
              <span className="flex items-center gap-1.5">
                <Volume2 className="w-3.5 h-3.5 text-indigo-400" /> Avg Intensity Written
              </span>
              <span className="text-white font-medium">70.7 dB</span>
            </div>
            <p className="mt-3 text-emerald-300/80 leading-relaxed bg-emerald-950/50 p-2 rounded-lg">
              <strong>Insight:</strong> 65% of your snoring occurs during Light Sleep. Your snoring
              intensity has decreased by 5dB compared to last week.
            </p>
          </div>
        </div>
      )}

      {timeRange === 'daily' && (
        <>
          <h3 className="text-lg font-medium text-white mb-4 mt-2">Audio Clips by Stage</h3>
          <div className="space-y-8 pb-4">
            {(['Light', 'Deep', 'REM'] as const).map(stage => {
              const events = DAILY_SNORE_EVENTS.filter(e => e.stage === stage);
              if (events.length === 0) return null;

              const stageColors = {
                Light: 'bg-teal-400',
                Deep: 'bg-blue-400',
                REM: 'bg-purple-400',
              };

              return (
                <div key={stage} className="space-y-3">
                  <div className="flex items-center gap-2 mb-1">
                    <div
                      className={cn(
                        'w-2 h-2 rounded-full shadow-[0_0_8px_currentColor] opacity-80',
                        stageColors[stage],
                      )}
                    />
                    <h4 className="text-sm font-semibold text-slate-300 tracking-wide uppercase">
                      {stage} Sleep
                    </h4>
                  </div>
                  {events.map(event => {
                    const idx = DAILY_SNORE_EVENTS.indexOf(event);
                    return (
                      <div
                        key={idx}
                        className="bg-slate-900/50 border border-slate-800 rounded-2xl p-4 transition-all"
                      >
                        <div className="flex items-center justify-between mb-3">
                          <div className="flex items-center gap-3">
                            <div
                              className={cn(
                                'w-2 h-2 rounded-full',
                                event.intensity >= 80
                                  ? 'bg-rose-500'
                                  : event.intensity >= 65
                                  ? 'bg-orange-500'
                                  : 'bg-yellow-500',
                              )}
                            />
                            <div>
                              <div className="text-white font-medium">{event.time}</div>
                              <div className="text-xs text-slate-400">{event.duration}s event</div>
                            </div>
                          </div>
                          <div className="flex items-center gap-3">
                            <span className="text-slate-300 font-mono text-xs bg-slate-800 px-2 py-1 rounded border border-slate-700">
                              {event.intensity} dB
                            </span>
                            <button
                              onClick={() => togglePlay(idx)}
                              className={cn(
                                'w-10 h-10 rounded-full flex items-center justify-center transition-all',
                                playingId === idx
                                  ? 'bg-indigo-500 shadow-lg shadow-indigo-500/40 text-white'
                                  : 'bg-indigo-500/10 text-indigo-400 hover:bg-indigo-500/20',
                              )}
                            >
                              {playingId === idx ? (
                                <Square className="w-4 h-4 fill-current" />
                              ) : (
                                <Play className="w-4 h-4 fill-current ml-0.5" />
                              )}
                            </button>
                          </div>
                        </div>
                        <div className="h-1.5 bg-slate-800 rounded-full overflow-hidden relative">
                          <div
                            className={cn(
                              'absolute inset-y-0 left-0 bg-indigo-500',
                              playingId === idx
                                ? 'w-full transition-all duration-[3000ms] ease-linear'
                                : 'w-0 transition-none',
                            )}
                          />
                        </div>
                      </div>
                    );
                  })}
                </div>
              );
            })}
          </div>
        </>
      )}
    </div>
  );
}
