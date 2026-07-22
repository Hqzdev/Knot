import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { servicePaths } from "./src/domain/servicePaths";

const services = {
  [servicePaths.api]: "http://127.0.0.1:8080",
  [servicePaths.gateway]: "http://127.0.0.1:8086",
  [servicePaths.presence]: "http://127.0.0.1:8081",
  [servicePaths.attachments]: "http://127.0.0.1:8082",
  [servicePaths.push]: "http://127.0.0.1:8083",
};

export default defineConfig({
  plugins: [
    {
      name: "knot-wasm-external",
      enforce: "pre",
      resolveId(source) {
        return source === "knot-crypto-wasm" ? { id: source, external: true } : null;
      },
    },
    react(),
  ],
  optimizeDeps: {
    exclude: ["knot-crypto-wasm"],
  },
  server: {
    host: "127.0.0.1",
    port: 5173,
    proxy: Object.fromEntries(
      Object.entries(services).map(([path, target]) => [
        path,
        {
          target,
          changeOrigin: true,
          ws: true,
          rewrite: (requestPath: string) => requestPath.slice(path.length),
        },
      ]),
    ),
  },
  build: {
    target: "es2022",
    rollupOptions: {
      external: ["knot-crypto-wasm"],
    },
  },
});
