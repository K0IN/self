import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import yaml from 'yaml'
import { parquetReadObjects } from 'hyparquet'
import { compressors } from 'hyparquet-compressors'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
export const snapshotPath = path.join(root, 'models', 'quality-rankings.json')
export const definitions = yaml.parse(fs.readFileSync(path.join(root, 'models', 'quality-benchmarks.yml'), 'utf8'))

export function benchmarkIds(model, id) {
    const ids = [...(definitions.types[model.type] ?? []), ...(definitions.supplementary_types?.[model.type] ?? []), ...(definitions.model_benchmarks?.[id] ?? [])]
    if (model.type === 'text' && model.capabilities?.input?.includes('vision')) ids.push('arena-vision')
    if (model.capabilities?.output?.includes('image-edit')) ids.push('arena-edit')
    return [...new Set(ids)]
}

export function modelBenchmarks(id, model, snapshot = {}) {
    return benchmarkIds(model, id).map(benchmark => ({
        id: benchmark,
        ...definitions.benchmarks[benchmark],
        status: 'not-fetched',
        ...snapshot.models?.[id]?.[benchmark]
    })).filter(result => ['ranked', 'listed', 'reported'].includes(result.status))
}

export function matchArena(rows, identities) {
    const matches = rows.filter(row => row.category === 'overall' && identities.includes(row.model_name))
    if (matches.length > 1) throw new Error('Ambiguous exact model match')
    const row = matches[0]
    if (!row) return { status: 'not-listed' }
    const rank = Number(row.rank)
    const votes = row.vote_count == null ? undefined : Number(row.vote_count)
    if (!Number.isSafeInteger(rank) || rank < 1 || !Number.isFinite(row.rating) || (votes != null && !Number.isSafeInteger(votes)) || !/^\d{4}-\d{2}-\d{2}$/.test(row.leaderboard_publish_date)) {
        throw new Error('Invalid Arena result')
    }
    return {
        status: 'ranked', evaluatedModel: row.model_name, rank,
        score: row.rating, lower: row.rating_lower, upper: row.rating_upper,
        votes, publishedAt: row.leaderboard_publish_date,
        scope: 'Upstream model; not this local quantization'
    }
}

export async function fetchArena(subset, fetcher = fetch, reader = parquetReadObjects) {
    const url = `https://huggingface.co/datasets/lmarena-ai/leaderboard-dataset/resolve/main/${subset}/latest-00000-of-00001.parquet`
    const response = await fetcher(url, { signal: AbortSignal.timeout(60000) })
    if (!response.ok) throw new Error(`Arena ${subset}: HTTP ${response.status}`)
    const buffer = await response.arrayBuffer()
    const data = await reader({ file: buffer, compressors })
    const rows = data.filter(row => row.category === 'overall')
    if (!rows.length) throw new Error('Missing overall leaderboard')
    return rows
}

export function matchMTEB(text, source) {
    const block = text.match(/MODEL_NAMES\s*=\s*(\[[\s\S]*?\])/)
    if (!block) throw new Error('Missing MTEB model index')
    const names = JSON.parse(block[1].replace(/,\s*\]$/, ']'))
    if (!Array.isArray(names) || !names.length || names.some(name => typeof name !== 'string')) throw new Error('Invalid MTEB model index')
    return names.includes(source)
        ? { status: 'listed', evaluatedModel: source, scope: 'Registered in MTEB; no suite-specific rank imported' }
        : { status: 'not-listed' }
}

export function cardEvaluations(text, source) {
    const frontmatter = text.match(/^---\r?\n([\s\S]*?)\r?\n---/)
    const metadata = frontmatter ? yaml.parse(frontmatter[1]) : {}
    const evaluations = (metadata?.['model-index'] ?? []).flatMap(entry => (entry.results ?? []).flatMap(result =>
        (result.metrics ?? []).filter(metric => Number.isFinite(metric.value)).map(metric => ({
            dataset: result.dataset?.name ?? result.dataset?.type,
            task: result.task?.name ?? result.task?.type,
            metric: metric.name ?? metric.type,
            score: metric.value,
            ...(result.dataset?.split ? { split: result.dataset.split } : {}),
            ...(result.dataset?.revision ? { revision: result.dataset.revision } : {})
        })))).filter(result => result.dataset && result.metric)
    return {
        status: evaluations.length ? 'reported' : 'no-structured-results',
        evaluatedModel: source,
        url: `https://huggingface.co/${source}/blob/main/README.md`,
        scope: 'Publisher-reported upstream checkpoint; not an independent rank or local quantization',
        evaluations
    }
}

async function fetchText(url, fetcher) {
    const response = await fetcher(url, { signal: AbortSignal.timeout(60000) })
    if (!response.ok) throw new Error(`HTTP ${response.status}: ${url}`)
    return response.text()
}

