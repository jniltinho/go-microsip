// The window's single state: the reducer made reactive, fed by backend events.
import { reactive, ref, watch } from "vue";
import * as api from "./api";
import { UiState } from "./state";
import { applySkin, cachedSkin, setDesktopDark } from "./themes";

export const ui = reactive(new UiState(api)) as UiState;
/** Wall clock for durations, ages and the notice's lifetime. */
export const now = ref(Date.now());
/** Version text of the application. */
export const about = ref("");
/** Why the phone engine cannot be used; empty when it answered. */
export const unavailable = ref("");
/** Every contact, to show names for numbers. */
export const book = ref<api.Contact[]>([]);

/** The contact name of a number, if any. */
export const nameOf = (number: string) =>
  book.value.find((c) => c.numbers.some((n) => n.number === number))?.name;

ui.theme = cachedSkin();
watch(() => ui.theme, applySkin, { immediate: true });
watch(
  () => ui.contactsRevision,
  () =>
    api
      .contacts("")
      .then((list) => (book.value = list))
      .catch(() => (book.value = [])),
);

/** Reload the saved settings into the state. */
export async function loadSettings() {
  const s = await api.settings();
  ui.account = s.account;
  ui.dnd = s.dnd;
  ui.theme = s.theme;
  ui.hasPassword = s.hasPassword;
}

/** Connect to the backend, then tell it the page has drawn (the self-test waits for that). */
export async function start() {
  setInterval(() => (now.value = Date.now()), 500);
  try {
    about.value = await api.version();
    await loadSettings();
    await api.subscribe((event) =>
      event.type === "desktop" ? setDesktopDark(event.dark) : ui.applyEvent(event),
    );
    ui.contactsRevision++;
  } catch (e) {
    unavailable.value = String(e);
  } finally {
    await api.ready().catch(() => {});
  }
}
