"use client";

import type { Conversation } from "@/domain/models";
import { useKnot } from "@/ui/ApplicationProvider";
import { type FormEvent, useMemo, useState } from "react";
import { ControlRoomIcon } from "./ControlRoomIcon";

export function ConversationList({ mobileOpen, onClose }: { mobileOpen: boolean; onClose: () => void }) {
  const { controller, state } = useKnot();
  const [newChat, setNewChat] = useState(false);
  const [mode, setMode] = useState<"direct" | "group">("direct");
  const [username, setUsername] = useState("");
  const [groupTitle, setGroupTitle] = useState("");
  const [members, setMembers] = useState("");
  const [pinned, setPinned] = useState<string[]>([]);
  const [muted, setMuted] = useState<string[]>([]);
  const values = useMemo(() => {
    const query = state.search.toLowerCase();
    return state.conversations
      .filter((value) => value.kind !== "wall")
      .filter((value) => title(value, state.session?.user.id ?? "").toLowerCase().includes(query))
      .sort((left, right) => Number(pinned.includes(right.id)) - Number(pinned.includes(left.id)));
  }, [pinned, state.conversations, state.search, state.session?.user.id]);
  const create = (event: FormEvent) => {
    event.preventDefault();
    const operation = mode === "direct"
      ? controller.createDirect(username)
      : controller.createGroup(groupTitle, members.split(",").map((value) => value.trim()).filter(Boolean));
    void operation.then(() => {
      setUsername("");
      setGroupTitle("");
      setMembers("");
      setNewChat(false);
      onClose();
    });
  };
  return (
    <aside className={`conversation-list ${mobileOpen ? "mobile-open" : ""}`}>
      <header>
        <div><p className="eyebrow">Private-ish channels</p><h2>Chats <sup>{values.length}</sup></h2></div>
        <div className="list-header-actions">
          <button aria-label="Create conversation" className="square-button" onClick={() => setNewChat((value) => !value)} title="Create conversation"><ControlRoomIcon name="plus" /></button>
          <button aria-label="Close conversations" className="square-button mobile-list-close" onClick={onClose} title="Close conversations"><ControlRoomIcon name="close" /></button>
        </div>
      </header>
      <input className="search-input" placeholder="Search exposed history" value={state.search} onChange={(event) => controller.setSearch(event.target.value)} />
      {newChat && (
        <form className="new-chat" onSubmit={create}>
          <div className="conversation-mode">
            <button type="button" aria-pressed={mode === "direct"} onClick={() => setMode("direct")}>Direct</button>
            <button type="button" aria-pressed={mode === "group"} onClick={() => setMode("group")}>Group</button>
          </div>
          {mode === "direct"
            ? <input autoFocus placeholder="Username" required value={username} onChange={(event) => setUsername(event.target.value)} />
            : <>
              <input autoFocus placeholder="Group title" required value={groupTitle} onChange={(event) => setGroupTitle(event.target.value)} />
              <input placeholder="Members: alice, bob" required value={members} onChange={(event) => setMembers(event.target.value)} />
            </>}
          <button className="create-conversation-button">Create</button>
        </form>
      )}
      <div className="conversation-items">
        {values.length === 0 && <div className="list-empty">No conversations yet.<br />Find someone worth exposing.</div>}
        {values.map((conversation) => {
          const messages = state.messages[conversation.id] ?? [];
          const last = messages.at(-1);
          const active = state.selectedConversationId === conversation.id;
          return (
            <article className={`conversation-row ${active ? "active" : ""}`} key={conversation.id}>
              <button className="conversation-select" onClick={() => void controller.selectConversation(conversation.id).then(onClose)}>
                <span className="avatar">{conversation.kind === "group" ? "GR" : title(conversation, state.session?.user.id ?? "").slice(0, 2).toUpperCase()}</span>
                <span className="conversation-copy">
                  <strong>{title(conversation, state.session?.user.id ?? "")}</strong>
                  <small>{last?.deletedAtUnixMillis ? "Message deleted · original retained" : last?.currentText || "Nothing intercepted yet"}</small>
                </span>
              </button>
              <span className="conversation-tools">
                <i className={`presence-dot ${conversation.members.some((member) => state.online[member.user_id]) ? "online" : ""}`} title={conversation.members.some((member) => state.online[member.user_id]) ? "Online" : "Offline"} />
                <span className="conversation-actions">
                  <button aria-label={pinned.includes(conversation.id) ? `Unpin ${title(conversation, state.session?.user.id ?? "")}` : `Pin ${title(conversation, state.session?.user.id ?? "")}`} aria-pressed={pinned.includes(conversation.id)} onClick={() => setPinned(toggle(pinned, conversation.id))} title={pinned.includes(conversation.id) ? "Unpin conversation" : "Pin conversation"}><ControlRoomIcon name="pin" size={15} /></button>
                  <button aria-label={muted.includes(conversation.id) ? `Unmute ${title(conversation, state.session?.user.id ?? "")}` : `Mute ${title(conversation, state.session?.user.id ?? "")}`} aria-pressed={muted.includes(conversation.id)} onClick={() => setMuted(toggle(muted, conversation.id))} title={muted.includes(conversation.id) ? "Unmute conversation" : "Mute conversation"}><ControlRoomIcon name="mute" size={15} /></button>
                </span>
              </span>
            </article>
          );
        })}
      </div>
      <footer><span>{state.connected ? "Live link" : "Reconnecting"}</span><b>{sessionMode(state.session?.mode)}</b></footer>
    </aside>
  );
}

export function conversationTitle(conversation: Conversation | undefined, currentUserId: string): string {
  return conversation ? title(conversation, currentUserId) : "Select a conversation";
}

function title(conversation: Conversation, currentUserId: string): string {
  if (conversation.title) {
    return conversation.title;
  }
  return conversation.members.find((member) => member.user_id !== currentUserId)?.username ?? "Saved signal";
}

function sessionMode(value: string | undefined): string {
  return value ? value[0].toUpperCase() + value.slice(1) : "";
}

function toggle(values: string[], id: string): string[] {
  return values.includes(id) ? values.filter((value) => value !== id) : [...values, id];
}
