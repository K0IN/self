import assert from 'node:assert/strict'
import test from 'node:test'
import { benchmarkIds, cardEvaluations, definitions, fetchArena, matchArena, matchDecisionLeaderboard, matchMTEB, modelBenchmarks, refresh } from './quality-benchmarks.mjs'

test('every model type has one named primary benchmark', () => {
    for (const type of ['text', 'decision', 'embedding', 'image', 'audio']) {
        assert.equal(definitions.types[type].length, 1)
        assert.notEqual(definitions.types[type][0], 'hf-evaluations')
    }
})

test('HF evaluation lookup uses original model, never GGUF or base model', async () => {
    const urls = []
    await refresh({
        models: {
            exact: {
                type: 'decision', info: { source: 'original/checkpoint', base_model: 'original/base' },
                q4: { repo: 'quantizer/checkpoint-GGUF' }
            }
        }
    }, {}, async url => {
        urls.push(url)
        return { ok: true, text: async () => '# Model card' }
    })
    assert.ok(urls.includes('https://huggingface.co/original/checkpoint/raw/main/README.md'))
    assert.ok(urls.every(url => !url.includes('quantizer') && !url.includes('original/base')))
})

test('display hides missing, unavailable and unfetched benchmarks', () => {
    const model = { type: 'text' }
    assert.deepEqual(modelBenchmarks('exact', model), [])
    for (const status of ['not-listed', 'unavailable', 'no-structured-results', 'unverified']) {
        assert.deepEqual(modelBenchmarks('exact', model, { models: { exact: { 'arena-text': { status } } } }), [])
    }
    assert.equal(modelBenchmarks('exact', model, { models: { exact: { 'arena-text': { status: 'ranked', rank: 3 } } } }).length, 1)
})

test('benchmarks follow modality and capabilities', () => {
    assert.deepEqual(benchmarkIds({ type: 'decision', capabilities: { input: ['vision'] } }), ['decision-index', 'jevbench', 'typed-decisions', 'hf-evaluations'])
    assert.deepEqual(benchmarkIds({ type: 'image', capabilities: { output: ['image-edit'] } }), ['arena-image', 'arena-edit'])
    for (const type of ['text', 'decision', 'embedding', 'image', 'audio']) assert.ok(benchmarkIds({ type }).length)
})
const row = { category: 'overall', model_name: 'exact', rank: 3, rating: 1200, leaderboard_publish_date: '2026-10-01' }
test('decision rankings match upstream identity, not base models or fuzzy names', () => {
    const entry = { engine: 'exact-q8', meta: { served_checkpoint: 'owner/exact', base_model: 'owner/base' }, scores: { balanced_skill: 49.27 }, v03: { rank_v: 47 } }
    const data = { models: [entry] }
    assert.equal(matchDecisionLeaderboard(data, 'decision-index', 'owner/exact').rank, 47)
    assert.equal(matchDecisionLeaderboard(data, 'decision-index', 'owner/base').status, 'not-listed')
    assert.equal(matchDecisionLeaderboard(data, 'decision-index', 'owner/exact-GGUF').status, 'not-listed')
    assert.equal(matchDecisionLeaderboard({ models: [entry, entry] }, 'decision-index', 'owner/exact').status, 'unverified')
    const jev = { systems: [{ repo: 'https://huggingface.co/owner/exact', rank: 11, jevbench_score: 68.9, ranked: true, full_coverage: true }] }
    assert.equal(matchDecisionLeaderboard(jev, 'jevbench', 'owner/exact').rank, 11)
    assert.equal(matchDecisionLeaderboard(jev, 'jevbench', 'owner/base').status, 'not-listed')
})
test('matching never inherits base-model scores or uses fuzzy names', () => {
    assert.equal(matchArena([row], ['exact-finetune']).status, 'not-listed')
    assert.equal(matchArena([row], ['exact']).rank, 3)
    assert.throws(() => matchArena([row, row], ['exact']), /Ambiguous/)
    assert.throws(() => matchArena([{ ...row, rank: null }], ['exact']), /Invalid/)
})
test('parquet integers serialize without loss', () => {
    const result = matchArena([{ ...row, rank: 3n, vote_count: 100n }], ['exact'])
    assert.equal(result.rank, 3)
    assert.equal(result.votes, 100)
    assert.doesNotThrow(() => JSON.stringify(result))
    assert.throws(() => matchArena([{ ...row, rank: 9007199254740993n }], ['exact']), /Invalid/)
})
test('fetch reads official export and selects overall category', async () => {
    const rows = await fetchArena('text_style_control', async url => {
        assert.ok(url.endsWith('/text_style_control/latest-00000-of-00001.parquet'))
        return { ok: true, arrayBuffer: async () => new ArrayBuffer(0) }
    }, async () => [row, { ...row, category: 'coding' }])
    assert.deepEqual(rows, [row])
})
test('outages preserve scores as stale; unverified modalities remain explicit', async () => {
    const previous = { models: { exact: { 'arena-text': { status: 'ranked', rank: 3 } } } }
    const snapshot = await refresh({ models: { exact: { type: 'text' }, speech: { type: 'audio' } } }, previous, async () => { throw new Error('offline') })
    assert.equal(snapshot.models.exact['arena-text'].rank, 3)
    assert.equal(snapshot.models.exact['arena-text'].stale, true)
    assert.equal(snapshot.models.speech['hf-evaluations'].status, 'unavailable')
})
test('MTEB membership is exact and does not invent ranks', () => {
    const text = 'MODEL_NAMES = ["nomic-ai/nomic-embed-text-v1.5",]'
    assert.equal(matchMTEB(text, 'nomic-ai/nomic-embed-text-v1.5').status, 'listed')
    assert.equal(matchMTEB(text, 'nomic-ai/nomic-embed-text-v1').status, 'not-listed')
    assert.equal(matchMTEB(text, 'nomic-ai/nomic-embed-text-v1.5').rank, undefined)
    assert.throws(() => matchMTEB('error page', 'model'), /Missing/)
})
test('HF results preserve datasets and metrics without inventing ranks', () => {
    const text = '---\nmodel-index:\n  - name: exact\n    results:\n      - dataset: {name: held-out, split: test}\n        task: {type: classification}\n        metrics: [{type: accuracy, value: 0.799}]\n---\n'
    const result = cardEvaluations(text, 'owner/exact')
    assert.equal(result.status, 'reported')
    assert.equal(result.evaluations[0].score, 0.799)
    assert.equal(result.evaluations[0].split, 'test')
    assert.equal(result.rank, undefined)
    assert.equal(cardEvaluations('# No results', 'owner/model').status, 'no-structured-results')
})
test('cited reports only apply to their exact upstream checkpoint', async () => {
    const models = {
        'qwen3-tts:1.7b': { type: 'audio', info: { source: 'Qwen/Qwen3-TTS-12Hz-1.7B-Base' } },
        'embeddinggemma-2:740m': { type: 'embedding', info: { source: 'different/checkpoint' } }
    }
    const snapshot = await refresh({ models }, {}, async () => { throw new Error('offline') })
    assert.equal(snapshot.models['qwen3-tts:1.7b']['seed-tts'].status, 'reported')
    assert.equal(snapshot.models['qwen3-tts:1.7b']['seed-tts'].evaluations[1].score, 1.24)
    assert.equal(snapshot.models['embeddinggemma-2:740m'].mteb.status, 'unavailable')
})