"use client";

import type { LinkPreview, Message } from "@/domain/models";
import { useKnot } from "@/ui/ApplicationProvider";
import { type ChangeEvent, type FormEvent, useEffect, useMemo, useRef, useState } from "react";
import { conversationTitle } from "./ConversationList";
import { ControlRoomIcon } from "./ControlRoomIcon";
import { VoiceTaxRecorder } from "./VoiceTaxRecorder";

const emojis = ["👁", "📡", "🚨", "💀", "🔓", "🤡", "🫡", "🧾", "❔", "🫣", "🧩"];

interface ChatPaneProps {
  inspectorOpen: boolean;
  onShowConversations?: () => void;
  onToggleInspector: () => void;
  wall?: boolean;
}

export function ChatPane({ inspectorOpen, onShowConversations, onToggleInspector, wall = false }: ChatPaneProps) {
  const { controller, state } = useKnot();
  const conversationId = wall ? "wall" : state.selectedConversationId;
  const conversation = state.conversations.find((value) => value.id === conversationId);
  const messages = conversationId ? state.messages[conversationId] ?? [] : [];
  const watchers = conversationId ? state.watchers[conversationId] ?? [] : [];
  const [text, setText] = useState("");
  const [reply, setReply] = useState<Message>();
  const [uploading, setUploading] = useState(false);
  const [unreliable, setUnreliable] = useState(false);
  const [effect, setEffect] = useState<"none" | "bureaucratic" | "caesar3">("none");
  const [hideFromAuthor, setHideFromAuthor] = useState(false);
  const endRef = useRef<HTMLDivElement>(null);
  const activeOptionCount = Number(unreliable) + Number(hideFromAuthor) + Number(effect !== "none") + Number(state.maximumSecurity);

  useEffect(() => {
    endRef.current?.scrollIntoView({ block: "end" });
  }, [messages.length]);

  const activeDrafts = useMemo(
    () => state.drafts.filter((value) => value.conversation_id === conversationId && value.session_id !== state.session?.session_id),
    [conversationId, state.drafts, state.session?.session_id],
  );

  if (!conversationId) {
    return (
      <section className="empty-chat">
        <div className="radar"><i /><i /><i /><b /></div>
        <p className="eyebrow">No conversation selected</p>
        <h2>Choose a conversation to intercept.</h2>
        <p>Or open Wiretap and skip the social convention of being invited.</p>
      </section>
    );
  }

  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (!text.trim()) {
      return;
    }
    void controller.send(conversationId, text.trim(), "", reply?.id, "", {
      deliveryMode: unreliable ? "unreliable" : "normal",
      textEffect: effect,
      authorHideAfterSeconds: hideFromAuthor ? 60 : 0,
    }).catch(() => undefined);
    setText("");
    setReply(undefined);
    setUnreliable(false);
    setHideFromAuthor(false);
    controller.publishDraft(conversationId, "");
  };

  const upload = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (!file) {
      return;
    }
    setUploading(true);
    try {
      await controller.upload(file, conversationId);
    } finally {
      setUploading(false);
      event.target.value = "";
    }
  };

  return (
    <section className="chat-pane">
      <header className="chat-header">
        {onShowConversations && <button className="mobile-conversation-button" onClick={onShowConversations}><ControlRoomIcon name="chat" size={16} /> Channels</button>}
        <div className="chat-heading">
          <h2>{wall ? "The Wall" : conversationTitle(conversation, state.session?.user.id ?? "")}</h2>
          <p>{wall ? "Global permanent room" : `${capitalise(conversation?.kind) || "Channel"} · plaintext transport`}</p>
        </div>
        <div className="header-status">
          <span className="status-item connected"><i />Listening</span>
          <span className="status-item"><ControlRoomIcon name="eye" size={14} />{watchers.length} watcher{watchers.length === 1 ? "" : "s"}</span>
          {!wall && conversation?.kind !== "burner" && <button aria-label="Create a one-minute Burner" className="status-button" title="Create a one-minute Burner" onClick={() => void controller.createBurner(conversationId)}><ControlRoomIcon name="clock" size={14} />60s</button>}
          <button aria-controls="conversation-inspector" aria-expanded={inspectorOpen} aria-label={inspectorOpen ? "Close Inspector" : "Open Inspector"} className="inspector-toggle" onClick={onToggleInspector} title={inspectorOpen ? "Close Inspector" : "Open Inspector"}><ControlRoomIcon name="panel" size={16} /></button>
        </div>
      </header>
      <div className="message-stream">
        <div className="retention-banner">
          <ControlRoomIcon name="alert" size={15} />
          <span><b>Retained forever</b> Deletes hide the participant copy only.</span>
        </div>
        {messages.length === 0 && (
          <div className="message-empty">
            <span className="signal-mark"><ControlRoomIcon name="signal" size={24} /></span>
            <h3>No activity captured.</h3>
            <p>The first message is stored in plaintext and retained by Wiretap.</p>
          </div>
        )}
        {messages.map((message) => (
          <MessageCard controller={controller} currentUserId={state.session?.user.id ?? ""} key={message.id} message={message} onReply={() => setReply(message)} saved={state.savedMessageIds.includes(message.id)} />
        ))}
        {activeDrafts.map((draft) => (
          <div className="draft-leak" key={draft.session_id}>
            <span>Live draft / {draft.username}</span>
            <p>{draft.text || "…"}</p>
            <time>Self-destructs from Redis at {new Date(draft.expires_at).toLocaleTimeString()}</time>
          </div>
        ))}
        <div ref={endRef} />
      </div>
      <form className="composer" onSubmit={submit}>
        {reply && <div className="reply-strip"><span>Replying to {reply.authorUsername}: {reply.originalText}</span><button type="button" onClick={() => setReply(undefined)}>×</button></div>}
        <div className="composer-meta">
          <details className="message-options">
            <summary><ControlRoomIcon name="settings" size={15} /><span>Message options</span>{activeOptionCount > 0 && <b>{activeOptionCount}</b>}<ControlRoomIcon name="chevronDown" size={14} /></summary>
            <div className="message-options-popover">
              <header><strong>Message options</strong><span>Applied to the next send</span></header>
              <label className="option-toggle"><span><b>Unreliable delivery</b><small>May route to a different target</small></span><input checked={unreliable} type="checkbox" onChange={(event) => setUnreliable(event.target.checked)} /></label>
              <label className="option-toggle"><span><b>Hide from me in 60s</b><small>Server and Wiretap keep the original</small></span><input checked={hideFromAuthor} type="checkbox" onChange={(event) => setHideFromAuthor(event.target.checked)} /></label>
              <label className="option-select"><span><b>Text effect</b><small>Changes participant-visible text</small></span><select value={effect} onChange={(event) => setEffect(event.target.value as typeof effect)}><option value="none">None</option><option value="bureaucratic">Boss</option><option value="caesar3">Premium Caesar-3</option></select></label>
              <label className="option-toggle"><span><b>Maximum security captcha</b><small>Adds friction without message privacy</small></span><input checked={state.maximumSecurity} type="checkbox" onChange={(event) => controller.setMaximumSecurity(event.target.checked)} /></label>
            </div>
          </details>
          <span className="exposure-status"><ControlRoomIcon name="eye" size={14} />Plaintext · server copy · live draft</span>
        </div>
        <div className="composer-row">
          <label aria-label="Upload a public file" className="attach-button" title="Upload a public file">
            {uploading ? "…" : <ControlRoomIcon name="paperclip" />}<input disabled={uploading} type="file" onChange={(event) => void upload(event)} />
          </label>
          <VoiceTaxRecorder onReady={(original, taxed, durationMillis, taxLevel) => controller.sendVoice(conversationId, original, taxed, durationMillis, taxLevel)} />
          <textarea
            aria-label="Message"
            placeholder="Message the whole server…"
            value={text}
            onChange={(event) => {
              setText(event.target.value);
              controller.publishDraft(conversationId, event.target.value);
            }}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.shiftKey) {
                event.preventDefault();
                event.currentTarget.form?.requestSubmit();
              }
            }}
          />
          <button className="send-button" disabled={!text.trim()}>Send</button>
        </div>
      </form>
    </section>
  );
}

