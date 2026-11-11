import { useEffect, type ReactNode } from "react";
function applyTheme(theme: string) {
  const dark =
    theme === "dark" || (theme === "system" && matchMedia("(prefers-color-scheme: dark)").matches);
  document.documentElement.classList.toggle("dark", dark);
}
export function ThemeProvider({ children }: { children: ReactNode }) {
  useEffect(() => {
    const media = matchMedia("(prefers-color-scheme: dark)");
    const update = () => applyTheme(localStorage.getItem("theme") ?? "system");
    update();
    media.addEventListener("change", update);
    window.addEventListener("themechange", update);
    return () => {
      media.removeEventListener("change", update);
      window.removeEventListener("themechange", update);
    };
  }, []);
  return children;
}
