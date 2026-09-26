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

let viewportBaseline = window.innerHeight;

restoreLastProfile();
initializeViewportHandling();

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
  await sendMessage(messageInput.value, true, true);
});

quickPrompts.addEventListener("click", async (event) => {
  const button = event.target.closest("button[data-prompt]");
  if (!button || state.sending) return;
  await sendMessage(button.dataset.prompt, true, false);
});

messageList.addEventListener("click", async (event) => {
  const button = event.target.closest("button[data-retry]");
  if (!button || state.sending) return;
  const failedRow = button.closest(".message-row");
  button.disabled = true;
  button.textContent = "正在重试…";
  await sendMessage(button.dataset.retry, false, false);
  failedRow?.remove();
});

function openChat() {
  welcomeView.hidden = true;
  chatView.hidden = false;
  document.body.classList.add("chat-active");
  identityBadge.textContent = roleLabels[state.profile.role] || "访客";
  messageList.replaceChildren();
  renderDayDivider();
  renderEmptyState();
  quickPrompts.hidden = false;
  syncVisualViewport();
  checkHealth();
  if (window.matchMedia("(pointer: fine)").matches) {
    requestAnimationFrame(() => messageInput.focus({ preventScroll: true }));
  }
}

function openWelcome() {
  chatView.hidden = true;
  welcomeView.hidden = false;
  document.body.classList.remove("chat-active");
  chatView.classList.remove("keyboard-open");
  networkBanner.hidden = true;
  requestAnimationFrame(() => displayNameInput.focus({ preventScroll: true }));
}

