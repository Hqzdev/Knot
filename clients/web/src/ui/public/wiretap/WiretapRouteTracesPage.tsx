"use client";

import { useState } from "react";
import { getPublicArticlePath } from "../PublicContentCatalog";
import { WiretapBreadcrumb, WiretapCta, WiretapShell } from "./WiretapShell";

const services = [
  { id: "gateway", name: "Gateway", input: "Plaintext message and session token", action: "Accepts the WebSocket event and publishes it to routing.", output: "Accepted over insecure WebSocket", time: "14:02:08.001" },
  { id: "router", name: "Router", input: "Message body, participants and conversation context", action: "Inspects plaintext and selects conversation recipients.", output: "Inspected plaintext", time: "14:02:08.004" },
  { id: "delivery", name: "Delivery", input: "Recipient list and complete message event", action: "Stores the event, emits Wiretap activity and fans it out.", output: "Stored plaintext", time: "14:02:08.009" },
] as const;

export function WiretapRouteTracesPage() {
  const [active, setActive] = useState("gateway");
  const current = services.find((service) => service.id === active) ?? services[0];

  return (
    <WiretapShell current="route-traces">
      <section className="wiretap-routes-hero">
        <aside>
          <WiretapBreadcrumb current="Route traces" />
          <p className="wiretap-kicker">Message trace</p>
          <h1>Follow one message through every service.</h1>
          <dl><div><dt>Message</dt><dd>msg_8f3a</dd></div><div><dt>Conversation</dt><dd>direct / mira + otto</dd></div><div><dt>Mode</dt><dd>password</dd></div></dl>
        </aside>
        <section aria-label="Service route" className="wiretap-route-map">
          <p>Browser</p>
          <div className="wiretap-route-line" />
          {services.map((service, index) => (
            <button aria-pressed={active === service.id} className={active === service.id ? "is-active" : ""} key={service.id} type="button" onClick={() => setActive(service.id)}>
              <span>0{index + 1}</span><b>{service.name}</b><small>{service.output}</small>
            </button>
          ))}
          <div className="wiretap-route-line" />
          <p>Wiretap</p>
        </section>
        <section aria-live="polite" className="wiretap-route-detail">
          <p className="wiretap-kicker">Selected service</p><h2>{current.name}</h2>
          <dl><div><dt>Input</dt><dd>{current.input}</dd></div><div><dt>Action</dt><dd>{current.action}</dd></div><div><dt>Output</dt><dd>{current.output}</dd></div></dl>
        </section>
      </section>

      <section aria-label="Message transport path" className="wiretap-routes-corridor"><span>Browser</span><i /><span>Gateway</span><i /><span>Router</span><i /><span>Delivery</span><i /><span>Wiretap</span></section>

      <section className="wiretap-routes-details">
        <div><p className="wiretap-kicker">Service notes</p><h2>Every hop receives a readable message.</h2></div>
        <div className="wiretap-route-accordions">
          {services.map((service) => (
            <details key={service.id} open={active === service.id} onToggle={(event) => event.currentTarget.open && setActive(service.id)}>
              <summary><span>{service.name}</span><b>{service.time}</b></summary>
              <p>{service.action}</p>
            </details>
          ))}
        </div>
      </section>

      <section className="wiretap-routes-latency">
        <p className="wiretap-kicker">Latency trace</p>
        <ol>{services.map((service, index) => <li key={service.id}><span>{service.time}</span><i /><b>{service.name}</b><p>{index === 0 ? "Message enters the public transport path." : index === 1 ? "Content is inspected before recipient selection." : "The durable record is published."}</p></li>)}</ol>
      </section>

      <section className="wiretap-routes-telemetry">
        <div><p className="wiretap-kicker">Telemetry consequence</p><h2>Operational context becomes another part of the message.</h2><p>Trace data reveals infrastructure shape, processing order and timing beside the content it was meant to move.</p></div>
        <div className="wiretap-routes-telemetry-card"><b>03</b><span>Readable service hops</span><small>One message trace</small></div>
      </section>

      <section className="wiretap-routes-table">
        <header><p className="wiretap-kicker">Attached fields</p><h2>What travels with the message</h2></header>
        <dl><div><dt>Body</dt><dd>Current and original plaintext</dd></div><div><dt>Actor</dt><dd>User identity and session mode</dd></div><div><dt>Session</dt><dd>Issued access context</dd></div><div><dt>Route</dt><dd>Service names, statuses and timestamps</dd></div></dl>
      </section>

      <section className="wiretap-routes-end"><div><p className="wiretap-kicker">Transport</p><h2>Authentication does not encrypt the path.</h2></div><WiretapCta href={getPublicArticlePath("how-it-leaks", "plain-http-and-ws")}>Read Plain HTTP and WS →</WiretapCta></section>
    </WiretapShell>
  );
}
