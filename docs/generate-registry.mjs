import fs from 'node:fs'
import path from 'node:path'
import yaml from 'yaml'

const root = path.resolve(import.meta.dirname, '..')
const docs = import.meta.dirname
const registryPath = process.env.REGISTRY_FILE || path.join(root, 'models', 'registry.yml')
const registryText = fs.readFileSync(registryPath, 'utf8')
const registry = yaml.parse(registryText)
const output = path.join(docs, 'registry')
const publicDir = path.join(docs, 'public')
const publicBenchmarkDir = path.join(publicDir, 'benchmarks')
// Benchmark model cards are generated from the checked-in source reports. The
// public copies are build artifacts so the browser can load one run at a time.
const benchmarkDir = path.join(root, 'benchmarks')
fs.rmSync(output, { recursive: true, force: true })
fs.mkdirSync(output, { recursive: true })
fs.mkdirSync(publicDir, { recursive: true })
fs.rmSync(publicBenchmarkDir, { recursive: true, force: true })
fs.mkdirSync(publicBenchmarkDir, { recursive: true })
fs.writeFileSync(path.join(publicDir, 'models.yml'), registryText)

const esc = value => String(value ?? '').replaceAll('|', '\\|')
const rows = Object.entries(registry.models ?? {}).map(([id, model]) => {
    const quants = Object.keys(model).filter(key => !['description', 'readme', 'type', 'default', 'capabilities', 'max_images', 'info', 'settings'].includes(key))
    const slug = id.replace(':', '/')
    const size = model[model.default]?.files?.reduce((total, file) => total + Number(file.size || 0), 0) || 0
    return {
        id,
        model,
        quants,
        slug,
        size: formatBytes(size),
        capabilities: [...(model.capabilities?.input ?? []), ...(model.capabilities?.output ?? [])]
    }
})

const catalog = rows.map(({ id, model, quants, slug, size, capabilities }) => ({
    id,
    type: model.type,
    description: model.description,
    quants,
    variants: quants.map(quant => {
        const variant = model[quant]
        return {
            name: quant,
            repo: variant.repo,
            repoUrl: `https://huggingface.co/${variant.repo}`,
            files: (variant.files ?? []).map(file => ({
                name: file.file,
                size: formatBytes(Number(file.size || 0)),
                sha256: file.sha256,
                url: `https://huggingface.co/${file.repo ?? variant.repo}/blob/${file.revision ?? 'main'}/${file.file}`
            }))
        }
    }),
    capabilities,
    size,
    href: `/registry/${slug}`
}))
fs.writeFileSync(path.join(publicDir, 'registry.json'), JSON.stringify(catalog, null, 2) + '\n')

const benchmarkReports = []
if (fs.existsSync(benchmarkDir)) {
    for (const filename of fs.readdirSync(benchmarkDir).filter(name => name.endsWith('.json')).sort()) {
        try {
            const report = JSON.parse(fs.readFileSync(path.join(benchmarkDir, filename), 'utf8'))
            if (report.schema !== 2 || !report.benchmark?.modality || !report.benchmark?.version || !report.benchmark?.self_commit || !report.model?.id || !Array.isArray(report.results) || !report.hardware || !report.created_at) {
                console.warn(`Skipping incomplete benchmark report: ${filename}`)
                continue
            }
            benchmarkReports.push({
                file: filename,
                model: report.model.id,
                quant: report.model.quant,
                adapter: report.model.adapter,
                engine: report.device?.engine || '',
                modality: report.benchmark.modality,
                benchmarkVersion: report.benchmark.version,
                selfCommit: report.benchmark.self_commit,
                createdAt: report.created_at,
                gpuUsed: Boolean(report.device?.gpu_used),
                gpu: report.device?.gpu_used ? report.hardware.gpus?.[0]?.name || 'GPU' : null,
                availableGpus: (report.hardware.gpus ?? []).map(gpu => gpu.name).filter(Boolean),
                cpu: report.hardware.cpu || 'CPU',
                scenarios: report.results.map(result => ({
                    name: result.scenario,
                    requestsPerSec: result.requests_per_sec,
                    inputTokensPerSec: result.input_tokens_per_sec,
                    outputTokensPerSec: result.output_tokens_per_sec || 0,
                    throughput: result.throughput || 0,
                    throughputUnit: result.throughput_unit || '',
                    p50Ms: result.latency_ms?.p50,
                    p95Ms: result.latency_ms?.p95
                }))
            })
        } catch (error) {
            console.warn(`Skipping invalid benchmark report ${filename}: ${error.message}`)
        }
    }
}
for (const report of benchmarkReports) {
    fs.writeFileSync(path.join(publicBenchmarkDir, report.file), JSON.stringify(report, null, 2) + '\n')
}
fs.writeFileSync(path.join(publicDir, 'benchmarks.json'), JSON.stringify(benchmarkReports.map(report => ({
    file: report.file,
    url: `/benchmarks/${report.file}`,
    model: report.model,
    quant: report.quant,
    adapter: report.adapter,
    engine: report.engine,
    modality: report.modality,
    gpu: report.gpu,
    availableGpus: report.availableGpus,
    gpuUsed: report.gpuUsed,
    cpu: report.cpu,
    createdAt: report.createdAt
})), null, 2) + '\n')

