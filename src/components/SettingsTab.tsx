import {
  HeartPulse,
  Smartphone,
  CheckCircle2,
  SlidersHorizontal,
  Info,
} from 'lucide-react';
import { cn } from '../lib/cn';
import type { Sensitivity } from '../types';

export function SettingsTab({
  thresholdDB,
  setThresholdDB,
  healthKitEnabled,
  setHealthKitEnabled,
  sensitivity,
  setSensitivity,
}: {
  thresholdDB: number;
  setThresholdDB: (val: number) => void;
  healthKitEnabled: boolean;
  setHealthKitEnabled: (val: boolean) => void;
  sensitivity: Sensitivity;
  setSensitivity: (val: Sensitivity) => void;
}) {
  return (
    <div className="flex-1 p-6 w-full z-10 relative overflow-y-auto">
      <h2 className="text-2xl font-semibold mb-8 text-indigo-50">Settings</h2>

      <div className="space-y-6 pb-12">
        <div className="bg-white/5 backdrop-blur-lg border border-white/10 rounded-3xl overflow-hidden divide-y divide-white/10">
          <div className="p-5 flex items-center justify-between">
            <div className="flex items-center gap-3 flex-1">
              <HeartPulse className="text-rose-400 w-5 h-5 shrink-0" />
              <div>
                <div className="text-white font-medium">Apple HealthKit Sync</div>
                <div className="text-[11px] leading-tight text-indigo-200 mt-0.5 max-w-[200px]">
                  Read Sleep Stages & write nightly Snoring Duration/Intensity.
                </div>
              </div>
            </div>
            <div
              onClick={() => setHealthKitEnabled(!healthKitEnabled)}
              className={cn(
                'w-12 h-6 rounded-full relative shadow-inner cursor-pointer shrink-0 transition-colors duration-300',
                healthKitEnabled ? 'bg-green-500' : 'bg-white/10',
              )}
            >
              <div
                className={cn(
                  'absolute top-1 bottom-1 w-4 bg-white rounded-full shadow transition-all duration-300',
                  healthKitEnabled ? 'right-1' : 'left-1',
                )}
              />
            </div>
          </div>

          <div className="p-5 flex items-center justify-between">
            <div className="flex items-center gap-3">
              <Smartphone className="text-blue-400 w-5 h-5" />
              <div>
                <div className="text-white font-medium">Background Mix</div>
                <div className="text-[11px] leading-tight text-indigo-200 mt-0.5 max-w-[200px]">
                  AVAudioSession enabled. Play audible/music via AirPods while tracking.
                </div>
              </div>
            </div>
            <div className="flex items-center text-xs text-blue-300 gap-1 bg-blue-500/10 px-2 py-1 rounded-full">
              <CheckCircle2 className="w-3 h-3" /> Active
            </div>
          </div>
        </div>

        <div className="flex items-center gap-2 ml-1 mt-8 mb-3">
          <SlidersHorizontal className="w-4 h-4 text-slate-400" />
          <h3 className="text-sm font-semibold text-slate-400 uppercase tracking-wider">
            Detection Details
          </h3>
        </div>
        <div className="bg-white/5 backdrop-blur-lg border border-white/10 rounded-3xl p-5 mb-6 space-y-8">
          <div>
            <div className="flex justify-between items-end mb-4">
              <div>
                <div className="text-white font-medium">Volume Threshold</div>
                <div className="text-[11px] text-slate-400 mt-1 max-w-[220px]">
                  Minimum decibel level for recording snippet.
                </div>
              </div>
              <div className="text-2xl font-light text-rose-400 shrink-0">
                {thresholdDB} <span className="text-sm text-slate-500">dB</span>
              </div>
            </div>

            <input
              type="range"
              min="30"
              max="90"
              value={thresholdDB}
              onChange={(e) => setThresholdDB(Number(e.target.value))}
              className="w-full accent-indigo-500 h-1.5 bg-slate-800 rounded-lg appearance-none cursor-pointer"
            />
            <div className="flex justify-between mt-2 text-[10px] text-slate-500 font-mono">
              <span>Quiet (30dB)</span>
              <span>Loud (90dB)</span>
            </div>
          </div>

          <div>
            <div className="text-white font-medium mb-1">Detector Sensitivity</div>
            <div className="text-[11px] text-slate-400 mb-4 max-w-[260px]">
              Tunes the heuristic detector: how strongly low frequencies must dominate the spectrum to count as a snore.
            </div>

            <div className="flex p-1 bg-slate-800/50 rounded-xl border border-slate-700/50">
              {(['low', 'medium', 'high'] as const).map(level => (
                <button
                  key={level}
                  onClick={() => setSensitivity(level)}
                  className={cn(
                    'flex-1 capitalize py-1.5 text-xs font-semibold rounded-lg transition-all',
                    sensitivity === level
                      ? 'bg-indigo-500 text-white shadow-lg'
                      : 'text-slate-400 hover:text-slate-200',
                  )}
                >
                  {level}
                </button>
              ))}
            </div>
          </div>
        </div>

        <div className="bg-indigo-500/10 border border-indigo-500/20 rounded-2xl p-4 flex gap-3 text-sm text-indigo-200">
          <Info className="w-5 h-5 text-indigo-400 shrink-0 mt-0.5" />
          <p className="text-[11px] leading-relaxed">
            <strong>How it works:</strong> A heuristic detector runs on client-side FFT data. It flags a frame as snore-like when the volume crosses your threshold and lower frequencies dominate the spectrum (separating snoring from broadband noise like fans or audiobooks). A frame must persist ~250 ms before it counts as an event. No machine-learning model is used in this prototype.
          </p>
        </div>

        <p className="text-[11px] text-slate-500 leading-relaxed text-center px-4 pt-2">
          SnoreGuard is a wellness prototype, not a medical device. It does not diagnose sleep
          apnea or any sleep disorder. Consult a physician for sleep concerns.
        </p>
      </div>
    </div>
  );
}
