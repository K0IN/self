import { createHash } from 'node:crypto'
import { readFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import { resolve } from 'node:path'
import YAML from 'yaml'

export async function checkRegistry(registry, request = fetch, log = console.log) {
    const cache = new Map()
    const failures = []
    let checked = 0
    for (const [model, entry] of Object.entries(registry.models)) {
        for (const [quant, variant] of Object.entries(entry)) {
            if (!Array.isArray(variant?.files)) continue
            for (const file of variant.files) {
                const repo = file.repo || variant.repo
                const revision = file.revision || 'main'
                const label = `${model}/${quant}: ${repo}@${revision}/${file.file}`
                const key = JSON.stringify([repo, revision, file.file])
                checked++
                try {
                    if (!cache.has(key)) {
                        const base = `https://huggingface.co/${repo}`
                        const response = await request(`https://huggingface.co/api/models/${repo}/paths-info/${encodeURIComponent(revision)}`, {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({ paths: [file.file] }),
                            signal: AbortSignal.timeout(60000),
                        })
                        if (!response.ok) throw new Error(`metadata HTTP ${response.status}`)
                        const metadata = (await response.json()).find(item => item.path === file.file)
                        if (!metadata || metadata.type !== 'file') throw new Error('file not found')
                        let hash = metadata.lfs?.oid
                        if (!hash) {
                            const path = file.file.split('/').map(encodeURIComponent).join('/')
                            const content = await request(`${base}/resolve/${encodeURIComponent(revision)}/${path}`, {
                                signal: AbortSignal.timeout(60000),
                            })
                            if (!content.ok) throw new Error(`download HTTP ${content.status}`)
                            hash = createHash('sha256').update(Buffer.from(await content.arrayBuffer())).digest('hex')
                        }
                        cache.set(key, { hash, size: metadata.size })
                    }
                    const actual = cache.get(key)
                    if (actual.hash !== file.sha256 || actual.size !== file.size) {
                        throw new Error(`expected ${file.sha256} (${file.size} bytes), upstream ${actual.hash} (${actual.size} bytes)`)
                    }
                    log(`OK ${label}`)
                } catch (error) {
                    failures.push(`${label}: ${error.message}`)
                }
            }
        }
    }
    if (!checked) throw new Error('No registry files checked')
    if (failures.length) throw new Error(`${failures.length}/${checked} registry pins failed:\n${failures.join('\n')}`)
    log(`Verified ${checked} registry file pins (${cache.size} unique upstream files).`)
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
    try {
        const registry = YAML.parse(await readFile(new URL('../models/registry.yml', import.meta.url), 'utf8'))
        await checkRegistry(registry)
    } catch (error) {
        console.error(error.message)
        process.exitCode = 1
    }
}