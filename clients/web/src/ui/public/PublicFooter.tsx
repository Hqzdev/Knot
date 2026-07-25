import { getPublicArticlePath, publicSections } from "./PublicContentCatalog";

export function PublicFooter() {
  return (
    <footer className="editorial-footer">
      {publicSections.map((section) => (
        <div key={section.slug}>
          <span>{section.label}</span>
          {section.groups.flatMap((group) => group.articles).slice(0, 4).map((article) => (
            <a href={getPublicArticlePath(section.slug, article.slug)} key={article.slug}>{article.label}</a>
          ))}
        </div>
      ))}
    </footer>
  );
}
