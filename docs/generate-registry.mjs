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
fs.rmSync(output, { recursive: true, force: true })
fs.mkdirSync(output, { recursive: true })
fs.mkdirSync(publicDir, { recursive: true })
fs.writeFileSync(path.join(publicDir, 'models.yml'), registryText)

const esc = value => String(value ?? '').replaceAll('|', '\\|')
const rows = Object.entries(registry.models ?? {}).map(([id, model]) => {
    const quants = Object.keys(model).filter(key => !['description', 'readme', 'type', 'default', 'capabilities', 'info', 'settings'].includes(key))
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
                url: `https://huggingface.co/${variant.repo}/blob/main/${file.file}`
            }))
        }
    }),
    capabilities,
    size,
    href: `/registry/${slug}`
}))
fs.writeFileSync(path.join(publicDir, 'registry.json'), JSON.stringify(catalog, null, 2) + '\n')

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
    const lines = [`${readme.trim()}`, '', `## Registry details`, '', `- Registry id: \`${id}\``, `- Type: ${model.type}`, `- Default quantization: \`${model.default}\``, `- Capabilities: ${(model.capabilities?.input ?? []).join(', ')} -> ${(model.capabilities?.output ?? []).join(', ')}`, '', '## Available quants', '', '| Quant | Adapter | Repository | File | Size | SHA-256 |', '| --- | --- | --- | --- | --- | --- |']
    for (const quant of quants) {
        const variant = model[quant]
        const files = variant.files ?? []
        const quantCell = `${quant}${quant === model.default ? ' (default)' : ''}`
        const repoCell = `[${variant.repo}](https://huggingface.co/${variant.repo})`
        files.forEach((file, index) => {
            const first = index === 0
            const fileUrl = `https://huggingface.co/${variant.repo}/blob/main/${file.file}`
            lines.push(`| ${first ? quantCell : ''} | ${first ? variant.adapter : ''} | ${first ? repoCell : ''} | [\`${file.file}\`](${fileUrl}) | ${formatBytes(Number(file.size || 0))} | \`${file.sha256}\` |`)
        })
    }
    fs.mkdirSync(path.dirname(path.join(output, slug)), { recursive: true })
    fs.writeFileSync(path.join(output, `${slug}.md`), lines.join('\n') + '\n')
}
