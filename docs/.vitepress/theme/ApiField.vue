<script setup>
defineProps({
    name: { type: String, required: true },
    type: { type: String, default: '' },
    required: { type: Boolean, default: false },
    optional: { type: Boolean, default: false }
})
</script>

<template>
    <div class="api-field">
        <div class="api-field-head">
            <code class="api-field-name">{{ name }}</code>
            <span v-if="type" class="api-field-type">{{ type }}</span>
            <span v-if="required" class="api-field-flag api-field-required">required</span>
            <span v-else-if="optional" class="api-field-flag">optional</span>
        </div>
        <div class="api-field-body"><slot /></div>
    </div>
</template>
