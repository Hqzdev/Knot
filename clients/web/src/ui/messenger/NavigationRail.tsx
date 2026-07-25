"use client";

import type { Section } from "@/domain/models";
import { useKnot } from "@/ui/ApplicationProvider";
import { ControlRoomIcon, type ControlRoomIconName } from "./ControlRoomIcon";

const primary: { id: Section; icon: ControlRoomIconName; label: string }[] = [
  { id: "chats", icon: "chat", label: "Chats" },
  { id: "wiretap", icon: "wiretap", label: "Wiretap" },
  { id: "wall", icon: "wall", label: "Wall" },
  { id: "roulette", icon: "roulette", label: "Roulette" },
];

const secondary: { id: Section; icon: ControlRoomIconName; label: string }[] = [
  { id: "contacts", icon: "contacts", label: "Contacts" },
  { id: "saved", icon: "bookmark", label: "Saved" },
  { id: "status", icon: "activity", label: "Status" },
];

export function NavigationRail() {
  const { controller, state } = useKnot();
  const item = ({ id, icon, label }: { id: Section; icon: ControlRoomIconName; label: string }, className = "") => (
    <button
      aria-label={label}
      aria-current={state.section === id ? "page" : undefined}
      className={`${className} ${state.section === id ? "active" : ""}`}
      key={id}
      onClick={() => controller.setSection(id)}
      title={label}
    >
      <ControlRoomIcon name={icon} /><span>{label}</span>
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
