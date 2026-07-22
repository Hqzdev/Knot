import { useEffect, useMemo, useState, useSyncExternalStore } from "react";
import type { FormEvent } from "react";
import type { KnotApplication } from "../application/KnotApplication";
import type { ApplicationState } from "../application/KnotApplication";
import { LocalIdentityUnavailableError } from "../application/ApplicationErrors";
import type { Group, LocalGroupMessage, LocalMessage, LocalProfile } from "../domain/contracts";
import { normalizeUsername } from "../domain/encoding";

interface AppProps {
  application: KnotApplication;
}

export function App({ application }: AppProps) {
  const state = useSyncExternalStore(application.subscribe, application.snapshot);

  useEffect(() => {
    void application.initialize();
  }, [application]);

  if (state.phase === "loading") {
    return <LoadingView />;
  }

  if (state.phase === "anonymous") {
    return <AuthView application={application} state={state} />;
  }

  return <MessengerView application={application} state={state} />;
}

function LoadingView() {
  return (
    <main className="loading-view">
      <Brand />
      <div className="loader" aria-label="Loading" />
      <p>Opening your encrypted vault</p>
    </main>
  );
}

function AuthView({ application, state }: AppProps & { state: ApplicationState }) {
  const [mode, setMode] = useState<"login" | "register" | "link">(
    state.deviceLink ? "link" : state.profiles.length ? "login" : "register",
  );
  const [username, setUsername] = useState(state.profiles[0]?.username ?? "");
  const [email, setEmail] = useState(state.profiles[0]?.email ?? "");
  const [password, setPassword] = useState("");
  const [deviceName, setDeviceName] = useState("Web browser");
  const [pendingRegistration, setPendingRegistration] = useState(false);
  const matchingProfiles = useMemo(
    () => state.profiles.filter((profile) =>
      normalizeUsername(profile.username) === normalizeUsername(username)
      || profile.email?.trim().toLowerCase() === username.trim().toLowerCase()
    ),
    [state.profiles, username],
  );
  const [deviceId, setDeviceId] = useState(matchingProfiles[0]?.device_id ?? "");
  const hasLocalProfile = matchingProfiles.length > 0;
  const showIdentityRecovery = mode === "login"
    && username.trim().length > 0
    && !hasLocalProfile
    && !pendingRegistration;

  useEffect(() => {
    if (!matchingProfiles.some((profile) => profile.device_id === deviceId)) {
      setDeviceId(matchingProfiles[0]?.device_id ?? "");
    }
  }, [deviceId, matchingProfiles]);

  const selectMode = (nextMode: "login" | "register" | "link") => {
    application.clearError();
    setPassword("");
    setPendingRegistration(false);
    setMode(nextMode);
  };

  const changeUsername = (value: string) => {
    application.clearError();
    setPassword("");
    setPendingRegistration(false);
    setUsername(value);
  };

  useEffect(() => {
    if (!state.deviceLink) {
      return;
    }
    const timer = window.setInterval(() => {
      void application.refreshDeviceLink().catch(() => undefined);
    }, 2_000);
    return () => window.clearInterval(timer);
  }, [application, state.deviceLink]);

  const submit = (event: FormEvent) => {
    event.preventDefault();
    const request = mode === "register"
      ? application.register(email, username, password, deviceName)
      : mode === "login"
        ? application.login(username, password, deviceId || undefined)
        : application.createDeviceLink(deviceName);
    void request.catch((error) => {
      if (error instanceof LocalIdentityUnavailableError) {
        application.clearError();
        setPassword("");
        setPendingRegistration(false);
      }
    });
  };

  return (
    <main className="auth-shell">
      <section className="auth-story">
        <Brand />
        <div className="story-copy">
          <p className="eyebrow">Private by construction</p>
          <h1>Conversation without an audience.</h1>
          <p className="story-text">
            Your keys stay in this browser. Messages leave it only after Rust encrypts them for every recipient device.
          </p>
        </div>
        <div className="security-line">
          <span className="pulse-dot" />
          X3DH · Double Ratchet · local encrypted vault
        </div>
      </section>
      <section className="auth-panel">
        <div className="auth-card">
          <div className="mode-switch three" aria-label="Authentication mode">
            <button className={mode === "login" ? "active" : ""} onClick={() => selectMode("login")} type="button">
              Sign in
            </button>
            <button className={mode === "register" ? "active" : ""} onClick={() => selectMode("register")} type="button">
              Create account
            </button>
            <button className={mode === "link" ? "active" : ""} onClick={() => selectMode("link")} type="button">
              Link device
            </button>
          </div>
          <div className="form-heading">
            <p className="eyebrow">Knot web</p>
            <h2>{mode === "login" ? "Welcome back" : mode === "register" ? "Create your first device" : "Link this browser"}</h2>
            <p>
              {mode === "login"
                ? "Only devices whose private keys live in this browser can be opened."
                : mode === "register"
                  ? "A new identity and prekey bundle will be generated locally."
                  : "Scan the QR code with a trusted iPhone or Mac. Your password never transfers encryption keys."}
            </p>
          </div>
          {mode === "link" && state.deviceLink ? (
            <div className="device-link-card">
              <img alt="Knot device-link QR code" src={state.deviceLink.qr_data_url} />
              <strong>{state.deviceLink.status === "approved" ? "Approved · finishing link" : "Waiting for approval"}</strong>
              <span>Expires {longTime(state.deviceLink.expires_at)}</span>
              <button className="secondary-button" onClick={() => void application.cancelDeviceLink()} type="button">Cancel</button>
              {state.error ? <ErrorBanner message={state.error} onDismiss={application.clearError.bind(application)} /> : null}
            </div>
          ) : <form onSubmit={submit}>
            {mode === "register" ? (
              <label>
                Email
                <input
                  autoComplete="email"
                  onChange={(event) => setEmail(event.target.value)}
                  required
                  type="email"
                  value={email}
                />
              </label>
            ) : null}
            <label>
              {mode === "login" ? "Email or username" : "Username"}
              <input
                autoComplete="username"
                minLength={mode === "login" ? 1 : 3}
                maxLength={32}
                onChange={(event) => changeUsername(event.target.value)}
                required={mode !== "link"}
                spellCheck={false}
                value={username}
              />
            </label>
            {mode === "register" || (mode === "login" && (hasLocalProfile || pendingRegistration)) ? <label>
              Password
              <input
                autoComplete={mode === "login" ? "current-password" : "new-password"}
                minLength={12}
                onChange={(event) => setPassword(event.target.value)}
                required
                type="password"
                value={password}
              />
            </label> : null}
            {mode === "register" || mode === "link" ? (
              <label>
                Device name
                <input
                  maxLength={80}
                  onChange={(event) => setDeviceName(event.target.value)}
                  required
                  value={deviceName}
                />
              </label>
            ) : mode === "login" && hasLocalProfile ? (
              <DeviceSelect profiles={matchingProfiles} value={deviceId} onChange={setDeviceId} />
            ) : null}
            {showIdentityRecovery ? (
              <LocalIdentityRecovery
                onLink={() => selectMode("link")}
                onPendingRegistration={() => setPendingRegistration(true)}
              />
            ) : null}
            {state.error ? <ErrorBanner message={state.error} onDismiss={application.clearError.bind(application)} /> : null}
            {mode !== "login" || hasLocalProfile || pendingRegistration ? (
              <button className="primary-button" disabled={Boolean(state.busy)} type="submit">
                {state.busy ?? (mode === "login" ? "Open Knot" : mode === "register" ? "Create encrypted account" : "Create QR code")}
              </button>
            ) : null}
          </form>}
        </div>
      </section>
    </main>
  );
}

