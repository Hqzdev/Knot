import type { Metadata } from "next";
import { AppGate } from "@/ui/AppGate";

export const metadata: Metadata = { title: "Create account" };

export default function RegisterPage() {
  return <AppGate initialAuthMode="register" target="auth" />;
}
