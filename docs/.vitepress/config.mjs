import { defineConfig } from 'vitepress'

export default defineConfig({
    title: 'self',
    description: 'Local AI model server and registry',
    base: process.env.DOCS_BASE || '/',
    cleanUrls: true,
    themeConfig: {
        nav: [
            { text: 'Guide', link: '/' },
            { text: 'Models', link: '/registry/' },
            { text: 'GitHub', link: 'https://github.com/k0in/self' }
        ],
        sidebar: [
            { text: 'Getting started', link: '/' },
            { text: 'Docker and model paths', link: '/docker' },
            { text: 'Local settings', link: '/settings' },
            { text: 'CLI and completions', link: '/cli' },
            { text: 'Benchmarks', link: '/benchmarks' },
            {
                text: 'API reference',
                link: '/api/',
                collapsed: false,
                items: [
                    { text: 'Decision', link: '/api/decision' },
                    { text: 'Audio generation', link: '/api/audio' },
                    { text: 'Text', link: '/api/text' },
                    { text: 'Embeddings', link: '/api/embeddings' },
                    { text: 'Image', link: '/api/images' },
                    { text: 'Speech to text', link: '/api/stt' }
                ]
            },
            { text: 'Model registry', link: '/registry/' }
        ],
        search: { provider: 'local' }
    }
})
