import React from 'react';
import { HeartPulse } from 'lucide-react';

export const LiveStatsCard = React.memo(function LiveStatsCard({
  sessionSnoreCount,
  sessionAvgIntensity,
  sessionTotalDuration,
  healthKitEnabled,
}: {
  sessionSnoreCount: number;
  sessionAvgIntensity: number;
  sessionTotalDuration: number;
  healthKitEnabled: boolean;
}) {
  return (
    <>
      <div className="mt-8 flex gap-4 items-center bg-white/5 backdrop-blur-md rounded-2xl p-4 border border-white/10 w-full max-w-sm">
        <div className="flex-1 text-center">
          <div className="text-2xl font-bold text-rose-400">{sessionSnoreCount}</div>
          <div className="text-[10px] text-indigo-200 uppercase tracking-wider mt-1">Events</div>
        </div>
        <div className="w-px h-10 bg-white/20" />
        <div className="flex-1 text-center">
          <div className="text-2xl font-bold text-indigo-400">
            {Math.round(sessionAvgIntensity)} <span className="text-sm">dB</span>
          </div>
          <div className="text-[10px] text-indigo-200 uppercase tracking-wider mt-1">Avg Intensity</div>
        </div>
        <div className="w-px h-10 bg-white/20" />
        <div className="flex-1 text-center">
          <div className="text-2xl font-bold text-purple-400">
            {sessionTotalDuration.toFixed(1)}<span className="text-sm">s</span>
          </div>
          <div className="text-[10px] text-indigo-200 uppercase tracking-wider mt-1">Duration</div>
        </div>
      </div>

      {healthKitEnabled && (
        <div className="mt-4 flex items-center gap-2 text-xs text-green-400 bg-green-400/10 px-3 py-1.5 rounded-full border border-green-400/20">
          <HeartPulse className="w-3 h-3" /> Syncing sleep stages & writing via HealthKit
        </div>
      )}
    </>
  );
});
// ⚡ Bolt: Memoized LiveStatsCard to prevent 60 FPS re-renders driven by volume updates in RecordTab
