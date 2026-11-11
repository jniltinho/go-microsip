import { createApp } from "vue";
import App from "./App.vue";
import "./style.css";

// No WebKit context menu (Back/Reload/Inspect): the app shows its own where it needs one.
window.addEventListener("contextmenu", (e) => e.preventDefault());

createApp(App).mount("#app");
