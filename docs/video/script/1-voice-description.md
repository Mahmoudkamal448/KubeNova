# KubeNova: Voice Description for TTS

| Attribute | Direction |
|---|---|
| **Gender / age** | Male, about 28–35 (a female voice of the same age also suits, with the same tone) |
| **Accent** | Natural Cairene Egyptian Arabic. English technical terms said the way Egyptian engineers say them in daily work, not with an American accent |
| **Tone** | Warm, confident and modern: a senior engineer giving a product launch keynote, not an advertisement announcer |
| **Pace** | Medium (about 150 words a minute). Slows slightly on product-name and key-benefit lines and speeds up a little on problem lines. Short natural pauses between sections |
| **Energy** | Builds from calm curiosity, through the frustration of the problem, to excitement at the end |
| **Personality** | Smart, friendly and credible, like a colleague who has lived the problem and found the fix |
| **Avoid** | Shouting, exaggerated "radio" delivery, or a robotic, flat reading of the English words |

## Short description (under 200 characters, for Cartesia and similar tools)

Warm, confident Egyptian male in his early 30s with a Cairene accent. Modern tech-launch tone: clear, friendly, medium pace, English tech terms said naturally. Not salesy.

## Longer prompt (for tools that allow more text)

A confident, warm male voice in his early thirties speaking natural Cairene Egyptian Arabic, pronouncing English tech terms the way Egyptian software engineers do. Modern tech-startup launch tone: clear, friendly, credible, medium pace, energy rising toward the end. Not salesy, not robotic.

## Notes

- The script runs about 75–85 seconds at this pace.
- Pauses are written as "…". If your TTS supports `<break time="400ms"/>`, add one after "طب لو كل ده في مكان واحد؟" and after "قابل KubeNova.".
- If the TTS mispronounces "KubeNova", write it as "كيوب نوفا" in those two lines only.
- Check before publishing: "an hour → minutes" is a marketing claim, not a measured number.
- Check before publishing: "applies the fix after your approval" needs **Allow the assistant to change clusters** turned on in Settings. The assistant also needs each user's own Anthropic API key or a local model, so "free" refers to KubeNova itself, not AI usage.