function DeviceSelect({ profiles, value, onChange }: { profiles: LocalProfile[]; value: string; onChange: (value: string) => void }) {
  return (
    <label>
      Local device
      <select value={value} onChange={(event) => onChange(event.target.value)}>
        {profiles.map((profile) => (
          <option key={profile.device_id} value={profile.device_id}>
            {profile.device_name} · {profile.device_id.slice(0, 8)}
          </option>
        ))}
      </select>
    </label>
  );
}

function LocalIdentityRecovery({
  onLink,
  onPendingRegistration,
}: {
  onLink: () => void;
  onPendingRegistration: () => void;
}) {
  return (
    <div className="local-identity-recovery" data-testid="local-identity-recovery">
      <strong>This browser is not linked yet</strong>
      <span>
        Your password cannot restore end-to-end encryption keys. Link this browser from a trusted Knot device instead.
      </span>
      <button className="primary-button" onClick={onLink} type="button">Link this browser</button>
      <button className="secondary-button" onClick={onPendingRegistration} type="button">
        Finish interrupted registration
      </button>
    </div>
  );
}

function MessengerView({ application, state }: AppProps & { state: ApplicationState }) {
  const [recipient, setRecipient] = useState("");
  const [draft, setDraft] = useState("");
  const [devicesOpen, setDevicesOpen] = useState(false);
  const [createGroupOpen, setCreateGroupOpen] = useState(false);
  const [groupOpen, setGroupOpen] = useState(false);
  const [search, setSearch] = useState("");
  const [dark, setDark] = useState(() => window.matchMedia("(prefers-color-scheme: dark)").matches);
  const [mobilePanel, setMobilePanel] = useState<"chats" | "chat">("chats");
  const conversations = useMemo(() => conversationPeers(state.messages), [state.messages]);
  const activeGroup = state.groups.find((group) => group.id === state.activeGroupId) ?? null;
  const activeMessages = state.activePeer
    ? state.messages.filter((message) => normalizeUsername(message.peer_username) === normalizeUsername(state.activePeer ?? ""))
    : [];
  const activeGroupMessages = activeGroup
    ? state.groupMessages.filter((message) => message.group_id === activeGroup.id)
    : [];
  const activeChat = state.chats.find(
    (chat) => normalizeUsername(chat.peer_username) === normalizeUsername(state.activePeer ?? ""),
  ) ?? null;
  const activeOnline = activeChat?.peer_user_id ? state.presence[activeChat.peer_user_id] : false;
  const activeTyping = activeChat?.peer_user_id ? state.typing[activeChat.peer_user_id] : false;
  const activeIdentityWarnings = state.identityWarnings.filter(
    (warning) => normalizeUsername(warning.peer_username) === normalizeUsername(state.activePeer ?? ""),
  );
  const activeAttachmentTransfers = Object.values(state.attachmentTransfers).filter(
    (transfer) => transfer.direction === "upload"
      && transfer.state !== "ready"
      && normalizeUsername(transfer.recipient_username ?? "") === normalizeUsername(state.activePeer ?? ""),
  );

  useEffect(() => {
    document.documentElement.dataset.theme = dark ? "dark" : "light";
  }, [dark]);

  const openRecipient = (event: FormEvent) => {
    event.preventDefault();
    if (recipient.trim()) {
      application.openConversation(recipient.trim());
      setMobilePanel("chat");
      setRecipient("");
    }
  };

  const send = (event: FormEvent) => {
    event.preventDefault();
    if ((!state.activePeer && !activeGroup) || !draft.trim()) {
      return;
    }
    const body = draft;
    setDraft("");
    application.setTyping(false);
    const request = activeGroup
      ? application.sendGroup(activeGroup.id, body)
      : application.send(state.activePeer ?? "", body);
    void request.catch(() => setDraft(body));
  };

  return (
    <main className={`messenger-shell mobile-${mobilePanel}`}>
      <aside className="sidebar">
        <div className="sidebar-top">
          <Brand />
          <div className="sidebar-actions">
            <button className="icon-button" aria-label="Toggle theme" onClick={() => setDark((value) => !value)} type="button">
              {dark ? "☀" : "◐"}
            </button>
            <button className="icon-button" aria-label="Manage devices" onClick={() => setDevicesOpen(true)} type="button">
              <DeviceIcon />
            </button>
          </div>
        </div>
        <label className="chat-search">
          <span>⌕</span>
          <input aria-label="Search conversations" onChange={(event) => setSearch(event.target.value)} placeholder="Search" value={search} />
        </label>
        <form className="new-chat" onSubmit={openRecipient}>
          <input
            aria-label="Recipient username"
            onChange={(event) => setRecipient(event.target.value)}
            placeholder="Message a username"
            spellCheck={false}
            value={recipient}
          />
          <button aria-label="Open conversation" type="submit">+</button>
        </form>
        <button className="new-group-button" onClick={() => setCreateGroupOpen(true)} type="button">
          <GroupIcon />
          New encrypted group
        </button>
        <nav className="conversation-list" aria-label="Conversations">
          {state.groups.length ? (
            <div className="conversation-section">
              <p>Groups</p>
              {state.groups.map((group) => {
                const selected = group.id === activeGroup?.id;
                const lastMessage = latestGroupMessage(state.groupMessages, group.id);
                return (
                  <button className={selected ? "conversation selected" : "conversation"} key={group.id} onClick={() => {
                    application.openGroup(group.id);
                    setMobilePanel("chat");
                  }} type="button">
                    <Avatar username={`group-${group.id}`} label="#" />
                    <span className="conversation-copy">
                      <strong>{groupTitle(group, state.session?.username ?? "")}</strong>
                      <span>{lastMessage?.body ?? `${group.members.length} members · revision ${group.revision}`}</span>
                    </span>
                    <time>{lastMessage ? shortTime(lastMessage.created_at) : `r${group.revision}`}</time>
                  </button>
                );
              })}
            </div>
          ) : null}
          {conversations.length ? <p className="list-label">Direct messages</p> : null}
          {conversations.length ? conversations.filter((peer) =>
            normalizeUsername(peer.username).includes(normalizeUsername(search))
          ).map((peer) => {
            const selected = normalizeUsername(peer.username) === normalizeUsername(state.activePeer ?? "");
            const chat = state.chats.find((value) => normalizeUsername(value.peer_username) === normalizeUsername(peer.username));
            const online = chat?.peer_user_id ? state.presence[chat.peer_user_id] : false;
            return (
              <button className={selected ? "conversation selected" : "conversation"} key={peer.username} onClick={() => {
                application.openConversation(peer.username);
                setMobilePanel("chat");
              }} type="button">
                <span className="avatar-presence"><Avatar username={peer.username} />{online ? <i /> : null}</span>
                <span className="conversation-copy">
                  <strong>@{peer.username}</strong>
                  <span>{chat?.peer_user_id && state.typing[chat.peer_user_id] ? "typing…" : peer.lastMessage.body}</span>
                </span>
                <span className="conversation-meta">
                  <time>{shortTime(peer.lastMessage.created_at)}</time>
                  {chat?.unread_count ? <b>{chat.unread_count}</b> : null}
                </span>
              </button>
            );
          }) : state.groups.length === 0 ? (
            <div className="empty-list">
              <span>01</span>
              Start with a username. Knot discovers every active device before encrypting.
            </div>
          ) : null}
        </nav>
        <div className="account-strip">
          <Avatar username={state.session?.username ?? ""} />
          <div>
            <strong>@{state.session?.username}</strong>
            <span><i className={`status-dot ${state.connection}`} />{state.connection}</span>
          </div>
          <button onClick={() => void application.logout()} type="button">Sign out</button>
        </div>
      </aside>
      <section className="chat-panel">
        {state.activePeer || activeGroup ? (
          <>
            <header className="chat-header">
              <button className="mobile-back" aria-label="Back to chats" onClick={() => setMobilePanel("chats")} type="button">‹</button>
              <div>
                <Avatar
                  username={activeGroup ? `group-${activeGroup.id}` : state.activePeer ?? ""}
                  label={activeGroup ? "#" : undefined}
                />
                <div>
                  <h2>{activeGroup ? groupTitle(activeGroup, state.session?.username ?? "") : `@${state.activePeer}`}</h2>
                  <p>
                    {activeGroup
                      ? `${activeGroup.members.length} members · Sender Keys · revision ${activeGroup.revision}`
                      : activeTyping
                        ? "typing…"
                        : `${activeOnline ? "online" : "offline"} · End-to-end encrypted`}
                  </p>
                </div>
              </div>
              <div className="header-actions">
                {activeGroup ? (
                  <button className="sync-button" onClick={() => setGroupOpen(true)} type="button">Members</button>
                ) : null}
                <button className="sync-button" disabled={state.busy === "Syncing"} onClick={() => void application.sync().catch(() => undefined)} type="button">
                  {state.busy === "Syncing" ? "Syncing" : "Sync"}
                </button>
              </div>
            </header>
            {activeIdentityWarnings.length ? (
              <div className="identity-warning" role="alert">
                <div>
                  <strong>Security key changed</strong>
                  <span>Verify this device with @{state.activePeer} before sending sensitive information.</span>
                </div>
                {activeIdentityWarnings.map((warning) => (
                  <button key={warning.device_id} onClick={() => void application.confirmIdentity(warning.device_id)} type="button">
                    Trust {warning.device_id.slice(0, 8)}
                  </button>
                ))}
              </div>
            ) : null}
            <div className="message-list">
              {activeGroup ? (
                activeGroupMessages.length
                  ? activeGroupMessages.map((message) => <GroupMessageBubble key={message.id} message={message} />)
                  : <GroupConversationEmpty group={activeGroup} username={state.session?.username ?? ""} />
              ) : activeMessages.length ? activeMessages.map((message) => (
                <MessageBubble application={application} key={message.id} message={message} state={state} />
              )) : (
                <div className="conversation-empty">
                  <div className="lock-mark"><LockIcon /></div>
                  <h3>A private line to @{state.activePeer}</h3>
                  <p>The first message creates an independent X3DH session with every active device.</p>
                </div>
              )}
              {activeAttachmentTransfers.map((transfer) => (
                <PendingAttachmentTransfer application={application} key={transfer.id} transfer={transfer} />
              ))}
            </div>
            <form className="composer" onSubmit={send}>
              {!activeGroup ? (
                <label className="attachment-button" aria-label="Attach a file">
                  <span>＋</span>
                  <input
                    onChange={(event) => {
                      const file = event.target.files?.[0];
                      if (file && state.activePeer) {
                        void application.sendAttachment(state.activePeer, file).catch(() => undefined);
                      }
                      event.target.value = "";
                    }}
                    type="file"
                  />
                </label>
              ) : null}
              <textarea
                aria-label="Message"
                onBlur={() => application.setTyping(false)}
                onChange={(event) => {
                  setDraft(event.target.value);
                  application.setTyping(Boolean(event.target.value.trim()));
                }}
                onKeyDown={(event) => {
                  if (event.key === "Enter" && !event.shiftKey) {
                    event.preventDefault();
                    event.currentTarget.form?.requestSubmit();
                  }
                }}
                placeholder={activeGroup
                  ? `Encrypted message to ${groupTitle(activeGroup, state.session?.username ?? "")}`
                  : `Encrypted message to @${state.activePeer}`}
                rows={1}
                value={draft}
              />
              <button disabled={Boolean(state.busy) || !draft.trim()} type="submit">
                <SendIcon />
              </button>
            </form>
          </>
        ) : (
          <div className="select-conversation">
            <div className="knot-orbit"><span /><span /><span /></div>
            <p className="eyebrow">No conversation selected</p>
            <h2>Tie a new knot.</h2>
            <p>Enter a username in the sidebar to begin an encrypted conversation.</p>
          </div>
        )}
        {state.error ? <div className="floating-error"><ErrorBanner message={state.error} onDismiss={application.clearError.bind(application)} /></div> : null}
      </section>
      {devicesOpen ? <DeviceDrawer application={application} state={state} onClose={() => setDevicesOpen(false)} /> : null}
      {createGroupOpen ? <CreateGroupDrawer application={application} state={state} onClose={() => setCreateGroupOpen(false)} /> : null}
      {groupOpen && activeGroup ? <GroupDrawer application={application} state={state} group={activeGroup} onClose={() => setGroupOpen(false)} /> : null}
      <nav className="mobile-tabs" aria-label="Primary navigation">
        <button className={mobilePanel === "chats" ? "active" : ""} onClick={() => setMobilePanel("chats")} type="button">Chats</button>
        <button className={mobilePanel === "chat" ? "active" : ""} disabled={!state.activePeer && !activeGroup} onClick={() => setMobilePanel("chat")} type="button">Conversation</button>
        <button onClick={() => setDevicesOpen(true)} type="button">Devices</button>
      </nav>
    </main>
  );
}

