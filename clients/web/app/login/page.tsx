import type { Metadata } from "next";
import { AppGate } from "@/ui/AppGate";

export const metadata: Metadata = { title: "Access Terminal" };

export default function LoginPage() {
  return <AppGate target="auth" />;
}
