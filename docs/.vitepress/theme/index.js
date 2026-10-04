import DefaultTheme from 'vitepress/theme'
import ApiField from './ApiField.vue'
import ModelCatalog from './ModelCatalog.vue'
import BenchmarkTable from './BenchmarkTable.vue'
import './custom.css'

export default {
    extends: DefaultTheme,
    enhanceApp({ app }) {
        app.component('ApiField', ApiField)
        app.component('ModelCatalog', ModelCatalog)
        app.component('BenchmarkTable', BenchmarkTable)
    }
}