function PendingAttachmentTransfer({
  application,
  transfer,
}: {
  application: KnotApplication;
  transfer: ApplicationState["attachmentTransfers"][string];
}) {
  const resumable = transfer.state === "paused" || transfer.state === "failed" || transfer.state === "cancelled";
  return (
    <article className="message outgoing pending-transfer" data-testid="pending-attachment">
      <div className="attachment-card">
        <div className="attachment-icon">↑</div>
        <div>
          <strong>{transfer.filename}</strong>
          <span>{transfer.state}{transfer.error ? ` · ${transfer.error}` : ""}</span>
        </div>
      </div>
      <div className="transfer-progress">
        <progress max={1} value={transfer.progress} />
        <span>{Math.round(transfer.progress * 100)}%</span>
        {transfer.state === "transferring" ? (
          <button onClick={() => application.cancelAttachment(transfer.id)} type="button">Cancel</button>
        ) : null}
        {resumable ? (
          <button onClick={() => void application.resumeAttachment(transfer.id).catch(() => undefined)} type="button">Resume</button>
        ) : null}
        {resumable ? (
          <button onClick={() => void application.discardAttachment(transfer.id)} type="button">Discard</button>
        ) : null}
      </div>
    </article>
  );
}

function CreateGroupDrawer({ application, state, onClose }: AppProps & { state: ApplicationState; onClose: () => void }) {
  const [members, setMembers] = useState("");
  const submit = (event: FormEvent) => {
    event.preventDefault();
    const usernames = splitUsernames(members);
    void application.createGroup(usernames).then(onClose).catch(() => undefined);
  };
  return (
    <div className="drawer-backdrop" onMouseDown={onClose} role="presentation">
      <aside className="device-drawer compact-drawer" onMouseDown={(event) => event.stopPropagation()}>
        <header>
          <div>
            <p className="eyebrow">Sender Keys</p>
            <h2>Create a group</h2>
          </div>
          <button className="close-button" onClick={onClose} type="button">×</button>
        </header>
        <form className="add-device" onSubmit={submit}>
          <p>Enter usernames separated by commas. A fresh signed Sender Key will be distributed through pairwise Double Ratchet sessions.</p>
          <label>
            Members
            <textarea
              autoFocus
              onChange={(event) => setMembers(event.target.value)}
              placeholder="alice, bob, carol"
              required
              rows={4}
              value={members}
            />
          </label>
          {state.error ? <ErrorBanner message={state.error} onDismiss={application.clearError.bind(application)} /> : null}
          <button className="primary-button" disabled={Boolean(state.busy) || splitUsernames(members).length === 0} type="submit">
            {state.busy === "Creating group" ? state.busy : "Create and distribute key"}
          </button>
        </form>
      </aside>
    </div>
  );
}

