## 2024-05-09 - [Prevented Unnecessary 60fps Re-renders]
**Learning:** The RecordTab component drives high-frequency re-renders via requestAnimationFrame for the volume visualizer, affecting all its child components.
**Action:** Always check high-frequency animation or real-time event loops (like audio monitoring) for children that receive static or rarely changing props, and wrap them in React.memo() to decouple them from the hot render path.
