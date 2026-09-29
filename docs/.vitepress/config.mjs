import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'self',
  description: 'Local AI model server and registry',
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
      { text: 'Model registry', link: '/registry/' }
    ],
    search: { provider: 'local' }
  }
})
