
## 2024-05-18 - [Fix Expensive Re-renders with Audio Context Tear Down]
**Learning:** [Including high-frequency changing state inside `useEffect` dependencies for Web Audio stream setup causes costly continuous teardowns and restarts, disrupting audio monitoring.]
**Action:** [Use `useRef` to maintain mutable state safely within the closure without re-triggering the `useEffect` on every state change.]
