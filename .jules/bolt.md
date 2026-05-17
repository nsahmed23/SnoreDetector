## 2024-05-18 - Memoizing Children of 60fps Render Loops
**Learning:** In audio/video apps where a parent component uses `requestAnimationFrame` to update state rapidly (e.g., a volume visualizer at 60fps), any static or infrequently-updating child components will also re-render 60 times a second. This causes cascading performance degradation.
**Action:** Always wrap static or infrequently-updating children of fast-updating components in `React.memo()`. This stops the render cascade at the boundary, saving CPU cycles for the fast path without premature optimization.
