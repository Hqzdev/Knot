"use client";

import { useKnot } from "@/ui/ApplicationProvider";
import { type FormEvent, useState } from "react";

export type AuthMode = "login" | "register";

interface AuthPageProps {
  initialMode: AuthMode;
}

interface PasswordFormProps {
  busy: boolean;
  mode: AuthMode;
  submit: (operation: () => Promise<void>) => Promise<void>;
}

interface ImpersonationFormProps {
  busy: boolean;
  submit: (operation: () => Promise<void>) => Promise<void>;
}

const copyByMode = {
  login: {
    eyebrow: "Account access",
    title: "Log in to your record.",
    description: "Enter the account credentials you created for this demonstration.",
    alternative: "New to Knot?",
    alternativeAction: "Create an account",
    alternativeHref: "/register",
  },
  register: {
    eyebrow: "New account",
    title: "Create a public account.",
    description: "Use disposable credentials. Everything after this point is visible to the service.",
    alternative: "Already have an account?",
    alternativeAction: "Log in",
    alternativeHref: "/login",
  },
} as const;

export function AuthPage({ initialMode }: Readonly<AuthPageProps>) {
  const { controller, state } = useKnot();
  const [busy, setBusy] = useState(false);

  if (!state.riskAccepted) {
    return <RiskDisclosure onAccept={() => controller.acceptRisk()} />;
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
    <main className={`auth-screen auth-screen--${initialMode}`}>
      <AuthHeader mode={initialMode} />
      {initialMode === "login" ? (
        <LoginLayout busy={busy} error={state.error} submit={execute} />
      ) : (
        <RegisterLayout busy={busy} error={state.error} submit={execute} />
      )}
      <footer className="auth-footer">Plaintext storage · Global audit · HTTP/WS · Research demonstration only</footer>
    </main>
  );
}

function RiskDisclosure({ onAccept }: Readonly<{ onAccept: () => void }>) {
  return (
    <main className="risk-screen">
      <section className="risk-panel" aria-labelledby="risk-title">
        <a className="wordmark" href="/">Knot <span>research demo</span></a>
        <p className="eyebrow">Required disclosure</p>
        <h1 id="risk-title">Before you enter.</h1>
        <p>Knot is an intentionally insecure research demonstrator. Messages, edits, reactions, deletions and drafts remain readable to the service and its global Wiretap.</p>
        <dl className="risk-metrics">
          <div><dt>Message privacy</dt><dd>None</dd></div>
          <div><dt>Server access</dt><dd>Unlimited</dd></div>
          <div><dt>Traffic</dt><dd>HTTP / WS</dd></div>
        </dl>
        <button className="primary-button" onClick={onAccept}>I understand, continue</button>
      </section>
    </main>
  );
}

function AuthHeader({ mode }: Readonly<{ mode: AuthMode }>) {
  const otherMode = mode === "login" ? "register" : "login";
  const otherLabel = mode === "login" ? "Create account" : "Log in";
  return (
    <header className="auth-header">
      <a className="wordmark" href="/">Knot <span>research demo</span></a>
      <nav className="auth-header-nav" aria-label="Account access">
        <span>{mode === "login" ? "Account access" : "New account"}</span>
        <a href={`/${otherMode}`}>{otherLabel}</a>
      </nav>
    </header>
  );
}

function LoginLayout({ busy, error, submit }: Readonly<{ busy: boolean; error?: string; submit: (operation: () => Promise<void>) => Promise<void> }>) {
  const { controller } = useKnot();
  const copy = copyByMode.login;
  return (
    <section className="auth-stage auth-stage--login" aria-labelledby="auth-title">
      <div className="auth-intro">
        <p className="eyebrow">{copy.eyebrow}</p>
        <h1 id="auth-title">{copy.title}</h1>
        <p>{copy.description}</p>
      </div>
      <div className="auth-grid auth-grid--login">
        <article className="auth-card auth-card-login">
          <div className="auth-card-header"><span>01</span><span>Password entry</span></div>
          {error && <div className="error-strip" role="alert">{error}</div>}
          <PasswordForm busy={busy} mode="login" submit={submit} />
          <div className="auth-divider"><span>or</span></div>
          <button className="secondary-button auth-guest-button" disabled={busy} onClick={() => void submit(() => controller.guest())}>Continue as guest</button>
          <details className="auth-alternate-access">
            <summary>Use an alternative access route</summary>
            <p>In this demonstration, entering a username can expose that account’s public history.</p>
            <ImpersonationForm busy={busy} submit={submit} />
          </details>
          <p className="auth-route-note">{copy.alternative} <a href={copy.alternativeHref}>{copy.alternativeAction} →</a></p>
        </article>
      </div>
    </section>
  );
}

