import { configDefaults, defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath, URL } from "node:url";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  build: {
    outDir: "../../internal/dashboard/assets",
    emptyOutDir: true,
    sourcemap: false,
    assetsInlineLimit: 4096
  },
  test: { environment: "jsdom", setupFiles: "./src/test/setup.ts", css: true, exclude: [...configDefaults.exclude, "e2e/**"] }
});
