"use client";

import { useMemo, useState } from "react";
import { getPublicArticlePath } from "../PublicContentCatalog";
import { WiretapBreadcrumb, WiretapShell } from "./WiretapShell";

type FilterKind = "all" | "message" | "edit" | "delete";

const filterEvents = [
  { id: "a-401", kind: "message", actor: "mira", room: "direct / otto", time: "14:02", detail: "Original message created" },
  { id: "a-402", kind: "edit", actor: "otto", room: "direct / mira", time: "14:04", detail: "Message body replaced" },
  { id: "a-403", kind: "delete", actor: "mira", room: "direct / otto", time: "14:06", detail: "Tombstone created" },
  { id: "a-404", kind: "message", actor: "ivy", room: "research", time: "14:08", detail: "Message forwarded" },
  { id: "a-405", kind: "edit", actor: "mira", room: "direct / otto", time: "14:11", detail: "Reaction set changed" },
] as const;

const recipes = [
  { name: "Follow one person", query: "actor:mira", description: "Trace every action from one identity across rooms." },
  { name: "Find removals", query: "event:delete", description: "Turn every tombstone into a searchable list." },
  { name: "Watch one room", query: "room:direct/otto", description: "Reduce the global feed to a single conversation context." },
] as const;

export function WiretapAuditFiltersPage() {
  const [kind, setKind] = useState<FilterKind>("all");
  const [actor, setActor] = useState("all");
  const [room, setRoom] = useState("all");
  const [period, setPeriod] = useState("today");
  const results = useMemo(() => filterEvents.filter((event) => (kind === "all" || event.kind === kind) && (actor === "all" || event.actor === actor) && (room === "all" || event.room === room)), [actor, kind, room]);
  const query = [kind !== "all" ? `event:${kind}` : "", actor !== "all" ? `actor:${actor}` : "", room !== "all" ? `room:${room}` : "", `time:${period}`].filter(Boolean).join(" ");
  const reset = () => { setKind("all"); setActor("all"); setRoom("all"); setPeriod("today"); };

  return (
    <WiretapShell current="audit-filters">
      <section className="wiretap-filters-workbench">
        <form onSubmit={(event) => event.preventDefault()}>
          <WiretapBreadcrumb current="Audit filters" />
          <p className="wiretap-kicker">Query workbench</p>
          <h1>Narrow the leak without reducing it.</h1>
          <p>Filters change which part of the global record is visible. They do not change who can read it.</p>
          <label>Event<select value={kind} onChange={(event) => setKind(event.target.value as FilterKind)}><option value="all">All events</option><option value="message">Messages</option><option value="edit">Edits</option><option value="delete">Deletes</option></select></label>
          <label>Actor<select value={actor} onChange={(event) => setActor(event.target.value)}><option value="all">Anyone</option><option value="mira">mira</option><option value="otto">otto</option><option value="ivy">ivy</option></select></label>
          <label>Room<select value={room} onChange={(event) => setRoom(event.target.value)}><option value="all">Any room</option><option value="direct / otto">direct / otto</option><option value="direct / mira">direct / mira</option><option value="research">research</option></select></label>
          <label>Time<select value={period} onChange={(event) => setPeriod(event.target.value)}><option value="today">Today</option><option value="last-hour">Last hour</option><option value="live">Live</option></select></label>
          <button type="button" onClick={reset}>Reset filters</button>
        </form>
        <section aria-label="Filtered audit results" className="wiretap-filters-results">
          <header><div><span>Active query</span><code>{query}</code></div><b>{results.length} results</b></header>
          <ol>{results.map((event) => <li key={event.id}><time>{event.time}</time><span className={`wiretap-filter-kind wiretap-filter-kind--${event.kind}`}>{event.kind}</span><p><b>{event.actor}</b> · {event.detail}<small>{event.room}</small></p></li>)}</ol>
          {results.length === 0 && <p className="wiretap-filter-empty">No records match this view. The records still exist outside it.</p>}
        </section>
      </section>

      <section className="wiretap-filters-rule"><p>Filters change the view.</p><span>They do not reduce access.</span><p>Structured records become more useful.</p></section>

      <section className="wiretap-filters-recipes"><header><p className="wiretap-kicker">Saved recipes</p><h2>Three ways to turn a broad feed into a targeted record.</h2></header><div>{recipes.map((recipe, index) => <article className={`wiretap-filter-recipe--${index + 1}`} key={recipe.name}><span>0{index + 1}</span><h3>{recipe.name}</h3><code>{recipe.query}</code><p>{recipe.description}</p></article>)}</div></section>

      <section className="wiretap-filters-narrowing"><div><p className="wiretap-kicker">From feed to person</p><h2>A query can make a global event stream feel like a private dossier.</h2><ol><li><span>01</span><p>Start with all globally visible events.</p></li><li><span>02</span><p>Choose one event type, such as delete.</p></li><li><span>03</span><p>Add an actor, room and time range.</p></li><li><span>04</span><p>Read a structured history of one person’s activity.</p></li></ol></div><aside><div aria-hidden="true" className="wiretap-filters-correlation"><i /><i /><i /><i /></div><p>Correlation is not a new permission. It is a more efficient use of the permission already granted.</p></aside></section>

      <section className="wiretap-filters-end"><a href={getPublicArticlePath("wiretap", "global-feed")}>Global feed →</a><p>Every filter starts with the same exposed record.</p><a href={getPublicArticlePath("research", "exposure-index")}>Exposure Index →</a></section>
    </WiretapShell>
  );
}
