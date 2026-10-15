import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// Wails 要求 base 为相对路径，便于嵌入打包。
export default defineConfig({
  base: './',
  plugins: [vue()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    target: 'esnext',
  },
  server: {
    port: 34115,
    strictPort: true,
    // Wails dev 会注入运行环境，无需严格同源
    cors: true,
  },
})
