"use client";

import { useKnot } from "@/ui/ApplicationProvider";

export function SavedPane() {
  const { controller, state } = useKnot();
  const messages = Object.values(state.messages).flat()
    .filter((message) => state.savedMessageIds.includes(message.id))
    .sort((left, right) => Number(right.createdAtUnixMillis) - Number(left.createdAtUnixMillis));
  return (
    <section className="saved-pane">
      <header><p className="eyebrow">LOCAL PLAINTEXT BOOKMARKS</p><h1>Saved signals</h1><p>Messages bookmarked in this browser and backed by server history.</p></header>
      <div>
        {messages.length === 0 && <div className="directory-empty">Nothing bookmarked yet.</div>}
        {messages.map((message) => (
          <article key={message.id}>
            <time>{new Date(Number(message.createdAtUnixMillis)).toLocaleString()}</time>
            <p>{message.originalText}</p>
            <footer><span>{message.conversationId}</span><b>{message.deletedAtUnixMillis ? "TOMBSTONED" : "PLAINTEXT"}</b><button onClick={() => controller.toggleSaved(message.id)}>REMOVE</button></footer>
          </article>
        ))}
      </div>
    </section>
  );
}
