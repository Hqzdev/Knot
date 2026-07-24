"use client";

import { KnotController } from "@/application/KnotController";
import type { AppState } from "@/domain/models";
import { createContext, type ReactNode, useContext, useEffect, useRef, useSyncExternalStore } from "react";

interface ApplicationContextValue {
  controller: KnotController;
  state: AppState;
}

const ApplicationContext = createContext<ApplicationContextValue | undefined>(undefined);
const serverSnapshot = (): AppState => initialServerState;
const initialServerState: AppState = {
  phase: "restoring",
  riskAccepted: false,
  section: "chats",
  conversations: [],
  messages: {},
  wiretap: [],
  contacts: [],
  savedMessageIds: [],
  watchers: {},
  drafts: [],
  online: {},
  roulette: "idle",
  search: "",
  connected: false,
};

export function ApplicationProvider({ children }: { children: ReactNode }) {
  const controllerRef = useRef<KnotController | null>(null);
  if (!controllerRef.current) {
    controllerRef.current = new KnotController();
  }
  const controller = controllerRef.current;
  const state = useSyncExternalStore(controller.subscribe, controller.snapshot, serverSnapshot);
  useEffect(() => {
    void controller.restore();
    return () => controller.dispose();
  }, [controller]);
  return <ApplicationContext.Provider value={{ controller, state }}>{children}</ApplicationContext.Provider>;
}

export function useKnot(): ApplicationContextValue {
  const value = useContext(ApplicationContext);
  if (!value) {
    throw new Error("Knot application is unavailable");
  }
  return value;
}
