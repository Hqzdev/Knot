"use client";

import { useKnot } from "@/ui/ApplicationProvider";
import { type FormEvent, useState } from "react";

type PasswordMode = "login" | "register";

export function AuthPage() {
  const { controller, state } = useKnot();
  const [passwordMode, setPasswordMode] = useState<PasswordMode>("login");
  const [busy, setBusy] = useState(false);

  if (!state.riskAccepted) {
    return (
      <main className="risk-screen">
        <div className="scan-line" />
        <section className="risk-panel">
          <p className="eyebrow">MANDATORY DISCLOSURE / 00</p>
          <h1>This messenger is watching.</h1>
          <p>
            Every message, file, reaction, edit, deletion and live draft is exposed to the server and the global
            Wiretap feed. Traffic uses plain HTTP and WS. Never enter a real password or private information.
          </p>
          <dl className="risk-metrics">
            <div><dt>Privacy score</dt><dd>0/100</dd></div>
            <div><dt>Server access</dt><dd>Unlimited</dd></div>
            <div><dt>Account theft</dt><dd>One username</dd></div>
          </dl>
          <button className="danger-button" onClick={() => controller.acceptRisk()}>
            I understand. Let the server listen.
          </button>
        </section>
      </main>
    );
  }

  const execute = async (operation: () => Promise<void>) => {
    setBusy(true);
    try {
      await operation();
      location.href = "/app";
    } finally {
      setBusy(false);
    }
  };

  return (
    <main className="auth-screen">
      <header className="auth-header">
        <a className="wordmark" href="/">KNOT <span>UNSECURE</span></a>
        <div className="live-label"><i /> SERVER IS LISTENING</div>
      </header>
      <section className="auth-intro">
        <p className="eyebrow">ACCESS TERMINAL / NO CLEARANCE REQUIRED</p>
        <h1>Choose how to be observed.</h1>
        <p>All three doors lead to the same public Wiretap.</p>
      </section>
      {state.error && <div className="error-strip" role="alert">{state.error}</div>}
      <section className="auth-grid">
        <article className="auth-card">
          <div className="card-number">01</div>
          <h2>Password Login</h2>
          <p>Use a disposable password. It is hashed, but everything you say is not.</p>
          <div className="mode-switch">
            <button aria-pressed={passwordMode === "login"} onClick={() => setPasswordMode("login")}>Login</button>
            <button aria-pressed={passwordMode === "register"} onClick={() => setPasswordMode("register")}>Register</button>
          </div>
          <PasswordForm busy={busy} mode={passwordMode} submit={execute} />
        </article>
        <article className="auth-card auth-card-warning">
          <div className="card-number">02</div>
          <h2>Steal an Account</h2>
          <p>Enter any username. No password. Full history. Every action receives an IMPERSONATED badge.</p>
          <ImpersonationForm busy={busy} submit={execute} />
        </article>
        <article className="auth-card auth-card-guest">
          <div className="card-number">03</div>
          <h2>Enter as Guest</h2>
          <p>The server assigns a random identity. Lose this browser session and the account is gone.</p>
          <button className="primary-button" disabled={busy} onClick={() => void execute(() => controller.guest())}>
            Generate exposed identity
          </button>
        </article>
      </section>
      <footer className="auth-footer">PLAINTEXT STORAGE · GLOBAL AUDIT · HTTP/WS · ZERO EXPECTATION OF PRIVACY</footer>
    </main>
  );
}

function PasswordForm({ busy, mode, submit }: { busy: boolean; mode: PasswordMode; submit: (operation: () => Promise<void>) => Promise<void> }) {
  const { controller } = useKnot();
  const [identifier, setIdentifier] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const handle = (event: FormEvent) => {
    event.preventDefault();
    void submit(() => mode === "login"
      ? controller.login(identifier, password)
      : controller.register(email, identifier, password));
  };
  return (
    <form className="auth-form" onSubmit={handle}>
      <label>Username {mode === "login" && "or email"}<input required value={identifier} onChange={(event) => setIdentifier(event.target.value)} /></label>
      {mode === "register" && <label>Disposable email<input type="email" value={email} onChange={(event) => setEmail(event.target.value)} /></label>}
      <label>Disposable password<input minLength={8} required type="password" value={password} onChange={(event) => setPassword(event.target.value)} /></label>
      <button className="primary-button" disabled={busy}>{mode === "login" ? "Open my records" : "Create public account"}</button>
    </form>
  );
}

function ImpersonationForm({ busy, submit }: { busy: boolean; submit: (operation: () => Promise<void>) => Promise<void> }) {
  const { controller } = useKnot();
  const [username, setUsername] = useState("");
  const handle = (event: FormEvent) => {
    event.preventDefault();
    void submit(() => controller.impersonate(username));
  };
  return (
    <form className="auth-form" onSubmit={handle}>
      <label>Victim username<input required value={username} onChange={(event) => setUsername(event.target.value)} /></label>
      <button className="danger-button" disabled={busy}>Impersonate user</button>
    </form>
  );
}
