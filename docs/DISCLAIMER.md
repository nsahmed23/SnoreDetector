# Disclaimer & Privacy

## Not a medical device

SnoreGuard is a **wellness prototype**. It is not a medical device. It has not been evaluated, cleared, or approved by the U.S. Food and Drug Administration (FDA), the U.K. Medicines and Healthcare products Regulatory Agency (MHRA), the European Union (CE marking under the Medical Device Regulation), or any other regulatory authority. It is not classified, certified, or registered as a medical device in any jurisdiction.

## No diagnosis

SnoreGuard does **not** diagnose, screen for, treat, monitor, or rule out any disease or condition, including but not limited to:

- Obstructive sleep apnea (OSA)
- Central sleep apnea
- Upper airway resistance syndrome (UARS)
- Insomnia
- Any other sleep disorder

If you are concerned about your snoring, breathing during sleep, daytime sleepiness, or any other health symptom, **consult a qualified physician**. Do not delay seeking medical advice because of anything you read in this app.

## Limitations of the heuristic detector

The detection algorithm is a **heuristic**, not a machine-learning model. It works by:

1. Computing FFT magnitudes on short audio frames captured from the device microphone.
2. Comparing the energy in lower frequency bins against the energy in higher frequency bins.
3. Flagging a frame as snore-like when the volume crosses your configured threshold AND the lower frequencies dominate the spectrum.
4. Counting an "event" only when this condition holds for ~250 ms.

Known failure modes include:

- Deep-voiced speech in the bedroom can be mistaken for snoring.
- Steady low-frequency noise (HVAC, fans, traffic) can produce false positives at the highest sensitivity setting.
- The dB readings are derived from FFT byte magnitudes; they are **not calibrated SPL** and should not be compared to medical-grade equipment.
- The algorithm has not been validated against polysomnography (PSG), the clinical gold standard for sleep monitoring.

The volume threshold and detector sensitivity controls in Settings exist to let you tune around your environment. They do not change the underlying limitations.

## Microphone & privacy

- The app needs microphone access to detect snoring. Audio is processed **on-device** using the heuristic detector.
- Short audio clips around detected events are saved to local storage so you can review them. Retention is governed by your settings; you can delete clips at any time.
- No audio is uploaded to any server unless you explicitly export or share it.
- Cloud sync (when enabled) transmits **summary statistics and metadata only** by default — never raw audio.

## HealthKit data handling

When you enable HealthKit sync, the app writes one `environmentalAudioExposure` HKQuantitySample per detected snore event, with the average noise level for that event window. **The dB values are uncalibrated relative magnitudes from a heuristic detector — they are not SPL meter readings, and the metadata on each sample makes that explicit.** The app also requests read access to `sleepAnalysis` (granted but unused in this build; phase-5 analytics will correlate snore events with sleep state). You can revoke either scope at any time from iOS Settings → Health → Data Access & Devices → SnoreGuard.

## "As-is"

This software is provided **"as is", without warranty of any kind**, express or implied, including but not limited to the warranties of merchantability, fitness for a particular purpose, and non-infringement. In no event shall the authors or copyright holders be liable for any claim, damages, or other liability, whether in an action of contract, tort, or otherwise, arising from, out of, or in connection with the software or the use or other dealings in the software.

## Contact

If you have a question about this disclaimer or about how the app handles your data, open an issue on the project's GitHub repository.