function GroupDrawer({ application, state, group, onClose }: AppProps & { state: ApplicationState; group: Group; onClose: () => void }) {
  const [members, setMembers] = useState("");
  const currentUsername = state.session?.username ?? "";
  const isOwner = normalizeUsername(group.owner_username) === normalizeUsername(currentUsername);
  const submit = (event: FormEvent) => {
    event.preventDefault();
    const usernames = splitUsernames(members);
    void application.addGroupMembers(group.id, usernames).then(() => setMembers("")).catch(() => undefined);
  };
  return (
    <div className="drawer-backdrop" onMouseDown={onClose} role="presentation">
      <aside className="device-drawer" onMouseDown={(event) => event.stopPropagation()}>
        <header>
          <div>
            <p className="eyebrow">Revision {group.revision}</p>
            <h2>{groupTitle(group, currentUsername)}</h2>
          </div>
          <button className="close-button" onClick={onClose} type="button">×</button>
        </header>
        {state.error ? <ErrorBanner message={state.error} onDismiss={application.clearError.bind(application)} /> : null}
        <div className="member-list">
          {group.members.map((member) => {
            const isCurrent = normalizeUsername(member.username) === normalizeUsername(currentUsername);
            const isGroupOwner = normalizeUsername(member.username) === normalizeUsername(group.owner_username);
            return (
              <article className="member" key={normalizeUsername(member.username)}>
                <Avatar username={member.username} />
                <div>
                  <strong>@{member.username}</strong>
                  <span>{isGroupOwner ? "Owner" : "Member"}{isCurrent ? " · you" : ""}</span>
                </div>
                <div className="member-actions">
                  {isOwner && !isCurrent ? (
                    <button onClick={() => void application.transferGroupOwnership(group.id, member.username).catch(() => undefined)} type="button">Make owner</button>
                  ) : null}
                  {(isOwner && !isGroupOwner) || (isCurrent && !isGroupOwner) ? (
                    <button className="danger-action" onClick={() => void application.removeGroupMember(group.id, member.username).catch(() => undefined)} type="button">
                      {isCurrent ? "Leave" : "Remove"}
                    </button>
                  ) : null}
                </div>
              </article>
            );
          })}
        </div>
        {isOwner ? (
          <form className="add-device" onSubmit={submit}>
            <h3>Add members</h3>
            <p>Membership changes rotate your Sender Key and distribute it only to the new exact device set.</p>
            <textarea onChange={(event) => setMembers(event.target.value)} placeholder="dave, erin" required rows={3} value={members} />
            <button className="primary-button" disabled={Boolean(state.busy) || splitUsernames(members).length === 0} type="submit">
              {state.busy === "Adding group members" ? state.busy : "Add and rotate key"}
            </button>
          </form>
        ) : null}
      </aside>
    </div>
  );
}

