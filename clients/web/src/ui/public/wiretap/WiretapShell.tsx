import type { ReactNode } from "react";
import { EditorialNavigation } from "../EditorialNavigation";
import { getPublicArticlePath } from "../PublicContentCatalog";
import { PublicFooter } from "../PublicFooter";

type WiretapShellProps = {
  children: ReactNode;
  current: string;
};

export function WiretapShell({ children, current }: Readonly<WiretapShellProps>) {
  return (
    <main className={`public-shell wiretap-shell wiretap-shell--${current}`}>
      <EditorialNavigation />
      {children}
      <PublicFooter />
    </main>
  );
}

export function WiretapBreadcrumb({ current }: Readonly<{ current: string }>) {
  return (
    <nav aria-label="Breadcrumb" className="wiretap-breadcrumb">
      <a href="/">Home</a>
      <span>/</span>
      <a href={getPublicArticlePath("wiretap", "global-feed")}>Wiretap</a>
      <span>/</span>
      <span>{current}</span>
    </nav>
  );
}

export function WiretapCta({ href, children }: Readonly<{ href: string; children: ReactNode }>) {
  return <a className="wiretap-cta" href={href}>{children}</a>;
}
