import DefaultTheme from 'vitepress/theme'
import ModelCatalog from './ModelCatalog.vue'
import './custom.css'

export default {
    extends: DefaultTheme,
    enhanceApp({ app }) {
        app.component('ModelCatalog', ModelCatalog)
    }
}