function GroupConversationEmpty({ group, username }: { group: Group; username: string }) {
  return (
    <div className="conversation-empty">
      <div className="lock-mark"><GroupIcon /></div>
      <h3>{groupTitle(group, username)}</h3>
      <p>Your device signs one group ciphertext and fans it out to every active member device.</p>
    </div>
  );
}

function DeviceDrawer({ application, state, onClose }: AppProps & { state: ApplicationState; onClose: () => void }) {
  const [deviceName, setDeviceName] = useState("Web browser");
  const submit = (event: FormEvent) => {
    event.preventDefault();
    void application.registerDevice(deviceName).then(() => setDeviceName("Web browser")).catch(() => undefined);
  };
  return (
    <div className="drawer-backdrop" onMouseDown={onClose} role="presentation">
      <aside className="device-drawer" onMouseDown={(event) => event.stopPropagation()}>
        <header>
          <div>
            <p className="eyebrow">Device security</p>
            <h2>Your devices</h2>
          </div>
          <button className="close-button" onClick={onClose} type="button">×</button>
        </header>
        <div className="device-list">
          {state.devices.map((device) => (
            <article className={device.revoked_at ? "device revoked" : "device"} key={device.id}>
              <DeviceIcon />
              <div>
                <strong>{device.name}</strong>
                <span>{device.platform} · {device.id.slice(0, 10)}</span>
                <small>{device.is_current ? "Current session" : device.revoked_at ? "Revoked" : "Active"}</small>
              </div>
              {!device.revoked_at ? (
                <button onClick={() => void application.revokeDevice(device.id).catch(() => undefined)} type="button">Revoke</button>
              ) : null}
            </article>
          ))}
        </div>
        <form className="add-device" onSubmit={submit}>
          <h3>Register another web device</h3>
          <p>The new private identity stays in this browser and appears as a local sign-in option.</p>
          <input maxLength={80} onChange={(event) => setDeviceName(event.target.value)} required value={deviceName} />
          <button className="primary-button" disabled={Boolean(state.busy)} type="submit">
            {state.busy === "Registering device" ? state.busy : "Generate device keys"}
          </button>
        </form>
      </aside>
    </div>
  );
}

