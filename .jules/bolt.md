## 2023-10-27 - [60fps React Context & Re-render Prevention]
 **Learning:** The frontend heavily relies on a 60fps `requestAnimationFrame` audio processing loop. While state inside closures using `useRef` avoids constant teardowns, any component state changes at 60fps (like `volume`) trigger re-renders down the tree. React doesn't auto-bailout components whose props haven't changed if they are deeply nested without memoization.
 **Action:** Always look for and apply `React.memo()` to static or infrequently-updating sub-components (like `LiveStatsCard`) that sit alongside 60fps-updating components (like `VolumeVisualizer`) to prevent wasteful DOM diffing.

## 2023-10-27 - [Audio Data Loop Fusion]
 **Learning:** In high-frequency 60fps audio processing loops (like `useAudioMonitor.ts`), splitting arrays into separate processing steps inside `requestAnimationFrame` creates unnecessary CPU overhead, even with fast array iterations.
 **Action:** Apply loop fusion to combine multiple passes over data arrays into a single iteration, particularly when calculating multiple heuristic properties simultaneously (like total average and low-frequency dominance).
