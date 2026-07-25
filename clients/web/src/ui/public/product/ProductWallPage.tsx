"use client";

import { useState } from "react";
import { getPublicArticlePath } from "../PublicContentCatalog";
import { ProductBreadcrumb, ProductShell } from "./ProductShell";

type AudienceMoment = {
  audience: string;
  detail: string;
  id: string;
  label: string;
  record: string;
  result: string;
};

const audienceMoments: readonly AudienceMoment[] = [
  {
    audience: "12 active identities",
    detail: "The post is placed in the shared room as soon as the server accepts it.",
    id: "publish",
    label: "Published",
    record: "Body, author, room and route enter the shared record.",
    result: "Available to the active Wall audience",
  },
  {
    audience: "12 active + 3 new readers",
    detail: "Later arrivals can read the room history without being selected by the author.",
    id: "arrival",
    label: "New readers",
    record: "History remains available beyond the first visible audience.",
    result: "The audience grows after the post",
  },
  {
    audience: "4 reactions · 2 replies",
    detail: "Interaction makes the publication socially visible as well as technically retained.",
    id: "reaction",
    label: "Responses",
    record: "Reaction and reply records stay linked to the original post.",
    result: "Attention becomes attached metadata",
  },
  {
    audience: "Historical readers",
    detail: "Leaving the room changes a live presence state, not the publication’s past reach.",
    id: "history",
    label: "Retained",
    record: "History, routes and related actions remain reconstructable.",
    result: "The post outlives the room visit",
  },
];

const wallPosts = [
  ["mira", "14:02", "Anyone can read this after the room receives it.", "published"],
  ["otto", "14:04", "A reply does not restore an audience boundary.", "replied"],
  ["ivy", "14:06", "Forwarded posts make the original context travel further.", "forwarded"],
] as const;

const wallLedger = [
  ["14:02", "mira", "Publish", "Wall · available to active identities"],
  ["14:04", "otto", "Reply", "Relationship attached to mira’s post"],
  ["14:06", "ivy", "Forward", "Research · original context retained"],
  ["14:08", "guest-17", "React", "Observation becomes activity data"],
] as const;

export function ProductWallPage() {
  const [momentId, setMomentId] = useState(audienceMoments[0].id);
  const moment = audienceMoments.find((item) => item.id === momentId) ?? audienceMoments[0];

  return (
    <ProductShell current="the-wall">
      <section className="product-wall-board">
        <section className="product-wall-posts">
          <ProductBreadcrumb current="The Wall" />
          <header><p className="product-kicker">Shared conversation</p><h1>The Wall makes the audience explicit.</h1><p>Publishing here looks like a familiar chat action, but there is no selected recipient set behind it.</p></header>
          <article className="product-wall-pinned"><span>Pinned record</span><p>“This room has no private side.”</p><small>Published by system · visible to every active identity</small></article>
          <ol aria-label="Sample Wall posts">
            {wallPosts.map(([actor, time, body, action]) => <li key={`${actor}-${time}`}><span><b>{actor}</b><small>{time}</small></span><p>{body}</p><i>{action}</i></li>)}
          </ol>
        </section>
        <aside className="product-wall-audience-card"><p className="product-kicker">Current room state</p><b>12</b><span>active identities</span><dl><div><dt>Audience</dt><dd>Shared by default</dd></div><div><dt>History</dt><dd>Available to later readers</dd></div><div><dt>Wiretap</dt><dd>Included in the event stream</dd></div></dl></aside>
      </section>

      <section className="product-wall-rule"><p>Membership opens the room.</p><span>It does not select the audience.</span><p>Every post has a shared route.</p></section>

      <section aria-label="Audience horizon" className="product-wall-horizon">
        <header><p className="product-kicker">Audience horizon</p><h2>A public post keeps gaining context after the author has sent it.</h2></header>
        <div className="product-wall-horizon-control" role="group" aria-label="Publication timeline">
          {audienceMoments.map((item, index) => <button aria-pressed={moment.id === item.id} key={item.id} type="button" onClick={() => setMomentId(item.id)}><span>0{index + 1}</span><b>{item.label}</b></button>)}
        </div>
        <article aria-live="polite" className="product-wall-horizon-result"><div><span>Visible audience</span><b>{moment.audience}</b></div><div><span>What changes</span><p>{moment.detail}</p></div><div><span>Durable record</span><p>{moment.record}</p></div><footer>{moment.result}</footer></article>
      </section>

      <section className="product-wall-sequence"><aside><p className="product-kicker">Publishing is a choice</p><h2>Wall visibility is not accidental.</h2><p>The interface names a shared room. The technical record preserves the same publication as it travels through delivery and history.</p></aside><ol><li><span>01</span><div><h3>Publish</h3><p>A named author places text into the shared room.</p></div></li><li><span>02</span><div><h3>Available</h3><p>Any active identity can see it without an invitation.</p></div></li><li><span>03</span><div><h3>Retained</h3><p>Replies, reactions and routes compound the original record.</p></div></li></ol></section>

      <section className="product-wall-ledger"><header><p className="product-kicker">Wall activity ledger</p><h2>The public room becomes a structured record.</h2></header><ol>{wallLedger.map(([time, actor, action, detail]) => <li key={`${time}-${actor}`}><time>{time}</time><b>{actor}</b><span>{action}</span><p>{detail}</p></li>)}</ol></section>

      <section className="product-wall-end"><div><p className="product-kicker">Follow the consequence</p><h2>One shared post can be read in the room or traced across the system.</h2></div><div><a href="/login" className="product-cta">Open Knot ↗</a><a href={getPublicArticlePath("wiretap", "global-feed")}>Explore Global feed →</a></div></section>
    </ProductShell>
  );
}