function MessageBubble({
  application,
  message,
  state,
}: {
  application: KnotApplication;
  message: LocalMessage;
  state: ApplicationState;
}) {
  const [preview, setPreview] = useState<string | null>(null);
  const transfer = message.attachment
    ? state.attachmentTransfers[message.attachment.attachment_id]
    : undefined;

  useEffect(() => () => {
    if (preview) {
      URL.revokeObjectURL(preview);
    }
  }, [preview]);

  const download = () => {
    if (!message.attachment) {
      return;
    }
    void application.downloadAttachment(message.attachment).then((blob) => {
      setPreview((current) => {
        if (current) {
          URL.revokeObjectURL(current);
        }
        return URL.createObjectURL(blob);
      });
    }).catch(() => undefined);
  };

  return (
    <article className={`message ${message.direction}`}>
      {message.attachment ? (
        <div className="attachment-card">
          <div className="attachment-icon">↧</div>
          <div>
            <strong>{message.attachment.filename}</strong>
            <span>{formatBytes(message.attachment.plaintext_size)} · {message.attachment.media_type}</span>
          </div>
          <button onClick={download} type="button">{preview ? "Open" : "Download"}</button>
        </div>
      ) : <p>{message.body}</p>}
      {transfer && transfer.state !== "ready" ? (
        <div className="transfer-progress">
          <progress max={1} value={transfer.progress} />
          <span>{Math.round(transfer.progress * 100)}%</span>
          {transfer.error ? <span className="transfer-error">{transfer.error}</span> : null}
          {transfer.state === "transferring" ? (
            <button onClick={() => application.cancelAttachment(transfer.id)} type="button">Cancel</button>
          ) : null}
        </div>
      ) : null}
      {preview && message.attachment ? (
        <AttachmentPreview capability={message.attachment} url={preview} />
      ) : null}
      <footer>
        <time>{longTime(message.created_at)}</time>
        {message.direction === "outgoing" ? (
          <span className={`delivery-state ${message.delivery_state ?? "sent"}`}>
            {message.delivery_state === "sending" ? "⌛ sending" : message.delivery_state === "failed" ? "! failed" : "✓ sent"}
          </span>
        ) : null}
        {message.delivery_state === "failed" ? (
          <button className="retry-button" onClick={() => void application.retryMessage(message.id)} type="button">Retry</button>
        ) : null}
      </footer>
    </article>
  );
}