const catalogJSON = JSON.stringify(catalog.map(({ variants, ...card }) => card)).replaceAll('&', '&amp;').replaceAll('"', '&quot;')
const index = `# Model registry\n\nThe registry is the source of truth for downloadable models. Search the catalog or [download models.yml](/models.yml).\n\n<ModelCatalog :models="${catalogJSON}" />\n`
fs.writeFileSync(path.join(output, 'index.md'), index)

function formatBytes(bytes) {
    if (bytes < 1024) return `${bytes} B`
    const units = ['KiB', 'MiB', 'GiB', 'TiB']
    let value = bytes
    let unit = -1
    do {
        value /= 1024
        unit++
    } while (value >= 1024 && unit < units.length - 1)
    return `${value >= 100 ? value.toFixed(0) : value.toFixed(1)} ${units[unit]}`
}

for (const { id, model, quants, slug } of rows) {
    const readmePath = path.join(root, 'models', model.readme)
    const readme = fs.readFileSync(readmePath, 'utf8')
    if (/^## Quantizations and Files\s*$/m.test(readme)) {
        fs.mkdirSync(path.dirname(path.join(output, slug)), { recursive: true })
        fs.writeFileSync(path.join(output, `${slug}.md`), `${readme.trim()}\n\n## Benchmarks\n\n<BenchmarkTable model-id="${id}" />\n`)
        continue
    }
    const lines = [`${readme.trim()}`, '', `## Registry details`, '', `- Registry id: \`${id}\``, `- Type: ${model.type}`, `- Default quantization: \`${model.default}\``, `- Capabilities: ${(model.capabilities?.input ?? []).join(', ')} -> ${(model.capabilities?.output ?? []).join(', ')}`, '', '## Benchmarks', '', `<BenchmarkTable model-id="${id}" />`, '', '## Available quants', '', '| Quant | Adapter | Repository | File | Size | SHA-256 |', '| --- | --- | --- | --- | --- | --- |']
    for (const quant of quants) {
        const variant = model[quant]
        const files = variant.files ?? []
        const quantCell = `${quant}${quant === model.default ? ' (default)' : ''}`
        const repoCell = `[${variant.repo}](https://huggingface.co/${variant.repo})`
        files.forEach((file, index) => {
            const first = index === 0
            const fileUrl = `https://huggingface.co/${file.repo ?? variant.repo}/blob/${file.revision ?? 'main'}/${file.file}`
            lines.push(`| ${first ? quantCell : ''} | ${first ? variant.adapter : ''} | ${first ? repoCell : ''} | [\`${file.file}\`](${fileUrl}) | ${formatBytes(Number(file.size || 0))} | \`${file.sha256}\` |`)
        })
    }
    fs.mkdirSync(path.dirname(path.join(output, slug)), { recursive: true })
    fs.writeFileSync(path.join(output, `${slug}.md`), lines.join('\n') + '\n')
}
