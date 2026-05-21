## 2024-05-24 - [Loop Fusion in 60fps Audio Loop]
**Learning:** In high-frequency React frontend audio processing loops (like inside `requestAnimationFrame`), applying loop fusion to combine multiple passes over data arrays avoids redundant iterations and improves performance. This directly combats CPU overhead within the 60fps cycle.
**Action:** Always check high-frequency processing loops (like `requestAnimationFrame` or Web Audio API `onaudioprocess`) for redundant array passes and combine them into a single loop.
