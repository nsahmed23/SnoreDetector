## 2025-03-08 - [Frontend Loop Fusion]
**Learning:** In high-frequency React/Vite audio processing loops (like `requestAnimationFrame` at 60fps in `useAudioMonitor.ts`), consecutive array iterations cause redundant CPU overhead.
**Action:** Apply loop fusion to merge multiple passes over data arrays (e.g. volume averaging and frequency separation) into a single pass to save critical milliseconds per frame.
