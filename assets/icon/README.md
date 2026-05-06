# App icon

The 1024×1024 master is `icon-1024.png`. Apple's xcassets pipeline downscales to:

- 60pt @1x, @2x, @3x (iPhone)
- 76pt @1x, @2x (iPad)
- 83.5pt @2x (iPad Pro)

`build-icon-set.sh` handles the downscaling on a Mac with `sips`. Run it after editing the master.

## Design choice

A stylized "zZ" superimposed on a sound-wave silhouette, both rendered in the app's accent color (a cool indigo from `Assets.xcassets/AccentColor.colorset` — `#4D6FEF` in light mode, `#80A6F4` in dark mode). The wave shape echoes the level meter on the Record tab. The "zZ" is a universally legible "sleep" marker; the wave is the audio-monitoring story.

No copyrighted imagery, no third-party fonts. The "z" glyphs are drawn paths, not type. The wave is a deterministic procedural curve.

The master is 1024×1024 RGBA PNG, sRGB, 8-bit.
