import { getPublicArticlePath, publicSections } from "./PublicContentCatalog";

type NavigationLink = {
  label: string;
  href: string;
};

type NavigationGroup = {
  label: string;
  links: NavigationLink[];
};

type NavigationItem = {
  label: string;
  groups: NavigationGroup[];
};

const navigationItems: NavigationItem[] = [
  ...publicSections.map((section) => ({
    label: section.label,
    groups: section.groups.map((group) => ({
      label: group.label,
      links: group.articles.map((article) => ({
        label: article.label,
        href: getPublicArticlePath(section.slug, article.slug),
      })),
    })),
  })),
];

const exploreItem: NavigationItem = {
  label: "Explore Knot",
  groups: [
    {
      label: "Explore product",
      links: [
        { label: "Chats", href: "/product/chats" },
        { label: "Wiretap", href: "/wiretap/global-feed" },
        { label: "The Wall", href: "/product/the-wall" },
        { label: "Roulette", href: "/product/roulette" },
      ],
    },
  ],
};

export function EditorialNavigation() {
  return (
    <header className="editorial-header">
      <nav className="editorial-nav">
        <a className="editorial-wordmark" href="/">Knot Unsecure</a>
        <div className="editorial-links">
          {navigationItems.map((item) => <EditorialNavigationItem item={item} key={item.label} />)}
        </div>
        <details className="editorial-mobile-menu">
          <summary>Menu</summary>
          <div className="editorial-mobile-panel">
            {navigationItems.map((item) => (
              <section key={item.label}>
                <h2>{item.label}</h2>
                {item.groups.flatMap((group) => group.links).map((link) => (
                  <a href={link.href} key={link.href}>{link.label}</a>
                ))}
              </section>
            ))}
          </div>
        </details>
        <div className="editorial-actions">
          <EditorialNavigationItem action item={exploreItem} />
          <a className="editorial-primary" href="/login">Open Knot ↗</a>
        </div>
      </nav>
    </header>
  );
}

function EditorialNavigationItem({ action = false, item }: Readonly<{ action?: boolean; item: NavigationItem }>) {
  return (
    <div className={`editorial-nav-item${action ? " editorial-action-menu" : ""}`}>
      <button aria-haspopup="true" aria-label={item.label} type="button">
        {item.label}
        {action && <span className="editorial-disclosure" aria-hidden="true">⌄</span>}
      </button>
      <div className="editorial-mega">
        <div className="editorial-mega-content">
          {item.groups.map((group) => (
            <div className="editorial-mega-group" key={group.label}>
              <span>{group.label}</span>
              {group.links.map((link) => <a href={link.href} key={link.label}>{link.label}</a>)}
            </div>
          ))}
        </div>
      </div>
      <span className="editorial-nav-scrim" />
    </div>
  );
}
