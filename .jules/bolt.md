
## 2024-05-24 - React State inside Fast-Loop Web Audio API `useEffect`
**Learning:** Including fast-updating React state variables (like a boolean indicating current snoring status) in the dependency array of a `useEffect` that initializes expensive Web APIs (like `AudioContext` and `getUserMedia`) causes devastating performance issues. The entire API stream gets torn down and rebuilt continuously inside a 60fps `requestAnimationFrame` loop whenever the state toggles, severely degrading performance.
**Action:** Always use a `useRef` to track fast-changing synchronous state inside an active event/animation loop, and ONLY call React state setters for UI rendering. Do NOT include the state variable in the main loop setup's dependency array.
