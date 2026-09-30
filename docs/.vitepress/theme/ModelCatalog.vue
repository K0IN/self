<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { withBase } from 'vitepress'

const props = defineProps({
    models: {
        type: Array,
        required: true
    }
})

const query = ref('')
const selectedType = ref('all')
const selectedCapability = ref('all')
const searchInput = ref(null)

const types = computed(() => ['all', ...new Set(props.models.map(model => model.type))])
const capabilities = computed(() => ['all', ...new Set(props.models.flatMap(model => model.capabilities))])
const normalizedQuery = computed(() => query.value.trim().toLowerCase())

const filteredModels = computed(() => props.models.filter(model => {
    const haystack = [model.id, model.description, model.type, ...model.capabilities, ...model.quants].join(' ').toLowerCase()
    return (!normalizedQuery.value || haystack.includes(normalizedQuery.value)) &&
        (selectedType.value === 'all' || model.type === selectedType.value) &&
        (selectedCapability.value === 'all' || model.capabilities.includes(selectedCapability.value))
}))

const label = value => value === 'all' ? 'All' : value

const openModel = event => {
    if (event.target.closest('a')) return
    window.location.href = withBase(event.currentTarget.dataset.href)
}

const handleModelKeydown = event => {
    if (event.target.closest('a')) return
    if (event.key === 'Enter' || event.key === ' ') {
        event.preventDefault()
        openModel(event)
    }
}

const focusSearch = event => {
    if (event.key === '/' && event.target?.tagName !== 'INPUT' && event.target?.tagName !== 'TEXTAREA') {
        event.preventDefault()
        searchInput.value?.focus()
    }
}

onMounted(() => window.addEventListener('keydown', focusSearch))
onBeforeUnmount(() => window.removeEventListener('keydown', focusSearch))
</script>

<template>
    <section class="model-catalog" aria-label="Model registry">
        <div class="catalog-toolbar">
            <label class="catalog-search">
                <span class="sr-only">Search models</span>
                <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m21 21-4.35-4.35m2.1-5.4a7.5 7.5 0 1 1-15 0 7.5 7.5 0 0 1 15 0Z" /></svg>
                <input ref="searchInput" v-model="query" type="search" placeholder="Search models" autocomplete="off">
                <kbd>/</kbd>
            </label>
            <select v-model="selectedType" aria-label="Filter by model type">
                <option v-for="type in types" :key="type" :value="type">{{ label(type) }}</option>
            </select>
            <select v-model="selectedCapability" aria-label="Filter by capability">
                <option v-for="capability in capabilities" :key="capability" :value="capability">{{ label(capability) }}</option>
            </select>
        </div>

        <div class="catalog-summary">
            <span>{{ filteredModels.length }} model{{ filteredModels.length === 1 ? '' : 's' }}</span>
            <a :href="withBase('/models.yml')">Download models.yml</a>
        </div>

        <div v-if="filteredModels.length" class="model-grid">
            <article v-for="model in filteredModels" :key="model.id" class="model-card" :data-href="model.href" tabindex="0" role="link" @click="openModel" @keydown="handleModelKeydown">
                <div class="model-card-topline">
                    <span class="model-kind">{{ model.type }}</span>
                    <span class="model-arrow" aria-hidden="true">↗</span>
                </div>
                <h2><a :href="withBase(model.href)">{{ model.id }}</a></h2>
                <p>{{ model.description }}</p>
                <div class="model-tags">
                    <span v-for="quant in model.quants" :key="quant" class="model-tag">{{ quant }}</span>
                </div>
                <div class="model-sources">
                    <div v-for="variant in model.variants" :key="variant.name" class="model-source">
                        <strong>{{ variant.name }}</strong>
                        <a :href="variant.repoUrl" target="_blank" rel="noreferrer">{{ variant.repo }}</a>
                        <ul>
                            <li v-for="file in variant.files" :key="file.name">
                                <a :href="file.url" target="_blank" rel="noreferrer">{{ file.name }}</a>
                                <code>sha256:{{ file.sha256 }}</code>
                            </li>
                        </ul>
                    </div>
                </div>
                <div class="model-card-footer">
                    <span>{{ model.size }}</span>
                    <span>{{ model.capabilities.join(' · ') }}</span>
                </div>
            </article>
        </div>
        <div v-else class="catalog-empty">
            <strong>No models found</strong>
            <span>Try a different name, type, or capability.</span>
        </div>
    </section>
</template>
