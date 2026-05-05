import { useState, useEffect } from 'react';
import { Play, Square } from 'lucide-react';
import { cn } from '../lib/cn';
import { useAudioMonitor } from '../hooks/useAudioMonitor';
import { VolumeVisualizer } from './VolumeVisualizer';
import { LiveStatsCard } from './LiveStatsCard';
import type { Sensitivity } from '../types';

export function RecordTab({
  thresholdDB,
  healthKitEnabled,
  sensitivity,
  isTracking,
  setIsTracking,
}: {
  thresholdDB: number;
  healthKitEnabled: boolean;
  sensitivity: Sensitivity;
  isTracking: boolean;
  setIsTracking: (val: boolean) => void;
}) {
  const [elapsed, setElapsed] = useState(0);
  const {
    volume,
    sessionSnoreCount,
    sessionAvgIntensity,
    sessionTotalDuration,
    isCurrentlySnoring,
  } = useAudioMonitor(isTracking, thresholdDB, sensitivity);

  useEffect(() => {
    let interval: ReturnType<typeof setInterval> | undefined;
    if (isTracking) {
      interval = setInterval(() => setElapsed(e => e + 1), 1000);
    } else {
      setElapsed(0);
    }
    return () => {
      if (interval) clearInterval(interval);
    };
  }, [isTracking]);

  const formatTime = (seconds: number) => {
    const h = Math.floor(seconds / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    const s = seconds % 60;
    return `${h.toString().padStart(2, '0')}:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`;
  };

  const currentVolumePercentage = Math.min((volume / 100) * 100, 100);

  return (
    <div className="flex flex-col items-center justify-center flex-1 p-6 z-10 w-full relative">
      {isTracking && (
        <div className="absolute inset-0 flex items-center justify-center opacity-30 pointer-events-none">
          <div
            className={cn(
              'rounded-full transition-all duration-75 ease-out',
              isCurrentlySnoring ? 'bg-rose-500/40' : 'bg-indigo-500/30',
            )}
            style={{
              width: 200 + currentVolumePercentage * 2.5 + 'px',
              height: 200 + currentVolumePercentage * 2.5 + 'px',
            }}
          />
          <div
            className={cn(
              'absolute rounded-full transition-all duration-100 ease-out delay-75',
              isCurrentlySnoring ? 'bg-rose-600/30' : 'bg-purple-500/20',
            )}
            style={{
              width: 150 + currentVolumePercentage * 1.5 + 'px',
              height: 150 + currentVolumePercentage * 1.5 + 'px',
            }}
          />
        </div>
      )}

      <div className="mb-8 w-full max-w-xs transition-opacity duration-500">
        <div
          className={cn(
            'text-xs font-semibold tracking-wider text-center py-1.5 px-3 rounded-full border mb-4 backdrop-blur-md mx-auto w-fit',
            isTracking
              ? isCurrentlySnoring
                ? 'bg-rose-500/20 border-rose-500/50 text-rose-300'
                : 'bg-green-500/20 border-green-500/50 text-green-300'
              : 'bg-white/5 border-white/10 text-slate-400',
          )}
        >
          {isTracking
            ? isCurrentlySnoring
              ? '● SNORE DETECTED'
              : '● LISTENING & FILTERING'
            : 'READY TO SLEEP'}
        </div>
      </div>

      <div className="text-7xl font-mono mb-4 font-extralight tracking-tight text-white drop-shadow-md">
        {formatTime(elapsed)}
      </div>

      <p className="text-indigo-200 mb-12 text-center text-sm px-4 max-w-sm">
        {isTracking
          ? `Heuristic detector active. Only logging audio over ${thresholdDB}dB to avoid audiobooks.`
          : 'AirPods & Audiobooks will continue to play undisturbed. We mix audio using AVAudioSession.'}
      </p>

      <button
        onClick={() => setIsTracking(!isTracking)}
        className={cn(
          'w-32 h-32 rounded-full flex items-center justify-center shadow-2xl transition-all duration-300 relative z-20 group transform active:scale-95',
          isTracking
            ? 'bg-rose-600 hover:bg-rose-500 text-white shadow-rose-600/40'
            : 'bg-indigo-600 hover:bg-indigo-500 text-white shadow-indigo-600/40',
        )}
      >
        {isTracking ? (
          <Square className="w-10 h-10 fill-current" />
        ) : (
          <Play className="w-12 h-12 ml-2 fill-current" />
        )}
        <div className="absolute inset-0 rounded-full border-2 border-white/20 scale-110 group-hover:scale-125 transition-transform duration-500 opacity-0 group-hover:opacity-100" />
      </button>

      <VolumeVisualizer volume={volume} thresholdDB={thresholdDB} />

      {isTracking && (
        <LiveStatsCard
          sessionSnoreCount={sessionSnoreCount}
          sessionAvgIntensity={sessionAvgIntensity}
          sessionTotalDuration={sessionTotalDuration}
          healthKitEnabled={healthKitEnabled}
        />
      )}
    </div>
  );
}