function MessageCard({
  controller,
  currentUserId,
  message,
  onReply,
  saved,
}: {
  controller: ReturnType<typeof useKnot>["controller"];
  currentUserId: string;
  message: Message;
  onReply: () => void;
  saved: boolean;
}) {
  const own = message.authorUserId === currentUserId;
  const deleted = Number(message.deletedAtUnixMillis ?? 0) > 0;
  const [now, setNow] = useState(Date.now());
  const hiddenAt = Number(message.authorHideAtUnixMillis ?? 0);
  const hidden = Boolean(message.authorProjectionHidden) || own && hiddenAt > 0 && hiddenAt <= now;
  const activeReactions = (message.reactions ?? []).filter((reaction) => (reaction.usernames?.length ?? 0) > 0);
  const readRef = useRef<HTMLElement>(null);
  const readSent = useRef(false);
  useEffect(() => {
    if (own || readSent.current || !readRef.current) {
      return;
    }
    const target = readRef.current;
    const observer = new IntersectionObserver((entries) => {
      if (!entries.some((entry) => entry.isIntersecting) || readSent.current) {
        return;
      }
      const timer = setTimeout(() => {
        readSent.current = true;
        controller.markRead(message.id);
      }, 1000);
      observer.disconnect();
      return () => clearTimeout(timer);
    }, { threshold: 0.7 });
    observer.observe(target);
    return () => observer.disconnect();
  }, [controller, message.id, own]);
  useEffect(() => {
    if (!own || hiddenAt <= Date.now()) {
      return;
    }
    const timer = setTimeout(() => setNow(Date.now()), hiddenAt - Date.now());
    return () => clearTimeout(timer);
  }, [hiddenAt, own]);
  const edit = () => {
    const value = window.prompt("Replace the participant-visible text. Wiretap keeps the original.", message.currentText);
    if (value?.trim()) {
      controller.edit(message.id, value.trim());
    }
  };
  return (
    <article ref={readRef} className={`message-card ${own ? "own" : ""} ${deleted ? "deleted" : ""}`}>
      <header>
        <strong>{message.authorUsername}</strong>
        {sessionLabel(message.sessionMode) && <b className="session-badge">{sessionLabel(message.sessionMode)}</b>}
        <time>{formatTime(message.createdAtUnixMillis)}</time>
      </header>
      {message.replyToId && <div className="message-reference">↳ Reply to {message.replyToId}</div>}
      {message.forwardedFromId && <div className="message-reference">Forwarded from {message.forwardedFromId}</div>}
      <p>{hidden ? "Message disappeared from your local projection only." : deleted ? "Message removed from participant view." : message.currentText}</p>
      {deleted && <small className="tombstone">Original retained in Wiretap: “{message.originalText}”</small>}
      {!deleted && message.originalText !== message.currentText && <small className="tombstone">Edited · original available in Wiretap</small>}
      {message.deliveryMode?.toLowerCase().includes("unreliable") && <small className="tombstone">Requested {message.requestedConversationId} · actual {message.conversationId}</small>}
      {message.textEffect?.toLowerCase().includes("caesar") && <small className="tombstone">Premium Caesar-3 · not cryptography</small>}
      {message.forwardChain?.length > 0 && <small className="tombstone">Forward chain: {message.forwardChain.map((hop) => `${hop.authorUsername}@${hop.conversationId}`).join(" → ")}</small>}
      {message.attachmentId && <a className="file-card" href={controller.attachmentURL(message.attachmentId)} target="_blank"><span>Public file</span><b>{message.currentText || message.attachmentId}</b><small>Anyone with this link can request it →</small></a>}
      {message.voice?.originalAttachmentId && <a className="file-card" href={controller.attachmentURL(message.voice.originalAttachmentId)} target="_blank"><span>Voice tax source</span><b>Original public recording</b><small>{message.voice.taxLevel} projection plays in the message attachment →</small></a>}
      {!deleted && <LinkPreviewCard controller={controller} text={message.currentText} />}
      <div className="reaction-row">
        {activeReactions.map((reaction) => <button key={reaction.emoji} onClick={() => controller.react(message.id, reaction.emoji, false)}>{reaction.emoji} {reaction.usernames?.length ?? 0}</button>)}
        <details>
          <summary>+</summary>
          <div>{emojis.map((emoji) => <button key={emoji} onClick={() => controller.react(message.id, emoji)}>{emoji}</button>)}</div>
        </details>
      </div>
      <details className="receipt-panel"><summary>Read on {message.readReceipts?.length ?? 0} device{(message.readReceipts?.length ?? 0) === 1 ? "" : "s"}</summary><div>{(message.readReceipts ?? []).map((receipt) => <p key={`${receipt.sessionId}-${receipt.device?.deviceId}`}><b>{receipt.username}</b> · {receipt.device?.browser ?? "Unknown browser"} / {receipt.device?.os ?? "Unknown OS"} · {new Date(Number(receipt.readAtUnixMillis)).toLocaleTimeString()}</p>)}</div></details>
      <footer className="message-actions">
        <button onClick={onReply}>Reply</button>
        <button onClick={() => controller.send(message.conversationId, message.currentText, message.attachmentId, "", message.id)}>Forward here</button>
        {own && !deleted && <button onClick={edit}>Edit</button>}
        {own && !deleted && <button onClick={() => controller.delete(message.id)}>Delete</button>}
        <button onClick={() => controller.markRead(message.id)}>Mark read</button>
        <button onClick={() => controller.toggleSaved(message.id)}>{saved ? "Unsave" : "Save"}</button>
        <span>Server seen {formatTime(message.serverSeenAtUnixMillis)}</span>
      </footer>
    </article>
  );
}

