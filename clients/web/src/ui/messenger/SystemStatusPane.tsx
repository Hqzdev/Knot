"use client";

import { useEffect, useState } from "react";

interface ServiceStatus {
  name: string;
  path: string;
  active: boolean;
  latency: number;
}

const services = [
  { name: "API", path: "/api/ready" },
  { name: "Gateway", path: "/gateway/ready" },
  { name: "Presence", path: "/presence/ready" },
  { name: "Attachments", path: "/attachments/readyz" },
  { name: "Preview", path: "/preview/healthz" },
  { name: "Toxic support", path: "/bot/ready" },
];

export function SystemStatusPane() {
  const [values, setValues] = useState<ServiceStatus[]>([]);
  useEffect(() => {
    let active = true;
    const load = async () => {
      const next = await Promise.all(services.map(async (service) => {
        const started = performance.now();
        try {
          const response = await fetch(service.path, { cache: "no-store" });
          return { ...service, active: response.ok, latency: Math.round(performance.now() - started) };
        } catch {
          return { ...service, active: false, latency: Math.round(performance.now() - started) };
        }
      }));
      if (active) {
        setValues(next);
      }
    };
    void load();
    const timer = setInterval(() => void load(), 30_000);
    return () => { active = false; clearInterval(timer); };
  }, []);
  const operational = values.length === services.length && values.every((value) => value.active);
  return (
    <section className="saved-pane">
      <header><p className="eyebrow">Actual readiness probes</p><h1>System status</h1><p>{operational ? "Working against expectations" : "One or more exhibit parts are honestly unavailable"}</p></header>
      <div>{values.map((value) => <article key={value.name}><p>{value.name}</p><footer><span>{value.active ? "Ready" : "Unavailable"}</span><b>{value.latency}ms</b></footer></article>)}</div>
    </section>
  );
}
