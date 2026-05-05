import { useState, useEffect } from 'react';
import { Moon, BarChart2, Settings } from 'lucide-react';
import { cn } from './lib/cn';
import { OnboardingTutorial } from './components/OnboardingTutorial';
import { RecordTab } from './components/RecordTab';
import { InsightsTab } from './components/InsightsTab';
import { SettingsTab } from './components/SettingsTab';
import { DAILY_SLEEP_DATA } from './data/mockData';
import type { Sensitivity, SleepDatum, Tab } from './types';

export default function App() {
  const [activeTab, setActiveTab] = useState<Tab>('record');

  const [thresholdDB, setThresholdDB] = useState(60);
  const [healthKitEnabled, setHealthKitEnabled] = useState(true);
  const [sensitivity, setSensitivity] = useState<Sensitivity>('medium');
  const [hasSeenTutorial, setHasSeenTutorial] = useState(false);

  const [isTracking, setIsTracking] = useState(false);
  const [liveSleepData, setLiveSleepData] = useState<SleepDatum[]>([]);

  useEffect(() => {
    if (isTracking) {
      if (liveSleepData.length === 0) {
        const lastHist = DAILY_SLEEP_DATA[DAILY_SLEEP_DATA.length - 1];
        setLiveSleepData([{ time: lastHist.time, stage: lastHist.stage, realtimeStage: lastHist.stage }]);
      }
      const interval = setInterval(() => {
        setLiveSleepData(prev => {
          const lastData = prev[prev.length - 1];
          const lastStage = lastData ? lastData.realtimeStage ?? 0 : 0;
          let nextStage = lastStage;
          if (Math.random() > 0.7) {
            nextStage = Math.max(0, Math.min(3, lastStage + (Math.random() > 0.5 ? 1 : -1)));
          }
          const now = new Date();
          const timeStr = now.toLocaleTimeString([], {
            hour: '2-digit',
            minute: '2-digit',
            second: '2-digit',
          });
          return [...prev, { time: timeStr, stage: nextStage, realtimeStage: nextStage }];
        });
      }, 5000);
      return () => clearInterval(interval);
    } else {
      setLiveSleepData([]);
    }
  }, [isTracking]);

  return (
    <div className="min-h-screen bg-slate-950 flex justify-center overflow-hidden font-sans selection:bg-indigo-500/30">
      <div className="w-full max-w-md h-[100dvh] bg-[#0a0f24] relative flex flex-col shadow-2xl overflow-hidden ring-1 ring-white/5">
        {!hasSeenTutorial && <OnboardingTutorial onComplete={() => setHasSeenTutorial(true)} />}

        <div className="absolute top-[-10%] left-[-10%] w-3/4 h-1/2 bg-indigo-600/20 blur-[100px] rounded-full mix-blend-screen pointer-events-none" />
        <div className="absolute bottom-[-10%] right-[-10%] w-3/4 h-1/2 bg-rose-600/10 blur-[100px] rounded-full mix-blend-screen pointer-events-none" />

        <main className="flex-1 flex flex-col overflow-hidden relative">
          <div className={cn('absolute inset-0 z-10 flex flex-col', activeTab === 'record' ? '' : 'hidden')}>
            <RecordTab
              thresholdDB={thresholdDB}
              healthKitEnabled={healthKitEnabled}
              sensitivity={sensitivity}
              isTracking={isTracking}
              setIsTracking={setIsTracking}
            />
          </div>

          <div className={cn('absolute inset-0 z-10 flex flex-col', activeTab === 'insights' ? '' : 'hidden')}>
            <InsightsTab
              healthKitEnabled={healthKitEnabled}
              isTracking={isTracking}
              liveSleepData={liveSleepData}
            />
          </div>

          <div className={cn('absolute inset-0 z-10 flex flex-col', activeTab === 'settings' ? '' : 'hidden')}>
            <SettingsTab
              thresholdDB={thresholdDB}
              setThresholdDB={setThresholdDB}
              healthKitEnabled={healthKitEnabled}
              setHealthKitEnabled={setHealthKitEnabled}
              sensitivity={sensitivity}
              setSensitivity={setSensitivity}
            />
          </div>
        </main>

        <nav className="h-20 bg-[#0a0f24]/90 backdrop-blur-2xl border-t border-white/10 flex items-center justify-around px-2 z-50 pb-safe shadow-[0_-10px_40px_rgba(0,0,0,0.5)]">
          <button
            onClick={() => setActiveTab('record')}
            className={cn(
              'flex flex-col items-center p-2 transition-colors',
              activeTab === 'record' ? 'text-white' : 'text-slate-500',
            )}
          >
            <Moon className={cn('w-6 h-6 mb-1', activeTab === 'record' && 'fill-indigo-500 text-indigo-500')} />
            <span className="text-[10px] font-medium tracking-wide">Sleep</span>
          </button>

          <button
            onClick={() => setActiveTab('insights')}
            className={cn(
              'flex flex-col items-center p-2 transition-colors',
              activeTab === 'insights' ? 'text-white' : 'text-slate-500',
            )}
          >
            <BarChart2 className={cn('w-6 h-6 mb-1', activeTab === 'insights' && 'text-indigo-400')} />
            <span className="text-[10px] font-medium tracking-wide">Analytics</span>
          </button>

          <button
            onClick={() => setActiveTab('settings')}
            className={cn(
              'flex flex-col items-center p-2 transition-colors',
              activeTab === 'settings' ? 'text-white' : 'text-slate-500',
            )}
          >
            <Settings className={cn('w-6 h-6 mb-1', activeTab === 'settings' && 'text-indigo-400')} />
            <span className="text-[10px] font-medium tracking-wide">Settings</span>
          </button>
        </nav>
      </div>
    </div>
  );
}
