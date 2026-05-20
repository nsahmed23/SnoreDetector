## 2026-05-20 - Loop Fusion in 60fps Audio Processing
**Learning:** In high-frequency frontend audio processing loops (like `useAudioMonitor.ts` running at 60fps via `requestAnimationFrame`), iterating over `dataArray` multiple times for different calculations (e.g., total average vs low-frequency dominance) introduces unnecessary computational overhead and poor cache locality.
**Action:** Apply loop fusion to combine multiple passes over data arrays into a single iteration to minimize CPU load and improve performance in real-time loops.
