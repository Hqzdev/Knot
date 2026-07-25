"use client";

import { type CSSProperties, useState } from "react";
import { getPublicArticlePath } from "../PublicContentCatalog";
import { WiretapBreadcrumb, WiretapCta, WiretapShell } from "./WiretapShell";

type Watcher = {
  id: string;
  name: string;
  role: string;
  room: string;
  duration: string;
  presence: readonly boolean[];
};

const watchers: readonly Watcher[] = [
  { id: "mira", name: "mira", role: "participant", room: "direct / otto", duration: "17 min", presence: [true, true, true, true, false, false, false, false] },
  { id: "guest-17", name: "guest-17", role: "guest", room: "wiretap", duration: "26 min", presence: [false, true, true, true, true, true, false, false] },
  { id: "otto", name: "otto", role: "participant", room: "direct / mira", duration: "8 min", presence: [false, false, true, true, true, false, false, false] },
  { id: "mira-copy", name: "mira-copy", role: "impersonated", room: "wiretap", duration: "11 min", presence: [true, false, false, true, true, true, true, false] },
  { id: "ivy", name: "ivy", role: "passive", room: "research", duration: "31 min", presence: [true, true, true, true, true, true, true, true] },
];

export function WiretapActiveWatchersPage() {
  const [selectedId, setSelectedId] = useState(watchers[0].id);
  const selected = watchers.find((watcher) => watcher.id === selectedId) ?? watchers[0];

  return (
    <WiretapShell current="active-watchers">
      <section className="wiretap-watchers-hero">
        <div>
          <WiretapBreadcrumb current="Active watchers" />
          <p className="wiretap-kicker">Observation is presence data</p>
          <h1>See who is looking at the room.</h1>
          <p>Watcher records expose attention, availability and relationships even when no one contributes a message.</p>
        </div>
        <section className="wiretap-presence-matrix" aria-label="Sample watcher presence matrix">
          <header><span>Observer</span>{["09", "10", "11", "12", "13", "14", "15", "16"].map((hour) => <span key={hour}>{hour}</span>)}</header>
          {watchers.map((watcher) => (
            <div className={selectedId === watcher.id ? "is-selected" : ""} key={watcher.id}>
              <button aria-pressed={selectedId === watcher.id} type="button" onClick={() => setSelectedId(watcher.id)}>{watcher.name}</button>
              {watcher.presence.map((present, index) => <button aria-label={`${watcher.name} at ${9 + index}:00`} className={present ? "is-present" : ""} key={`${watcher.id}-${index}`} type="button" onClick={() => setSelectedId(watcher.id)} />)}
            </div>
          ))}
          <footer aria-live="polite"><b>{selected.name}</b><span>{selected.role}</span><span>{selected.room}</span><span>{selected.duration}</span></footer>
        </section>
      </section>

      <section className="wiretap-watchers-introduction">
        <div className="wiretap-watchers-count"><b>05</b><span>Named watcher records</span><small>One room, one interval</small></div>
        <div><p className="wiretap-kicker">Silent reading is still activity</p><h2>Presence makes an audience measurable.</h2><p>Joins, departures and duration records show who was available to observe a room. A participant can infer attention; the service can correlate it across the product.</p></div>
      </section>

      <section className="wiretap-watchers-roster">
        <header><p className="wiretap-kicker">Watcher roster</p><h2>Different identities, one observation surface.</h2></header>
        <div>{watchers.map((watcher, index) => <article className={selectedId === watcher.id ? "is-selected" : ""} key={watcher.id}><span>0{index + 1}</span><h3>{watcher.name}</h3><p>{watcher.role}</p><b>{watcher.room}</b><small>{watcher.duration}</small></article>)}</div>
      </section>

      <section className="wiretap-watchers-attention"><p className="wiretap-kicker">Attention over time</p><div>{[32, 54, 66, 48, 82, 74, 41, 19].map((height, index) => <span key={height} style={{ "--attention-height": `${height}%` } as CSSProperties}><b>{height}</b><i /><small>{9 + index}:00</small></span>)}</div><p>Visible overlap does not identify intent, but it can reveal routine, relationships and availability.</p></section>

      <section className="wiretap-watchers-inference">
        <div aria-hidden="true" className="wiretap-watchers-clusters"><span /><span /><span /><span /><span /><i /><i /><i /></div>
        <div><article><p className="wiretap-kicker">Participants can infer</p><h2>Who was around to read.</h2><p>Repeated room presence can suggest attention, response expectations and social proximity.</p></article><article><p className="wiretap-kicker">The server can infer</p><h2>Patterns across rooms and time.</h2><p>Correlated watcher records build a behavioral map without requiring any message-body analysis.</p></article></div>
      </section>

      <section className="wiretap-watchers-end"><div><p className="wiretap-kicker">Research</p><h2>Observation has a history of its own.</h2></div><WiretapCta href={getPublicArticlePath("research", "watcher-studies")}>Read Watcher studies →</WiretapCta></section>
    </WiretapShell>
  );
}
