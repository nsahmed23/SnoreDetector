## 2024-07-06 - [Web Audio API Re-initialization Bottleneck]
**Learning:** Placing high-frequency state variables (like `isCurrentlySnoring`) in the `useEffect` dependency array of a Web Audio API setup causes the entire media stream to be torn down and restarted repeatedly. This causes massive audio dropouts and performance stuttering, especially inside a 60fps loop.
**Action:** Use a `useRef` to track high-frequency mutable state within the `requestAnimationFrame` loop, updating the ref synchronously alongside the React state setter to avoid race conditions, and remove the state from the `useEffect` dependencies.

## 2024-07-06 - [Audio Processing Loop Fusion]
**Learning:** Iterating over the same audio frequency array multiple times (e.g., once for average, once for low/high frequency split) inside a `requestAnimationFrame` 60fps loop adds redundant computational overhead.
**Action:** Apply loop fusion to combine multiple passes over data arrays in high-frequency audio processing loops to improve performance and reduce overhead.

## 2024-07-06 - [React.memo in 60fps Loops]
**Learning:** In a 60fps loop (like tracking audio volume with requestAnimationFrame), static or infrequently-updating children of fast-updating components (like LiveStatsCard inside RecordTab) should be wrapped in React.memo() to prevent unnecessary cascading re-renders. Conversely, avoid using React.memo() on components that receive continuously changing props (like the volume visualizer itself), as it adds overhead without preventing re-renders.
**Action:** Wrap LiveStatsCard in React.memo().
