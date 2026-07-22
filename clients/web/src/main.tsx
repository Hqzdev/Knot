import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./ui/App";
import { createApplication } from "./application/createApplication";
import "./ui/styles.css";

const application = createApplication();
const root = document.getElementById("root");

if (!root) {
  throw new Error("Application root is missing");
}

createRoot(root).render(
  <StrictMode>
    <App application={application} />
  </StrictMode>,
);
