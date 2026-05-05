import { cn } from '../lib/cn';

export function VolumeVisualizer({
  volume,
  thresholdDB,
}: {
  volume: number;
  thresholdDB: number;
}) {
  const currentVolumePercentage = Math.min((volume / 100) * 100, 100);
  const thresholdPercentage = Math.min((thresholdDB / 100) * 100, 100);

  return (
    <div className="mt-12 w-full max-w-xs space-y-2">
      <div className="flex justify-between text-xs text-indigo-300">
        <span>Live Mic Vol</span>
        <span>Threshold ({thresholdDB}dB)</span>
      </div>
      <div className="h-2 w-full bg-slate-800 rounded-full overflow-hidden relative">
        <div
          className="absolute top-0 bottom-0 w-0.5 bg-rose-500 z-10"
          style={{ left: `${thresholdPercentage}%` }}
        />
        <div
          className={cn(
            'h-full transition-all duration-100',
            volume >= thresholdDB ? 'bg-rose-500' : 'bg-indigo-500',
          )}
          style={{ width: `${currentVolumePercentage}%` }}
        />
      </div>
    </div>
  );
}
