## 2025-06-25 - Prevent cascading Web Audio API teardowns in React
**Learning:** Adding frequently-updating mutable state (like `isCurrentlySnoring` which changes often based on a 60fps audio processing loop) to a `useEffect` dependency array that initializes `AudioContext` and `MediaStream` causes disastrous performance. React tears down and restarts the audio stream every time the snoring status toggles.
**Action:** Use `useRef` to track fast-changing values that need to be read inside closures but shouldn't trigger re-initialization of expensive external API connections.
