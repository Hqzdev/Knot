"use client";

import { useKnot } from "@/ui/ApplicationProvider";
import { ChatPane } from "./ChatPane";
import { ContactsPane } from "./ContactsPane";
import { ConversationList } from "./ConversationList";
import { Inspector } from "./Inspector";
import { NavigationRail } from "./NavigationRail";
import { RoulettePane } from "./RoulettePane";
import { SavedPane } from "./SavedPane";
import { WiretapPane } from "./WiretapPane";
import { useState } from "react";

export function ControlRoom() {
  const { state } = useKnot();
  const [mobileListOpen, setMobileListOpen] = useState(false);
  const selectedMessages = state.selectedConversationId ? state.messages[state.selectedConversationId] ?? [] : [];
  const inspected = selectedMessages.at(-1);
  const chatLayout = state.section === "chats" || state.section === "wall";
  return (
    <main className={`control-room ${chatLayout ? "with-list" : "focus-view"}`}>
      <NavigationRail />
      {state.section === "chats" && <ConversationList mobileOpen={mobileListOpen} onClose={() => setMobileListOpen(false)} />}
      <div className="workspace">
        {state.section === "chats" && <ChatPane onShowConversations={() => setMobileListOpen(true)} />}
        {state.section === "wall" && <ChatPane wall />}
        {state.section === "wiretap" && <WiretapPane />}
        {state.section === "roulette" && <RoulettePane />}
        {state.section === "contacts" && <ContactsPane />}
        {state.section === "saved" && <SavedPane />}
      </div>
      {chatLayout && <Inspector message={inspected} />}
      {state.error && <div className="toast" role="alert"><b>CONTROL ROOM ERROR</b><span>{state.error}</span></div>}
      {state.session?.mode === "impersonated" && <div className="impersonation-ribbon">IMPERSONATED SESSION · ACTIONS ATTRIBUTED PUBLICLY</div>}
    </main>
  );
}
