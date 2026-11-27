import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'
import { fileURLToPath, URL } from 'node:url'

/**
 * HCS-inspired bundle strategy:
 * - Element Plus JS components & directives are tree-shaken on demand via unplugin-vue-components.
 * - Element Plus base CSS is imported ONCE in main.ts before our tokens & overrides so cascade order is deterministic.
 * - Heavy domain widgets (xterm, codemirror, xyflow) are lazy chunks isolated from first-screen entry JS (O-09 <= 350 KiB gzip).
 */
function chunkOf(id: string): string | undefined {
  if (!id.includes('node_modules')) return undefined
  if (/node_modules[\\/](vue|@vue|vue-router|pinia|vue-i18n)[\\/]/.test(id)) return 'vue-vendor'
  if (/node_modules[\\/](@xterm)[\\/]/.test(id)) return 'xterm'
  if (/node_modules[\\/](@codemirror|@lezer)[\\/]/.test(id)) return 'codemirror'
  if (/node_modules[\\/](@xyflow)[\\/]/.test(id)) return 'xyflow'
  if (/node_modules[\\/](lucide-vue-next)[\\/]/.test(id)) return 'icons'
  return undefined
}

export default defineConfig({
  plugins: [
    vue(),
    Components({
      resolvers: [ElementPlusResolver({ importStyle: false })],
      dirs: [],
      dts: false,
    }),
  ],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://127.0.0.1:8080', changeOrigin: true },
      '/healthz': { target: 'http://127.0.0.1:8080', changeOrigin: true },
      '/readyz': { target: 'http://127.0.0.1:8080', changeOrigin: true },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: false,
    rollupOptions: {
      output: {
        manualChunks: chunkOf,
      },
    },
  },
})
