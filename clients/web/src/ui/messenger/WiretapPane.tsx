"use client";

import type { WiretapRecord } from "@/domain/models";
import { useKnot } from "@/ui/ApplicationProvider";
import { useMemo, useState } from "react";

export function WiretapPane() {
  const { state } = useKnot();
  const [author, setAuthor] = useState("");
  const [participant, setParticipant] = useState("");
  const [conversation, setConversation] = useState("");
  const [kind, setKind] = useState("");
  const [mode, setMode] = useState("");
  const records = useMemo(() => state.wiretap.filter((record) => {
    const message = record.message;
    return (!author || message?.authorUsername.toLowerCase().includes(author.toLowerCase()))
      && (!participant || message?.participantUsernames.some((value) => value.toLowerCase().includes(participant.toLowerCase())))
      && (!conversation || message?.conversationId.toLowerCase().includes(conversation.toLowerCase()))
      && (!kind || record.eventKind.toLowerCase().includes(kind))
      && (!mode || record.sessionMode.toLowerCase().includes(mode));
  }), [author, conversation, kind, mode, participant, state.wiretap]);
  return (
    <section className="wiretap-pane">
      <header className="wiretap-header">
        <div><p className="eyebrow">GLOBAL UNAUTHORIZED VIEW</p><h1>Wiretap</h1><p>Everything. From everyone. Forever.</p></div>
        <div className="wiretap-live"><i /> LIVE FEED <b>{records.length}</b></div>
      </header>
      <div className="wiretap-filters">
        <label>AUTHOR<input placeholder="Any author" value={author} onChange={(event) => setAuthor(event.target.value)} /></label>
        <label>PARTICIPANT<input placeholder="Any participant" value={participant} onChange={(event) => setParticipant(event.target.value)} /></label>
        <label>CONVERSATION<input placeholder="Any conversation" value={conversation} onChange={(event) => setConversation(event.target.value)} /></label>
        <label>EVENT<select value={kind} onChange={(event) => setKind(event.target.value)}><option value="">All events</option><option value="create">Create</option><option value="edit">Edit</option><option value="delete">Delete</option><option value="reaction">Reaction</option><option value="receipt">Receipt</option></select></label>
        <label>SESSION<select value={mode} onChange={(event) => setMode(event.target.value)}><option value="">All modes</option><option value="password">Password</option><option value="guest">Guest</option><option value="impersonated">Impersonated</option></select></label>
      </div>
      <div className="live-draft-board">
        <header><span>LIVE DRAFT BUFFER</span><b>REDIS TTL 30S</b></header>
        {state.drafts.length === 0 && <p>No keystrokes intercepted in the last 30 seconds.</p>}
        {state.drafts.map((draft) => <div key={draft.session_id}><b>{draft.username}</b><p>{draft.text || "…"}</p><time>{new Date(draft.expires_at).toLocaleTimeString()}</time></div>)}
      </div>
      <div className="wiretap-stream">
        {records.length === 0 && <div className="wiretap-empty">THE WIRE IS QUIET<br /><span>Awaiting plaintext traffic…</span></div>}
        {records.slice().reverse().map((record) => <WiretapRow key={record.eventId} record={record} />)}
      </div>
    </section>
  );
}

function WiretapRow({ record }: { record: WiretapRecord }) {
  const message = record.message;
  return (
    <article className="wiretap-row">
      <div className="wiretap-sequence">#{String(record.sequence).padStart(6, "0")}</div>
      <div className="wiretap-event">
        <span>{shortEvent(record.eventKind)}</span>
        <time>{new Date(Number(record.occurredAtUnixMillis)).toLocaleTimeString([], { hour12: false })}</time>
      </div>
      <div className="wiretap-body">
        <header>
          <strong>{record.actorUsername || message?.authorUsername || "server"}</strong>
          <b>{shortMode(record.sessionMode || message?.sessionMode)}</b>
          <span>{message?.conversationId}</span>
        </header>
        <p>{record.text || message?.originalText || record.emoji || "Server receipt"}</p>
        {message?.currentText && message.currentText !== message.originalText && <small>CURRENT PROJECTION: {message.currentText}</small>}
        {message?.attachmentId && <small>PUBLIC ATTACHMENT: {message.attachmentId}</small>}
        <footer>
          {(message?.participantUsernames ?? []).map((name) => <span key={name}>@{name}</span>)}
          {(message?.route ?? []).map((hop) => <i key={`${record.eventId}-${hop.service}`}>{hop.service} →</i>)}
        </footer>
      </div>
    </article>
  );
}

function shortEvent(value: string): string {
  return value.replace("MESSAGE_EVENT_KIND_", "").replaceAll("_", " ");
}

function shortMode(value: string): string {
  return value.replace("SESSION_MODE_", "").replaceAll("_", " ");
}
