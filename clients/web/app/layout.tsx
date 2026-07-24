import type { Metadata } from "next";
import localFont from "next/font/local";
import type { ReactNode } from "react";
import { ApplicationProvider } from "@/ui/ApplicationProvider";
import "@/ui/styles.css";

const inter = localFont({
  src: "../node_modules/@fontsource-variable/inter/files/inter-latin-wght-normal.woff2",
  display: "swap",
  variable: "--font-inter",
  weight: "100 900",
});

export const metadata: Metadata = {
  metadataBase: new URL(process.env.NEXT_PUBLIC_SITE_URL ?? "http://localhost:5173"),
  title: {
    default: "Knot Unsecure — Server Is Listening",
    template: "%s | Knot Unsecure",
  },
  description: "A deliberately unprotected plaintext messenger with a public Wiretap.",
  openGraph: {
    title: "Knot Unsecure — Server Is Listening",
    description: "A deliberately unprotected plaintext messenger with a public Wiretap.",
    images: [{ url: "/og.png", width: 1200, height: 630, alt: "Knot Unsecure plaintext archive" }],
  },
  twitter: {
    card: "summary_large_image",
    title: "Knot Unsecure — Server Is Listening",
    description: "A deliberately unprotected plaintext messenger with a public Wiretap.",
    images: ["/og.png"],
  },
};

export default function RootLayout({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <html lang="en">
      <body className={inter.variable}><ApplicationProvider>{children}</ApplicationProvider></body>
    </html>
  );
}
