# Synthesizes the music bed and sound effects for the KubeNova film, timed to
# the same cues as the visuals, and writes bed.wav (stereo, 48 kHz). The bed is
# ducked under the voice-over using the voice's own envelope.
import numpy as np, subprocess, wave
from scipy.signal import butter, sosfilt, sosfiltfilt

SR = 48000
DUR = 104.4
N = int(SR * DUR)
rng = np.random.default_rng(7)
t_all = np.arange(N) / SR

def lp(x, f, order=2): return sosfilt(butter(order, f, "low", fs=SR, output="sos"), x)
def hp(x, f, order=2): return sosfilt(butter(order, f, "high", fs=SR, output="sos"), x)
def bp(x, lo, hi, order=2): return sosfilt(butter(order, [lo, hi], "band", fs=SR, output="sos"), x)
def db(x): return 10 ** (x / 20)
def note(n): return 440.0 * 2 ** ((n - 69) / 12)  # midi -> Hz

L = np.zeros(N); Rr = np.zeros(N)
def add(sig, at, gain=1.0, pan=0.0):
    i = int(at * SR)
    if i >= N: return
    sig = sig[: N - i] * gain
    l, r = np.cos((pan + 1) * np.pi / 4), np.sin((pan + 1) * np.pi / 4)
    L[i:i + len(sig)] += sig * l * 1.414; Rr[i:i + len(sig)] += sig * r * 1.414

def env(n, a, d, s=1.0, r=None):
    e = np.ones(n) * s
    na = max(1, int(a * SR)); e[:na] = np.linspace(0, 1, na)
    if r:
        nr = min(n, int(r * SR)); e[-nr:] *= np.linspace(1, 0, nr)
    if d: e *= np.exp(-np.arange(n) / SR / d) if s == 0 else 1
    return e

# ---------------- music ----------------
music_L = np.zeros(N); music_R = np.zeros(N)
def madd(sig, at, gain=1.0, pan=0.0):
    i = int(at * SR)
    if i >= N: return
    sig = sig[: N - i] * gain
    l, r = np.cos((pan + 1) * np.pi / 4), np.sin((pan + 1) * np.pi / 4)
    music_L[i:i + len(sig)] += sig * l * 1.414; music_R[i:i + len(sig)] += sig * r * 1.414

def saw_pad(freqs, dur, cutoff=1600, att=.6, rel=1.2):
    n = int(dur * SR); tt = np.arange(n) / SR; out = np.zeros(n)
    for f in freqs:
        for det in (-0.12, 0, 0.12):
            ff = f * 2 ** (det / 12)
            ph = (tt * ff + rng.random()) % 1.0
            out += (2 * ph - 1) * .33
    out = lp(out / len(freqs), cutoff, 2)
    e = np.minimum(1, tt / att) * np.minimum(1, (dur - tt) / rel).clip(0, 1)
    return out * e

# A: dark drone 0 - 20.2
n = int(20.2 * SR); tt = np.arange(n) / SR
drone = (np.sin(2 * np.pi * 55 * tt) * .6 + np.sin(2 * np.pi * 82.4 * tt) * .25) * (1 + .15 * np.sin(2 * np.pi * .2 * tt))
drone = lp(drone + .08 * rng.standard_normal(n), 400) * np.minimum(1, tt / 3) * np.clip((20.2 - tt) / .3, 0, 1)
madd(drone, 0, .28)
shimmer = sum(np.sin(2 * np.pi * note(m) * tt + k) for k, m in enumerate([81, 88, 93])) * (0.5 + .5 * np.sin(2 * np.pi * .35 * tt)) * np.minimum(1, tt / 4) * np.clip((18.2 - tt) / 1.5, 0, 1)
madd(shimmer, 0, .025)
# darker tension pad from the incident onward
madd(saw_pad([note(45), note(52), note(57), note(60)], 10.6, cutoff=900, att=2.0, rel=1.0), 9.6, .22)
# riser into the reveal
n = int(2.05 * SR); tt = np.arange(n) / SR; u = tt / tt[-1]
noise = rng.standard_normal(n); riser = np.zeros(n)
for k in range(0, n, 2400):  # sweep band in blocks
    c = 300 * (20 ** u[k]); seg = noise[k:k + 2400]
    riser[k:k + 2400] = bp(seg, max(60, c * .7), min(20000, c * 1.4))
riser = riser * (u ** 2.2) + .3 * np.sin(2 * np.pi * (110 * tt + 330 * tt * u / 2)) * u ** 2
madd(riser, 18.15, .5)

