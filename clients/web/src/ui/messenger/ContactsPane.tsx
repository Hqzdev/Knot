"use client";

import type { User } from "@/domain/models";
import { useKnot } from "@/ui/ApplicationProvider";
import { type FormEvent, useState } from "react";

export function ContactsPane() {
  const { controller, state } = useKnot();
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<User[]>([]);
  const [busy, setBusy] = useState(false);
  const search = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    try {
      setResults(await controller.searchUsers(query));
    } finally {
      setBusy(false);
    }
  };
  return (
    <section className="directory-pane">
      <header><p className="eyebrow">Public identity directory</p><h1>Contacts</h1><p>Find an account, observe its profile, start a channel.</p></header>
      <form onSubmit={search}><input required minLength={1} placeholder="Search username" value={query} onChange={(event) => setQuery(event.target.value)} /><button disabled={busy}>Scan</button></form>
      <div className="contact-strip">
        <span>Saved contacts</span>
        {state.contacts.length === 0 && <p>No contacts retained.</p>}
        {state.contacts.map((user) => (
          <button key={user.id} onClick={() => setResults([user])}>@{user.username}</button>
        ))}
      </div>
      <div className="directory-results">
        {results.length === 0 && <div className="directory-empty">No identity selected.</div>}
        {results.map((user) => (
          <article key={user.id}>
            <span>{user.username.slice(0, 2).toUpperCase()}</span>
            <div><strong>{user.display_name}</strong><small>@{user.username} · {user.kind}</small></div>
            <div className="directory-actions">
              <button onClick={() => void controller.createDirect(user.username)}>Open chat</button>
              <button onClick={() => void controller.setContact(user.username, !state.contacts.some((contact) => contact.id === user.id))}>
                {state.contacts.some((contact) => contact.id === user.id) ? "Remove" : "Save"}
              </button>
            </div>
          </article>
        ))}
      </div>
    </section>
  );
}
