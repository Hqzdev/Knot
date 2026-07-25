"use client";

import { useEffect, useMemo, useState } from "react";
import { getPublicArticlePath } from "../PublicContentCatalog";
import { WiretapBreadcrumb, WiretapCta, WiretapShell } from "./WiretapShell";

type FeedKind = "all" | "messages" | "edits" | "deletes";

type FeedEvent = {
  id: string;
  kind: Exclude<FeedKind, "all">;
  actor: string;
  detail: string;
  room: string;
  time: string;
};

const feedEvents: FeedEvent[] = [
  { id: "e-184", kind: "messages", actor: "mira", detail: "published a plaintext message", room: "direct / otto", time: "14:02:08" },
  { id: "e-185", kind: "edits", actor: "otto", detail: "replaced the original body", room: "direct / mira", time: "14:02:14" },
  { id: "e-186", kind: "messages", actor: "guest-17", detail: "posted to The Wall", room: "wall", time: "14:02:21" },
  { id: "e-187", kind: "deletes", actor: "mira", detail: "created a deletion tombstone", room: "direct / otto", time: "14:02:33" },
  { id: "e-188", kind: "messages", actor: "ivy", detail: "forwarded a message record", room: "research", time: "14:02:47" },
  { id: "e-189", kind: "edits", actor: "otto", detail: "changed reaction state", room: "direct / mira", time: "14:02:52" },
];

const feedTabs: { id: FeedKind; label: string }[] = [
  { id: "all", label: "All" },
  { id: "messages", label: "Messages" },
  { id: "edits", label: "Edits" },
  { id: "deletes", label: "Deletes" },
];

export function WiretapGlobalFeedPage() {
  const [kind, setKind] = useState<FeedKind>("all");
  const [paused, setPaused] = useState(false);
  const [highlighted, setHighlighted] = useState(0);
  const visibleEvents = useMemo(
    () => feedEvents.filter((event) => kind === "all" || event.kind === kind),
    [kind],
  );

  useEffect(() => {
    if (paused || visibleEvents.length < 2) return;
    const interval = window.setInterval(() => setHighlighted((value) => (value + 1) % visibleEvents.length), 2200);
    return () => window.clearInterval(interval);
  }, [paused, visibleEvents.length]);

  useEffect(() => setHighlighted(0), [kind]);

  return (
    <WiretapShell current="global-feed">
      <section className="wiretap-feed-hero">
        <div className="wiretap-feed-hero-copy">
          <WiretapBreadcrumb current="Global feed" />
          <p className="wiretap-kicker">Public observation surface</p>
          <h1>A feed of everything the server sees.</h1>
          <p>Wiretap removes conversation membership as an access boundary and turns operational events into a readable, shared stream.</p>
          <WiretapCta href="/login">Open live Wiretap ↗</WiretapCta>
        </div>
        <div className="wiretap-feed-stage">
          <section aria-label="Sample global event feed" className="wiretap-event-tape">
            <header>
              <div>
                <span>Live sample</span>
                <b aria-live="polite">{paused ? "Paused" : "Streaming"}</b>
              </div>
              <button aria-pressed={paused} type="button" onClick={() => setPaused((value) => !value)}>{paused ? "Resume" : "Pause"}</button>
            </header>
            <div aria-label="Feed category" className="wiretap-feed-tabs" role="tablist">
              {feedTabs.map((tab) => (
                <button aria-selected={kind === tab.id} key={tab.id} role="tab" type="button" onClick={() => setKind(tab.id)}>{tab.label}</button>
              ))}
            </div>
            <ol>
              {visibleEvents.map((event, index) => (
                <li className={index === highlighted ? "is-highlighted" : ""} key={event.id}>
                  <time>{event.time}</time>
                  <span className={`wiretap-event-dot wiretap-event-dot--${event.kind}`} />
                  <p><b>{event.actor}</b> {event.detail}<small>{event.room}</small></p>
                </li>
              ))}
            </ol>
          </section>
        </div>
      </section>

      <section className="wiretap-feed-disclosure" aria-label="Wiretap scope">
        <p>Not only participants</p>
        <p>Not only messages</p>
        <p>Not only history</p>
      </section>

      <section className="wiretap-feed-atlas">
        <div className="wiretap-feed-atlas-record">
          <p className="wiretap-kicker">Record reconstruction</p>
          <ol>
            <li><span>01</span><p>Creation gives the system a body, actor and room.</p></li>
            <li><span>02</span><p>Revisions connect the current sentence to its original.</p></li>
            <li><span>03</span><p>Route data makes the delivery path part of the same record.</p></li>
          </ol>
        </div>
        <div className="wiretap-feed-atlas-copy">
          <p className="wiretap-kicker">The record travels with context</p>
          <h2>One event is enough to start rebuilding a conversation.</h2>
          <div>
            <article><span>Content</span><p>Original text, revisions and tombstones stay attached to the event history.</p></article>
            <article><span>Identity</span><p>Author, session mode and actor remain visible to every observer.</p></article>
            <article><span>Route</span><p>Processing services and timestamps show exactly how the record moved.</p></article>
          </div>
        </div>
      </section>

      <section className="wiretap-feed-timeline">
        <header>
          <p className="wiretap-kicker">One message, four durable events</p>
          <h2>The visible chat is only the latest state.</h2>
        </header>
        <ol>
          <li><b>01</b><span>Create</span><p>Original body enters the feed.</p></li>
          <li><b>02</b><span>Edit</span><p>Replacement text is added, not substituted.</p></li>
          <li><b>03</b><span>Reaction</span><p>Attention becomes identity-linked metadata.</p></li>
          <li><b>04</b><span>Delete</span><p>A tombstone documents the attempt to erase.</p></li>
        </ol>
      </section>

      <section className="wiretap-feed-actions">
        <div>
          <p className="wiretap-kicker">Next surface</p>
          <h2>Watch the global record, or narrow it down.</h2>
        </div>
        <div>
          <WiretapCta href="/login">Enter Wiretap ↗</WiretapCta>
          <a href={getPublicArticlePath("wiretap", "audit-filters")}>Explore Audit filters →</a>
        </div>
      </section>
    </WiretapShell>
  );
}
