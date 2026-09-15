import { StrictMode } from "react";
import { createApp } from "react-dom/client";
import App from "./App.jsx";
import "./styles.css";

document.documentElement.dataset.theme =
  localStorage.getItem("cassette-theme") || "dark";

createApp(document.getElementById("root")).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
