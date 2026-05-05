import { useState, useEffect, useRef, useMemo } from 'react';
import { Play, Square, Settings, BarChart2, Activity, Moon, Clock, HeartPulse, Smartphone, Volume2, History, Info, CheckCircle2, Download, ChevronRight, X, Calendar, SlidersHorizontal, Sparkles } from 'lucide-react';
import { AreaChart, Area, XAxis, YAxis, Tooltip, ResponsiveContainer, ReferenceDot, CartesianGrid, ComposedChart, Bar, Line, Legend } from 'recharts';
import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

// -- MOCK DATA --
const DAILY_SLEEP_DATA = [
  { time: '11:00 PM', stage: 0 }, // Awake
  { time: '11:30 PM', stage: 1 }, // Light
  { time: '12:00 AM', stage: 2 }, // Deep
  { time: '12:30 AM', stage: 2 },
  { time: '1:00 AM', stage: 1 },
  { time: '1:30 AM', stage: 3 }, // REM
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
const DAILY_SNORE_EVENTS = [
  { time: '12:15 AM', intensity: 75, duration: 45, stage: 'Deep' },
  { time: '1:45 AM', intensity: 65, duration: 30, stage: 'REM' },
  { time: '4:10 AM', intensity: 82, duration: 120, stage: 'Light' },
  { time: '4:20 AM', intensity: 61, duration: 20, stage: 'Light' },
];

const WEEKLY_DATA = [
  { name: 'Mon', freq: 14, intensity: 68, durationMin: 12 },
  { name: 'Tue', freq: 8, intensity: 62, durationMin: 5 },
  { name: 'Wed', freq: 22, intensity: 75, durationMin: 28 },
  { name: 'Thu', freq: 4, intensity: 58, durationMin: 2 },
  { name: 'Fri', freq: 19, intensity: 71, durationMin: 21 },
  { name: 'Sat', freq: 25, intensity: 78, durationMin: 35 },
  { name: 'Sun', freq: 11, intensity: 65, durationMin: 9 },
];

const MONTHLY_DATA = [
  { name: 'Week 1', freq: 85, intensity: 65, durationMin: 120 },
  { name: 'Week 2', freq: 112, intensity: 71, durationMin: 165 },
  { name: 'Week 3', freq: 64, intensity: 62, durationMin: 85 },
  { name: 'Week 4', freq: 42, intensity: 59, durationMin: 50 }, // Getting better!
];

// -- HOOK FOR SMART AUDIO MONITORING --
function useAudioMonitor(isTracking: boolean, thresholdDB: number, sensitivity: 'low' | 'medium' | 'high') {
  const [volume, setVolume] = useState(0);
  const [sessionSnoreCount, setSessionSnoreCount] = useState(0);
  const [sessionAvgIntensity, setSessionAvgIntensity] = useState(0);
  const [sessionTotalDuration, setSessionTotalDuration] = useState(0); // in seconds
  const [isCurrentlySnoring, setIsCurrentlySnoring] = useState(false);
  
  const requestRef = useRef<number>();
  const streamRef = useRef<MediaStream | null>(null);
  const audioContextRef = useRef<AudioContext | null>(null);

  const activeSnoreFrames = useRef(0);
  const activeSnoreIntensities = useRef<number[]>([]);

  useEffect(() => {
    if (isTracking) {
      navigator.mediaDevices.getUserMedia({ audio: true })
        .then((stream) => {
          streamRef.current = stream;
          const audioContext = new AudioContext();
          audioContextRef.current = audioContext;
          const analyzer = audioContext.createAnalyser();
          analyzer.fftSize = 256;
          const source = audioContext.createMediaStreamSource(stream);
          source.connect(analyzer);
          const dataArray = new Uint8Array(analyzer.frequencyBinCount);

          const updateVolume = () => {
             if (audioContextRef.current?.state === 'closed') return;
             analyzer.getByteFrequencyData(dataArray);
             let sum = 0;
             for (let i = 0; i < dataArray.length; i++) {
               sum += dataArray[i];
             }
             const avg = sum / dataArray.length;

             // Enhance algorithm: check if lower frequencies dominate (typical of snoring vs white noise/audiobooks)
             let lowFreqSum = 0;
             let highFreqSum = 0;
             const midPoint = Math.floor(dataArray.length / 4); // Frequencies usually concentrated lower
             for (let i = 0; i < dataArray.length; i++) {
               if (i < midPoint) lowFreqSum += dataArray[i];
               else highFreqSum += dataArray[i];
             }
             
             // Dynamic sensitivity threshold mimicking ML model adjustments
             const multiplier = sensitivity === 'low' ? 2.0 : sensitivity === 'high' ? 1.2 : 1.5;
             const isLowFreqDominant = lowFreqSum > highFreqSum * multiplier;

             // Scale raw values roughly to a simulated decibel limit for prototype
             const simulatedDb = Math.min(Math.max((avg / 255) * 110, 30), 100); 
             
             setVolume(simulatedDb);

             if (simulatedDb >= thresholdDB && isLowFreqDominant) {
               activeSnoreFrames.current++;
               activeSnoreIntensities.current.push(simulatedDb);
               if (activeSnoreFrames.current > 15 && !isCurrentlySnoring) { // ~250ms sustained
                 setIsCurrentlySnoring(true);
               }
             } else {
               if (isCurrentlySnoring) {
                 // End of snore event
                 setIsCurrentlySnoring(false);
                 setSessionSnoreCount(c => c + 1);
                 
                 // Add to duration (60 frames per second roughly)
                 const durationSecs = activeSnoreFrames.current / 60;
                 setSessionTotalDuration(d => d + durationSecs);
                 
                 // Update running average intensity
                 const eventAvg = activeSnoreIntensities.current.reduce((a,b)=>a+b,0) / activeSnoreIntensities.current.length;
                 setSessionAvgIntensity(prev => prev === 0 ? eventAvg : (prev + eventAvg) / 2);
               }
               activeSnoreFrames.current = 0;
               activeSnoreIntensities.current = [];
             }

             requestRef.current = requestAnimationFrame(updateVolume);
          };
          updateVolume();
        })
        .catch((err) => {
          console.error("Microphone access denied.", err);
          alert("Microphone access is required to track snoring in this prototype.");
        });
    } else {
      if (requestRef.current) cancelAnimationFrame(requestRef.current);
      if (streamRef.current) streamRef.current.getTracks().forEach(t => t.stop());
      if (audioContextRef.current) audioContextRef.current.close();
      setVolume(0);
      setIsCurrentlySnoring(false);
      activeSnoreFrames.current = 0;
      activeSnoreIntensities.current = [];
    }

    return () => {
      if (requestRef.current) cancelAnimationFrame(requestRef.current);
      if (streamRef.current) streamRef.current.getTracks().forEach(t => t.stop());
      if (audioContextRef.current && audioContextRef.current.state !== 'closed') {
        audioContextRef.current.close();
      }
    };
  }, [isTracking, thresholdDB, isCurrentlySnoring]);

  return { volume, sessionSnoreCount, sessionAvgIntensity, sessionTotalDuration, isCurrentlySnoring };
}


// -- COMPONENTS --

function RecordTab({ thresholdDB, healthKitEnabled, sensitivity, isTracking, setIsTracking }: { thresholdDB: number, healthKitEnabled: boolean, sensitivity: 'low' | 'medium' | 'high', isTracking: boolean, setIsTracking: (val: boolean) => void }) {
  const [elapsed, setElapsed] = useState(0);
  const { volume, sessionSnoreCount, sessionAvgIntensity, sessionTotalDuration, isCurrentlySnoring } = useAudioMonitor(isTracking, thresholdDB, sensitivity);

  useEffect(() => {
    let interval: NodeJS.Timeout;
    if (isTracking) {
      interval = setInterval(() => setElapsed(e => e + 1), 1000);
    } else {
      setElapsed(0);
    }
    return () => clearInterval(interval);
  }, [isTracking]);

  const formatTime = (seconds: number) => {
    const h = Math.floor(seconds / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    const s = seconds % 60;
    return `${h.toString().padStart(2, '0')}:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`;
  };

  const currentVolumePercentage = Math.min((volume / 100) * 100, 100);
  const thresholdPercentage = Math.min((thresholdDB / 100) * 100, 100);

  return (
    <div className="flex flex-col items-center justify-center flex-1 p-6 z-10 w-full relative">
       {/* Background visualizer ripples */}
       {isTracking && (
         <div className="absolute inset-0 flex items-center justify-center opacity-30 pointer-events-none">
           <div 
             className={cn(
               "rounded-full transition-all duration-75 ease-out",
               isCurrentlySnoring ? "bg-rose-500/40" : "bg-indigo-500/30"
             )}
             style={{ 
               width: 200 + currentVolumePercentage * 2.5 + 'px', 
               height: 200 + currentVolumePercentage * 2.5 + 'px' 
             }}
           />
           <div 
             className={cn(
               "absolute rounded-full transition-all duration-100 ease-out delay-75",
               isCurrentlySnoring ? "bg-rose-600/30" : "bg-purple-500/20"
             )}
             style={{ 
               width: 150 + currentVolumePercentage * 1.5 + 'px', 
               height: 150 + currentVolumePercentage * 1.5 + 'px' 
             }}
           />
         </div>
       )}

      <div className="mb-8 w-full max-w-xs transition-opacity duration-500">
         <div className={cn(
           "text-xs font-semibold tracking-wider text-center py-1.5 px-3 rounded-full border mb-4 backdrop-blur-md mx-auto w-fit",
           isTracking 
             ? isCurrentlySnoring ? "bg-rose-500/20 border-rose-500/50 text-rose-300" : "bg-green-500/20 border-green-500/50 text-green-300"
             : "bg-white/5 border-white/10 text-slate-400"
         )}>
           {isTracking 
             ? isCurrentlySnoring ? "● SNORE DETECTED" : "● LISTENING & FILTERING"
             : "READY TO SLEEP"}
         </div>
      </div>

      <div className="text-7xl font-mono mb-4 font-extralight tracking-tight text-white drop-shadow-md">
        {formatTime(elapsed)}
      </div>

      <p className="text-indigo-200 mb-12 text-center text-sm px-4 max-w-sm">
        {isTracking 
          ? `Algorithm active. Only recording audio over ${thresholdDB}dB to avoid audiobooks.` 
          : 'AirPods & Audiobooks will continue to play undisturbed. We mix audio using AVAudioSession.'}
      </p>

      <button
        onClick={() => setIsTracking(!isTracking)}
        className={cn(
          "w-32 h-32 rounded-full flex items-center justify-center shadow-2xl transition-all duration-300 relative z-20 group transform active:scale-95",
          isTracking 
            ? "bg-rose-600 hover:bg-rose-500 text-white shadow-rose-600/40" 
            : "bg-indigo-600 hover:bg-indigo-500 text-white shadow-indigo-600/40"
        )}
      >
        {isTracking ? <Square className="w-10 h-10 fill-current" /> : <Play className="w-12 h-12 ml-2 fill-current" />}
        <div className="absolute inset-0 rounded-full border-2 border-white/20 scale-110 group-hover:scale-125 transition-transform duration-500 opacity-0 group-hover:opacity-100"></div>
      </button>

      {/* Live Volume Meter */}
      <div className="mt-12 w-full max-w-xs space-y-2">
         <div className="flex justify-between text-xs text-indigo-300">
           <span>Live Mic Vol</span>
           <span>Threshold ({thresholdDB}dB)</span>
         </div>
         <div className="h-2 w-full bg-slate-800 rounded-full overflow-hidden relative">
            {/* Threshold marker */}
            <div className="absolute top-0 bottom-0 w-0.5 bg-rose-500 z-10" style={{ left: `${thresholdPercentage}%` }}></div>
            {/* Active volume bar */}
            <div 
              className={cn("h-full transition-all duration-100", volume >= thresholdDB ? "bg-rose-500" : "bg-indigo-500")}
              style={{ width: `${currentVolumePercentage}%` }}
            ></div>
         </div>
      </div>

      {isTracking && (
        <div className="mt-8 flex gap-4 items-center bg-white/5 backdrop-blur-md rounded-2xl p-4 border border-white/10 w-full max-w-sm">
          <div className="flex-1 text-center">
            <div className="text-2xl font-bold text-rose-400">{sessionSnoreCount}</div>
            <div className="text-[10px] text-indigo-200 uppercase tracking-wider mt-1">Events</div>
          </div>
          <div className="w-px h-10 bg-white/20"></div>
          <div className="flex-1 text-center">
             <div className="text-2xl font-bold text-indigo-400">{Math.round(sessionAvgIntensity)} <span className="text-sm">dB</span></div>
             <div className="text-[10px] text-indigo-200 uppercase tracking-wider mt-1">Avg Intensity</div>
          </div>
          <div className="w-px h-10 bg-white/20"></div>
          <div className="flex-1 text-center">
             <div className="text-2xl font-bold text-purple-400">{(sessionTotalDuration).toFixed(1)}<span className="text-sm">s</span></div>
             <div className="text-[10px] text-indigo-200 uppercase tracking-wider mt-1">Duration</div>
          </div>
        </div>
      )}

      {healthKitEnabled && isTracking && (
        <div className="mt-4 flex items-center gap-2 text-xs text-green-400 bg-green-400/10 px-3 py-1.5 rounded-full border border-green-400/20">
           <HeartPulse className="w-3 h-3" /> Syncing sleep stages & writing via HealthKit
        </div>
      )}
    </div>
  );
}

function OnboardingTutorial({ onComplete }: { onComplete: () => void }) {
  const [step, setStep] = useState(0);
  
  const steps = [
    {
      icon: <Sparkles className="w-12 h-12 text-indigo-400 mb-6" />,
      title: "Smart Snore Detection",
      desc: "SnoreGuard uses advanced algorithms to distinguish your snoring from audiobooks, pets, or city noise."
    },
    {
      icon: <HeartPulse className="w-12 h-12 text-emerald-400 mb-6" />,
      title: "HealthKit Integration",
      desc: "Connect seamlessly to Apple Health to correlate snoring events directly with your sleep stages."
    },
    {
      icon: <BarChart2 className="w-12 h-12 text-purple-400 mb-6" />,
      title: "Interactive Insights",
      desc: "Play back recorded audio clips and view detailed charts to understand your sleep health trends."
    }
  ];

  return (
    <div className="absolute inset-0 z-[100] bg-slate-950/90 backdrop-blur-lg flex flex-col items-center justify-center p-8">
      <div className="flex-1 flex flex-col items-center justify-center text-center">
        {steps[step].icon}
        <h2 className="text-2xl font-bold text-white mb-4">{steps[step].title}</h2>
        <p className="text-slate-300 text-sm leading-relaxed max-w-[260px]">
          {steps[step].desc}
        </p>
      </div>
      <div className="w-full flex items-center justify-between pb-12">
        <div className="flex gap-2">
          {steps.map((_, i) => (
            <div key={i} className={cn("h-1.5 rounded-full transition-all duration-300", step === i ? "w-6 bg-indigo-500" : "w-1.5 bg-slate-700")} />
          ))}
        </div>
        <button 
          onClick={() => step < steps.length - 1 ? setStep(s => s + 1) : onComplete()}
          className="bg-indigo-600 hover:bg-indigo-500 text-white px-6 py-2.5 rounded-full font-semibold flex items-center gap-2 transition-transform active:scale-95 shadow-lg shadow-indigo-600/20"
        >
          {step < steps.length - 1 ? "Next" : "Get Started"} <ChevronRight className="w-4 h-4" />
        </button>
      </div>
    </div>
  );
}

function CustomDailyTooltip({ active, payload, label }: any) {
  if (active && payload && payload.length) {
    const stageMap = { 0: 'Awake', 1: 'Light Sleep', 2: 'Deep Sleep', 3: 'REM Sleep' };
    const historicalPayload = payload.find((p:any) => p.dataKey === 'stage');
    const realtimePayload = payload.find((p:any) => p.dataKey === 'realtimeStage');
    
    const stageValue = realtimePayload?.value !== undefined ? realtimePayload.value : historicalPayload?.value;
    const isRealtime = realtimePayload?.value !== undefined && historicalPayload?.value === undefined;
    
    const snore = DAILY_SNORE_EVENTS.find(s => s.time === label);
    
    let snoreColor = "text-rose-400";
    if (snore) {
       if (snore.stage === 'Light') snoreColor = "text-teal-400";
       else if (snore.stage === 'Deep') snoreColor = "text-blue-400";
       else if (snore.stage === 'REM') snoreColor = "text-purple-400";
    }

    return (
      <div className="bg-slate-900 border border-slate-700 p-3 rounded-xl shadow-xl text-sm">
        <p className="text-indigo-300 mb-1 font-medium">{label}</p>
        <p className="text-white flex items-center gap-2">
          {stageMap[stageValue as keyof typeof stageMap]} 
          {isRealtime ? (
            <span className="text-emerald-500/80 text-xs flex items-center gap-1">
               <span className="w-1.5 h-1.5 rounded-full bg-emerald-500 animate-pulse"></span>
               Live Data
            </span>
          ) : (
            <span className="text-indigo-500/50 text-xs">(HealthKit)</span>
          )}
        </p>
        {snore && (
          <div className="mt-3 pt-3 border-t border-slate-800">
            <p className={cn("font-semibold mb-1 flex items-center gap-1", snoreColor)}>
              <Activity className="w-3 h-3" /> Snore Event Recorded
            </p>
            <p className="text-slate-300 text-xs">Intensity: <span className="text-white font-medium">{snore.intensity} dB</span></p>
            <p className="text-slate-300 text-xs">Duration: <span className="text-white font-medium">{snore.duration}s</span></p>
          </div>
        )}
      </div>
    );
  }
  return null;
}

function WeeklyMonthlyTooltip({ active, payload, label }: any) {
  if (active && payload && payload.length) {
    return (
      <div className="bg-slate-900 border border-slate-700 p-3 rounded-xl shadow-xl text-sm">
         <p className="text-indigo-300 mb-2 font-medium">{label}</p>
         {payload.map((p:any, idx:number) => (
           <p key={idx} className="flex items-center justify-between gap-4 mb-1" style={{color: p.color}}>
             <span>{p.name}:</span>
             <span className="font-semibold">{p.value} {p.name.includes('Intensity') ? 'dB' : ''}{p.name.includes('Duration') ? 'm' : ''}</span>
           </p>
         ))}
      </div>
    );
  }
  return null;
}

function InsightsTab({ healthKitEnabled, isTracking, liveSleepData }: { healthKitEnabled: boolean, isTracking: boolean, liveSleepData: any[] }) {
  const [timeRange, setTimeRange] = useState<'daily' | 'weekly' | 'monthly'>('daily');
  const [playingId, setPlayingId] = useState<number | null>(null);
  const [showExportModal, setShowExportModal] = useState(false);
  const [exportRange, setExportRange] = useState('7days');
  
  const combinedData = useMemo(() => {
    const data: any[] = [...DAILY_SLEEP_DATA];
    if (isTracking && liveSleepData.length > 0) {
       // ensure connection
       data[data.length - 1] = { ...data[data.length - 1], realtimeStage: data[data.length - 1].stage };
       return [...data, ...liveSleepData.slice(1)];
    }
    return data;
  }, [isTracking, liveSleepData]);

  const handleExportCSV = () => {
    // In actual implementation, we would filter data based on exportRange here before CSV generation.
    const csvContent = "data:text/csv;charset=utf-8," 
      + "Time,Intensity (dB),Duration (sec),Sleep Stage\n" 
      + DAILY_SNORE_EVENTS.map(e => `${e.time},${e.intensity},${e.duration},${e.stage}`).join("\n");
      
    const encodedUri = encodeURI(csvContent);
    const link = document.createElement("a");
    link.setAttribute("href", encodedUri);
    link.setAttribute("download", `snore_data_${exportRange}.csv`);
    document.body.appendChild(link); 
    link.click();
    document.body.removeChild(link);
    setShowExportModal(false);
  };

  const togglePlay = (idx: number) => {
    if (playingId === idx) {
      setPlayingId(null);
    } else {
      setPlayingId(idx);
      // Simulate playback duration
      setTimeout(() => {
        setPlayingId(current => current === idx ? null : current);
      }, 3000);
    }
  };

  const renderDailyChart = () => (
    <div className="h-64 w-full -ml-4 mt-6">
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={combinedData} margin={{ top: 10, right: 10, left: 0, bottom: 0 }}>
          <defs>
            <linearGradient id="colorStage" x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%" stopColor="#818cf8" stopOpacity={0.4}/>
              <stop offset="95%" stopColor="#818cf8" stopOpacity={0}/>
            </linearGradient>
            <linearGradient id="colorRealtime" x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%" stopColor="#10b981" stopOpacity={0.4}/>
              <stop offset="95%" stopColor="#10b981" stopOpacity={0}/>
            </linearGradient>
          </defs>
          <CartesianGrid strokeDasharray="3 3" stroke="#fff" opacity={0.05} vertical={false} />
          <YAxis 
            domain={[0, 3]} 
            ticks={[0, 1, 2, 3]} 
            tickFormatter={(val) => {
              return {0: 'Awake', 1: 'Light', 2: 'Deep', 3: 'REM'}[val as 0|1|2|3] || '';
            }}
            axisLine={false} 
            tickLine={false} 
            tick={{fill: '#64748b', fontSize: 10}} 
            width={45}
            reversed
          />
          <XAxis dataKey="time" tick={{fill: '#64748b', fontSize: 10}} axisLine={false} tickLine={false} minTickGap={30} />
          <Tooltip content={<CustomDailyTooltip />} cursor={{ stroke: 'rgba(255,255,255,0.1)', strokeWidth: 2 }} />
          <Area type="stepAfter" dataKey="stage" stroke="#818cf8" strokeWidth={2} fillOpacity={1} fill="url(#colorStage)" />
          {isTracking && (
             <Area type="stepAfter" dataKey="realtimeStage" stroke="#10b981" strokeDasharray="5 5" strokeWidth={2} fillOpacity={1} fill="url(#colorRealtime)" isAnimationActive={false} />
          )}
          
          {DAILY_SNORE_EVENTS.map((snore, idx) => {
              const dataPoint = DAILY_SLEEP_DATA.find(d => d.time === snore.time);
              if (!dataPoint) return null;
              // Size dot based on duration/intensity
              const radius = Math.max(3, Math.min(snore.duration / 10, 8));
              
              let dotColor = "#fb7185";
              if (snore.stage === 'Light') dotColor = "#2dd4bf"; // teal-400
              else if (snore.stage === 'Deep') dotColor = "#60a5fa"; // blue-400
              else if (snore.stage === 'REM') dotColor = "#c084fc"; // purple-400

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
              )
          })}
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );

  const renderTrendChart = () => {
    const data = timeRange === 'weekly' ? WEEKLY_DATA : MONTHLY_DATA;
    return (
      <div className="h-64 w-full -ml-2 mt-6">
        <ResponsiveContainer width="100%" height="100%">
          <ComposedChart data={data} margin={{ top: 10, right: 0, left: -20, bottom: 0 }}>
            <CartesianGrid strokeDasharray="3 3" stroke="#fff" opacity={0.05} vertical={false}/>
            <XAxis dataKey="name" axisLine={false} tickLine={false} tick={{fill: '#64748b', fontSize: 10}} />
            <YAxis yAxisId="left" orientation="left" stroke="#818cf8" tick={{fontSize: 10, fill: '#64748b'}} axisLine={false} tickLine={false} />
            <YAxis yAxisId="right" orientation="right" stroke="#fb7185" tick={{fontSize: 10, fill: '#64748b'}} axisLine={false} tickLine={false} />
            <Tooltip content={<CustomWeeklyMonthlyTooltip />} cursor={{fill: 'rgba(255,255,255,0.05)'}} />
            <Legend wrapperStyle={{ fontSize: 10, color: '#94a3b8', paddingTop: '10px' }} iconType="circle" />
            <Bar yAxisId="left" dataKey="freq" fill="#818cf8" radius={[4, 4, 0, 0]} name="Freq (Events)" maxBarSize={30} />
            <Line yAxisId="right" type="monotone" dataKey="intensity" stroke="#fb7185" strokeWidth={2} dot={{r: 4, fill: '#fb7185', strokeWidth: 2, stroke: '#1e293b'}} name="Avg Intensity" />
            {timeRange === 'monthly' && <Line yAxisId="right" type="step" dataKey="durationMin" stroke="#a78bfa" strokeDasharray="3 3" strokeWidth={2} dot={false} name="Duration (min)" />}
          </ComposedChart>
        </ResponsiveContainer>
      </div>
    );
  };

  const CustomWeeklyMonthlyTooltip = WeeklyMonthlyTooltip; // alias for scope

  return (
    <div className="flex-1 p-6 overflow-y-auto pb-24 w-full relative z-10">
      {/* Export Modal */}
      {showExportModal && (
        <div className="absolute inset-0 z-50 bg-slate-950/80 backdrop-blur-sm flex items-center justify-center p-6">
           <div className="bg-slate-900 border border-slate-700 rounded-3xl p-6 w-full max-w-sm shadow-2xl relative">
              <button onClick={() => setShowExportModal(false)} className="absolute top-4 right-4 text-slate-400 hover:text-white transition-colors">
                 <X className="w-5 h-5" />
              </button>
              <h3 className="text-lg font-semibold text-white mb-2 flex items-center gap-2">
                 <Download className="w-5 h-5 text-indigo-400" /> Export Data
              </h3>
              <p className="text-slate-400 text-xs mb-6">Select a date range to export your sleep and snoring analytics as a CSV.</p>
              
              <div className="space-y-2 mb-6">
                 {['7days', '30days', 'custom'].map(r => (
                    <label key={r} className={cn("flex items-center gap-3 p-3 rounded-xl border cursor-pointer transition-colors", exportRange === r ? "bg-indigo-500/10 border-indigo-500/50" : "bg-slate-800/50 border-transparent hover:bg-slate-800")}>
                       <div className={cn("w-4 h-4 rounded-full border-2 flex items-center justify-center", exportRange === r ? "border-indigo-400" : "border-slate-500")}>
                         {exportRange === r && <div className="w-2 h-2 rounded-full bg-indigo-400" />}
                       </div>
                       <span className="text-sm font-medium text-slate-200">{r === '7days' ? 'Last 7 Days' : r === '30days' ? 'Last 30 Days' : 'Custom Date Range...'}</span>
                    </label>
                 ))}
              </div>
              <button onClick={handleExportCSV} className="w-full bg-indigo-600 hover:bg-indigo-500 text-white font-semibold py-3 rounded-xl transition-transform active:scale-95 shadow-lg shadow-indigo-600/20">
                Download CSV
              </button>
           </div>
        </div>
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

      {/* Time Range Toggle */}
      <div className="flex bg-slate-900/80 p-1 rounded-xl mb-6 shadow-inner ring-1 ring-white/10">
        {['daily', 'weekly', 'monthly'].map((rng) => (
          <button
            key={rng}
            onClick={() => setTimeRange(rng as any)}
            className={cn(
              "flex-1 py-1.5 text-xs font-semibold rounded-lg capitalize transition-all",
              timeRange === rng ? "bg-indigo-600 text-white shadow-md shadow-indigo-900/50" : "text-slate-400 hover:text-slate-200"
            )}
          >
            {rng}
          </button>
        ))}
      </div>

      {/* Nightly Summary (Only visible when daily is selected) */}
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
            <div className="text-2xl font-light text-white">70.7 <span className="text-sm font-normal text-orange-200/60">dB</span></div>
          </div>
        </div>
      )}

      <div className="bg-white/5 backdrop-blur-lg border border-white/10 rounded-3xl p-5 mb-6">
        {timeRange !== 'daily' && (
          <div className="mb-2 flex justify-between items-end">
            <div>
              <h3 className="text-indigo-200 text-sm mb-1">{timeRange === 'weekly' ? 'Avg Sleep' : 'Avg Sleep'}</h3>
              <div className="text-3xl font-light text-white">
                {timeRange === 'weekly' ? '6h 50m' : '7h 05m'}
              </div>
            </div>
            <div className="text-right">
               <h3 className="text-indigo-200 text-sm mb-1">{timeRange === 'weekly' ? 'Total Snores' : 'Total Snores'}</h3>
               <div className="text-3xl font-light text-rose-400">
                 {timeRange === 'weekly' ? '83' : '303'}
               </div>
            </div>
          </div>
        )}

        {timeRange === 'daily' ? renderDailyChart() : renderTrendChart()}
      </div>

      {/* HealthKit Status Card */}
      {healthKitEnabled && (
        <div className="bg-gradient-to-br from-emerald-950/40 to-slate-900/50 border border-emerald-500/20 rounded-2xl p-4 mb-6">
           <div className="flex items-center gap-2 mb-3 text-emerald-400">
              <HeartPulse className="w-5 h-5" />
              <h3 className="font-medium text-sm">HealthKit Daily Sync</h3>
           </div>
           <div className="space-y-2 text-xs text-slate-300">
              <div className="flex justify-between border-b border-emerald-500/10 pb-1">
                <span className="flex items-center gap-1.5"><History className="w-3.5 h-3.5 text-emerald-500"/> Sleep Stages Read</span>
                <span className="text-white font-medium">Synced at 7:00 AM</span>
              </div>
              <div className="flex justify-between border-b border-emerald-500/10 pb-1">
                <span className="flex items-center gap-1.5"><Activity className="w-3.5 h-3.5 text-rose-400"/> Snoring Duration Written</span>
                <span className="text-white font-medium">14 minutes</span>
              </div>
              <div className="flex justify-between">
                <span className="flex items-center gap-1.5"><Volume2 className="w-3.5 h-3.5 text-indigo-400"/> Avg Intensity Written</span>
                <span className="text-white font-medium">70.7 dB</span>
              </div>
              <p className="mt-3 text-emerald-300/80 leading-relaxed bg-emerald-950/50 p-2 rounded-lg">
                <strong>Insight:</strong> 65% of your snoring occurs during Light Sleep. Your snoring intensity has decreased by 5dB compared to last week.
              </p>
           </div>
        </div>
      )}

      {timeRange === 'daily' && (
        <>
          <h3 className="text-lg font-medium text-white mb-4 mt-2">Audio Clips by Stage</h3>
          <div className="space-y-8 pb-4">
            {['Light', 'Deep', 'REM'].map(stage => {
              const events = DAILY_SNORE_EVENTS.filter(e => e.stage === stage);
              if (events.length === 0) return null;
              
              const stageColors = {
                 'Light': 'bg-teal-400',
                 'Deep': 'bg-blue-400',
                 'REM': 'bg-purple-400'
              };

              return (
                <div key={stage} className="space-y-3">
                  <div className="flex items-center gap-2 mb-1">
                     <div className={cn("w-2 h-2 rounded-full shadow-[0_0_8px_currentColor] opacity-80", stageColors[stage as keyof typeof stageColors])} />
                     <h4 className="text-sm font-semibold text-slate-300 tracking-wide uppercase">{stage} Sleep</h4>
                  </div>
                  {events.map((event, i) => {
                    const idx = DAILY_SNORE_EVENTS.indexOf(event);
                    return (
                      <div key={idx} className="bg-slate-900/50 border border-slate-800 rounded-2xl p-4 transition-all">
                        <div className="flex items-center justify-between mb-3">
                          <div className="flex items-center gap-3">
                            <div className={cn(
                              "w-2 h-2 rounded-full", 
                              event.intensity >= 80 ? "bg-rose-500" : 
                              event.intensity >= 65 ? "bg-orange-500" : "bg-yellow-500"
                            )}></div>
                            <div>
                              <div className="text-white font-medium">{event.time}</div>
                              <div className="text-xs text-slate-400">{event.duration}s event</div>
                            </div>
                          </div>
                          <div className="flex items-center gap-3">
                            <span className="text-slate-300 font-mono text-xs bg-slate-800 px-2 py-1 rounded border border-slate-700">{event.intensity} dB</span>
                            <button 
                              onClick={() => togglePlay(idx)}
                              className={cn(
                                "w-10 h-10 rounded-full flex items-center justify-center transition-all",
                                playingId === idx ? "bg-indigo-500 shadow-lg shadow-indigo-500/40 text-white" : "bg-indigo-500/10 text-indigo-400 hover:bg-indigo-500/20"
                              )}
                            >
                              {playingId === idx ? <Square className="w-4 h-4 fill-current" /> : <Play className="w-4 h-4 fill-current ml-0.5" />}
                            </button>
                          </div>
                        </div>
                        {/* Audio Playback Progress Bar */}
                        <div className="h-1.5 bg-slate-800 rounded-full overflow-hidden relative">
                           <div className={cn(
                             "absolute inset-y-0 left-0 bg-indigo-500",
                             playingId === idx ? "w-full transition-all duration-[3000ms] ease-linear" : "w-0 transition-none"
                           )} />
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

function SettingsTab({ 
  thresholdDB, 
  setThresholdDB,
  healthKitEnabled,
  setHealthKitEnabled,
  sensitivity,
  setSensitivity
}: { 
  thresholdDB: number, 
  setThresholdDB: (val: number) => void,
  healthKitEnabled: boolean,
  setHealthKitEnabled: (val: boolean) => void,
  sensitivity: 'low' | 'medium' | 'high',
  setSensitivity: (val: 'low' | 'medium' | 'high') => void
}) {
  return (
    <div className="flex-1 p-6 w-full z-10 relative overflow-y-auto">
      <h2 className="text-2xl font-semibold mb-8 text-indigo-50">Settings</h2>
      
      <div className="space-y-6 pb-12">
        <div className="bg-white/5 backdrop-blur-lg border border-white/10 rounded-3xl overflow-hidden divide-y divide-white/10">
          
          {/* HealthKit Toggle */}
          <div className="p-5 flex items-center justify-between">
            <div className="flex items-center gap-3 flex-1">
              <HeartPulse className="text-rose-400 w-5 h-5 shrink-0" />
              <div>
                <div className="text-white font-medium">Apple HealthKit Sync</div>
                <div className="text-[11px] leading-tight text-indigo-200 mt-0.5 max-w-[200px]">Read Sleep Stages & write nightly Snoring Duration/Intensity.</div>
              </div>
            </div>
            <div 
              onClick={() => setHealthKitEnabled(!healthKitEnabled)}
              className={cn("w-12 h-6 rounded-full relative shadow-inner cursor-pointer shrink-0 transition-colors duration-300", healthKitEnabled ? "bg-green-500" : "bg-white/10")}
            >
              <div className={cn("absolute top-1 bottom-1 w-4 bg-white rounded-full shadow transition-all duration-300", healthKitEnabled ? "right-1" : "left-1")}></div>
            </div>
          </div>
          
          {/* Audiobooks Note */}
          <div className="p-5 flex items-center justify-between">
            <div className="flex items-center gap-3">
              <Smartphone className="text-blue-400 w-5 h-5" />
              <div>
                <div className="text-white font-medium">Background Mix</div>
                <div className="text-[11px] leading-tight text-indigo-200 mt-0.5 max-w-[200px]">AVAudioSession enabled. Play audible/music via AirPods while tracking.</div>
              </div>
            </div>
             <div className="flex items-center text-xs text-blue-300 gap-1 bg-blue-500/10 px-2 py-1 rounded-full">
               <CheckCircle2 className="w-3 h-3" /> Active
             </div>
          </div>
        </div>

        {/* Algorithm Settings */}
        <div className="flex items-center gap-2 ml-1 mt-8 mb-3">
          <SlidersHorizontal className="w-4 h-4 text-slate-400" />
          <h3 className="text-sm font-semibold text-slate-400 uppercase tracking-wider">Detection Details</h3>
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
             <div className="text-white font-medium mb-1">Algorithm Sensitivity</div>
             <div className="text-[11px] text-slate-400 mb-4 max-w-[260px]">
               Adjusts how easily the ML model classifies an audio spike as a "snore" based on frequency profiles.
             </div>
             
             <div className="flex p-1 bg-slate-800/50 rounded-xl border border-slate-700/50">
               {(['low', 'medium', 'high'] as const).map(level => (
                 <button
                   key={level}
                   onClick={() => setSensitivity(level)}
                   className={cn(
                     "flex-1 capitalize py-1.5 text-xs font-semibold rounded-lg transition-all",
                     sensitivity === level ? "bg-indigo-500 text-white shadow-lg" : "text-slate-400 hover:text-slate-200"
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
             <strong>How it works:</strong> The algorithm uses a simulated ML model on client-side FFT data. It checks if lower frequencies dominate (differentiating snoring from background noise). Tuning sensitivity adjusts the required acoustic signature strictness.
           </p>
        </div>
      </div>
    </div>
  );
}

// -- MAIN APP --

export default function App() {
  const [activeTab, setActiveTab] = useState<'record' | 'insights' | 'settings'>('record');
  
  // Shared global state for prototype simulating persistent device settings
  const [thresholdDB, setThresholdDB] = useState(60);
  const [healthKitEnabled, setHealthKitEnabled] = useState(true);
  const [sensitivity, setSensitivity] = useState<'low' | 'medium' | 'high'>('medium');
  const [hasSeenTutorial, setHasSeenTutorial] = useState(false);
  
  // Real-time tracking state
  const [isTracking, setIsTracking] = useState(false);
  const [liveSleepData, setLiveSleepData] = useState<{time: string, realtimeStage?: number}[]>([]);

  useEffect(() => {
    if (isTracking) {
      if (liveSleepData.length === 0) {
        const lastHist = DAILY_SLEEP_DATA[DAILY_SLEEP_DATA.length - 1];
        setLiveSleepData([{ time: lastHist.time, realtimeStage: lastHist.stage }]);
      }
      const interval = setInterval(() => {
         setLiveSleepData(prev => {
            const lastData = prev[prev.length - 1];
            const lastStage = lastData ? lastData.realtimeStage || 0 : 0;
            // simulate realistic stage changing
            let nextStage = lastStage;
            if (Math.random() > 0.7) {
               nextStage = Math.max(0, Math.min(3, lastStage + (Math.random() > 0.5 ? 1 : -1)));
            }
            const now = new Date();
            const timeStr = now.toLocaleTimeString([], {hour: '2-digit', minute:'2-digit', second:'2-digit'});
            return [...prev, { time: timeStr, realtimeStage: nextStage }];
         });
      }, 5000); // add a point every 5 seconds
      return () => clearInterval(interval);
    } else {
      setLiveSleepData([]);
    }
  }, [isTracking]);

  return (
    <div className="min-h-screen bg-slate-950 flex justify-center overflow-hidden font-sans selection:bg-indigo-500/30">
      {/* Mobile container constraint to simulate iPhone display */}
      <div className="w-full max-w-md h-[100dvh] bg-[#0a0f24] relative flex flex-col shadow-2xl overflow-hidden ring-1 ring-white/5">
        
        {!hasSeenTutorial && <OnboardingTutorial onComplete={() => setHasSeenTutorial(true)} />}
        
        {/* Decorative background blurs that react slightly to app state */}
        <div className="absolute top-[-10%] left-[-10%] w-3/4 h-1/2 bg-indigo-600/20 blur-[100px] rounded-full mix-blend-screen pointer-events-none"></div>
        <div className="absolute bottom-[-10%] right-[-10%] w-3/4 h-1/2 bg-rose-600/10 blur-[100px] rounded-full mix-blend-screen pointer-events-none"></div>

        {/* Content Area */}
        <main className="flex-1 flex flex-col overflow-hidden relative">
           <div className={cn("absolute inset-0 z-10 flex flex-col", activeTab === 'record' ? "" : "hidden")}>
             <RecordTab thresholdDB={thresholdDB} healthKitEnabled={healthKitEnabled} sensitivity={sensitivity} isTracking={isTracking} setIsTracking={setIsTracking} />
           </div>
           
           <div className={cn("absolute inset-0 z-10 flex flex-col", activeTab === 'insights' ? "" : "hidden")}>
             <InsightsTab healthKitEnabled={healthKitEnabled} isTracking={isTracking} liveSleepData={liveSleepData} />
           </div>

           <div className={cn("absolute inset-0 z-10 flex flex-col", activeTab === 'settings' ? "" : "hidden")}>
             <SettingsTab thresholdDB={thresholdDB} setThresholdDB={setThresholdDB} healthKitEnabled={healthKitEnabled} setHealthKitEnabled={setHealthKitEnabled} sensitivity={sensitivity} setSensitivity={setSensitivity} />
           </div>
        </main>

        {/* Bottom Navigation */}
        <nav className="h-20 bg-[#0a0f24]/90 backdrop-blur-2xl border-t border-white/10 flex items-center justify-around px-2 z-50 pb-safe shadow-[0_-10px_40px_rgba(0,0,0,0.5)]">
          <button 
            onClick={() => setActiveTab('record')}
            className={cn("flex flex-col items-center p-2 transition-colors", activeTab === 'record' ? 'text-white' : 'text-slate-500')}
          >
            <Moon className={cn("w-6 h-6 mb-1", activeTab === 'record' && "fill-indigo-500 text-indigo-500")} />
            <span className="text-[10px] font-medium tracking-wide">Sleep</span>
          </button>
          
          <button 
            onClick={() => setActiveTab('insights')}
            className={cn("flex flex-col items-center p-2 transition-colors", activeTab === 'insights' ? 'text-white' : 'text-slate-500')}
          >
            <BarChart2 className={cn("w-6 h-6 mb-1", activeTab === 'insights' && "text-indigo-400")} />
            <span className="text-[10px] font-medium tracking-wide">Analytics</span>
          </button>

          <button 
            onClick={() => setActiveTab('settings')}
            className={cn("flex flex-col items-center p-2 transition-colors", activeTab === 'settings' ? 'text-white' : 'text-slate-500')}
          >
            <Settings className={cn("w-6 h-6 mb-1", activeTab === 'settings' && "text-indigo-400")} />
            <span className="text-[10px] font-medium tracking-wide">Settings</span>
          </button>
        </nav>
      </div>
    </div>
  );
}
