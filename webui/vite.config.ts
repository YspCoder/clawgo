import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  base: '/webui/',
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/webui/api': {
        target: 'http://127.0.0.1:18790',
        changeOrigin: true,
      },
    },
  },
})
