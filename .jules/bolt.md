## 2024-05-18 - [Optimizing Hot Loops in React Audio Hooks]
**Learning:** React hooks driven by `requestAnimationFrame` (running 60 times a second) are extremely sensitive to array traversals and component re-renders. A seemingly harmless double loop for frequency analysis can add up, and child components receiving frequent prop updates will cause excessive DOM diffing unless memoized.
**Action:** Always combine O(N) loops where possible inside `requestAnimationFrame` callbacks, and proactively use `React.memo` on display components that sit beneath rapidly updating audio or sensor state.
