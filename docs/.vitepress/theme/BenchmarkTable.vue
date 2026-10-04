<script setup>
import { computed, onMounted, ref } from 'vue'
import { withBase } from 'vitepress'

const props = defineProps({ modelId: { type: String, default: '' } })
const reports = ref([])
const hardware = ref('default')

onMounted(async () => {
    const response = await fetch(withBase('/benchmarks.json'))
    if (!response.ok) return
    const manifest = await response.json()
    const runs = await Promise.all(manifest.map(async entry => {
        const report = await fetch(withBase(entry.url))
        return report.ok ? report.json() : null
    }))
    reports.value = runs.filter(Boolean)
})

const modelReports = computed(() => reports.value.filter(report => !props.modelId || report.model === props.modelId))
const reportHardware = report => report.gpuUsed ? report.gpu || 'GPU' : 'CPU'
const hardwareOptions = computed(() => ['default', ...new Set(modelReports.value.map(reportHardware))])
const detectedGpus = computed(() => [...new Set(modelReports.value.flatMap(report => report.availableGpus || []))])
const newestReports = computed(() => {
    const newest = new Map()
    for (const report of modelReports.value) {
        const key = `${report.model}|${report.quant}|${report.modality}|${reportHardware(report)}`
        const current = newest.get(key)
        if (!current || report.createdAt > current.createdAt) newest.set(key, report)
    }
    return [...newest.values()]
})
const visibleReports = computed(() => newestReports.value.filter(report => hardware.value === 'default' || reportHardware(report) === hardware.value))
const engineName = report => report.engine?.split('/').pop() || 'unknown engine'
const format = value => typeof value === 'number' ? value.toFixed(1) : '-'
</script>

<template>
    <section class="benchmark-table" aria-label="Model benchmarks">
        <div class="benchmark-heading">
            <p v-if="!visibleReports.length">No submitted benchmark reports yet.</p>
            <label v-if="visibleReports.length">Hardware
                <select v-model="hardware" aria-label="Filter benchmark hardware">
                    <option v-for="option in hardwareOptions" :key="option" :value="option">{{ option === 'default' ? 'Default' : option }}</option>
                </select>
            </label>
        </div>
        <div v-for="report in visibleReports" :key="report.file" class="benchmark-report">
            <div class="benchmark-meta"><strong>{{ report.model }}</strong><span>{{ reportHardware(report) }} · {{ report.quant }} · {{ report.modality }} · {{ report.adapter }} · {{ engineName(report) }} · benchmark v{{ report.benchmarkVersion }} · <code>{{ report.selfCommit }}</code></span></div>
            <table>
                <thead><tr><th>Scenario</th><th>p50</th><th>p95</th><th>Throughput</th><th>Requests/s</th></tr></thead>
                <tbody><tr v-for="scenario in report.scenarios" :key="scenario.name"><td>{{ scenario.name }}</td><td>{{ format(scenario.p50Ms) }} ms</td><td>{{ format(scenario.p95Ms) }} ms</td><td>{{ format(scenario.throughput || scenario.inputTokensPerSec) }} {{ scenario.throughputUnit || 'input_tokens_per_sec' }}</td><td>{{ format(scenario.requestsPerSec) }}</td></tr></tbody>
            </table>
        </div>
    </section>
</template>