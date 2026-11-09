import { createRoot } from "react-dom/client";
import { App } from "./app/App";
import "./index.css";

// Suppress non-critical logs in production
if (import.meta.env.PROD) {
  console.log = () => {};
  console.debug = () => {};
  console.info = () => {};
}

const rootElement = document.getElementById("root");
if (!rootElement) {
  throw new Error("Failed to find the root element.");
}

createRoot(rootElement).render(<App />);
