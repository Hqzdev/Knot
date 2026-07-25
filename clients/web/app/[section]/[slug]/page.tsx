import type { Metadata } from "next";
import { notFound } from "next/navigation";
import {
  findPublicArticle,
  getAllPublicArticles,
} from "@/ui/public/PublicContentCatalog";
import { PublicArticlePage } from "@/ui/public/PublicArticlePage";
import { ProductChatsPage } from "@/ui/public/product/ProductChatsPage";
import { ProductRoulettePage } from "@/ui/public/product/ProductRoulettePage";
import { ProductWallPage } from "@/ui/public/product/ProductWallPage";
import { WiretapActiveWatchersPage } from "@/ui/public/wiretap/WiretapActiveWatchersPage";
import { WiretapAuditFiltersPage } from "@/ui/public/wiretap/WiretapAuditFiltersPage";
import { WiretapDeletedOriginalsPage } from "@/ui/public/wiretap/WiretapDeletedOriginalsPage";
import { WiretapGlobalFeedPage } from "@/ui/public/wiretap/WiretapGlobalFeedPage";
import { WiretapLiveDraftsPage } from "@/ui/public/wiretap/WiretapLiveDraftsPage";
import { WiretapRouteTracesPage } from "@/ui/public/wiretap/WiretapRouteTracesPage";

type PublicContentPageProps = {
  params: Promise<{
    section: string;
    slug: string;
  }>;
};

export function generateStaticParams() {
  return getAllPublicArticles().map(({ section, article }) => ({
    section: section.slug,
    slug: article.slug,
  }));
}

export async function generateMetadata({ params }: PublicContentPageProps): Promise<Metadata> {
  const { section, slug } = await params;
  const match = findPublicArticle(section, slug);

  if (!match) {
    return {};
  }

  return {
    title: match.article.title,
    description: match.article.summary,
    openGraph: {
      title: match.article.title,
      description: match.article.summary,
    },
    twitter: {
      title: match.article.title,
      description: match.article.summary,
    },
  };
}

export default async function PublicContentPage({ params }: PublicContentPageProps) {
  const { section, slug } = await params;
  const match = findPublicArticle(section, slug);

  if (!match) {
    notFound();
  }

  if (section === "wiretap") {
    if (slug === "global-feed") return <WiretapGlobalFeedPage />;
    if (slug === "deleted-originals") return <WiretapDeletedOriginalsPage />;
    if (slug === "live-drafts") return <WiretapLiveDraftsPage />;
    if (slug === "route-traces") return <WiretapRouteTracesPage />;
    if (slug === "active-watchers") return <WiretapActiveWatchersPage />;
    if (slug === "audit-filters") return <WiretapAuditFiltersPage />;
  }

  if (section === "product" && slug === "chats") {
    return <ProductChatsPage />;
  }

  if (section === "product" && slug === "the-wall") {
    return <ProductWallPage />;
  }

  if (section === "product" && slug === "roulette") {
    return <ProductRoulettePage />;
  }

  return <PublicArticlePage article={match.article} section={match.section} />;
}
