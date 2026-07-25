import type { Metadata } from "next";
import { AppGate } from "@/ui/AppGate";

export const metadata: Metadata = { title: "Log in" };

export default function LoginPage() {
  return <AppGate target="auth" />;
}
