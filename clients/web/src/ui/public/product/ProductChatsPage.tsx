"use client";

import { useState } from "react";
import { getPublicArticlePath } from "../PublicContentCatalog";
import { ProductBreadcrumb, ProductShell } from "./ProductShell";

type ChatMessage = {
  actor: string;
  current: string;
  depth: 0 | 1 | 2;
  id: string;
  original: string;
  relation: string;
  route: string;
  session: string;
  time: string;
};

const chatMessages: readonly ChatMessage[] = [
  {
    actor: "mira",
    current: "The delivery route keeps a copy before this message appears in the room.",
    depth: 0,
    id: "mira-original",
    original: "The delivery route keeps a copy before this message appears in the room.",
    relation: "Root message · direct / mira + otto",
    route: "Gateway → Router → Delivery",
    session: "Password session",
    time: "14:02",
  },
  {
    actor: "otto",
    current: "So the room only looks private from the client.",
    depth: 1,
    id: "otto-reply",
    original: "So this room only looks private from the client.",
    relation: "Reply to mira · revised once",
    route: "Gateway → Router → Delivery",
    session: "Password session",
    time: "14:04",
  },
  {
    actor: "ivy",
    current: "Forwarded into research with the reply chain attached.",
    depth: 2,
    id: "ivy-forward",
    original: "Forwarded into research with the reply chain attached.",
    relation: "Forward of otto’s reply · research",
    route: "History → Delivery → Wiretap",
    session: "Guest session",
    time: "14:07",
  },
  {
    actor: "mira",
    current: "The reply relationship is visible even after the room changes state.",
    depth: 1,
    id: "mira-follow-up",
    original: "The reply relationship is visible even after the room changes state.",
    relation: "Reply to mira · direct / mira + otto",
    route: "Gateway → Router → Delivery",
    session: "Password session",
    time: "14:09",
  },
];

const participants = [
  ["mira", "Author", "4 records"],
  ["otto", "Recipient", "2 records"],
  ["ivy", "Forward recipient", "1 record"],
] as const;

export function ProductChatsPage() {
  const [selectedId, setSelectedId] = useState(chatMessages[0].id);
  const selected = chatMessages.find((message) => message.id === selectedId) ?? chatMessages[0];

  return (
    <ProductShell current="chats">
      <section className="product-chats-hero">
        <aside className="product-chats-participants">
          <ProductBreadcrumb current="Chats" />
          <p className="product-kicker">Direct conversation</p>
          <h1>Chats organize people. They do not contain the record.</h1>
          <p>Choose a message to inspect how replies, revisions and forwards stay attached to it.</p>
          <ul aria-label="Conversation participants">
            {participants.map(([name, role, records]) => <li key={name}><b>{name}</b><span>{role}</span><small>{records}</small></li>)}
          </ul>
        </aside>

        <section aria-label="Conversation reply tree" className="product-chats-thread">
          <header><div><p className="product-kicker">Conversation record</p><h2>direct / mira + otto</h2></div><span>4 events</span></header>
          <ol>
            {chatMessages.map((message) => (
              <li data-depth={message.depth} key={message.id}>
                <button aria-pressed={selected.id === message.id} type="button" onClick={() => setSelectedId(message.id)}>
                  <span><b>{message.actor}</b><small>{message.time}</small></span>
                  <p>{message.current}</p>
                  <i>{message.relation}</i>
                </button>
              </li>
            ))}
          </ol>
        </section>

        <aside aria-live="polite" className="product-chats-inspector">
          <p className="product-kicker">Selected message</p>
          <h2>{selected.actor} · {selected.time}</h2>
          <dl>
            <div><dt>Current</dt><dd>{selected.current}</dd></div>
            <div><dt>Original</dt><dd>{selected.original}</dd></div>
            <div><dt>Relation</dt><dd>{selected.relation}</dd></div>
            <div><dt>Session</dt><dd>{selected.session}</dd></div>
            <div><dt>Route</dt><dd>{selected.route}</dd></div>
          </dl>
        </aside>
      </section>

      <section className="product-chats-layers">
        <header><p className="product-kicker">A message has layers</p><h2>The chat bubble is only one readable view of the same record.</h2></header>
        <div>
          <article className="product-chats-current"><span>Current content</span><p>What the room renders now can be revised, removed or replaced.</p></article>
          <article className="product-chats-original"><span>Original content</span><p>The first body remains connected to later state changes in history and audit events.</p></article>
          <article className="product-chats-context"><span>Conversation context</span><p>Actor, audience, session and reply relationship make the body more identifiable.</p></article>
        </div>
      </section>

      <section className="product-chats-audience">
        <article><p className="product-kicker">Membership boundary</p><h2>Participants see a room.</h2><p>Membership helps people decide where to speak and who should reply. It is an interface convention.</p></article>
        <article><p className="product-kicker">Server audience</p><h2>Infrastructure sees a message event.</h2><p>Delivery, history and Wiretap receive structured content regardless of who appears in the room’s participant rail.</p><a href={getPublicArticlePath("wiretap", "route-traces")}>Trace one message route →</a></article>
      </section>

      <section className="product-chats-end"><div><p className="product-kicker">Use the product</p><h2>Familiar messaging controls need a trustworthy path beneath them.</h2></div><a className="product-cta" href="/login">Open Knot ↗</a></section>
    </ProductShell>
  );
}
