/// <reference types="vite/client" />
interface ImportMetaEnv {
  /** Set by vite.config.ts from the mode: "mock" (the simulated engine, src/mock.ts) or "wails". */
  readonly VITE_BACKEND: "mock" | "wails";
}
declare module "*.vue" {
  import type { DefineComponent } from "vue";
  const component: DefineComponent<object, object, unknown>;
  export default component;
}
