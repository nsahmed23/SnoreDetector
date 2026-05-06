# SnoreGuard (SnoreDetector)

[![Rust core](https://github.com/nsahmed23/SnoreDetector/actions/workflows/rust.yml/badge.svg)](https://github.com/nsahmed23/SnoreDetector/actions/workflows/rust.yml)
[![Go backend](https://github.com/nsahmed23/SnoreDetector/actions/workflows/go.yml/badge.svg)](https://github.com/nsahmed23/SnoreDetector/actions/workflows/go.yml)
[![Go integration](https://github.com/nsahmed23/SnoreDetector/actions/workflows/integration.yml/badge.svg)](https://github.com/nsahmed23/SnoreDetector/actions/workflows/integration.yml)
[![Web prototype](https://github.com/nsahmed23/SnoreDetector/actions/workflows/web.yml/badge.svg)](https://github.com/nsahmed23/SnoreDetector/actions/workflows/web.yml)
[![iOS app](https://github.com/nsahmed23/SnoreDetector/actions/workflows/ios.yml/badge.svg)](https://github.com/nsahmed23/SnoreDetector/actions/workflows/ios.yml)

A smart, mobile-first snore monitoring application prototype built with React, TypeScript, and Tailwind CSS. SnoreGuard intelligently tracks your snoring while ignoring background noises like audiobooks or fans, integrates with Apple HealthKit, and provides rich insights into your sleep habits.

## 🌟 Features

* **Smart Snore Detection**: Uses real-time audio analysis (FFT) with adjustable volume thresholds and sensitivity levels. Designed to differentiate between actual snoring and background noise (like Audible or Spotify playing on AirPods).
* **Apple HealthKit Sync**: Mocks the capability to read sleep stages (Awake, Light, Deep, REM) and writes the total duration and average intensity of nightly snoring events back to Apple Health.
* **Live Sleep & Audio Monitoring**: Features a beautiful, interactive recording interface with real-time decibel meters, duration tracking, and live sleep stage plotting on the daily chart.
* **Rich Analytics & Insights**: 
  * View daily, weekly, and monthly trends using `recharts`.
  * Snoring events are color-coded and mapped directly over your sleep stages to show exactly when you snore the most.
* **Audio Playback**: Review audio clips of your snoring events, neatly categorized by the sleep stage they occurred in.
* **Data Export**: Export your snoring records to a CSV file (Last 7 days, 30 days, or custom ranges) for personal records or to share with a doctor.
* **Interactive Onboarding**: A brief, polished tutorial welcomes new users and explains core capabilities.

## 🚀 Getting Started

### Prerequisites
Make sure you have Node.js and npm installed.

### Installation

1. Clone the repository:
   ```bash
   git clone https://github.com/nsahmed23/SnoreDetector.git
   cd SnoreDetector
   ```

2. Install dependencies:
   ```bash
   npm install
   ```

3. Start the development server:
   ```bash
   npm run dev
   ```

4. Open your browser and navigate to `http://localhost:3000`. To experience the mobile-first design best, view it on a mobile device or toggle your browser's developer tools to mobile view.

## 🛠 Tech Stack

* **Frontend Framework**: React 19, TypeScript, Vite
* **Styling**: Tailwind CSS v4, Lucide React (Icons)
* **Data Visualization**: Recharts
* **Audio Processing**: Web Audio API (AudioContext interface for real-time frequency analysis)

## 📱 iOS Context Note

While this is a web-based prototype, it is designed with iOS mechanics in mind:
* The recording functionality assumes the use of `AVAudioSession` with the `mixWithOthers` category on a native iOS build, allowing users to listen to audiobooks via AirPods while the app silently monitors background audio using the microphone.
