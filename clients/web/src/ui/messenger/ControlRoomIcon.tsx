import type { ReactNode } from "react";

export type ControlRoomIconName =
  | "activity"
  | "alert"
  | "bookmark"
  | "chat"
  | "chevronDown"
  | "clock"
  | "close"
  | "contacts"
  | "eye"
  | "microphone"
  | "mute"
  | "panel"
  | "paperclip"
  | "pin"
  | "plus"
  | "roulette"
  | "settings"
  | "signal"
  | "wall"
  | "wiretap";

interface ControlRoomIconProps {
  name: ControlRoomIconName;
  size?: number;
}

export function ControlRoomIcon({ name, size = 18 }: ControlRoomIconProps) {
  return (
    <svg
      aria-hidden="true"
      className="control-room-icon"
      fill="none"
      height={size}
      viewBox="0 0 24 24"
      width={size}
    >
      {icon(name)}
    </svg>
  );
}

function icon(name: ControlRoomIconName): ReactNode {
  switch (name) {
    case "activity":
      return <><path d="M3 12h4l2.2-6 4.2 12 2.2-6H21" /><path d="M4 4h16v16H4z" /></>;
    case "alert":
      return <><path d="M12 3 2.8 20h18.4L12 3Z" /><path d="M12 9v4.5" /><path d="M12 17h.01" /></>;
    case "bookmark":
      return <path d="M6.5 4.5h11v15l-5.5-3.6-5.5 3.6v-15Z" />;
    case "chat":
      return <><path d="M4 5.5h16v11H9l-5 3v-14Z" /><path d="M8 9h8M8 12.5h5" /></>;
    case "chevronDown":
      return <path d="m7 9.5 5 5 5-5" />;
    case "clock":
      return <><circle cx="12" cy="12" r="8.5" /><path d="M12 7.5V12l3 2" /></>;
    case "close":
      return <path d="m7 7 10 10M17 7 7 17" />;
    case "contacts":
      return <><circle cx="9" cy="9" r="3" /><circle cx="17" cy="10" r="2.3" /><path d="M3.5 19c.5-3.2 2.3-5 5.5-5s5 1.8 5.5 5M14.5 15c2.8-.4 4.8.8 5.5 3.5" /></>;
    case "eye":
      return <><path d="M2.5 12s3.4-5.5 9.5-5.5 9.5 5.5 9.5 5.5-3.4 5.5-9.5 5.5S2.5 12 2.5 12Z" /><circle cx="12" cy="12" r="2.5" /></>;
    case "microphone":
      return <><rect height="9" rx="3.5" width="6" x="9" y="3.5" /><path d="M6.5 11.5a5.5 5.5 0 0 0 11 0M12 17v3.5M9 20.5h6" /></>;
    case "mute":
      return <><path d="M5 10h3l4-3v10l-4-3H5v-4Z" /><path d="m16 10 4 4M20 10l-4 4" /></>;
    case "panel":
      return <><rect height="16" rx="2" width="18" x="3" y="4" /><path d="M15 4v16" /></>;
    case "paperclip":
      return <path d="m9 12.5 5.7-5.7a3 3 0 1 1 4.3 4.3l-7.4 7.4a5 5 0 0 1-7.1-7.1l7.1-7.1" />;
    case "pin":
      return <><path d="m8 4 8 8M14.5 3.5l6 6-3.2 1.1-4.7 4.7-1.1 3.2-6-6 3.2-1.1 4.7-4.7 1.1-3.2Z" /><path d="m8.5 15.5-5 5" /></>;
    case "plus":
      return <path d="M12 5v14M5 12h14" />;
    case "roulette":
      return <><path d="M4 7h3.2c4.8 0 4.8 10 9.6 10H20" /><path d="m17 14 3 3-3 3M4 17h3.2c1.4 0 2.4-.8 3.3-2M13.5 9c.9-1.2 1.9-2 3.3-2H20M17 4l3 3-3 3" /></>;
    case "settings":
      return <><path d="M4 7h5M15 7h5M4 17h9M17 17h3" /><circle cx="12" cy="7" r="2.5" /><circle cx="16" cy="17" r="2.5" /></>;
    case "signal":
      return <><circle cx="12" cy="12" r="2" /><path d="M7.5 16.5a6.4 6.4 0 0 1 0-9M16.5 7.5a6.4 6.4 0 0 1 0 9M4.5 19.5a10.6 10.6 0 0 1 0-15M19.5 4.5a10.6 10.6 0 0 1 0 15" /></>;
    case "wall":
      return <><rect height="16" rx="2" width="18" x="3" y="4" /><path d="M3 9h18M3 15h18M9 4v5M15 9v6M10 15v5" /></>;
    case "wiretap":
      return <><circle cx="12" cy="12" r="2" /><path d="M8.5 8.5a5 5 0 0 0 0 7M15.5 8.5a5 5 0 0 1 0 7M5.5 5.5a9.2 9.2 0 0 0 0 13M18.5 5.5a9.2 9.2 0 0 1 0 13" /></>;
  }
}