# B/C: progression after the reveal (120 bpm, 2 bars per chord)
prog = [[53, 57, 60, 64], [48, 55, 60, 64], [55, 59, 62, 67], [57, 60, 64, 67]]  # Fmaj7, C, G, Am
def section(t0, t1, energy):
    t = t0; k = 0
    while t < t1 - .5:
        d = min(4.0, t1 - t)
        ch = prog[k % 4]
        madd(saw_pad([note(m) for m in ch], d + .9, cutoff=1300 + 500 * energy, att=.4, rel=.9), t, .24)
        madd(saw_pad([note(ch[0] - 12)], d + .5, cutoff=300, att=.1, rel=.4), t, .28)  # bass
        # arpeggio, eighth notes
        for j in range(int(d / .25)):
            m = ch[[0, 1, 2, 3, 2, 1, 3, 2][j % 8]] + 12
            nn = int(.35 * SR); tt = np.arange(nn) / SR
            pl = (np.sin(2 * np.pi * note(m) * tt) + .3 * np.sin(4 * np.pi * note(m) * tt)) * np.exp(-tt * 9)
            madd(pl, t + j * .25, .045 * (0.6 + .4 * energy), pan=.35 if j % 2 else -.35)
        t += 4.0; k += 1
section(20.2, 61.9, .6)
section(63.0, 88.2, 1.0)
section(88.2, 94.5, 1.2)
# final resolve
madd(saw_pad([note(m) for m in [53, 57, 60, 64, 69]], 9.9, cutoff=1700, att=.3, rel=3.0), 94.67, .2)
madd(saw_pad([note(41)], 9.9, cutoff=300, att=.1, rel=3.0), 94.67, .22)

# drums
def kick():
    nn = int(.45 * SR); tt = np.arange(nn) / SR
    f = 45 + 90 * np.exp(-tt * 30)
    return np.sin(2 * np.pi * np.cumsum(f) / SR) * np.exp(-tt * 7)
def hat():
    nn = int(.06 * SR); return hp(rng.standard_normal(nn), 7000, 2) * np.exp(-np.arange(nn) / SR * 70)
for t0, t1, hats in [(22.2, 61.6, False), (27.6, 44.6, True), (63.6, 88.0, True), (88.4, 94.4, True)]:
    b = t0
    while b < t1:
        if not hats: madd(kick(), b, .33)
        else: madd(hat(), b + .25, .05, pan=.2)
        b += .5
for b in np.arange(63.6, 94.4, .5): madd(kick(), b, .3)

# ducking under the voice
v = subprocess.run(["ffmpeg", "-loglevel", "error", "-i", "../vo.wav", "-ac", "1", "-ar", str(SR), "-f", "f32le", "-"], capture_output=True).stdout
voice = np.frombuffer(v, dtype=np.float32)
venv = np.sqrt(lp(np.pad(voice, (0, max(0, N - len(voice))))[:N] ** 2, 8, 1).clip(0))
gate = np.clip(venv / .03, 0, 1)
duck = 1 - .55 * gate
# smooth: fast attack, slow release
sm = np.empty_like(duck); cur = 1.0
a_k, r_k = np.exp(-1 / (.03 * SR)), np.exp(-1 / (.35 * SR))
for i in range(0, N, 48):  # block-wise for speed
    target = duck[i:i + 48].min()
    k = a_k if target < cur else r_k
    cur = target + (cur - target) * (k ** 48)
    sm[i:i + 48] = cur
# hush for "والأقوى؟"
hush = np.ones(N); i0, i1 = int(61.85 * SR), int(63.0 * SR)
hush[i0:i1] = np.linspace(1, .05, i1 - i0) ** .3
music_L *= sm * hush; music_R *= sm * hush
L += music_L; Rr += music_R

# ---------------- sound effects ----------------
def whoosh(d=.55, lo=300, hi=5000):
    nn = int(d * SR); tt = np.arange(nn) / SR; u = tt / d
    x = rng.standard_normal(nn); out = np.zeros(nn)
    for k in range(0, nn, 1200):
        c = lo * (hi / lo) ** np.sin(np.pi * u[k] / 2); out[k:k + 1200] = bp(x[k:k + 1200], c * .6, min(20000, c * 1.5))
    return out * np.sin(np.pi * u) ** 2
def impact(big=1.0):
    nn = int(2.2 * SR); tt = np.arange(nn) / SR
    f = 32 + 70 * np.exp(-tt * 9)
    sub = np.sin(2 * np.pi * np.cumsum(f) / SR) * np.exp(-tt * 2.2)
    crack = lp(rng.standard_normal(nn), 2500) * np.exp(-tt * 9)
    air = hp(rng.standard_normal(nn), 4000) * np.exp(-tt * 3) * .2
    return (sub * .9 + crack * .45 + air) * big
def pop(freq=880, d=.12):
    nn = int(d * SR); tt = np.arange(nn) / SR
    return np.sin(2 * np.pi * freq * tt * (1 + .4 * np.exp(-tt * 40))) * np.exp(-tt * 28)
def tick():
    nn = int(.02 * SR); return hp(rng.standard_normal(nn), 3000) * np.exp(-np.arange(nn) / SR * 300)
