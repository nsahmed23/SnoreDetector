import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
  ReferenceDot,
  CartesianGrid,
} from 'recharts';
import { Activity } from 'lucide-react';
import { cn } from '../../lib/cn';
import { DAILY_SLEEP_DATA, DAILY_SNORE_EVENTS } from '../../data/mockData';
import type { SleepDatum } from '../../types';

function CustomDailyTooltip({ active, payload, label }: any) {
  if (active && payload && payload.length) {
    const stageMap = { 0: 'Awake', 1: 'Light Sleep', 2: 'Deep Sleep', 3: 'REM Sleep' };
    const historicalPayload = payload.find((p: any) => p.dataKey === 'stage');
    const realtimePayload = payload.find((p: any) => p.dataKey === 'realtimeStage');

    const stageValue =
      realtimePayload?.value !== undefined ? realtimePayload.value : historicalPayload?.value;
    const isRealtime =
      realtimePayload?.value !== undefined && historicalPayload?.value === undefined;

    const snore = DAILY_SNORE_EVENTS.find(s => s.time === label);

    let snoreColor = 'text-rose-400';
    if (snore) {
      if (snore.stage === 'Light') snoreColor = 'text-teal-400';
      else if (snore.stage === 'Deep') snoreColor = 'text-blue-400';
      else if (snore.stage === 'REM') snoreColor = 'text-purple-400';
    }

    return (
      <div className="bg-slate-900 border border-slate-700 p-3 rounded-xl shadow-xl text-sm">
        <p className="text-indigo-300 mb-1 font-medium">{label}</p>
        <p className="text-white flex items-center gap-2">
          {stageMap[stageValue as keyof typeof stageMap]}
          {isRealtime ? (
            <span className="text-emerald-500/80 text-xs flex items-center gap-1">
              <span className="w-1.5 h-1.5 rounded-full bg-emerald-500 animate-pulse" />
              Live Data
            </span>
          ) : (
            <span className="text-indigo-500/50 text-xs">(HealthKit)</span>
          )}
        </p>
        {snore && (
          <div className="mt-3 pt-3 border-t border-slate-800">
            <p className={cn('font-semibold mb-1 flex items-center gap-1', snoreColor)}>
              <Activity className="w-3 h-3" /> Snore Event Recorded
            </p>
            <p className="text-slate-300 text-xs">
              Intensity: <span className="text-white font-medium">{snore.intensity} dB</span>
            </p>
            <p className="text-slate-300 text-xs">
              Duration: <span className="text-white font-medium">{snore.duration}s</span>
            </p>
          </div>
        )}
      </div>
    );
  }
  return null;
}

export function DailyChart({
  combinedData,
  isTracking,
}: {
  combinedData: SleepDatum[];
  isTracking: boolean;
}) {
  return (
    <div className="h-64 w-full -ml-4 mt-6">
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={combinedData} margin={{ top: 10, right: 10, left: 0, bottom: 0 }}>
          <defs>
            <linearGradient id="colorStage" x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%" stopColor="#818cf8" stopOpacity={0.4} />
              <stop offset="95%" stopColor="#818cf8" stopOpacity={0} />
            </linearGradient>
            <linearGradient id="colorRealtime" x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%" stopColor="#10b981" stopOpacity={0.4} />
              <stop offset="95%" stopColor="#10b981" stopOpacity={0} />
            </linearGradient>
          </defs>
          <CartesianGrid strokeDasharray="3 3" stroke="#fff" opacity={0.05} vertical={false} />
          <YAxis
            domain={[0, 3]}
            ticks={[0, 1, 2, 3]}
            tickFormatter={(val) => {
              return { 0: 'Awake', 1: 'Light', 2: 'Deep', 3: 'REM' }[val as 0 | 1 | 2 | 3] || '';
            }}
            axisLine={false}
            tickLine={false}
            tick={{ fill: '#64748b', fontSize: 10 }}
            width={45}
            reversed
          />
          <XAxis
            dataKey="time"
            tick={{ fill: '#64748b', fontSize: 10 }}
            axisLine={false}
            tickLine={false}
            minTickGap={30}
          />
          <Tooltip
            content={<CustomDailyTooltip />}
            cursor={{ stroke: 'rgba(255,255,255,0.1)', strokeWidth: 2 }}
          />
          <Area
            type="stepAfter"
            dataKey="stage"
            stroke="#818cf8"
            strokeWidth={2}
            fillOpacity={1}
            fill="url(#colorStage)"
          />
          {isTracking && (
            <Area
              type="stepAfter"
              dataKey="realtimeStage"
              stroke="#10b981"
              strokeDasharray="5 5"
              strokeWidth={2}
              fillOpacity={1}
              fill="url(#colorRealtime)"
              isAnimationActive={false}
            />
          )}

          {DAILY_SNORE_EVENTS.map((snore, idx) => {
            const dataPoint = DAILY_SLEEP_DATA.find(d => d.time === snore.time);
            if (!dataPoint) return null;
            const radius = Math.max(3, Math.min(snore.duration / 10, 8));

            let dotColor = '#fb7185';
            if (snore.stage === 'Light') dotColor = '#2dd4bf';
            else if (snore.stage === 'Deep') dotColor = '#60a5fa';
            else if (snore.stage === 'REM') dotColor = '#c084fc';

            return (
              <ReferenceDot
                key={idx}
                x={snore.time}
                y={dataPoint.stage}
                r={radius}
                fill={dotColor}
                stroke="#1e293b"
                strokeWidth={2}
              />
            );
          })}
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
}
