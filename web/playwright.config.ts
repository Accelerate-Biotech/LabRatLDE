import { defineConfig, devices } from '@playwright/test';

// SPEC-008 feeds test-results/results.json into `make validate`.
export default defineConfig({
  testDir: 'e2e',
  use: {
    baseURL: process.env.LABRAT_URL ?? 'http://localhost:8080',
  },
  reporter: [['list'], ['json', { outputFile: 'test-results/results.json' }]],
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
});