function AttachmentPreview({ capability, url }: { capability: NonNullable<LocalMessage["attachment"]>; url: string }) {
  if (capability.media_type.startsWith("image/")) {
    return <img className="attachment-preview" alt={capability.filename} src={url} />;
  }
  if (capability.media_type.startsWith("audio/")) {
    return <audio className="attachment-preview" controls src={url} />;
  }
  if (capability.media_type.startsWith("video/")) {
    return <video className="attachment-preview" controls src={url} />;
  }
  return <a className="download-link" download={capability.filename} href={url}>Save decrypted file</a>;
}

function GroupMessageBubble({ message }: { message: LocalGroupMessage }) {
  return (
    <article className={`message ${message.direction}`}>
      {message.direction === "incoming" ? <strong className="message-sender">@{message.sender_username}</strong> : null}
      <p>{message.body}</p>
      <footer>
        <time>{longTime(message.created_at)}</time>
        {message.direction === "outgoing" ? <span>{message.recipient_device_ids.length} device{message.recipient_device_ids.length === 1 ? "" : "s"}</span> : null}
        <span>r{message.revision}</span>
      </footer>
    </article>
  );
}

function ErrorBanner({ message, onDismiss }: { message: string; onDismiss: () => void }) {
  return (
    <div className="error-banner" role="alert">
      <span>{message}</span>
      <button aria-label="Dismiss error" onClick={onDismiss} type="button">×</button>
    </div>
  );
}

