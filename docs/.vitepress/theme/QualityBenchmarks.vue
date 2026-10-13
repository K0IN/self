<script setup>
defineProps({ results: { type: Array, required: true }, compact: Boolean })
const formatEvaluation = evaluation => evaluation.score.toFixed(2)
const labels = {
    ranked: 'Ranked', listed: 'Listed; rank not imported', reported: 'Publisher-reported',
    'not-listed': 'Not listed', 'no-structured-results': 'No structured results',
    unavailable: 'Source unavailable', unverified: 'Coverage unverified', 'not-fetched': 'Not fetched'
}
</script>

<template>
    <div class="quality-results">
        <div v-for="result in results" :key="result.id" class="quality-result">
            <a :href="result.url" @click.stop>{{ result.name }}</a>:
            <span v-if="result.status === 'ranked'">#{{ result.rank }} ({{ result.score.toFixed(2) }})</span>
            <span v-else>{{ labels[result.status] || result.status }}</span>
            <span v-if="compact && result.evaluations?.length"> ({{ result.evaluations[0].metric }}: {{ formatEvaluation(result.evaluations[0]) }})</span>
            <strong v-if="result.stale"> (stale)</strong>
            <template v-if="!compact">
                <p v-if="result.evaluatedModel"><code>{{ result.evaluatedModel }}</code></p>
                <p v-if="result.scope">{{ result.scope }}</p>
                <p v-if="result.evaluatedConfiguration">Evaluated: {{ result.evaluatedConfiguration }}</p>
                <p v-if="result.edition">Edition: {{ result.edition }}</p>
                <p v-if="result.modelPin">Checkpoint: <code>{{ result.modelPin }}</code></p>
                <p v-if="result.publishedAt">Published {{ result.publishedAt }}; {{ result.votes }} votes</p>
                <p v-if="result.fetchedAt">Checked {{ result.fetchedAt.slice(0, 10) }}</p>
                <div v-if="result.evaluations?.length" class="quality-table">
                    <table>
                        <thead><tr><th>Dataset</th><th>Metric</th><th>Score</th></tr></thead>
                        <tbody><tr v-for="(evaluation, index) in result.evaluations" :key="index">
                            <td>{{ evaluation.dataset }}{{ evaluation.split ? ` (${evaluation.split})` : '' }}</td>
                            <td>{{ evaluation.metric }}</td><td>{{ formatEvaluation(evaluation) }}</td>
                        </tr></tbody>
                    </table>
                </div>
            </template>
        </div>
    </div>
</template>

<style scoped>
.quality-results { font-size: 14px; line-height: 1.5; }
.quality-result { margin: 8px 0; overflow-wrap: anywhere; }
.quality-result p { margin: 4px 0; color: var(--vp-c-text-2); }
.quality-table { overflow-x: auto; }
</style>