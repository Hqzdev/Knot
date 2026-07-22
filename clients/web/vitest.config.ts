import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    environment: "node",
    include: ["src/**/*.test.ts"],
    pool: "forks",
    maxWorkers: 1,
    fileParallelism: false,
    testTimeout: 10_000,
    hookTimeout: 10_000,
  },
});
