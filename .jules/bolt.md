## 2025-02-28 - Expensive AudioContext teardowns due to useEffect dependency

**Learning:** Including frequently updating state (like `isCurrentlySnoring`) in a `useEffect` dependency array that manages an `AudioContext` and `MediaStream` causes disastrous performance issues. Every time the state toggles, it stops the microphone stream, closes the AudioContext, and requests microphone access again. This causes audio stuttering and massive CPU spikes.

**Action:** High-frequency mutable state inside closures that run within `requestAnimationFrame` should be tracked using a `useRef` to maintain state synchronously inside the loop. The ref should be updated alongside the state setter to prevent race conditions and avoid including the state in the `useEffect` dependency array, preventing expensive cascading teardowns.