function RegisterLayout({ busy, error, submit }: Readonly<{ busy: boolean; error?: string; submit: (operation: () => Promise<void>) => Promise<void> }>) {
  const copy = copyByMode.register;
  return (
    <section className="auth-stage auth-stage--register" aria-labelledby="auth-title">
      <div className="auth-grid auth-grid--register">
        <aside className="auth-card auth-card-register-note">
          <p className="eyebrow">Registration record</p>
          <h1 id="auth-title">{copy.title}</h1>
          <p>{copy.description}</p>
          <ol className="auth-steps">
            <li><span>01</span><div><strong>Name the account</strong><p>This identifier remains part of every room record.</p></div></li>
            <li><span>02</span><div><strong>Use a disposable email</strong><p>Never enter an address that belongs to you.</p></div></li>
            <li><span>03</span><div><strong>Enter knowingly</strong><p>Your account joins a visible system, not a private one.</p></div></li>
          </ol>
          <p className="auth-route-note">{copy.alternative} <a href={copy.alternativeHref}>{copy.alternativeAction} →</a></p>
        </aside>
        <article className="auth-card auth-card-register-form">
          <div className="auth-card-header"><span>01 / 03</span><span>Create record</span></div>
          <h2>Account details</h2>
          {error && <div className="error-strip" role="alert">{error}</div>}
          <PasswordForm busy={busy} mode="register" submit={submit} />
        </article>
      </div>
    </section>
  );
}

function PasswordForm({ busy, mode, submit }: Readonly<PasswordFormProps>) {
  const { controller } = useKnot();
  const [identifier, setIdentifier] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    void submit(() => mode === "login"
      ? controller.login(identifier, password)
      : controller.register(email, identifier, password));
  };
  const identifierLabel = mode === "login" ? "Username or email" : "Username";
  return (
    <form className="auth-form" onSubmit={handleSubmit}>
      <label htmlFor={`${mode}-identifier`}>{identifierLabel}
        <input autoComplete="username" id={`${mode}-identifier`} name="identifier" required value={identifier} onChange={(event) => setIdentifier(event.target.value)} />
      </label>
      {mode === "register" && <label htmlFor="register-email">Disposable email
        <input autoComplete="email" id="register-email" name="email" required type="email" value={email} onChange={(event) => setEmail(event.target.value)} />
      </label>}
      <label htmlFor={`${mode}-password`}>Disposable password
        <input autoComplete={mode === "login" ? "current-password" : "new-password"} id={`${mode}-password`} minLength={8} name="password" required type="password" value={password} onChange={(event) => setPassword(event.target.value)} />
      </label>
      {mode === "register" && password && <small className="password-meter">{passwordAssessment(password)}</small>}
      <button className="primary-button" disabled={busy}>{busy ? "Working…" : mode === "login" ? "Continue" : "Create account"}</button>
    </form>
  );
}

function ImpersonationForm({ busy, submit }: Readonly<ImpersonationFormProps>) {
  const { controller } = useKnot();
  const [username, setUsername] = useState("");
  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    void submit(() => controller.impersonate(username));
  };
  return (
    <form className="auth-form auth-impersonation-form" onSubmit={handleSubmit}>
      <label htmlFor="impersonated-username">Username to inspect
        <input autoComplete="off" id="impersonated-username" name="username" required value={username} onChange={(event) => setUsername(event.target.value)} />
      </label>
      <button className="secondary-button" disabled={busy}>Open exposed record</button>
    </form>
  );
}

function passwordAssessment(password: string): string {
  const repeated = /(.)\1{2,}/.test(password);
  const common = /password|qwerty|1234|letmein/i.test(password);
  const categories = [/[a-z]/, /[A-Z]/, /\d/, /[^\w]/].filter((pattern) => pattern.test(password)).length;
  const score = Math.min(4, Math.floor(password.length / 4) + categories - Number(repeated) - Number(common));
  const label = ["Very easy to guess", "Easy to reuse", "Suitable for a demo", "Reasonably distinct", "More than this demo needs"][Math.max(0, score)];
  return `Password note: ${label} · ${password.length} characters`;
}
