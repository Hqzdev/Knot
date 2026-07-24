"use client";

import { useKnot } from "@/ui/ApplicationProvider";

export function RoulettePane() {
  const { controller, state } = useKnot();
  return (
    <section className="roulette-pane">
      <div className="roulette-radar"><span /><span /><span /><i /></div>
      <p className="eyebrow">RANDOM EXPOSURE PROTOCOL</p>
      <h1>Roulette</h1>
      <p className="roulette-copy">
        Enter the queue. The server pairs you with a random waiting session and creates a permanent conversation.
        Self-matches are excluded. Regret is not.
      </p>
      <div className={`roulette-state ${state.roulette}`}>
        <span>{state.roulette === "waiting" ? "SCANNING PUBLIC SESSIONS" : state.roulette === "matched" ? "MATCH CAPTURED" : "QUEUE STANDING BY"}</span>
        <div><i /><i /><i /><i /><i /></div>
      </div>
      {state.roulette === "waiting"
        ? <button className="secondary-button" onClick={() => controller.leaveRoulette()}>Leave queue</button>
        : <button className="danger-button" onClick={() => controller.joinRoulette()}>Expose me to a stranger</button>}
      <dl className="roulette-facts">
        <div><dt>Match retention</dt><dd>Permanent</dd></div>
        <div><dt>Observer access</dt><dd>Global</dd></div>
        <div><dt>Identity control</dt><dd>None</dd></div>
      </dl>
    </section>
  );
}
