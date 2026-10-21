# KubeNova launch film (Egyptian Arabic voice-over)

Motion graphics timed to the Cartesia voice-over (`docs/video/script/`).

- `index.html` — the animation (GSAP timeline + canvas effects). `window.seek(t)` draws any moment.
- `cues.md` — voice cue → visual mapping; timings come from Whisper transcription of the voice-over.
- `render.cjs` — renders frames with Playwright (`node render.cjs frames <start> <end> 60 <dir>`), or preview stills.
- `sound.py` — synthesizes the music bed and sound effects (numpy/scipy), ducked under the voice.

Final mix: voice + bed (−4.5 dB), loudness-normalised to −14 LUFS, true peak −1.5 dBFS.
The rendered MP4 (1080p60) is not committed because of its size.
