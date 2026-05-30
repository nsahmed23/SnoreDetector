## 2025-03-01 - [Prevent cascading Web Audio stream teardowns via dependency array]
 **Learning:** Including highly volatile state variables like `isCurrentlySnoring` in a `useEffect` dependency array that manages a Web Audio stream will cause the stream to unnecessarily restart on every state change, severely degrading performance.
 **Action:** Use a `useRef` to track fast-changing state values inside the closure, updating the ref synchronously alongside the React state setter, and remove the state from the `useEffect` dependency array.

## 2025-03-01 - [Loop fusion for audio processing]
 **Learning:** In high-frequency loops, such as 60fps `requestAnimationFrame` audio processing, making multiple passes over arrays like `Uint8Array` wastes CPU cycles.
 **Action:** Use loop fusion to combine multiple iterations (e.g., summing array values and segregating low vs. high frequencies) into a single pass to improve cache locality and efficiency.