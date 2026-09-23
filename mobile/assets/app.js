const welcomeView = document.querySelector("#welcome-view");
const chatView = document.querySelector("#chat-view");
const identityForm = document.querySelector("#identity-form");
const displayNameInput = document.querySelector("#display-name");
const nameError = document.querySelector("#name-error");
const backButton = document.querySelector("#back-button");
const identityButton = document.querySelector("#identity-button");
const identityBadge = document.querySelector("#identity-badge");
const connectionStatus = document.querySelector("#connection-status");
const networkBanner = document.querySelector("#network-banner");
const messageList = document.querySelector("#message-list");
const quickPrompts = document.querySelector("#quick-prompts");
const composer = document.querySelector("#composer");
const messageInput = document.querySelector("#message-input");
const sendButton = document.querySelector("#send-button");

const roleLabels = {
  customer: "客户",
  service: "客服",
  admin: "管理员",
};

const state = {
  profile: null,
  sending: false,
  sessionId: getOrCreateSessionId(),
};

restoreLastProfile();

identityForm.addEventListener("submit", (event) => {
  event.preventDefault();
  const displayName = displayNameInput.value.trim();
  const roleInput = identityForm.querySelector('input[name="role"]:checked');

  if (!displayName) {
    nameError.textContent = "请输入你的称呼";
    displayNameInput.focus();
    return;
  }

  nameError.textContent = "";
  state.profile = {
    displayName,
    role: roleInput?.value || "customer",
  };
  localStorage.setItem("lab-chat-profile", JSON.stringify(state.profile));
  openChat();
});

displayNameInput.addEventListener("input", () => {
  if (displayNameInput.value.trim()) {
    nameError.textContent = "";
  }
});

backButton.addEventListener("click", openWelcome);
identityButton.addEventListener("click", openWelcome);

messageInput.addEventListener("input", () => {
  resizeComposer();
  updateSendButton();
});

messageInput.addEventListener("keydown", (event) => {
  if (event.key === "Enter" && !event.shiftKey && !event.isComposing) {
    event.preventDefault();
    composer.requestSubmit();
  }
});

composer.addEventListener("submit", async (event) => {
  event.preventDefault();
  await sendMessage(messageInput.value);
});

quickPrompts.addEventListener("click", async (event) => {
  const button = event.target.closest("button[data-prompt]");
  if (!button || state.sending) return;
  await sendMessage(button.dataset.prompt);
});

messageList.addEventListener("click", async (event) => {
  const button = event.target.closest("button[data-retry]");
  if (!button || state.sending) return;
  button.closest(".message-row")?.remove();
  await sendMessage(button.dataset.retry, false);
});

function openChat() {
  welcomeView.hidden = true;
  chatView.hidden = false;
  identityBadge.textContent = roleLabels[state.profile.role] || "访客";
  messageList.replaceChildren();
  renderDayDivider();
  appendMessage({
    type: "assistant",
    text: `${state.profile.displayName}，你好！我是智能客服。\n当前已进入${roleLabels[state.profile.role]}体验通道，可以发送一条消息确认服务连接。`,
  });
  checkHealth();
  requestAnimationFrame(() => messageInput.focus({ preventScroll: true }));
}

function openWelcome() {
  chatView.hidden = true;
  welcomeView.hidden = false;
  networkBanner.hidden = true;
  requestAnimationFrame(() => displayNameInput.focus({ preventScroll: true }));
}

async function sendMessage(rawText, appendUser = true) {
  const text = rawText.trim();
  if (!text || state.sending) return;

  state.sending = true;
  messageInput.value = "";
  resizeComposer();
  updateSendButton();
  quickPrompts.hidden = true;

  if (appendUser) {
    appendMessage({ type: "user", text });
  }
  const typing = appendTyping();

  const controller = new AbortController();
  const timeout = window.setTimeout(() => controller.abort(), 12000);
  const minimumLoading = new Promise((resolve) => window.setTimeout(resolve, 420));

  try {
    const response = await fetch("/chat", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        message: text,
        sessionId: state.sessionId,
        role: state.profile.role,
      }),
      signal: controller.signal,
    });
    await minimumLoading;

    const payload = await response.json().catch(() => null);
    if (!response.ok) {
      throw new Error(payload?.error?.message || "服务返回异常");
    }

    typing.remove();
    appendMessage({ type: "assistant", text: payload.answer || "已收到你的消息" });
    setConnectionState(true);
  } catch (error) {
    typing.remove();
    const reason = error.name === "AbortError" ? "请求超时，请稍后重试" : "消息发送失败，请检查网络后重试";
    appendError(reason, text);
    setConnectionState(false);
  } finally {
    window.clearTimeout(timeout);
    state.sending = false;
    updateSendButton();
    messageInput.focus({ preventScroll: true });
  }
}

