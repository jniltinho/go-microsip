import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import tailwindcss from "@tailwindcss/vite";

// Built with Vite Plus (`vp build`). Output goes to internal/gui/dist, which the Go binary embeds.
export default defineConfig({
  plugins: [vue(), tailwindcss()],
  base: "./",
  build: {
    outDir: "../internal/gui/dist",
    emptyOutDir: true,
  },
});
