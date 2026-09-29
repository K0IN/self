import fs from 'node:fs'
import path from 'node:path'
import yaml from 'yaml'

const root = path.resolve(import.meta.dirname, '..')
const docs = import.meta.dirname
const registryPath = process.env.REGISTRY_FILE || path.join(root, 'models', 'registry.yml')
const registryText = fs.readFileSync(registryPath, 'utf8')
const registry = yaml.parse(registryText)
const output = path.join(docs, 'registry')
fs.rmSync(output, { recursive: true, force: true })
fs.mkdirSync(output, { recursive: true })
fs.writeFileSync(path.join(docs, 'public-models.yml'), registryText)

const esc = value => String(value ?? '').replaceAll('|', '\\|')
const rows = Object.entries(registry.models ?? {}).map(([id, model]) => {
  const quants = Object.keys(model).filter(key => !['description', 'readme', 'type', 'default', 'capabilities', 'info', 'settings'].includes(key))
  const slug = id.replace(':', '/')
  return { id, model, quants, slug }
})

const index = `# Model registry\n\nThe registry is the source of truth for downloadable models. The raw YAML is available at [/models.yml](/models.yml).\n\n| Model | Type | Quantizations | Description |\n| --- | --- | --- | --- |\n${rows.map(({ id, model, quants, slug }) => `| [${id}](/registry/${slug}) | ${model.type} | ${quants.join(', ')} | ${esc(model.description)} |`).join('\n')}\n\n## Suggestions\n\n- **kev:0.5b** for low-resource CPU development\n- **kev:0.8b** for a small quality step up\n- **kev:4b** when quality matters more than memory\n- **decider:2b-vision** for text and image decisions\n`
fs.writeFileSync(path.join(output, 'index.md'), index)

for (const { id, model, quants, slug } of rows) {
  const readmePath = path.join(root, 'models', model.readme)
  const readme = fs.readFileSync(readmePath, 'utf8')
  const lines = [`${readme.trim()}`, '', `## Registry details`, '', `- Registry id: \`${id}\``, `- Type: ${model.type}`, `- Default quantization: \`${model.default}\``, `- Capabilities: ${(model.capabilities?.input ?? []).join(', ')} -> ${(model.capabilities?.output ?? []).join(', ')}`, '', '## Available quants', '', '| Quant | Adapter | Files |', '| --- | --- | --- |']
  for (const quant of quants) {
    const variant = model[quant]
    lines.push(`| ${quant}${quant === model.default ? ' (default)' : ''} | ${variant.adapter} | ${(variant.files ?? []).map(file => `\`${file.file}\``).join('<br>')} |`)
  }
  fs.mkdirSync(path.dirname(path.join(output, slug)), { recursive: true })
  fs.writeFileSync(path.join(output, `${slug}.md`), lines.join('\n') + '\n')
}
