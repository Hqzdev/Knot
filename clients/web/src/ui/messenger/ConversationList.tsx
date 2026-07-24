"use client";

import type { Conversation } from "@/domain/models";
import { useKnot } from "@/ui/ApplicationProvider";
import { type FormEvent, useMemo, useState } from "react";

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
        <div><p className="eyebrow">PRIVATE-ISH CHANNELS</p><h2>Chats <sup>{values.length}</sup></h2></div>
        <div className="list-header-actions">
          <button className="square-button" onClick={() => setNewChat((value) => !value)}>+</button>
          <button className="square-button mobile-list-close" onClick={onClose}>×</button>
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
            <button className={`conversation-row ${active ? "active" : ""}`} key={conversation.id} onClick={() => void controller.selectConversation(conversation.id).then(onClose)}>
              <span className="avatar">{conversation.kind === "group" ? "GR" : title(conversation, state.session?.user.id ?? "").slice(0, 2).toUpperCase()}</span>
              <span className="conversation-copy">
                <strong>{title(conversation, state.session?.user.id ?? "")}</strong>
                <small>{last?.deletedAtUnixMillis ? "Message deleted · original retained" : last?.currentText || "Nothing intercepted yet"}</small>
              </span>
              <span className="conversation-tools">
                <i className={conversation.members.some((member) => state.online[member.user_id]) ? "online" : ""} />
                <span onClick={(event) => { event.stopPropagation(); setPinned(toggle(pinned, conversation.id)); }}>{pinned.includes(conversation.id) ? "◆" : "◇"}</span>
                <span onClick={(event) => { event.stopPropagation(); setMuted(toggle(muted, conversation.id)); }}>{muted.includes(conversation.id) ? "M" : "S"}</span>
              </span>
            </button>
          );
        })}
      </div>
      <footer><span>{state.connected ? "LIVE LINK" : "RECONNECTING"}</span><b>{state.session?.mode.toUpperCase()}</b></footer>
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

function toggle(values: string[], id: string): string[] {
  return values.includes(id) ? values.filter((value) => value !== id) : [...values, id];
}
