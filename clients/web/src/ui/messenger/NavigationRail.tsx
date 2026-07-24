"use client";

import type { Section } from "@/domain/models";
import { useKnot } from "@/ui/ApplicationProvider";

const primary: { id: Section; short: string; label: string }[] = [
  { id: "chats", short: "CH", label: "Chats" },
  { id: "wiretap", short: "WT", label: "Wiretap" },
  { id: "wall", short: "WL", label: "Wall" },
  { id: "roulette", short: "RX", label: "Roulette" },
];

const secondary: { id: Section; short: string; label: string }[] = [
  { id: "contacts", short: "CT", label: "Contacts" },
  { id: "saved", short: "SV", label: "Saved" },
];

export function NavigationRail() {
  const { controller, state } = useKnot();
  const item = ({ id, short, label }: { id: Section; short: string; label: string }, className = "") => (
    <button className={`${className} ${state.section === id ? "active" : ""}`} key={id} onClick={() => controller.setSection(id)}>
      <b>{short}</b><span>{label}</span>
    </button>
  );
  return (
    <nav className="navigation-rail" aria-label="Main navigation">
      <a className="rail-mark" href="/">K</a>
      <div className="rail-main">{primary.map((value) => item(value))}{secondary.map((value) => item(value, "desktop-secondary"))}</div>
      <details className="mobile-more">
        <summary>•••<span>More</span></summary>
        <div>{secondary.map((value) => item(value))}</div>
      </details>
      <button className="rail-avatar" title="Sign out" onClick={() => void controller.logout()}>
        {state.session?.user.username.slice(0, 2).toUpperCase()}
      </button>
    </nav>
  );
}
