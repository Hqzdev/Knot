"use client";

import type { LinkPreview, Message } from "@/domain/models";
import { useKnot } from "@/ui/ApplicationProvider";
import { type ChangeEvent, type FormEvent, useEffect, useMemo, useRef, useState } from "react";
import { conversationTitle } from "./ConversationList";

const emojis = ["👁", "📡", "🚨", "💀", "🔓", "🤡", "🫡"];

export function ChatPane({ wall = false, onShowConversations }: { wall?: boolean; onShowConversations?: () => void }) {
  const { controller, state } = useKnot();
  const conversationId = wall ? "wall" : state.selectedConversationId;
  const conversation = state.conversations.find((value) => value.id === conversationId);
  const messages = conversationId ? state.messages[conversationId] ?? [] : [];
  const watchers = conversationId ? state.watchers[conversationId] ?? [] : [];
  const [text, setText] = useState("");
  const [reply, setReply] = useState<Message>();
  const [uploading, setUploading] = useState(false);
  const endRef = useRef<HTMLDivElement>(null);

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
        <p className="eyebrow">NO TARGET SELECTED</p>
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
    controller.send(conversationId, text.trim(), "", reply?.id);
    setText("");
    setReply(undefined);
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
        {onShowConversations && <button className="mobile-conversation-button" onClick={onShowConversations}>CHANNELS</button>}
        <div>
          <p className="eyebrow">{wall ? "GLOBAL PERMANENT ROOM" : conversation?.kind.toUpperCase() ?? "CHANNEL"}</p>
          <h2>{wall ? "The Wall" : conversationTitle(conversation, state.session?.user.id ?? "")}</h2>
        </div>
        <div className="header-status">
          <span className="listener"><i /> SERVER IS LISTENING</span>
          <span>{watchers.length} WATCHER{watchers.length === 1 ? "" : "S"}</span>
        </div>
      </header>
      <div className="message-stream">
        <div className="retention-banner">
          <b>RETENTION: FOREVER</b>
          <span>Deletion only changes what participants see. Wiretap keeps the original.</span>
        </div>
        {messages.length === 0 && (
          <div className="message-empty">
            <span>00:00:00.000</span>
            <h3>No signal captured.</h3>
            <p>The first message will be stored in plaintext and marked with 👁 automatically.</p>
          </div>
        )}
        {messages.map((message) => (
          <MessageCard controller={controller} currentUserId={state.session?.user.id ?? ""} key={message.id} message={message} onReply={() => setReply(message)} saved={state.savedMessageIds.includes(message.id)} />
        ))}
        {activeDrafts.map((draft) => (
          <div className="draft-leak" key={draft.session_id}>
            <span>LIVE DRAFT / {draft.username}</span>
            <p>{draft.text || "…"}</p>
            <time>SELF-DESTRUCTS FROM REDIS AT {new Date(draft.expires_at).toLocaleTimeString()}</time>
          </div>
        ))}
        <div ref={endRef} />
      </div>
      <form className="composer" onSubmit={submit}>
        {reply && <div className="reply-strip"><span>Replying to {reply.authorUsername}: {reply.originalText}</span><button type="button" onClick={() => setReply(undefined)}>×</button></div>}
        <div className="composer-row">
          <label className="attach-button" title="Upload a public file">
            {uploading ? "…" : "+"}<input disabled={uploading} type="file" onChange={(event) => void upload(event)} />
          </label>
          <textarea
            aria-label="Message"
            placeholder="Type something the whole server can read…"
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
          <button className="send-button" disabled={!text.trim()}>SEND <span>↗</span></button>
        </div>
        <footer><span>PLAINTEXT</span><span>SERVER COPY ENABLED</span><span>DRAFT BROADCAST ON</span></footer>
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
  const edit = () => {
    const value = window.prompt("Replace the participant-visible text. Wiretap keeps the original.", message.currentText);
    if (value?.trim()) {
      controller.edit(message.id, value.trim());
    }
  };
  return (
    <article className={`message-card ${own ? "own" : ""} ${deleted ? "deleted" : ""}`}>
      <header>
        <strong>{message.authorUsername}</strong>
        {sessionLabel(message.sessionMode) && <b className="session-badge">{sessionLabel(message.sessionMode)}</b>}
        <time>{formatTime(message.createdAtUnixMillis)}</time>
      </header>
      {message.replyToId && <div className="message-reference">↳ Reply to {message.replyToId}</div>}
      {message.forwardedFromId && <div className="message-reference">Forwarded from {message.forwardedFromId}</div>}
      <p>{deleted ? "Message removed from participant view." : message.currentText}</p>
      {deleted && <small className="tombstone">ORIGINAL RETAINED IN WIRETAP: “{message.originalText}”</small>}
      {!deleted && message.originalText !== message.currentText && <small className="tombstone">EDITED · ORIGINAL AVAILABLE IN WIRETAP</small>}
      {message.attachmentId && <a className="file-card" href={controller.attachmentURL(message.attachmentId)} target="_blank"><span>PUBLIC FILE</span><b>{message.currentText || message.attachmentId}</b><small>Anyone with this link can request it →</small></a>}
      {!deleted && <LinkPreviewCard controller={controller} text={message.currentText} />}
      <div className="reaction-row">
        {(message.reactions ?? []).map((reaction) => <button key={reaction.emoji} onClick={() => controller.react(message.id, reaction.emoji, false)}>{reaction.emoji} {reaction.usernames.length}</button>)}
        <details>
          <summary>+</summary>
          <div>{emojis.map((emoji) => <button key={emoji} onClick={() => controller.react(message.id, emoji)}>{emoji}</button>)}</div>
        </details>
      </div>
      <footer className="message-actions">
        <button onClick={onReply}>Reply</button>
        <button onClick={() => controller.send(message.conversationId, message.currentText, message.attachmentId, "", message.id)}>Forward here</button>
        {own && !deleted && <button onClick={edit}>Edit</button>}
        {own && !deleted && <button onClick={() => controller.delete(message.id)}>Delete</button>}
        <button onClick={() => controller.markRead(message.id)}>Mark read</button>
        <button onClick={() => controller.toggleSaved(message.id)}>{saved ? "Unsave" : "Save"}</button>
        <span>SERVER SEEN {formatTime(message.serverSeenAtUnixMillis)}</span>
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
    return "IMPERSONATED";
  }
  if (value.includes("guest")) {
    return "GUEST";
  }
  return "";
}

function formatTime(value: string | number): string {
  const number = Number(value);
  if (!number) {
    return "--:--:--";
  }
  return new Date(number).toLocaleTimeString([], { hour12: false });
}
