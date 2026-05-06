# Launch screen

iOS 14+ wants a `LaunchScreen.storyboard` rather than a static PNG, but you can use a single image as the storyboard's background. The image lives at `launch-1284x2778.png` (Apple's largest 6.7" portrait, used at every size by aspect-fit).

## Design choice

A 60% darkened version of `../icon/icon-1024.png` centered on a deep-indigo gradient. No text — the launch screen is a sub-second glimpse, words won't read.

## Build

`build-launch-placeholder.py` generates the placeholder PNG. Real design is a Mac-side polish step.
