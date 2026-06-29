## 2024-06-03 - React useEffect Teardown on High-Frequency State Change
**Learning:** Adding high-frequency state updates (like `isCurrentlySnoring`) to a `useEffect` dependency array that manages hardware connections (Web Audio API) causes continuous, expensive teardowns and recreations of streams.
**Action:** Use `useRef` to track fast-changing state values inside the effect synchronously, bypassing the dependency array and preserving the connection lifecycle.
