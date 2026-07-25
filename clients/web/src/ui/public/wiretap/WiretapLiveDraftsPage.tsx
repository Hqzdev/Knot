"use client";

import { useEffect, useState } from "react";
import { getPublicArticlePath } from "../PublicContentCatalog";
import { WiretapBreadcrumb, WiretapCta, WiretapShell } from "./WiretapShell";

const draftStages = [
  { name: "First keystroke", text: "I should probably tell you", status: "Draft relayed", watchers: "1 observer" },
  { name: "Revision", text: "I should probably not tell you what happened after the shift", status: "Draft updated", watchers: "3 observers" },
  { name: "Abandoned", text: "I should probably not tell you what happened after the shift…", status: "Draft expired in Redis", watchers: "3 delivered copies" },
] as const;

export function WiretapLiveDraftsPage() {
  const [stage, setStage] = useState(0);
  const [playing, setPlaying] = useState(true);
  const selected = draftStages[stage];

  useEffect(() => {
    if (!playing) return;
    const interval = window.setInterval(() => setStage((value) => (value + 1) % draftStages.length), 3600);
    return () => window.clearInterval(interval);
  }, [playing]);

  return (
    <WiretapShell current="live-drafts">
      <section className="wiretap-drafts-hero">
        <WiretapBreadcrumb current="Live drafts" />
        <div className="wiretap-drafts-time wiretap-drafts-time--left">00:00:01</div>
        <div className="wiretap-drafts-time wiretap-drafts-time--right">30 second window</div>
        <p className="wiretap-kicker">Unsent does not mean unseen</p>
        <h1>Read the message before it becomes a message.</h1>
        <p className="wiretap-drafts-typed" aria-live="polite">{selected.text}<span>|</span></p>
        <p className="wiretap-drafts-caption">The text leaves the composer while its author is still deciding whether to send it.</p>
      </section>

      <section aria-label="Draft playback" className="wiretap-drafts-player">
        <header><div><span>Draft playback</span><b>{selected.name}</b></div><button aria-pressed={playing} type="button" onClick={() => setPlaying((value) => !value)}>{playing ? "Pause" : "Play"}</button></header>
        <input aria-label="Draft playback position" max={draftStages.length - 1} min="0" type="range" value={stage} onChange={(event) => { setPlaying(false); setStage(Number(event.target.value)); }} />
        <div><span>{selected.status}</span><span>{selected.watchers}</span><span>{stage === 2 ? "00:00:30" : `00:00:0${stage + 1}`}</span></div>
      </section>

      <section className="wiretap-drafts-publishing">
        <div aria-hidden="true" className="wiretap-drafts-signal"><span /><span /><span /><span /></div>
        <div><p className="wiretap-kicker">Typing is publishing</p><h2>The first exposure happens before the send button exists.</h2><p>Presence infrastructure carries the entire composer value instead of a simple typing indicator. A revision can reveal more than the message that eventually survives.</p></div>
      </section>

      <section className="wiretap-drafts-stages">
        {draftStages.map((item, index) => (
          <article className={index === stage ? "is-current" : ""} key={item.name}>
            <span>0{index + 1}</span><h2>{item.name}</h2><p>{item.status}. {item.watchers}.</p>
          </article>
        ))}
      </section>

      <section className="wiretap-drafts-relay">
        <span>Composer</span><i /><span>Presence relay</span><i /><span>Observer copy</span>
      </section>

      <section className="wiretap-drafts-retention">
        <article><p className="wiretap-kicker">Redis expiry</p><h2>Short-lived source state</h2><p>Redis removes the live value after thirty seconds. That limit affects a store, not the audience that already received the value.</p></article>
        <article><p className="wiretap-kicker">Delivered copies</p><h2>Persistent observer knowledge</h2><p>Wiretap clients can render, record or relay the draft before expiry. Delivery has already happened by the time the key disappears.</p></article>
      </section>

      <section className="wiretap-drafts-end">
        <div><p className="wiretap-kicker">The relay layer</p><h2>Ephemeral is a storage policy, not a privacy policy.</h2></div>
        <WiretapCta href={getPublicArticlePath("how-it-leaks", "live-draft-relay")}>Inspect Live draft relay →</WiretapCta>
      </section>
    </WiretapShell>
  );
}
