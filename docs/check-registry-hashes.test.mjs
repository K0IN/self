import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import test from 'node:test'
import { checkRegistry } from './check-registry-hashes.mjs'

const hash = 'a'.repeat(64)
const registry = files => ({ models: { 'model:1b': { q4: { repo: 'owner/model', files } } } })
const file = { file: 'model.gguf', size: 12, sha256: hash }
const metadata = (size = 12, oid = hash) => ({ ok: true, json: async () => [{ path: file.file, type: 'file', size, lfs: { oid } }] })
const quiet = () => {}

test('checks hashes and sizes, honors overrides, and caches duplicate files', async () => {
    const overridden = { ...file, repo: 'other/repo', revision: 'refs/pr/1' }
    let calls = 0
    await checkRegistry(registry([overridden, overridden]), async (url, options) => {
        calls++
        assert.match(url, /other\/repo\/paths-info\/refs%2Fpr%2F1$/)
        assert.deepEqual(JSON.parse(options.body), { paths: [file.file] })
        return metadata()
    }, quiet)
    assert.equal(calls, 1)
})

test('reports hash and size drift and continues checking other files', async () => {
    let calls = 0
    await assert.rejects(checkRegistry(registry([file, { ...file, file: 'second.gguf' }]), async () => {
        calls++
        return metadata(13, 'b'.repeat(64))
    }, quiet), /2\/2 registry pins failed/)
    assert.equal(calls, 2)
})

test('rejects missing files and failed metadata requests', async () => {
    await assert.rejects(checkRegistry(registry([file]), async () => ({ ok: true, json: async () => [] }), quiet), /file not found/)
    await assert.rejects(checkRegistry(registry([file]), async () => ({ ok: false, status: 503 }), quiet), /metadata HTTP 503/)
})

test('hashes non-LFS file contents instead of comparing Git blob IDs', async () => {
    const content = Buffer.from('small file')
    const small = { ...file, size: content.length, sha256: createHash('sha256').update(content).digest('hex') }
    await checkRegistry(registry([small]), async url => url.includes('/paths-info/')
        ? { ok: true, json: async () => [{ path: file.file, type: 'file', size: content.length }] }
        : { ok: true, arrayBuffer: async () => content }, quiet)
})

test('rejects empty registries', async () => {
    await assert.rejects(checkRegistry({ models: {} }, undefined, quiet), /No registry files checked/)
})