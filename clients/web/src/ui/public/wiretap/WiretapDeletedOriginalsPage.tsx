"use client";

import { type CSSProperties, useState } from "react";
import { getPublicArticlePath } from "../PublicContentCatalog";
import { WiretapBreadcrumb, WiretapCta, WiretapShell } from "./WiretapShell";

const evidence = [
  ["01", "Creation", "The first submitted body is stored with its author, session and route."],
  ["02", "Revision", "A later edit becomes a new event while the original text remains readable."],
  ["03", "Deletion", "The conversation receives a tombstone and Wiretap receives a new audit event."],
  ["04", "Reload", "Persistent history reconstructs the full sequence after the interface has moved on."],
] as const;

export function WiretapDeletedOriginalsPage() {
  const [comparison, setComparison] = useState(52);
  const originalVisible = comparison < 55;

  return (
    <WiretapShell current="deleted-originals">
      <section className="wiretap-deleted-case">
        <aside>
          <WiretapBreadcrumb current="Deleted originals" />
          <p className="wiretap-kicker">Case file 01</p>
          <h1>Deletion adds a tombstone. It does not remove the record.</h1>
          <p>The conversation can hide a message while the global audit stream keeps the original body and every later state change.</p>
          <a href="#comparison">Inspect the evidence ↓</a>
        </aside>

        <div className="wiretap-deleted-folder">
          <div className="wiretap-folder-tab">Direct / mira + otto</div>
          <article>
            <span>Message record</span>
            <p>“Meet me at the east entrance after the shift ends.”</p>
            <small>Created by mira · 14:02:08 · session-password</small>
          </article>
          <article className="is-tombstone">
            <span>Current chat state</span>
            <p>This message was deleted.</p>
            <small>Delete request by mira · 14:02:33</small>
          </article>
          <div className="wiretap-folder-seal">Record retained</div>
        </div>
      </section>

      <section className="wiretap-deleted-comparison" id="comparison">
        <header>
          <p className="wiretap-kicker">Original ↔ Tombstone</p>
          <h2>Move the divider. The interface changes; the evidence does not.</h2>
        </header>
        <div className="wiretap-deleted-slider" style={{ "--comparison-position": `${comparison}%` } as CSSProperties}>
          <div className="wiretap-deleted-original">
            <span>Wiretap record</span>
            <p>Meet me at the east entrance after the shift ends.</p>
            <small>Original body · retained</small>
          </div>
          <div className="wiretap-deleted-tombstone">
            <span>Conversation view</span>
            <p>This message was deleted.</p>
            <small>Current state · shown to participants</small>
          </div>
          <input aria-label="Compare original message with tombstone" max="100" min="0" type="range" value={comparison} onChange={(event) => setComparison(Number(event.target.value))} />
          <span aria-hidden="true" className="wiretap-deleted-handle">↔</span>
        </div>
        <div className="wiretap-deleted-fields" aria-live="polite">
          <span>{originalVisible ? "Original text remains visible to Wiretap" : "Tombstone is visible in the conversation"}</span>
          <p>Actor · session · route · creation time · delete time</p>
        </div>
      </section>

      <section className="wiretap-deleted-evidence">
        <div>
          <p className="wiretap-kicker">Evidence chain</p>
          {evidence.map(([number, title, description]) => (
            <article key={number}>
              <span>{number}</span>
              <div><h2>{title}</h2><p>{description}</p></div>
            </article>
          ))}
        </div>
      </section>

      <section className="wiretap-deleted-limits">
        <article>
          <p className="wiretap-kicker">What deletion changes</p>
          <h2>It changes the current chat presentation.</h2>
          <p>Participants see a tombstone in place of the message and can no longer use the current body as ordinary chat content.</p>
        </article>
        <article>
          <p className="wiretap-kicker">What deletion cannot change</p>
          <h2>It cannot retract a record that was already delivered.</h2>
          <p>Wiretap, persistent history and browser copies can retain the original message and its surrounding audit context.</p>
        </article>
      </section>

      <section className="wiretap-deleted-conclusion">
        <div><p className="wiretap-kicker">Conclusion</p><h2>The delete control changes what is shown, not what is known.</h2></div>
        <WiretapCta href={getPublicArticlePath("research", "tombstone-behavior")}>Read Tombstone behavior →</WiretapCta>
      </section>
    </WiretapShell>
  );
}
