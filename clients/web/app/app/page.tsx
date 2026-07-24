import type { Metadata } from "next";
import { AppGate } from "@/ui/AppGate";

export const metadata: Metadata = { title: "Control Room" };

export default function WorkspacePage() {
  return <AppGate target="app" />;
}
