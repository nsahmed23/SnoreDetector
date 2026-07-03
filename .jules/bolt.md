## 2024-07-03 - Frontend React 60FPS Optimization
**Learning:** In audio-monitoring components driven by `requestAnimationFrame` (like `useAudioMonitor`), state changes propagate up to 60 times a second. Parent components like `RecordTab` updating rapidly will trigger re-renders on all children.
**Action:** Always wrap static or slow-changing children (e.g., `LiveStatsCard`) in `React.memo()` to avoid cascading re-renders during high-frequency loops. Additionally, loop fusion inside the frame loop keeps computational overhead low for better caching and CPU usage.
