import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import "./index.css";

const container = document.getElementById("root");
if (!container) {
  throw new Error("root 容器未找到:#root");
}

createRoot(container).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
