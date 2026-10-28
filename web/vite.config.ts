import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// The API runs on :8080. The proxy is for `npm run dev` only; the built
// assets are embedded in the Go binary and served by the same origin.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist',
    assetsDir: 'assets',
  },
  server: {
    proxy: {
      '/v1': 'http://localhost:8080',
    },
  },
});
