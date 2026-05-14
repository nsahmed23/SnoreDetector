## 2024-05-14 - React.memo on 60fps audio children
**Learning:** In a heavily requestAnimationFrame-driven loop (like useAudioMonitor feeding 60fps states to React components), child components that don't need to re-render every frame become severe bottlenecks. Batching O(n) array loops within that 60fps loop is critical since the loop runs very frequently.
**Action:** Always verify if child components in a fast-loop environment can be wrapped with `React.memo()`. Also, combine multiple array passes into a single iteration where possible.
