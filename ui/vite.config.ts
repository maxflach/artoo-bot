import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  base: '/chat/',
  plugins: [react(), tailwindcss()],
  build: { outDir: '../src/webchat_dist', emptyOutDir: true },
})
