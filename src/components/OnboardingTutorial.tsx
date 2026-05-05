import { useState } from 'react';
import { Sparkles, HeartPulse, BarChart2, ChevronRight } from 'lucide-react';
import { cn } from '../lib/cn';

export function OnboardingTutorial({ onComplete }: { onComplete: () => void }) {
  const [step, setStep] = useState(0);

  const steps = [
    {
      icon: <Sparkles className="w-12 h-12 text-indigo-400 mb-6" />,
      title: 'Smart Snore Detection',
      desc: 'SnoreGuard uses a frequency-aware heuristic to distinguish your snoring from audiobooks, pets, or city noise.',
    },
    {
      icon: <HeartPulse className="w-12 h-12 text-emerald-400 mb-6" />,
      title: 'HealthKit Integration',
      desc: 'Connect seamlessly to Apple Health to correlate snoring events directly with your sleep stages.',
    },
    {
      icon: <BarChart2 className="w-12 h-12 text-purple-400 mb-6" />,
      title: 'Interactive Insights',
      desc: 'Play back recorded audio clips and view detailed charts to understand your sleep health trends.',
    },
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
            <div
              key={i}
              className={cn(
                'h-1.5 rounded-full transition-all duration-300',
                step === i ? 'w-6 bg-indigo-500' : 'w-1.5 bg-slate-700',
              )}
            />
          ))}
        </div>
        <button
          onClick={() => (step < steps.length - 1 ? setStep(s => s + 1) : onComplete())}
          className="bg-indigo-600 hover:bg-indigo-500 text-white px-6 py-2.5 rounded-full font-semibold flex items-center gap-2 transition-transform active:scale-95 shadow-lg shadow-indigo-600/20"
        >
          {step < steps.length - 1 ? 'Next' : 'Get Started'} <ChevronRight className="w-4 h-4" />
        </button>
      </div>
    </div>
  );
}
