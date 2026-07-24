"use client";

import type { Message } from "@/domain/models";
import { useKnot } from "@/ui/ApplicationProvider";

export function Inspector({ message }: { message?: Message }) {
  const { state } = useKnot();
  const conversationId = message?.conversationId ?? state.selectedConversationId;
  const watchers = conversationId ? state.watchers[conversationId] ?? [] : [];
  const route = message?.route ?? [];
  return (
    <aside className="inspector">
      <header><p className="eyebrow">SURVEILLANCE PANEL</p><h2>Inspector</h2></header>
      <section className="privacy-score">
        <span>PRIVACY SCORE</span>
        <strong>0<small>/100</small></strong>
        <div><i /></div>
        <p>No transport privacy. No message privacy. No plausible deniability.</p>
      </section>
      <section>
        <h3>ACTIVE READERS <b>{watchers.length}</b></h3>
        <ul className="reader-list">
          {watchers.length === 0 && <li><span>—</span><p>No active browser watcher</p></li>}
          {watchers.map((watcher) => <li key={watcher.session_id}><span>{watcher.username.slice(0, 2).toUpperCase()}</span><p><b>{watcher.username}</b><small>{watcher.mode.toUpperCase()}</small></p><i /></li>)}
        </ul>
      </section>
      <section className="route-trace">
        <h3>ROUTE TRACE <b>{route.length}</b></h3>
        {route.length === 0 && <p className="route-empty">Select a captured message to inspect its route.</p>}
        {route.map((hop, index) => (
          <div className="route-hop" key={`${hop.service}-${index}`}>
            <span>{String(index + 1).padStart(2, "0")}</span>
            <p><b>{hop.service.toUpperCase()}</b><small>{hop.status}</small></p>
            <i />
          </div>
        ))}
      </section>
      <section className="server-note">
        <span>SERVER NOTE</span>
        <p>“We read it so you don’t have to wonder if we can.”</p>
      </section>
    </aside>
  );
}