function LinkPreviewCard({ controller, text }: { controller: ReturnType<typeof useKnot>["controller"]; text: string }) {
  const url = text.match(/https?:\/\/[^\s]+/i)?.[0];
  const [preview, setPreview] = useState<LinkPreview>();
  useEffect(() => {
    let active = true;
    setPreview(undefined);
    if (url) {
      void controller.preview(url).then((value) => {
        if (active) {
          setPreview(value);
        }
      }).catch(() => undefined);
    }
    return () => {
      active = false;
    };
  }, [controller, url]);
  if (!url || !preview) {
    return null;
  }
  return (
    <a className="link-preview" href={preview.url} rel="noreferrer" target="_blank">
      {preview.image_url && <img alt="" src={preview.image_url} />}
      <span><small>{preview.site_name || new URL(preview.url).hostname}</small><b>{preview.title || preview.url}</b><p>{preview.description}</p></span>
    </a>
  );
}

function sessionLabel(mode: string): string {
  const value = mode.toLowerCase();
  if (value.includes("impersonated")) {
    return "Impersonated";
  }
  if (value.includes("guest")) {
    return "Guest";
  }
  return "";
}

function capitalise(value: string | undefined): string {
  return value ? value[0].toUpperCase() + value.slice(1) : "";
}

function formatTime(value: string | number): string {
  const number = Number(value);
  if (!number) {
    return "--:--:--";
  }
  return new Date(number).toLocaleTimeString([], { hour12: false });
}
