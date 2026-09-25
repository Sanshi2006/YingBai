const uploadForm = document.querySelector("#upload-form");
const uploadButton = document.querySelector("#upload-button");
const documentFile = document.querySelector("#document-file");
const fileLabel = document.querySelector("#file-label");
const filterForm = document.querySelector("#filter-form");
const resetFilter = document.querySelector("#reset-filter");
const documentList = document.querySelector("#document-list");
const totalBadge = document.querySelector("#total-badge");
const pageNotice = document.querySelector("#page-notice");
const previousPage = document.querySelector("#previous-page");
const nextPage = document.querySelector("#next-page");
const pageLabel = document.querySelector("#page-label");

const state = { page: 1, pageSize: 10, totalPages: 0, loading: false };

documentFile.addEventListener("change", () => {
  fileLabel.textContent = documentFile.files[0]?.name || "选择需要入库的文件";
});

uploadForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  if (state.loading || !documentFile.files[0]) return;
  setLoading(true, "正在提取、切片并向量化，请稍候…");
  try {
    const response = await fetch("/api/v1/documents", { method: "POST", body: new FormData(uploadForm) });
    const payload = await readJSON(response);
    if (!response.ok) throw new Error(payload?.error?.message || "文档上传失败");
    showNotice(`“${payload.document.originalName}”已完成入库。`, "success");
    uploadForm.reset();
    fileLabel.textContent = "选择需要入库的文件";
    state.page = 1;
    await loadDocuments();
  } catch (error) {
    showNotice(error.message || "文档上传失败，请稍后重试。", "error");
  } finally {
    setLoading(false);
  }
});

filterForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  state.page = 1;
  await loadDocuments();
});
resetFilter.addEventListener("click", async () => {
  filterForm.reset();
  state.page = 1;
  await loadDocuments();
});
previousPage.addEventListener("click", async () => {
  if (state.page <= 1 || state.loading) return;
  state.page -= 1;
  await loadDocuments();
});
nextPage.addEventListener("click", async () => {
  if (state.page >= state.totalPages || state.loading) return;
  state.page += 1;
  await loadDocuments();
});
documentList.addEventListener("click", async (event) => {
  const button = event.target.closest("button[data-action]");
  if (!button || state.loading) return;
  const { action, id, name } = button.dataset;
  if (action === "delete" && !window.confirm(`确定删除“${name}”吗？该操作会同时删除文本分块和向量。`)) return;
  await runDocumentAction(action, id, name);
});

async function loadDocuments() {
  setLoading(true);
  documentList.replaceChildren(createLoadingCard());
  const data = new FormData(filterForm);
  const params = new URLSearchParams({ page: String(state.page), pageSize: String(state.pageSize) });
  for (const [key, value] of data.entries()) {
    const normalized = String(value).trim();
    if (normalized) params.set(key, normalized);
  }
  try {
    const response = await fetch(`/api/v1/documents?${params}`);
    const payload = await readJSON(response);
    if (!response.ok) throw new Error(payload?.error?.message || "文档列表加载失败");
    state.totalPages = payload.totalPages || 0;
    if (state.totalPages > 0 && state.page > state.totalPages) {
      state.page = state.totalPages;
      await loadDocuments();
      return;
    }
    renderDocuments(payload.documents || []);
    totalBadge.textContent = `${payload.total || 0} 份`;
    pageLabel.textContent = state.totalPages > 0 ? `第 ${state.page} / ${state.totalPages} 页` : "暂无分页";
    previousPage.disabled = state.page <= 1;
    nextPage.disabled = state.totalPages === 0 || state.page >= state.totalPages;
  } catch (error) {
    documentList.replaceChildren(createEmptyCard(error.message || "文档列表加载失败"));
    showNotice(error.message || "文档列表加载失败", "error");
  } finally {
    setLoading(false);
  }
}

async function runDocumentAction(action, id, name) {
  const endpoints = {
    delete: { method: "DELETE", path: `/api/v1/documents/${encodeURIComponent(id)}` },
    retry: { method: "POST", path: `/api/v1/documents/${encodeURIComponent(id)}/retry` },
    revectorize: { method: "POST", path: `/api/v1/documents/${encodeURIComponent(id)}/revectorize` },
  };
  const request = endpoints[action];
  if (!request) return;
  const labels = { delete: "正在删除文档…", retry: "正在重新处理失败文档…", revectorize: "正在重新提取并生成向量…" };
  setLoading(true, labels[action]);
  try {
    const response = await fetch(request.path, { method: request.method });
    const payload = await readJSON(response);
    if (!response.ok) throw new Error(payload?.error?.message || "操作失败");
    const successText = action === "delete" ? `“${name}”已删除。` : `“${name}”已重新完成入库。`;
    showNotice(successText, "success");
    await loadDocuments();
  } catch (error) {
    showNotice(error.message || "操作失败，请稍后重试。", "error");
  } finally {
    setLoading(false);
  }
}

function renderDocuments(documents) {
  documentList.replaceChildren();
  if (!documents.length) {
    documentList.append(createEmptyCard("没有符合条件的文档"));
    return;
  }
  for (const document of documents) documentList.append(createDocumentCard(document));
}

function createDocumentCard(document) {
  const article = createElement("article", "document-card");
  const main = createElement("div", "document-main");
  const titleRow = createElement("div", "document-title-row");
  const title = createElement("h3", "", document.originalName);
  const status = createElement("span", `status-badge status-${document.status}`, document.status === "vectorized" ? "已入库" : "处理失败");
  titleRow.append(title, status);
  const metadata = createElement("div", "metadata-row");
  for (const value of [document.category, document.type, document.permission, document.format?.toUpperCase()]) {
    metadata.append(createElement("span", "metadata-pill", value));
  }
  const details = createElement("p", "document-details", `${formatBytes(document.size)} · ${document.chunkCount || 0} 个分块 · 更新于 ${formatTime(document.updatedAt || document.uploadedAt)}`);
  main.append(titleRow, metadata, details);
  if (document.processingError) main.append(createElement("p", "processing-error", document.processingError));
  const actions = createElement("div", "document-actions");
  if (document.status === "failed") actions.append(createActionButton("retry", document, "重试处理", "primary-action"));
  else actions.append(createActionButton("revectorize", document, "重新向量化", ""));
  actions.append(createActionButton("delete", document, "删除", "danger-action"));
  article.append(main, actions);
  return article;
}

function createActionButton(action, document, label, className) {
  const button = createElement("button", className, label);
  button.type = "button";
  button.dataset.action = action;
  button.dataset.id = document.id;
  button.dataset.name = document.originalName;
  return button;
}
function createLoadingCard() { return createElement("div", "empty-card", "正在加载文档…"); }
function createEmptyCard(message) { return createElement("div", "empty-card", message); }
function createElement(tag, className, text) {
  const element = document.createElement(tag);
  if (className) element.className = className;
  if (text !== undefined && text !== null) element.textContent = text;
  return element;
}
function showNotice(message, type) {
  pageNotice.textContent = message;
  pageNotice.className = `notice notice-${type}`;
  pageNotice.hidden = false;
}
function setLoading(loading, message = "") {
  state.loading = loading;
  uploadButton.disabled = loading;
  if (message) showNotice(message, "info");
}
async function readJSON(response) { return response.json().catch(() => null); }
function formatBytes(size) {
  if (!Number.isFinite(size)) return "未知大小";
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / 1024 / 1024).toFixed(1)} MB`;
}
function formatTime(value) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "未知时间";
  return new Intl.DateTimeFormat("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" }).format(date);
}

loadDocuments();
