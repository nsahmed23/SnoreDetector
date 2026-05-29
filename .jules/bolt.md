## 2025-05-29 - [Web Audio Stream Teardowns & React Hooks]
**Learning:** Placing state variables used for UI (like `isCurrentlySnoring`) in the `useEffect` dependency array of a Web Audio stream initialization causes expensive cascading teardowns and restarts of the stream every time the state changes.
**Action:** Use a `useRef` to track high-frequency UI state within the effect closure without adding it to the dependency array, avoiding unnecessary stream restarts.

## 2025-05-29 - [High-Frequency Animation Loops]
**Learning:** In high-frequency frontend audio processing loops (like a 60fps `requestAnimationFrame`), doing multiple separate passes over the same data array (e.g., once for the average, once for sums) creates redundant iterations and overhead.
**Action:** Apply loop fusion to combine multiple passes into a single iteration to improve performance and reduce overhead within the animation cycle.
