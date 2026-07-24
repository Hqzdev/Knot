"use client";

import { useKnot } from "@/ui/ApplicationProvider";
import { AuthPage } from "@/ui/auth/AuthPage";
import { ControlRoom } from "@/ui/messenger/ControlRoom";
import { useRouter } from "next/navigation";
import { useEffect } from "react";

export function AppGate({ target }: { target: "auth" | "app" }) {
  const { state } = useKnot();
  const router = useRouter();
  const destination = state.phase === "authenticated" ? "/app" : "/login";
  const valid = target === "app" ? state.phase === "authenticated" : state.phase === "anonymous";
  useEffect(() => {
    if (state.phase !== "restoring" && !valid) {
      router.replace(destination);
    }
  }, [destination, router, state.phase, valid]);
  if (state.phase === "restoring") {
    return <main className="loading-screen"><div className="scanner-box"><span /></div><p>OPENING PLAINTEXT ARCHIVE</p></main>;
  }
  if (target === "app" && state.phase === "authenticated") {
    return <ControlRoom />;
  }
  if (target === "auth" && state.phase === "anonymous") {
    return <AuthPage />;
  }
  return <main className="loading-screen"><p>REDIRECTING OBSERVER</p></main>;
}
