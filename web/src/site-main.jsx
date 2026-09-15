import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import Root from "./Root.jsx";
import "./styles.css";

document.documentElement.dataset.theme =
  localStorage.getItem("cassette-theme") || "dark";

createRoot(document.getElementById("root")).render(
  <StrictMode>
    <Root />
  </StrictMode>,
);
