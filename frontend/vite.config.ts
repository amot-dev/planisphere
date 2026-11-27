import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  test: {
    setupFiles: ['./src/test/setup.ts'],
    clearMocks: true,
    restoreMocks: true,
    coverage: {
      provider: 'v8',
      include: ['src/**/*.{ts,tsx}'],
      exclude: [
        'src/**/*.test.{ts,tsx}',
        'src/**/*.d.ts',
        'src/test/**',
        'src/profile/**',
        'src/features/export/exportTestFixtures.ts',
        'src/version.ts',
        'src/main.tsx',
      ],
      reportsDirectory: '../coverage/frontend',
      reporter: ['text', 'json-summary', 'json', 'lcov', 'html'],
    },
  },
})
