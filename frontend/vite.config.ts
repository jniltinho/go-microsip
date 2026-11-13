import { defineConfig } from "vite-plus";
import vue from "@vitejs/plugin-vue";
import tailwindcss from "@tailwindcss/vite";

// Built with Vite Plus (`vp build`). Output goes to internal/gui/dist, which the Go binary embeds.
// The backend transport of src/api.ts follows the mode: `vp dev` (development) and `--mode mock` use
// the simulated engine, every other build the Wails one. The mock build goes to dist-mock and is
// never embedded.
export default defineConfig(({ mode }) => ({
  define: {
    "import.meta.env.VITE_BACKEND": JSON.stringify(
      mode === "mock" || mode === "development" ? "mock" : "wails",
    ),
  },
  plugins: [vue(), tailwindcss()],
  base: "./",
  build: {
    outDir: mode === "mock" ? "dist-mock" : "../internal/gui/dist",
    emptyOutDir: true,
  },
  // The skin tests read the stylesheets as text.
  test: { css: true },
  // `vp check` also type-checks.
  lint: { options: { typeAware: true, typeCheck: true } },
}));
