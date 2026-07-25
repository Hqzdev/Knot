import type { ReactNode } from "react";
import { EditorialNavigation } from "../EditorialNavigation";
import { getPublicArticlePath } from "../PublicContentCatalog";
import { PublicFooter } from "../PublicFooter";

type ProductShellProps = {
  children: ReactNode;
  current: string;
};

export function ProductShell({ children, current }: Readonly<ProductShellProps>) {
  return (
    <main className={`public-shell product-shell product-shell--${current}`}>
      <EditorialNavigation />
      {children}
      <PublicFooter />
    </main>
  );
}

export function ProductBreadcrumb({ current }: Readonly<{ current: string }>) {
  return (
    <nav aria-label="Breadcrumb" className="product-breadcrumb">
      <a href="/">Home</a>
      <span>/</span>
      <a href={getPublicArticlePath("product", "chats")}>Product</a>
      <span>/</span>
      <span>{current}</span>
    </nav>
  );
}
