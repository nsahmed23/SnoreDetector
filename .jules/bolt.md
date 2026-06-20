## 2025-02-20 - High-frequency Effect Dependencies Anti-Pattern
**Learning:** Including high-frequency mutable state like `isCurrentlySnoring` in the dependency array of the main `useEffect` that handles the Web Audio API stream caused cascading teardowns and restarts of the stream.
**Action:** Use `useRef` for tracking state synchronously alongside the state setter inside closures, avoiding passing high-frequency toggled state as effect dependencies. Also apply React.memo to static children of fast-updating components to prevent continuous expensive re-renders.
