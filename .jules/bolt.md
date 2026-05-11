## 2024-05-11 - React 60fps Optimization Pattern & Array Traversal
**Learning:** In audio processing flows (like Web Audio API's `requestAnimationFrame` loop), `dataArray` traversals happen 60 times a second. Also, parent components re-rendering at 60fps (for visualizers) will indiscriminately re-render children unless they are memoized.
**Action:** Always combine array operations inside `requestAnimationFrame` loops (reducing O(n*k) to O(n)). Always wrap static or infrequently-updating children of 60fps components in `React.memo()` to prevent cascading renders.
