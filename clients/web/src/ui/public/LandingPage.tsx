import { EditorialNavigation } from "./EditorialNavigation";

const news = [
  { image: "live-draft", title: "Drafts now disappear after thirty seconds. Screenshots do not.", meta: "Product · 4 min read" },
  { image: "roulette-match", title: "Roulette matches strangers and retains the room forever", meta: "Product · Jul 24, 2026" },
  { image: "wiretap-wall", title: "Why deleting a message creates more audit history", meta: "Research · 7 min read" },
  { image: "plaintext-archive", title: "The server has entered the conversation", meta: "Company · Jul 23, 2026" },
  { image: "wiretap-wall", title: "A practical taxonomy of people reading over your shoulder", meta: "Research · 9 min read" },
  { image: "live-draft", title: "Plaintext attachments: exactly the bytes you uploaded", meta: "Product · 5 min read" },
];

const stories = [
  { image: "wiretap-wall", title: "Building a global feed nobody asked to join", meta: "Wiretap · Jul 24, 2026" },
  { image: "live-draft", title: "Designing for thoughts that have not been sent yet", meta: "Drafts · Jul 24, 2026" },
  { image: "roulette-match", title: "Two strangers, one permanent conversation", meta: "Roulette · Jul 24, 2026" },
];

export function LandingPage() {
  return (
    <main className="landing">
      <EditorialNavigation />

      <section className="prompt-hero">
        <div>
          <h1>What can everyone help themselves to?</h1>
          <a className="prompt-box" href="/login">
            <span>Write a message the entire server can read</span>
            <b>↑</b>
          </a>
          <div className="prompt-chips">
            <a href="#wiretap">Read Wiretap</a>
            <a href="#product">Plaintext chats</a>
            <a href="#stories">Live drafts</a>
            <a href="#stories">Roulette</a>
            <a href="/login">More</a>
          </div>
        </div>
      </section>

      <section className="editorial-feature" id="product">
        <article className="feature-lead">
          <div className="editorial-image feature-image">
            <img alt="A bright archive receiving openly exposed message records" src="/editorial/plaintext-archive.webp" />
            <span>KNOT<br />OPEN</span>
            <b>0.0</b>
          </div>
          <h2>Every conversation,<br />scaled to an audience</h2>
          <p>Product <span>18 min read</span></p>
        </article>
        <div className="feature-side">
          <p className="feature-date">Product <span>Jul 24, 2026</span></p>
          <article>
            <img alt="An open catalog retaining every version of a message" src="/editorial/wiretap-wall.webp" />
            <h3>Wiretap keeps the version you hoped was gone</h3>
            <p>6 min read</p>
          </article>
          <article>
            <img alt="Message drafts displayed in a bright research installation" src="/editorial/live-draft.webp" />
            <h3>Read the message before it becomes a message</h3>
            <p>4 min read</p>
          </article>
        </div>
      </section>

      <section className="news-section" id="wiretap">
        <header><h2>Recent leaks</h2><a href="/login">View more</a></header>
        <div className="news-grid">
          {news.map((item, index) => (
            <article key={`${item.title}-${index}`}>
              <img alt="" src={`/editorial/${item.image}.webp`} />
              <div><h3>{item.title}</h3><p>{item.meta}</p></div>
            </article>
          ))}
        </div>
      </section>

      <section className="stories-section" id="stories">
        <header><h2>Stories</h2><a href="/login">View all</a></header>
        <div>
          {stories.map((story) => (
            <article key={story.title}>
              <img alt="" src={`/editorial/${story.image}.webp`} />
              <h3>{story.title}</h3>
              <p>{story.meta}</p>
            </article>
          ))}
        </div>
      </section>

      <section className="landing-cta">
        <h2>Get started with Knot Unsecure</h2>
        <a href="/login">Enter the archive</a>
      </section>

      <footer className="editorial-footer" id="company">
        <div><span>Product</span><a href="/login">Chats ↗</a><a href="/login">Wiretap ↗</a><a href="/login">The Wall ↗</a><a href="/login">Roulette ↗</a></div>
        <div><span>Exposure</span><a href="#wiretap">Plaintext history</a><a href="#stories">Public drafts</a><a href="#product">Public files</a><a href="#product">Route traces</a></div>
        <div><span>Company</span><a href="#product">About Knot</a><a href="#wiretap">News</a><a href="#stories">Stories</a><a href="/">Privacy score: 0/100</a></div>
        <div><span>Access</span><a href="/login">Password login</a><a href="/login">Steal an account</a><a href="/login">Enter as guest</a></div>
        <div><span>Terms & warnings</span><a href="/">Never use real passwords</a><a href="/">Never upload private files</a><a href="/">Server is listening</a></div>
      </footer>
    </main>
  );
}
