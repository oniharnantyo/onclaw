import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";
import "./styles.css";

// SAFETY: index.html ships a <div id="root"> and the script is
// module-deferred, so the element exists before this runs.
ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
);
