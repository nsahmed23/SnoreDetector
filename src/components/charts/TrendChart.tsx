import {
  ComposedChart,
  Bar,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
  CartesianGrid,
  Legend,
} from 'recharts';
import { WEEKLY_DATA, MONTHLY_DATA } from '../../data/mockData';

function WeeklyMonthlyTooltip({ active, payload, label }: any) {
  if (active && payload && payload.length) {
    return (
      <div className="bg-slate-900 border border-slate-700 p-3 rounded-xl shadow-xl text-sm">
        <p className="text-indigo-300 mb-2 font-medium">{label}</p>
        {payload.map((p: any, idx: number) => (
          <p
            key={idx}
            className="flex items-center justify-between gap-4 mb-1"
            style={{ color: p.color }}
          >
            <span>{p.name}:</span>
            <span className="font-semibold">
              {p.value} {p.name.includes('Intensity') ? 'dB' : ''}
              {p.name.includes('Duration') ? 'm' : ''}
            </span>
          </p>
        ))}
      </div>
    );
  }
  return null;
}

export function TrendChart({ timeRange }: { timeRange: 'weekly' | 'monthly' }) {
  const data = timeRange === 'weekly' ? WEEKLY_DATA : MONTHLY_DATA;
  return (
    <div className="h-64 w-full -ml-2 mt-6">
      <ResponsiveContainer width="100%" height="100%">
        <ComposedChart data={data} margin={{ top: 10, right: 0, left: -20, bottom: 0 }}>
          <CartesianGrid strokeDasharray="3 3" stroke="#fff" opacity={0.05} vertical={false} />
          <XAxis
            dataKey="name"
            axisLine={false}
            tickLine={false}
            tick={{ fill: '#64748b', fontSize: 10 }}
          />
          <YAxis
            yAxisId="left"
            orientation="left"
            stroke="#818cf8"
            tick={{ fontSize: 10, fill: '#64748b' }}
            axisLine={false}
            tickLine={false}
          />
          <YAxis
            yAxisId="right"
            orientation="right"
            stroke="#fb7185"
            tick={{ fontSize: 10, fill: '#64748b' }}
            axisLine={false}
            tickLine={false}
          />
          <Tooltip
            content={<WeeklyMonthlyTooltip />}
            cursor={{ fill: 'rgba(255,255,255,0.05)' }}
          />
          <Legend
            wrapperStyle={{ fontSize: 10, color: '#94a3b8', paddingTop: '10px' }}
            iconType="circle"
          />
          <Bar
            yAxisId="left"
            dataKey="freq"
            fill="#818cf8"
            radius={[4, 4, 0, 0]}
            name="Freq (Events)"
            maxBarSize={30}
          />
          <Line
            yAxisId="right"
            type="monotone"
            dataKey="intensity"
            stroke="#fb7185"
            strokeWidth={2}
            dot={{ r: 4, fill: '#fb7185', strokeWidth: 2, stroke: '#1e293b' }}
            name="Avg Intensity"
          />
          {timeRange === 'monthly' && (
            <Line
              yAxisId="right"
              type="step"
              dataKey="durationMin"
              stroke="#a78bfa"
              strokeDasharray="3 3"
              strokeWidth={2}
              dot={false}
              name="Duration (min)"
            />
          )}
        </ComposedChart>
      </ResponsiveContainer>
    </div>
  );
}
