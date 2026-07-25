import { EditorialNavigation } from "./EditorialNavigation";
import {
  getPublicArticlePath,
  getRelatedArticles,
  type PublicArticle,
  type PublicSection,
} from "./PublicContentCatalog";
import { PublicFooter } from "./PublicFooter";

type PublicArticlePageProps = {
  section: PublicSection;
  article: PublicArticle;
};

export function PublicArticlePage({ section, article }: Readonly<PublicArticlePageProps>) {
  const relatedArticles = getRelatedArticles(section, article.slug);

  return (
    <main className="public-shell">
      <EditorialNavigation />
      <article className="reference-article">
        <nav aria-label="Breadcrumb" className="reference-breadcrumb">
          <a href="/">Home</a>
          <span>/</span>
          <span>{section.label}</span>
        </nav>

        <header className="reference-hero">
          <h1>{article.title}</h1>
          <p>{article.summary}</p>
        </header>

        <section className="reference-notice" aria-label="Key point">
          <div aria-hidden="true">{section.mark}</div>
          <div>
            <h2>Key point</h2>
            <p>{article.notice}</p>
          </div>
        </section>

        <section className="reference-section">
          <header className="reference-section-heading">
            <h2>What the system reveals</h2>
            <p>{section.introduction}</p>
          </header>
          <div className="reference-card-grid">
            {article.signals.map((signal, index) => (
              <article className="reference-card" key={signal.title}>
                <span aria-hidden="true">{String(index + 1).padStart(2, "0")}</span>
                <h3>{signal.title}</h3>
                <p>{signal.description}</p>
              </article>
            ))}
          </div>
        </section>

        <section className="reference-analysis">
          <article>
            <h2>How it works</h2>
            <p>{article.detail}</p>
          </article>
          <article>
            <h2>Why it matters</h2>
            <p>{article.consequence}</p>
          </article>
        </section>

        <section className="reference-section reference-related">
          <header className="reference-section-heading">
            <h2>Continue exploring {section.label}</h2>
            <p>Related pages from the same part of the Knot reference.</p>
          </header>
          <div className="reference-related-grid">
            {relatedArticles.map((relatedArticle) => (
              <a href={getPublicArticlePath(section.slug, relatedArticle.slug)} key={relatedArticle.slug}>
                <span>{relatedArticle.label}</span>
                <p>{relatedArticle.summary}</p>
                <b aria-hidden="true">→</b>
              </a>
            ))}
          </div>
        </section>

        <section className="reference-entry">
          <div>
            <h2>See the exposure in the product</h2>
            <p>Enter only with disposable credentials and information you are willing to publish.</p>
          </div>
          <a href="/login">Open Knot ↗</a>
        </section>
      </article>
      <PublicFooter />
    </main>
  );
}
