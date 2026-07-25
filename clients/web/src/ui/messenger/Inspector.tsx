"use client";

import type { Message } from "@/domain/models";
import { useKnot } from "@/ui/ApplicationProvider";
import { useState } from "react";
import { ControlRoomIcon } from "./ControlRoomIcon";

export function Inspector({ message, onClose }: { message?: Message; onClose: () => void }) {
  const { state } = useKnot();
  const conversationId = message?.conversationId ?? state.selectedConversationId;
  const watchers = conversationId ? state.watchers[conversationId] ?? [] : [];
  const route = message?.route ?? [];
  const [lockOpen, setLockOpen] = useState(false);
  return (
    <aside className="inspector" id="conversation-inspector">
      <header><div><h2>Inspector</h2><p>Conversation intelligence</p></div><button aria-label="Close Inspector" className="inspector-close" onClick={onClose} title="Close Inspector"><ControlRoomIcon name="close" size={16} /></button></header>
      <section className="privacy-score">
        <header><span className="exposure-badge"><ControlRoomIcon name="eye" size={13} />Exposed</span><strong>0<small>/100</small></strong></header>
        <p>No transport privacy. No message privacy. No plausible deniability.</p>
        <button aria-expanded={lockOpen} className="privacy-breakdown" onClick={() => setLockOpen((value) => !value)}><span>Leak inventory</span><ControlRoomIcon name="chevronDown" size={14} /></button>
        {lockOpen && <ul className="leak-inventory"><li>Open message text</li><li>Public attachments</li><li>Global Wiretap</li><li>Device receipts</li></ul>}
      </section>
      <section className="inspector-section">
        <h3><span>Active readers</span><b>{watchers.length}</b></h3>
        <ul className="reader-list">
          {watchers.length === 0 && <li className="inspector-empty-row"><span><ControlRoomIcon name="eye" size={15} /></span><p><b>No active readers</b><small>The room is still server-visible</small></p></li>}
          {watchers.map((watcher) => <li key={watcher.session_id}><span>{watcher.username.slice(0, 2).toUpperCase()}</span><p><b>{watcher.username}</b><small>{watcher.browser ?? "Unknown browser"} · {watcher.os ?? "Unknown OS"} · {watcher.mode}</small></p><i /></li>)}
        </ul>
      </section>
      <section className="inspector-section route-trace">
        <h3><span>Route trace</span><b>{route.length}</b></h3>
        {route.length === 0 && <div className="route-empty"><span><ControlRoomIcon name="signal" size={17} /></span><p><b>No message selected</b><small>Capture activity to inspect its route.</small></p></div>}
        {route.map((hop, index) => (
          <div className="route-hop" key={`${hop.service}-${index}`}>
            <span>{String(index + 1).padStart(2, "0")}</span>
            <p><b>{hop.service}</b><small>{hop.status}</small></p>
            <i />
          </div>
        ))}
      </section>
    </aside>
  );
}