async function checkHealth() {
  try {
    const response = await fetch("/health", { cache: "no-store" });
    const payload = await response.json();
    setConnectionState(response.ok && payload.status === "ok");
  } catch {
    setConnectionState(false);
  }
}

function setConnectionState(online) {
  connectionStatus.classList.toggle("offline", !online);
  connectionStatus.innerHTML = "";
  const dot = document.createElement("span");
  dot.className = "status-dot";
  connectionStatus.append(dot, document.createTextNode(online ? "服务在线" : "连接异常"));
  networkBanner.hidden = online;
}

function appendMessage({ type, text }) {
  const row = document.createElement("article");
  row.className = `message-row ${type}`;

  const avatar = document.createElement("div");
  avatar.className = "message-avatar";
  avatar.setAttribute("aria-hidden", "true");
  avatar.textContent = type === "user" ? shortName(state.profile?.displayName) : "AI";

  const stack = document.createElement("div");
  stack.className = "message-stack";

  const bubble = document.createElement("div");
  bubble.className = "message-bubble";
  bubble.textContent = text;

  const time = document.createElement("time");
  time.className = "message-time";
  time.dateTime = new Date().toISOString();
  time.textContent = formatTime(new Date());

  stack.append(bubble, time);
  row.append(avatar, stack);
  messageList.append(row);
  scrollToLatest();
  return row;
}

function appendTyping() {
  const row = document.createElement("article");
  row.className = "message-row assistant";
  row.setAttribute("aria-label", "智能客服正在输入");

  const avatar = document.createElement("div");
  avatar.className = "message-avatar";
  avatar.textContent = "AI";

  const stack = document.createElement("div");
  stack.className = "message-stack";
  const bubble = document.createElement("div");
  bubble.className = "message-bubble typing-bubble";

  for (let index = 0; index < 3; index += 1) {
    bubble.append(document.createElement("span"));
  }

  stack.append(bubble);
  row.append(avatar, stack);
  messageList.append(row);
  scrollToLatest();
  return row;
}

function appendError(message, retryText) {
  const row = appendMessage({ type: "assistant", text: "" });
  const bubble = row.querySelector(".message-bubble");
  bubble.classList.add("message-error");

  const copy = document.createElement("span");
  copy.textContent = message;
  const retry = document.createElement("button");
  retry.type = "button";
  retry.className = "retry-button";
  retry.dataset.retry = retryText;
  retry.textContent = "重新发送";
  bubble.replaceChildren(copy, retry);
}

function renderDayDivider() {
  const divider = document.createElement("div");
  divider.className = "day-divider";
  const label = document.createElement("span");
  label.textContent = "今天";
  divider.append(label);
  messageList.append(divider);
}

function resizeComposer() {
  messageInput.style.height = "auto";
  messageInput.style.height = `${Math.min(messageInput.scrollHeight, 112)}px`;
}

function updateSendButton() {
  sendButton.disabled = state.sending || !messageInput.value.trim();
}

function scrollToLatest() {
  requestAnimationFrame(() => {
    messageList.scrollTop = messageList.scrollHeight;
  });
}

function formatTime(date) {
  return new Intl.DateTimeFormat("zh-CN", {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(date);
}

function shortName(name = "用户") {
  return Array.from(name.trim()).slice(-2).join("") || "用户";
}

function getOrCreateSessionId() {
  const existing = localStorage.getItem("lab-chat-session-id");
  if (existing) return existing;

  const sessionId = globalThis.crypto?.randomUUID?.() || `session-${Date.now()}-${Math.random().toString(16).slice(2)}`;
  localStorage.setItem("lab-chat-session-id", sessionId);
  return sessionId;
}

function restoreLastProfile() {
  try {
    const profile = JSON.parse(localStorage.getItem("lab-chat-profile"));
    if (!profile?.displayName || !roleLabels[profile.role]) return;

    displayNameInput.value = profile.displayName;
    const roleInput = identityForm.querySelector(`input[name="role"][value="${profile.role}"]`);
    if (roleInput) roleInput.checked = true;
  } catch {
    localStorage.removeItem("lab-chat-profile");
  }
}
