## 2024-05-24 - React.memo on children of requestAnimationFrame-driven parents
**Learning:** Components that render at 60 FPS (like RecordTab driven by `requestAnimationFrame` volume updates) will violently re-render all their children unnecessarily.
**Action:** Always wrap static or infrequently updating child components of highly volatile parents in `React.memo()`, as was done with `LiveStatsCard`.

## 2024-05-24 - Array Iteration within requestAnimationFrame
**Learning:** Even simple loops inside a `requestAnimationFrame` handler can become a CPU bottleneck if called 60 times a second on large arrays (like FFT bins).
**Action:** Combine multiple iterations over the same array into a single pass when inside high-frequency animation loops.
