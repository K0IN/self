import { defineConfig } from 'vitepress'

export default defineConfig({
    title: 'self',
    description: 'Local AI model server and registry',
    base: '/self/',
    cleanUrls: true,
    themeConfig: {
        nav: [
            { text: 'Guide', link: '/self/' },
            { text: 'Models', link: '/registry/' },
            { text: 'GitHub', link: 'https://github.com/k0in/self' }
        ],
        sidebar: [
            { text: 'Getting started', link: '/self/' },
            { text: 'Docker and model paths', link: '/self/docker' },
            { text: 'Local settings', link: '/self/settings' },
            { text: 'CLI and completions', link: '/self/cli' },
            { text: 'Model registry', link: '/registry/' }
        ],
        search: { provider: 'local' }
    }
})
