# Model quality benchmarks

Quality results are separate from the local throughput and latency reports in [Benchmarks](./benchmarks.md). The registry overview and model pages show only confirmed membership or published results for the exact upstream model (`info.source`), never a quantization's GGUF repository or an inherited base-model score. Models with no confirmed entries have no quality section.

One primary benchmark is selected per type: MMLU-Pro for text, Decision Index for decisions, MTEB for embeddings, Arena Text-to-Image for images, and Seed-TTS for audio. Decision models also show JevBench and Typed Decisions whenever a confirmed entry exists. Arena vision/editing and structured HF model-card results are supplementary.

Decision Index imports the published full score and `rank_v` from its HF JSON export. JevBench imports the official composite score and rank from its versioned v1.6.1 export, not the live merged board. Model repository identity is matched exactly; ambiguous configurations are hidden. Evaluated engine versions, precision and checkpoint pins are displayed where available. Repository membership does not establish identical weight revisions or equivalence to the local runtime. Typed Decisions comparison results are cited separately and do not invent an official rank.

## Sources and coverage

Checked on 2026-10-13:

| Modality | Established reference | Verified registry coverage |
| --- | --- | --- |
| Text and image understanding | [MMLU-Pro](https://huggingface.co/spaces/TIGER-Lab/MMLU-Pro); supplementary [Arena](https://arena.ai/leaderboard/text) | Upstream publisher MMLU-Pro accuracy: Qwen3.5 9B 82.5%, Gemma 4 E4B 69.4%, Gemma 4 12B 77.2%. These are published scores, not independent ranks. The exact checkpoints are absent from the fetched style-controlled Arena exports. |
| Embeddings | [MTEB](https://huggingface.co/spaces/mteb/leaderboard) | Both Nomic checkpoints occur in the official model index. Nomic v1.5 publishes structured per-task results. EmbeddingGemma 2 publishes full-precision MTEB scores but is absent from that index. CLM is not listed; its fine-tuned verifier results must not be inherited by the served checkpoint. |
| Image generation and editing | [Arena image](https://arena.ai/leaderboard/text-to-image) and [editing](https://arena.ai/leaderboard/image-edit) | Qwen Image 2.1: generation #19, editing #18. FLUX.2-klein 9B: generation #62, editing #42. Qwen Image 2.1 Turbo is not listed and does not inherit the non-Turbo score. |
| Speech synthesis | [Seed-TTS evaluation](https://github.com/BytedanceSpeech/seed-tts-eval), [LibriSpeech](https://www.openslr.org/12) | Qwen3-TTS-12Hz-1.7B-Base publishes Seed-TTS WER: 0.77% Chinese, 1.24% English. These are publisher-reported BF16 results, not a listening-preference rank. Kyutai publishes an older English Pocket TTS evaluation; checkpoint/language correspondence has not been established for our current six variants, so no score is assigned. |
| Typed decisions | [Typed Decisions](https://huggingface.co/datasets/LocalLLaMA/typed-decisions) | Laya Typed-Decisions: 0.766 teacher agreement, 0.062 Brier, 0.213 ECE on 400 test cases / 2000 decisions. It was fine-tuned on train; this is not zero-shot or ground-truth correctness. Kev 0.5B/0.8B/4B have supplementary structured held-out results. Base-model scores are not substitutes. |

Arena and MTEB are established large evaluation sources, but coverage is incomplete. HF model cards are an additional source, not an independent benchmark. No single trusted leaderboard was verified to cover every registry model.

## Refresh

Run with Node 22:

```sh
cd docs
npm ci
node --test quality-benchmarks.test.mjs
node quality-benchmarks.mjs
npm run build
```

Definitions, exact Arena aliases and cited publisher reports live in `models/quality-benchmarks.yml`. The checked-in snapshot is `models/quality-rankings.json`. Curated reports require manual re-verification; the script does not scrape arbitrary prose or manufacture a rank from unrelated scores.

The weekly/manual GitHub action fetches results, uploads a review artifact and proposes a snapshot-update PR. GitHub Actions must have permission to create pull requests in repository settings. PR-triggered runs test matching without fetching or publishing changes. Merging a snapshot update makes it available to the next documentation build.

Failed fetches preserve previous results with a stale marker. Gated HF cards remain unavailable without authentication. MTEB membership only confirms registration, not completion of a particular suite. Scores across different datasets, splits, languages, prompts or precision settings are not directly comparable.