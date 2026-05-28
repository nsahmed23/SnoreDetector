## 2025-05-28 - Optimize High-Frequency Audio Loop Re-renders
**Learning:** Found a specific anti-pattern: un-fused data array loops in the 60fps `requestAnimationFrame` audio processing loop (`useAudioMonitor.ts`) and missing memoization on static children (`LiveStatsCard`) causing cascading re-renders when parent states (`volume`) update continuously.
**Action:** Always fuse operations on frequency data arrays to improve cache locality and ensure static/infrequently updating children of 60fps states are wrapped in `React.memo()`.
