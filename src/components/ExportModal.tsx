import { Download, X } from 'lucide-react';
import { cn } from '../lib/cn';
import { DAILY_SNORE_EVENTS } from '../data/mockData';
import type { ExportRange } from '../types';

export function ExportModal({
  exportRange,
  setExportRange,
  onClose,
}: {
  exportRange: ExportRange;
  setExportRange: (r: ExportRange) => void;
  onClose: () => void;
}) {
  const handleExportCSV = () => {
    const csvContent =
      'data:text/csv;charset=utf-8,' +
      'Time,Intensity (dB),Duration (sec),Sleep Stage\n' +
      DAILY_SNORE_EVENTS.map(e => `${e.time},${e.intensity},${e.duration},${e.stage}`).join('\n');

    const encodedUri = encodeURI(csvContent);
    const link = document.createElement('a');
    link.setAttribute('href', encodedUri);
    link.setAttribute('download', `snore_data_${exportRange}.csv`);
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    onClose();
  };

  return (
    <div className="absolute inset-0 z-50 bg-slate-950/80 backdrop-blur-sm flex items-center justify-center p-6">
      <div className="bg-slate-900 border border-slate-700 rounded-3xl p-6 w-full max-w-sm shadow-2xl relative">
        <button
          onClick={onClose}
          className="absolute top-4 right-4 text-slate-400 hover:text-white transition-colors"
        >
          <X className="w-5 h-5" />
        </button>
        <h3 className="text-lg font-semibold text-white mb-2 flex items-center gap-2">
          <Download className="w-5 h-5 text-indigo-400" /> Export Data
        </h3>
        <p className="text-slate-400 text-xs mb-6">
          Select a date range to export your sleep and snoring analytics as a CSV.
        </p>

        <div className="space-y-2 mb-6">
          {(['7days', '30days', 'custom'] as const).map(r => (
            <label
              key={r}
              className={cn(
                'flex items-center gap-3 p-3 rounded-xl border cursor-pointer transition-colors',
                exportRange === r
                  ? 'bg-indigo-500/10 border-indigo-500/50'
                  : 'bg-slate-800/50 border-transparent hover:bg-slate-800',
              )}
              onClick={() => setExportRange(r)}
            >
              <div
                className={cn(
                  'w-4 h-4 rounded-full border-2 flex items-center justify-center',
                  exportRange === r ? 'border-indigo-400' : 'border-slate-500',
                )}
              >
                {exportRange === r && <div className="w-2 h-2 rounded-full bg-indigo-400" />}
              </div>
              <span className="text-sm font-medium text-slate-200">
                {r === '7days'
                  ? 'Last 7 Days'
                  : r === '30days'
                  ? 'Last 30 Days'
                  : 'Custom Date Range...'}
              </span>
            </label>
          ))}
        </div>
        <button
          onClick={handleExportCSV}
          className="w-full bg-indigo-600 hover:bg-indigo-500 text-white font-semibold py-3 rounded-xl transition-transform active:scale-95 shadow-lg shadow-indigo-600/20"
        >
          Download CSV
        </button>
      </div>
    </div>
  );
}
