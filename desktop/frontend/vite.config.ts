import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath } from 'node:url'
import { fileIcons } from '../../tools/vite-file-icons'

const here = fileURLToPath(new URL('.', import.meta.url))

export default defineConfig({
  base: './',
  server: {
    host: '127.0.0.1',
    port: 9245,
    strictPort: true,
  },
  plugins: [
    react(),
    tailwindcss(),
    fileIcons({ packageDirs: [here + 'node_modules/material-icon-theme'] }),
  ],
  build: {
    rollupOptions: {
      treeshake: {
        moduleSideEffects: (id) => !id.endsWith('/components/agent-session/index.ts'),
      },
    },
  },
})

