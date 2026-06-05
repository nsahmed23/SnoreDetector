## 2026-06-05 - Prevent Web Audio API Teardowns in 60fps Loops
**Learning:** Tying high-frequency state (like `isCurrentlySnoring`) directly to `useEffect` dependency arrays in Web Audio API loops causes expensive cascading teardowns and restarts of the MediaStream and AudioContext.
**Action:** Track this state with a `useRef` synchronously alongside the `useState` setter to break the dependency chain and prevent race conditions, avoiding expensive teardowns of long-lived objects.
