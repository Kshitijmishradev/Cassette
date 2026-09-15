import { useState } from "react";

export function ThemeToggle() {
  const initial = document.documentElement.dataset.theme || "dark";
  const [theme, setTheme] = useState(initial);

  const toggle = () => {
    const next = theme === "dark" ? "light" : "dark";
    document.documentElement.dataset.theme = next;
    localStorage.setItem("cassette-theme", next);
    setTheme(next);
  };

  return (
    <button
      className="icon-button"
      type="button"
      onClick={toggle}
      title={`Use ${theme === "dark" ? "light" : "dark"} theme`}
    >
      <span aria-hidden="true">{theme === "dark" ? "☼" : "◐"}</span>
      <span className="sr-only">
        Use {theme === "dark" ? "light" : "dark"} theme
      </span>
    </button>
  );
}
