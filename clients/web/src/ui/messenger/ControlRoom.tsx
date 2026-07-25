"use client";

import { useKnot } from "@/ui/ApplicationProvider";
import { ChatPane } from "./ChatPane";
import { ContactsPane } from "./ContactsPane";
import { ConversationList } from "./ConversationList";
import { Inspector } from "./Inspector";
import { NavigationRail } from "./NavigationRail";
import { RoulettePane } from "./RoulettePane";
import { SavedPane } from "./SavedPane";
import { SystemStatusPane } from "./SystemStatusPane";
import { WiretapPane } from "./WiretapPane";
import { useEffect, useState } from "react";

export function ControlRoom() {
  const { state } = useKnot();
  const [mobileListOpen, setMobileListOpen] = useState(false);
  const [inspectorOpen, setInspectorOpen] = useState(true);
  const selectedMessages = state.selectedConversationId ? state.messages[state.selectedConversationId] ?? [] : [];
  const inspected = selectedMessages.at(-1);
  const chatLayout = state.section === "chats" || state.section === "wall";
  const hasConversationList = state.section === "chats";
  useEffect(() => {
    if (inspected?.id) {
      setInspectorOpen(true);
    }
  }, [inspected?.id]);
  return (
    <main className={`control-room ${hasConversationList ? "with-list" : "focus-view"} ${chatLayout && inspectorOpen ? "inspector-open" : ""}`}>
      <NavigationRail />
      {state.section === "chats" && <ConversationList mobileOpen={mobileListOpen} onClose={() => setMobileListOpen(false)} />}
      <div className="workspace">
        {state.section === "chats" && <ChatPane inspectorOpen={inspectorOpen} onShowConversations={() => setMobileListOpen(true)} onToggleInspector={() => setInspectorOpen((value) => !value)} />}
        {state.section === "wall" && <ChatPane inspectorOpen={inspectorOpen} onToggleInspector={() => setInspectorOpen((value) => !value)} wall />}
        {state.section === "wiretap" && <WiretapPane />}
        {state.section === "roulette" && <RoulettePane />}
        {state.section === "contacts" && <ContactsPane />}
        {state.section === "saved" && <SavedPane />}
        {state.section === "status" && <SystemStatusPane />}
      </div>
      {chatLayout && inspectorOpen && <Inspector message={inspected} onClose={() => setInspectorOpen(false)} />}
      {state.error && <div className="toast" role="alert"><b>Control room error</b><span>{state.error}</span></div>}
      {state.session?.mode === "impersonated" && <div className="impersonation-ribbon">Impersonated session · actions attributed publicly</div>}
    </main>
  );
}
