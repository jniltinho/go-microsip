// The window's API: github.com/jniltinho/go-microsip/internal/gui.App, bound by Wails v3.
import { Call } from "@wailsio/runtime";

const svc = "github.com/jniltinho/go-microsip/internal/gui.App.";
// A failed call rejects with the Go error's text alone.
const call = <T>(method: string, ...args: unknown[]) =>
  (Call.ByName(svc + method, ...args) as Promise<T>).catch((e: unknown) => {
    throw e instanceof Error ? e.message : e;
  });

/** The application's version and the commit it was built from. */
export const version = () => call<string>("Version");
/** Tells the backend the page has drawn; `--self-test` then exits 0. */
export const ready = () => call<void>("Ready");
