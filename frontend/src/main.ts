import { createApp } from "vue";
import App from "./App.vue";
import "./style.css";

// No WebKit context menu (Back/Reload/Inspect): the app shows its own where it needs one.
window.addEventListener("contextmenu", (e) => e.preventDefault());

// `autofocus` only works when the page loads or a dialog opens. A view drawn later takes the focus
// with v-focus, so a person at the keyboard can type at once.
createApp(App)
  .directive("focus", { mounted: (el: HTMLElement) => el.focus() })
  .mount("#app");
