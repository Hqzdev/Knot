"use client";

import { useState } from "react";
import { getPublicArticlePath } from "../PublicContentCatalog";
import { ProductBreadcrumb, ProductShell } from "./ProductShell";

type MatchRecord = {
  createdAt: string;
  guest: string;
  room: string;
  session: string;
  user: string;
};

const matches: readonly MatchRecord[] = [
  { createdAt: "14:02:08", guest: "guest-17", room: "roulette_8f3a", session: "Guest session", user: "mira" },
  { createdAt: "14:07:41", guest: "ivy", room: "roulette_c912", session: "Password session", user: "otto" },
  { createdAt: "14:11:26", guest: "mira-copy", room: "roulette_4d70", session: "Guest session", user: "guest-17" },
];

const queue = ["mira", "otto", "guest-17", "ivy", "mira-copy"] as const;

const retainedEvents = [
  ["Room", "roulette_8f3a", "Created when two queue entries were paired"],
  ["Messages", "04", "Stored with authors, relationship and delivery route"],
  ["Presence", "02", "Join and leave states remain observable"],
  ["History", "Available", "Leaving does not remove the completed room"],
] as const;

export function ProductRoulettePage() {
  const [matchIndex, setMatchIndex] = useState(0);
  const [hasDrawn, setHasDrawn] = useState(false);
  const current = matches[matchIndex];
  const drawMatch = () => {
    setMatchIndex((index) => (index + 1) % matches.length);
    setHasDrawn(true);
  };

  return (
    <ProductShell current="roulette">
      <section className="product-roulette-chamber">
        <aside className="product-roulette-queue">
          <ProductBreadcrumb current="Roulette" />
          <p className="product-kicker">Available queue</p>
          <h1>A random match is still a durable room.</h1>
          <p>The queue changes who appears on the other side. It does not change what the service creates when the pair is made.</p>
          <ol aria-label="Sample matching queue">
            {queue.map((identity) => <li className={identity === current.user || identity === current.guest ? "is-matched" : ""} key={identity}><span /><b>{identity}</b><small>{identity === current.user || identity === current.guest ? "paired" : "waiting"}</small></li>)}
          </ol>
        </aside>

        <section className="product-roulette-draw">
          <span>Queue</span><i /><button type="button" onClick={drawMatch}>Draw a match</button><i /><span>Room</span>
        </section>

        <section aria-live="polite" className="product-roulette-record">
          <p className="product-kicker">Created session</p>
          <code>{current.room}</code>
          <div className="product-roulette-pair"><b>{current.user}</b><i>×</i><b>{current.guest}</b></div>
          <dl><div><dt>Created</dt><dd>{current.createdAt}</dd></div><div><dt>Access</dt><dd>{current.session}</dd></div><div><dt>Status</dt><dd>{hasDrawn ? "Matched and recorded" : "Ready to record"}</dd></div></dl>
        </section>
      </section>

      <section className="product-roulette-rule"><p>Random matching changes the partner.</p><span>It does not remove identity.</span><p>It does not remove history.</p></section>

      <section className="product-roulette-lifecycle"><header><p className="product-kicker">One match, four states</p><h2>A temporary social premise produces a persistent system record.</h2></header><ol><li><span>01</span><div><h3>Queue</h3><p>An available identity enters a service-visible matching pool.</p></div></li><li><span>02</span><div><h3>Pair</h3><p>Two queue entries become a named room with a creation event.</p></div></li><li><span>03</span><div><h3>Talk</h3><p>Messages follow the same delivery, history and Wiretap path as any chat.</p></div></li><li><span>04</span><div><h3>Leave</h3><p>Presence ends while the room’s messages and related records remain.</p></div></li></ol></section>

      <section className="product-roulette-contrast"><article><p className="product-kicker">What the user chooses</p><h2>To enter a random conversation.</h2><p>The user consents to uncertain social context. The other participant is selected from the active queue.</p></article><article><p className="product-kicker">What the service creates</p><h2>A traceable room with two identities.</h2><p>Matching produces a room ID, access state, presence events, delivery records and history before either person decides to leave.</p></article></section>

      <section className="product-roulette-retention"><header><p className="product-kicker">After the match</p><h2>The room remains meaningful after its social moment ends.</h2></header><dl>{retainedEvents.map(([label, value, detail]) => <div key={label}><dt>{label}</dt><dd><b>{value}</b><span>{detail}</span></dd></div>)}</dl></section>

      <section className="product-roulette-end"><div><p className="product-kicker">Follow the record</p><h2>Randomness is a matching feature, not a privacy guarantee.</h2></div><div><a className="product-cta" href="/login">Open Knot ↗</a><a href={getPublicArticlePath("wiretap", "route-traces")}>Inspect Route traces →</a></div></section>
    </ProductShell>
  );
}