export function matchDecisionLeaderboard(data, benchmark, source) {
    const rows = benchmark === 'decision-index' ? data.models : data.systems
    if (!Array.isArray(rows) || !rows.length) throw new Error('Missing decision leaderboard rows')
    const modelURL = `https://huggingface.co/${source}`
    const matches = rows.filter(row => benchmark === 'decision-index'
        ? (row.meta?.served_checkpoint ?? row.meta?.weights_repo) === source || row.meta?.model_url === modelURL
        : row.repo === modelURL || row.model_pin?.split('@')[0] === source)
    if (!matches.length) return { status: 'not-listed' }
    if (matches.length > 1) return { status: 'unverified', reason: 'Multiple evaluated configurations; no unique match' }
    const row = matches[0]
    const rank = benchmark === 'decision-index' ? row.v03?.rank_v : row.rank
    const score = benchmark === 'decision-index' ? row.scores?.balanced_skill : row.jevbench_score
    if (!Number.isFinite(score) || !Number.isInteger(rank) || rank < 1) throw new Error('Invalid decision ranking')
    if (benchmark === 'jevbench' && (!row.ranked || !row.full_coverage)) return { status: 'unverified' }
    return {
        status: 'ranked', rank, score, evaluatedModel: source,
        evaluatedConfiguration: benchmark === 'decision-index' ? `${row.engine}${row.variant ? ` (${row.variant})` : ''}` : row.display,
        modelPin: row.model_pin,
        edition: benchmark === 'decision-index' ? data.suite?.label : data.revision,
        scope: 'Published result for the named upstream model and evaluated configuration; checkpoint revision and local quantization equivalence are not guaranteed.'
    }
}

export async function refresh(registry, previous = {}, fetcher = fetch) {
    const snapshot = { version: 1, sources: {}, models: {} }
    const cards = new Map()
    for (const [benchmark, definition] of Object.entries(definitions.benchmarks)) {
        if (!Object.entries(registry.models).some(([id, model]) => benchmarkIds(model, id).includes(benchmark))) continue
        let rows
        let membership
        let leaderboard
        let error
        if (definition.export_url) {
            try {
                leaderboard = JSON.parse(await fetchText(definition.export_url, fetcher))
                matchDecisionLeaderboard(leaderboard, benchmark, '')
            } catch (failure) { error = failure.message; leaderboard = undefined }
        }
        if (definition.arena_subset) {
            try { rows = await fetchArena(definition.arena_subset, fetcher) }
            catch (failure) { error = failure.message }
        }
        if (definition.membership_url) {
            try {
                membership = await fetchText(definition.membership_url, fetcher)
                matchMTEB(membership, '')
            } catch (failure) { error = failure.message }
        }
        snapshot.sources[benchmark] = rows || membership
            ? { status: 'ok', fetchedAt: new Date().toISOString(), dataset: 'https://huggingface.co/datasets/lmarena-ai/leaderboard-dataset', subset: definition.arena_subset, license: 'CC-BY-4.0' }
            : { ...previous.sources?.[benchmark], status: error ? 'fetch-error' : 'per-model', ...(error ? { error } : {}) }
        if (membership) snapshot.sources[benchmark] = { status: 'ok', fetchedAt: new Date().toISOString(), url: definition.membership_url }
        if (leaderboard) snapshot.sources[benchmark] = { status: 'ok', fetchedAt: new Date().toISOString(), url: definition.export_url }
        for (const [id, model] of Object.entries(registry.models)) {
            if (!benchmarkIds(model, id).includes(benchmark)) continue
            snapshot.models[id] ??= {}
            const identities = [model.info?.source, ...(definitions.aliases[id]?.[benchmark] ?? [])].filter(Boolean)
            let modelError = error
            let result = rows ? matchArena(rows, identities) : membership ? matchMTEB(membership, model.info?.source) : { status: error ? 'unavailable' : 'unverified' }
            if (leaderboard) result = matchDecisionLeaderboard(leaderboard, benchmark, model.info?.source)
            if (benchmark === 'hf-evaluations') {
                const source = model.info?.source
                try {
                    if (!source || !/^[\w.-]+\/[\w.-]+$/.test(source)) throw new Error('Invalid HF model identity')
                    if (!cards.has(source)) cards.set(source, fetchText(`https://huggingface.co/${source}/raw/main/README.md`, fetcher))
                    result = { ...cardEvaluations(await cards.get(source), source), fetchedAt: new Date().toISOString() }
                } catch (failure) { modelError = failure.message; result = { status: 'unavailable', error: modelError } }
            }
            const report = definitions.reports?.[id]?.[benchmark]
            if (report && report.evaluatedModel === model.info?.source && !['ranked', 'reported'].includes(result.status)) {
                result = { ...report, membershipStatus: result.status }
                modelError = undefined
            }
            snapshot.models[id][benchmark] = modelError && previous.models?.[id]?.[benchmark]
                ? { ...previous.models[id][benchmark], stale: true }
                : result
            if (rows || membership || leaderboard) snapshot.models[id][benchmark].fetchedAt = snapshot.sources[benchmark].fetchedAt
        }
    }
    snapshot.coverage = Object.fromEntries(Object.entries(registry.models).map(([id, model]) => [id, {
        type: model.type,
        ranked: Object.values(snapshot.models[id] ?? {}).some(result => result.status === 'ranked' && !result.stale),
        publishedResults: Object.values(snapshot.models[id] ?? {}).some(result => ['ranked', 'reported'].includes(result.status) && !result.stale)
    }]))
    return snapshot
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
    const registry = yaml.parse(fs.readFileSync(path.join(root, 'models', 'registry.yml'), 'utf8'))
    const previous = fs.existsSync(snapshotPath) ? JSON.parse(fs.readFileSync(snapshotPath, 'utf8')) : {}
    const snapshot = await refresh(registry, previous)
    fs.writeFileSync(snapshotPath, JSON.stringify(snapshot, null, 2) + '\n')
    for (const [id, source] of Object.entries(snapshot.sources)) {
        if (source.status === 'fetch-error') console.warn(`${id}: ${source.error}`)
    }
}