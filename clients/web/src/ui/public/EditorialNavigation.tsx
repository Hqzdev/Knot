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
  {
    label: "Wiretap",
    groups: [
      {
        label: "Explore Wiretap",
        links: [
          { label: "Global feed ↗", href: "/login" },
          { label: "Deleted originals", href: "#wiretap" },
          { label: "Live drafts", href: "#stories" },
        ],
      },
      {
        label: "Observation tools",
        links: [
          { label: "Route traces", href: "#product" },
          { label: "Active watchers", href: "#product" },
          { label: "Audit filters", href: "#wiretap" },
        ],
      },
    ],
  },
  {
    label: "Product",
    groups: [
      {
        label: "Explore Product",
        links: [
          { label: "Chats ↗", href: "/login" },
          { label: "The Wall", href: "/login" },
          { label: "Roulette", href: "/login" },
        ],
      },
      {
        label: "Resources",
        links: [
          { label: "Plaintext files", href: "#product" },
          { label: "Reactions", href: "#product" },
          { label: "Receipts", href: "#product" },
        ],
      },
    ],
  },
  {
    label: "How it leaks",
    groups: [
      {
        label: "Exposure model",
        links: [
          { label: "Plain HTTP and WS", href: "#product" },
          { label: "Open local storage", href: "#wiretap" },
          { label: "Permanent history", href: "#stories" },
        ],
      },
      {
        label: "Server access",
        links: [
          { label: "Public Wiretap", href: "#wiretap" },
          { label: "Live draft relay", href: "#stories" },
          { label: "Account impersonation", href: "/login" },
        ],
      },
    ],
  },
  {
    label: "Research",
    groups: [
      {
        label: "Explore Research",
        links: [
          { label: "Exposure Index", href: "#wiretap" },
          { label: "Watcher Studies", href: "#stories" },
          { label: "Retention Lab", href: "#product" },
        ],
      },
      {
        label: "Latest findings",
        links: [
          { label: "Privacy score 0/100", href: "#company" },
          { label: "Tombstone behavior", href: "#wiretap" },
          { label: "Draft visibility", href: "#stories" },
        ],
      },
    ],
  },
  {
    label: "Company",
    groups: [
      {
        label: "About Knot",
        links: [
          { label: "Company", href: "#company" },
          { label: "Unsecure charter", href: "#company" },
          { label: "Warnings", href: "#company" },
        ],
      },
      {
        label: "Access",
        links: [
          { label: "Password login", href: "/login" },
          { label: "Steal an account", href: "/login" },
          { label: "Enter as guest", href: "/login" },
        ],
      },
    ],
  },
];

const loginItem: NavigationItem = {
  label: "Log in",
  groups: [
    {
      label: "Choose an identity",
      links: [
        { label: "Password Login", href: "/login" },
        { label: "Steal an Account", href: "/login" },
        { label: "Enter as Guest", href: "/login" },
      ],
    },
    {
      label: "Before entering",
      links: [
        { label: "Accept the risk", href: "/login" },
        { label: "Read the warnings", href: "#company" },
        { label: "Privacy score 0/100", href: "#company" },
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
        <div className="editorial-actions">
          <EditorialNavigationItem item={loginItem} login />
          <a className="editorial-primary" href="/login">Open Knot ↗</a>
        </div>
      </nav>
    </header>
  );
}

function EditorialNavigationItem({ item, login = false }: Readonly<{ item: NavigationItem; login?: boolean }>) {
  return (
    <div className={`editorial-nav-item${login ? " editorial-login" : ""}`}>
      <button aria-haspopup="true" type="button">{item.label}{login ? " ⌄" : ""}</button>
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
