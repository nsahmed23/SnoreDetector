# ADR 0001: Use SwiftUI for the iOS app

## Status

Accepted.

## Context

SnoreGuard ships as a native iOS app targeting iOS 17 and newer. The app
needs:

- Tight integration with HealthKit (`HKHealthStore`), `AVAudioEngine`,
  Sign in with Apple, and Keychain.
- A live-updating recording UI (real-time meter, ripple visualizer) that
  stays smooth at 60 fps.
- App Store distribution under our team account, with TestFlight as the
  primary beta channel.
- Single-engineer development velocity — no second engineer to keep a
  cross-platform abstraction in sync.

## Decision

Build the iOS app in **SwiftUI** with an `@Observable` view-model layer.
The root container is a three-tab `TabView` (`ContentView`); features
live under `Features/Record`, `Features/Insights`, and `Features/Settings`.

## Consequences

**Positive**:

- Native rendering, native gestures, native dark-mode support — the UI
  feels like an iOS app because it is one.
- HealthKit, Sign in with Apple, and Swift Charts have first-class
  Swift APIs; no bridge layer needed.
- `@Observable` + `Combine`/`AsyncStream` give us a clean reactive path
  for live audio data without bespoke state machinery.
- Easy access to background audio entitlement, lock-screen recording,
  and route-change handling.

**Negative**:

- Locked to iOS 17+ to keep `@Observable` (replaces `ObservableObject`).
  Older iOS versions are out of scope.
- No code share with a future Android port; that is an explicit
  non-goal (see `PRODUCT_SPEC.md`).
- SwiftUI animation timing on very old hardware can require
  `TimelineView` workarounds, but our minimum supported devices ship
  with iOS 17, so this is bounded.

**Neutral**:

- TestFlight remains the beta channel; no third-party distribution
  layer.

## Alternatives considered

- **UIKit**. Rejected: more verbose for the same UI surface, and the
  recording / insights views are inherently declarative
  (live volume bound to a meter view, segmented controls bound to
  detector settings). UIKit would add significant boilerplate without
  gaining us anything; SwiftUI's `@Observable` model is a better fit.
- **React Native**. Rejected: introduces a JS runtime on the audio path,
  fights us on background audio entitlements, requires native modules
  for HealthKit and Sign in with Apple anyway, and the App Store review
  bar for "wellness app records audio while screen is locked" is much
  easier to justify with a fully-native binary. Cross-platform is a
  non-goal.
