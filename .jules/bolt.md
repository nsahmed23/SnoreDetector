## 2024-05-13 - [Performance Optimization]
**Learning:** 60fps audio loops inside React can trigger excessive re-renders. `requestAnimationFrame` driving state changes causes the parent component to re-render constantly.
**Action:** Always wrap static or infrequently changing child components inside fast-updating parents with `React.memo()`. Optimize inner loops inside high-frequency functions.
