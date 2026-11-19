/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// In development the fleet API (Java) and ingest service (Go) run on separate ports;
// in production nginx serves the same paths, so the app always uses relative URLs.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": process.env.FLEET_API_URL ?? "http://localhost:8080",
      "/ws": { target: process.env.INGEST_WS_URL ?? "ws://localhost:8081", ws: true },
    },
  },
  build: {
    sourcemap: true,
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test/setup.ts"],
    css: false,
  },
});