def thud():
    nn = int(.25 * SR); tt = np.arange(nn) / SR
    return (np.sin(2 * np.pi * (70 + 60 * np.exp(-tt * 25)) * tt) * np.exp(-tt * 14) + lp(rng.standard_normal(nn), 1200) * np.exp(-tt * 30) * .5)
def beep(f, d=.18):
    nn = int(d * SR); tt = np.arange(nn) / SR
    return np.sign(np.sin(2 * np.pi * f * tt)) * .4 * np.minimum(1, (d - tt) / .02) * np.minimum(1, tt / .005)
def chime(fs):
    out = np.zeros(int((1.25 + .09 * len(fs)) * SR))
    for j, f in enumerate(fs):
        nn = int(1.2 * SR); tt = np.arange(nn) / SR
        s = (np.sin(2 * np.pi * f * tt) + .25 * np.sin(4 * np.pi * f * tt)) * np.exp(-tt * 4)
        i = int(j * .09 * SR); out[i:i + nn] += s
    return out
def glitch(d=.35):
    nn = int(d * SR); x = rng.standard_normal(nn); g = (np.sin(2 * np.pi * 37 * np.arange(nn) / SR) > 0) * 1.0
    return bp(x, 400, 3000) * g * np.exp(-np.arange(nn) / SR * 6)

W = .16  # whoosh level
for t, p in [(3.55, 0), (13.25, .5), (13.45, .3), (13.65, .1), (18.25, 0), (21.75, 0), (27.45, 0), (33.6, 0), (36.4, -.4), (39.55, 0), (44.85, 0), (51.45, .3), (54.75, -.3), (58.35, 0), (75.05, 0), (78.3, 0), (84.85, 0), (88.2, 0), (91.7, 0), (97.6, 0)]:
    add(whoosh(), t - .2, W, p)
add(impact(.6), 3.62, .35)
add(impact(1.0), 20.2, .55)
add(impact(.7), 63.55, .4)
add(impact(.7), 76.95, .42)
add(impact(1.0), 90.33, .5)
add(impact(1.0), 94.67, .55)
for i in range(64): add(pop(1200 + (i % 7) * 120, .06), 3.62 + i * .012, .025, pan=(i % 11) / 5 - 1)
for i in range(10): add(tick(), 5.0 + i * .11, .12, pan=(i % 5) / 2 - 1)
for i, t in enumerate([7.17, 7.42, 7.62, 7.86, 8.2, 8.45]): add(thud(), t, .35, pan=[-.5, .4, -.2, .6, -.6, .2][i])
add(beep(880), 10.0, .07); add(beep(660), 10.22, .07); add(beep(880), 10.6, .07); add(beep(660), 10.82, .07)
add(glitch(), 11.4, .25)
for t in [14.4, 14.85, 15.15]: add(pop(520, .15), t, .12)
add(glitch(.25), 15.64, .2)
for k in range(5): add(tick(), 15.7 + k * .5, .22)
for t in [29.0, 29.95, 31.0, 31.35, 32.85]: add(pop(990, .14), t - .05, .14)
for t in [46.92, 47.45, 48.15, 49.15]: add(pop(760, .1), t, .1)
add(tick(), 40.75, .3); add(pop(1320, .2), 40.8, .12)
add(chime([note(88), note(95)]), 41.45, .05)
for i in range(18): add(tick(), 66.42 + i * .083, .13, pan=.3)
for t in [69.25, 70.05, 70.55]: add(pop(1100, .12), t, .1, pan=.4)
add(pop(600, .2), 71.0, .12)
add(tick(), 74.0, .3)
add(chime([note(76), note(83), note(88)]), 74.2, .09)
add(whoosh(.3, 1500, 8000), 56.85, .12); add(whoosh(.3, 1500, 8000), 76.4, .12)
for i, t in enumerate([78.6, 79.2, 80.2, 80.65, 81.1, 82.5, 83.35]): add(pop(660 + i * 70, .16), t - .05, .14, pan=(i - 3) / 4)
for i in range(6): add(pop(1300, .08), 88.5 + i * .1, .06)
add(chime([note(84), note(88), note(91), note(96)]), 90.33, .1)
for i, t in enumerate([92.55, 93.05, 93.55]): add(thud(), t - .05, .3, pan=(i - 1) * .5)
add(chime([note(81), note(88), note(93)]), 98.05, .06)

mix = np.stack([L, Rr], 1)
fade = np.ones(N); i0 = int(103.0 * SR); fade[i0:] = np.linspace(1, 0, N - i0) ** 2
mix *= fade[:, None]
mix /= max(1e-9, np.abs(mix).max()) / db(-3)
with wave.open("bed.wav", "wb") as w:
    w.setnchannels(2); w.setsampwidth(2); w.setframerate(SR)
    w.writeframes((mix * 32767).astype(np.int16).tobytes())
print("bed.wav written", mix.shape)
