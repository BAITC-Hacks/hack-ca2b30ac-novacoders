import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { createRequire } from 'node:module'
import { readFile } from 'node:fs/promises'
import path from 'node:path'
import type { Plugin as EsbuildPlugin } from 'esbuild'

// Optional restricted-Windows compatibility: Node resolves exact dependency files,
// avoiding esbuild's directory enumeration above the workspace. No access is added.
const restrictedFs: EsbuildPlugin = {
  name: 'workspace-dependency-resolver',
  setup(build) {
    build.onResolve({ filter: /.*/ }, (args) => {
      if (args.kind === 'entry-point') return undefined
      const resolver = createRequire(path.join(args.resolveDir || process.cwd(), '__resolve.cjs'))
      return { path: resolver.resolve(args.path), namespace: 'workspace-dependency' }
    })
    build.onLoad({ filter: /.*/, namespace: 'workspace-dependency' }, async (args) => ({
      contents: await readFile(args.path, 'utf8'),
      loader: args.path.endsWith('.json') ? 'json' : 'js',
      resolveDir: path.dirname(args.path),
    }))
  },
}

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  return {
    plugins: [react(), tailwindcss()],
    optimizeDeps: process.env.SUPPLYLENS_RESTRICTED_FS === '1' ? {
      noDiscovery: true,
      include: ['react', 'react-dom/client', 'react/jsx-dev-runtime', 'react/jsx-runtime', 'lucide-react'],
      esbuildOptions: { plugins: [restrictedFs] },
    } : undefined,
    server: { port: 5173, strictPort: true, proxy: { '/api': { target: env.API_PROXY_TARGET || 'http://127.0.0.1:8080', changeOrigin: true } } },
    build: { chunkSizeWarningLimit: 700 },
  }
})