async function sendMessage(rawText, appendUser = true, restoreInputFocus = true) {
  const text = rawText.trim();
  if (!text || state.sending) return;

  state.sending = true;
  composer.setAttribute("aria-busy", "true");
  messageInput.value = "";
  resizeComposer();
  updateSendButton();
  quickPrompts.hidden = true;
  messageList.querySelector(".chat-empty-state")?.remove();

  if (appendUser) {
    appendMessage({ type: "user", text });
  }
  const typing = appendTyping();

  const controller = new AbortController();
  const timeout = window.setTimeout(() => controller.abort(), 60000);
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
    appendMessage({
      type: "assistant",
      text: payload.answer || "已收到你的消息",
      citations: Array.isArray(payload.citations) ? payload.citations : [],
      order: payload.order || null,
    });
    setConnectionState(true);
  } catch (error) {
    typing.remove();
    const reason =
      error.name === "AbortError"
        ? "请求超时，请稍后重试"
        : error.message && error.message !== "Failed to fetch"
          ? error.message
          : "消息发送失败，请检查网络后重试";
    appendError(reason, text);
    setConnectionState(false);
  } finally {
    window.clearTimeout(timeout);
    state.sending = false;
    composer.setAttribute("aria-busy", "false");
    updateSendButton();
    if (restoreInputFocus) {
      messageInput.focus({ preventScroll: true });
    }
    scrollToLatest();
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

function appendMessage({ type, text, citations = [], order = null }) {
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
  const copy = document.createElement("span");
  copy.className = "message-copy";
  copy.textContent = text;
  bubble.append(copy);

  if (type === "assistant" && citations.length > 0) {
    bubble.append(createCitationDetails(citations));
  }

  if (type === "assistant" && order) {
    bubble.append(createOrderCard(order));
  }

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

function createOrderCard(order) {
  const found = order?.found === true;
  const card = document.createElement("section");
  card.className = `order-card${found ? "" : " not-found"}`;
  card.setAttribute("aria-label", found ? "Mock 订单进度" : "Mock 订单未找到");

  const header = document.createElement("div");
  header.className = "order-card-header";
  const heading = document.createElement("strong");
  heading.textContent = found ? "订单进度" : "未找到订单";
  const badge = document.createElement("span");
  badge.textContent = "Mock LIMS";
  header.append(heading, badge);

  const orderNumber = document.createElement("p");
  orderNumber.className = "order-number";
  orderNumber.textContent = String(order?.orderNumber || "未提供订单号");
  card.append(header, orderNumber);

  if (!found) {
    const note = document.createElement("p");
    note.className = "order-not-found-note";
    note.textContent = "请核对订单号后重新查询";
    card.append(note);
    return card;
  }

  const details = document.createElement("dl");
  details.className = "order-details";
  appendOrderField(details, "订单状态", order.status || "未知");
  appendOrderField(details, "报告状态", order.reportStatus || "未知");
  card.append(details);

  const progressValue = Math.min(100, Math.max(0, Number(order.progress) || 0));
  const progressHeader = document.createElement("div");
  progressHeader.className = "order-progress-header";
  const progressLabel = document.createElement("span");
  progressLabel.textContent = "检测进度";
  const progressText = document.createElement("strong");
  progressText.textContent = `${progressValue}%`;
  progressHeader.append(progressLabel, progressText);

  const progress = document.createElement("progress");
  progress.className = "order-progress";
  progress.max = 100;
  progress.value = progressValue;
  progress.setAttribute("aria-label", `检测进度 ${progressValue}%`);
  card.append(progressHeader, progress);
  return card;
}

function appendOrderField(list, label, value) {
  const group = document.createElement("div");
  const term = document.createElement("dt");
  term.textContent = label;
  const description = document.createElement("dd");
  description.textContent = String(value);
  group.append(term, description);
  list.append(group);
}

function createCitationDetails(citations) {
  const uniqueSources = [];
  const seen = new Set();
  citations.forEach((citation) => {
    const filename = String(citation?.originalName || "").trim();
    if (!filename) return;
    const key = String(citation?.documentId || filename);
    if (seen.has(key)) return;
    seen.add(key);
    uniqueSources.push(filename);
  });

  const details = document.createElement("details");
  details.className = "citation-details";
  const summary = document.createElement("summary");
  summary.textContent = `引用来源（${uniqueSources.length}）`;
  details.append(summary);

  const list = document.createElement("ul");
  list.className = "citation-list";
  uniqueSources.forEach((filename) => {
    const item = document.createElement("li");
    item.textContent = filename;
    list.append(item);
  });
  details.append(list);
  return details;
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

  const label = document.createElement("span");
  label.className = "typing-label";
  label.textContent = "正在查询，请稍候";
  const dots = document.createElement("span");
  dots.className = "typing-dots";

  for (let index = 0; index < 3; index += 1) {
    dots.append(document.createElement("span"));
  }

  bubble.append(label, dots);

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

function renderEmptyState() {
  const empty = document.createElement("section");
  empty.className = "chat-empty-state";
  empty.setAttribute("aria-label", "开始新对话");

  const mark = document.createElement("div");
  mark.className = "empty-state-mark";
  mark.textContent = "AI";
  const title = document.createElement("h2");
  title.textContent = `${state.profile.displayName}，想了解什么？`;
  const description = document.createElement("p");
  description.textContent = `当前为${roleLabels[state.profile.role]}通道，可以咨询检测知识或查询订单。`;
  empty.append(mark, title, description);
  messageList.append(empty);
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

function initializeViewportHandling() {
  window.addEventListener("resize", syncVisualViewport);
  window.visualViewport?.addEventListener("resize", syncVisualViewport);
  window.visualViewport?.addEventListener("scroll", syncVisualViewport);
  messageInput.addEventListener("focus", () => {
    window.setTimeout(() => {
      syncVisualViewport();
      scrollToLatest();
    }, 160);
  });
  messageInput.addEventListener("blur", () => {
    window.setTimeout(syncVisualViewport, 80);
  });
  syncVisualViewport();
}

function syncVisualViewport() {
  const viewportHeight = Math.round(window.visualViewport?.height || window.innerHeight);
  document.documentElement.style.setProperty("--app-height", `${viewportHeight}px`);

  const inputFocused = document.activeElement === messageInput;
  if (!inputFocused) {
    viewportBaseline = Math.max(viewportBaseline, viewportHeight);
  }
  const keyboardOpen = inputFocused && viewportBaseline - viewportHeight > 120;
  chatView.classList.toggle("keyboard-open", keyboardOpen);
  if (keyboardOpen && !chatView.hidden) {
    scrollToLatest();
  }
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
