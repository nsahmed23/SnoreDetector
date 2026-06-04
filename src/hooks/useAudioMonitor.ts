import { useState, useEffect, useRef } from 'react';
import type { Sensitivity } from '../types';

export function useAudioMonitor(
  isTracking: boolean,
  thresholdDB: number,
  sensitivity: Sensitivity,
) {
  const [volume, setVolume] = useState(0);
  const [sessionSnoreCount, setSessionSnoreCount] = useState(0);
  const [sessionAvgIntensity, setSessionAvgIntensity] = useState(0);
  const [sessionTotalDuration, setSessionTotalDuration] = useState(0);
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
            // Heuristic: snoring concentrates energy in the lower portion of the spectrum.
            // Compare lower-quarter bins against the rest to separate snoring from broadband noise.
            // Optimization: loop fusion to combine sum and frequency band calculations
            let sum = 0;
            let lowFreqSum = 0;
            let highFreqSum = 0;
            const midPoint = Math.floor(dataArray.length / 4);

            for (let i = 0; i < dataArray.length; i++) {
              const val = dataArray[i];
              sum += val;
              if (i < midPoint) lowFreqSum += val;
              else highFreqSum += val;
            }
            const avg = sum / dataArray.length;

            // Sensitivity multiplier tunes the heuristic's low-frequency dominance ratio.
            const multiplier = sensitivity === 'low' ? 2.0 : sensitivity === 'high' ? 1.2 : 1.5;
            const isLowFreqDominant = lowFreqSum > highFreqSum * multiplier;

            // Map FFT byte-magnitude average to an approximate dB scale (prototype heuristic, not calibrated SPL).
            const simulatedDb = Math.min(Math.max((avg / 255) * 110, 30), 100);

            setVolume(simulatedDb);

            if (simulatedDb >= thresholdDB && isLowFreqDominant) {
              activeSnoreFrames.current++;
              activeSnoreIntensities.current.push(simulatedDb);
              if (activeSnoreFrames.current > 15 && !isCurrentlySnoring) {
                setIsCurrentlySnoring(true);
              }
            } else {
              if (isCurrentlySnoring) {
                setIsCurrentlySnoring(false);
                setSessionSnoreCount(c => c + 1);

                const durationSecs = activeSnoreFrames.current / 60;
                setSessionTotalDuration(d => d + durationSecs);

                const eventAvg =
                  activeSnoreIntensities.current.reduce((a, b) => a + b, 0) /
                  activeSnoreIntensities.current.length;
                setSessionAvgIntensity(prev => (prev === 0 ? eventAvg : (prev + eventAvg) / 2));
              }
              activeSnoreFrames.current = 0;
              activeSnoreIntensities.current = [];
            }

            requestRef.current = requestAnimationFrame(updateVolume);
          };
          updateVolume();
        })
        .catch((err) => {
          console.error('Microphone access denied.', err);
          alert('Microphone access is required to track snoring in this prototype.');
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
  }, [isTracking, thresholdDB, isCurrentlySnoring, sensitivity]);

  return {
    volume,
    sessionSnoreCount,
    sessionAvgIntensity,
    sessionTotalDuration,
    isCurrentlySnoring,
  };
}