function Brand() {
  return (
    <div className="brand" aria-label="Knot">
      <span className="brand-mark"><i /><i /></span>
      <strong>Knot</strong>
    </div>
  );
}

function Avatar({ username, label }: { username: string; label?: string }) {
  const displayLabel = (label ?? username.trim().slice(0, 2).toUpperCase()) || "K";
  let seed = 0;
  for (const character of username) {
    seed = (seed + character.charCodeAt(0)) % 12;
  }
  return <span className={`avatar avatar-tone-${seed}`}>{displayLabel}</span>;
}

function DeviceIcon() {
  return <svg viewBox="0 0 24 24" aria-hidden="true"><rect x="5" y="3" width="14" height="18" rx="3" /><path d="M9 6h6M11 18h2" /></svg>;
}

function LockIcon() {
  return <svg viewBox="0 0 24 24" aria-hidden="true"><rect x="5" y="10" width="14" height="11" rx="3" /><path d="M8 10V7a4 4 0 0 1 8 0v3" /></svg>;
}

function SendIcon() {
  return <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m4 12 16-8-5 16-3-6-8-2Z" /><path d="m12 14 8-10" /></svg>;
}

function GroupIcon() {
  return <svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="8" cy="8" r="3" /><circle cx="17" cy="9" r="2.5" /><path d="M2.5 20c.5-4 2.4-6 5.5-6s5 2 5.5 6M13.5 15c3.9-.8 6.6.9 7.5 4.5" /></svg>;
}

function conversationPeers(messages: LocalMessage[]): Array<{ username: string; lastMessage: LocalMessage }> {
  const peers = new Map<string, { username: string; lastMessage: LocalMessage }>();
  for (const message of messages) {
    peers.set(normalizeUsername(message.peer_username), { username: message.peer_username, lastMessage: message });
  }
  return [...peers.values()].sort((left, right) => right.lastMessage.created_at.localeCompare(left.lastMessage.created_at));
}

function latestGroupMessage(messages: LocalGroupMessage[], groupId: string): LocalGroupMessage | null {
  return messages.filter((message) => message.group_id === groupId).at(-1) ?? null;
}

function groupTitle(group: Group, currentUsername: string): string {
  const peers = group.members
    .map((member) => member.username)
    .filter((username) => normalizeUsername(username) !== normalizeUsername(currentUsername));
  if (peers.length === 0) {
    return "Encrypted group";
  }
  const visible = peers.slice(0, 3).map((username) => `@${username}`).join(", ");
  return peers.length > 3 ? `${visible} +${peers.length - 3}` : visible;
}

function splitUsernames(value: string): string[] {
  return value
    .split(/[\n,]/u)
    .map((username) => username.trim())
    .filter(Boolean);
}

function shortTime(timestamp: string): string {
  return new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" }).format(new Date(timestamp));
}

function longTime(timestamp: string): string {
  return new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit", month: "short", day: "numeric" }).format(new Date(timestamp));
}

function formatBytes(value: number): string {
  if (value < 1_024) {
    return `${value} B`;
  }
  if (value < 1_048_576) {
    return `${(value / 1_024).toFixed(1)} KB`;
  }
  return `${(value / 1_048_576).toFixed(1)} MB`;
}
