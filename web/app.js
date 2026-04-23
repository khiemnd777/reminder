const statusEl = document.getElementById("status");
const eventsEl = document.getElementById("events");
const windowLabelEl = document.getElementById("windowLabel");
const googleConnectCardEl = document.getElementById("googleConnectCard");
const createModalEl = document.getElementById("createModal");
const appointmentFormEl = document.getElementById("appointmentForm");
const openCreateModalButtonEl = document.getElementById("openCreateModalButton");
const syncGoogleButtonEl = document.getElementById("syncGoogleButton");
const closeCreateModalButtonEl = document.getElementById("closeCreateModalButton");
const cancelCreateModalButtonEl = document.getElementById("cancelCreateModalButton");
const modalBackdropEl = document.getElementById("modalBackdrop");
const deleteConfirmModalEl = document.getElementById("deleteConfirmModal");
const deleteModalBackdropEl = document.getElementById("deleteModalBackdrop");
const closeDeleteModalButtonEl = document.getElementById("closeDeleteModalButton");
const cancelDeleteButtonEl = document.getElementById("cancelDeleteButton");
const confirmDeleteButtonEl = document.getElementById("confirmDeleteButton");
const deleteConfirmMessageEl = document.getElementById("deleteConfirmMessage");
const toggleFiltersButtonEl = document.getElementById("toggleFiltersButton");
const filterSummaryEl = document.getElementById("filterSummary");
const filtersSectionEl = document.getElementById("filtersSection");
const rangeFromFieldEl = document.getElementById("rangeFromField");
const rangeToFieldEl = document.getElementById("rangeToField");
const rangeFromEl = document.getElementById("rangeFrom");
const rangeToEl = document.getElementById("rangeTo");
const applyRangeButtonEl = document.getElementById("applyRangeButton");

let currentRange = buildDefaultRange();
let pendingDelete = null;
let renderedEvents = [];
let reminderTimerId = null;
let audioContext = null;
const deliveredReminderKeys = new Set();

const reminderLevels = [
  { key: "5m", minutes: 5, className: "event-reminder-5", label: "Starts in 5 minutes" },
  { key: "15m", minutes: 15, className: "event-reminder-15", label: "Starts in 15 minutes" },
  { key: "30m", minutes: 30, className: "event-reminder-30", label: "Starts in 30 minutes" },
];

document.getElementById("refreshButton").addEventListener("click", refreshDashboard);
syncGoogleButtonEl.addEventListener("click", syncGoogleToSystem);
appointmentFormEl.addEventListener("submit", createAppointment);
openCreateModalButtonEl.addEventListener("click", openCreateModal);
closeCreateModalButtonEl.addEventListener("click", closeCreateModal);
cancelCreateModalButtonEl.addEventListener("click", closeCreateModal);
modalBackdropEl.addEventListener("click", closeCreateModal);
deleteModalBackdropEl.addEventListener("click", closeDeleteModal);
closeDeleteModalButtonEl.addEventListener("click", closeDeleteModal);
cancelDeleteButtonEl.addEventListener("click", closeDeleteModal);
confirmDeleteButtonEl.addEventListener("click", confirmDeleteAppointment);
document.addEventListener("keydown", handleGlobalKeydown);
applyRangeButtonEl.addEventListener("click", applyCustomRange);
toggleFiltersButtonEl.addEventListener("click", toggleFiltersSection);
document.addEventListener("pointerdown", handleFirstInteraction, { passive: true });

initializeRangeControls();
startReminderMonitor();

async function refreshDashboard() {
  await loadConnections();
  await loadAppointments();
}

async function loadAppointments() {
  const from = currentRange.from;
  const to = currentRange.to;
  windowLabelEl.textContent = `${formatDate(from)} to ${formatDate(to)}`;
  statusEl.textContent = "Loading appointments...";

  try {
    const response = await fetch(`/appointments?from=${encodeURIComponent(from.toISOString())}&to=${encodeURIComponent(to.toISOString())}`);
    const data = await readJSON(response);

    if (!response.ok) {
      throw new Error(data.error || "Unable to load appointments");
    }

    const events = normalizeEvents(data);
    renderEvents(events);
    statusEl.textContent = events.length ? "" : "No appointments in this window.";
  } catch (error) {
    statusEl.textContent = error.message;
    eventsEl.innerHTML = "";
  }
}

async function createAppointment(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const raw = Object.fromEntries(new FormData(form).entries());
  const payload = {
    title: raw.title,
    startAt: new Date(raw.startAt).toISOString(),
  };
  if (raw.endAt) {
    payload.endAt = new Date(raw.endAt).toISOString();
  }

  statusEl.textContent = "Creating appointment...";
  try {
    const response = await fetch("/appointments", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    const data = await readJSON(response);

    if (!response.ok) {
      throw new Error(data.error || "Unable to create appointment");
    }

    form.reset();
    closeCreateModal();
    const events = normalizeEvents(data);
    statusEl.textContent = `Created on ${events.map((item) => item.source).join(", ")}.`;
    await refreshDashboard();
  } catch (error) {
    statusEl.textContent = error.message;
  }
}

async function deleteAppointment(eventID, source) {
  pendingDelete = { eventID, source };
  if (source === "system") {
    deleteConfirmMessageEl.textContent = "This action will permanently remove the appointment from the local system.";
  } else {
    deleteConfirmMessageEl.textContent = "This action will permanently remove the appointment from Google Calendar.";
  }
  openDeleteModal();
}

async function confirmDeleteAppointment() {
  if (!pendingDelete) {
    return;
  }

  statusEl.textContent = "Deleting appointment...";
  try {
    const response = await fetch(`/appointments/${encodeURIComponent(pendingDelete.eventID)}?source=${encodeURIComponent(pendingDelete.source || "google")}`, {
      method: "DELETE",
    });
    if (!response.ok) {
      const data = await readJSON(response);
      throw new Error(data.error || "Unable to delete appointment");
    }

    statusEl.textContent = "Appointment deleted.";
    closeDeleteModal();
    await refreshDashboard();
  } catch (error) {
    statusEl.textContent = error.message;
  }
}

async function loadConnections() {
  try {
    const response = await fetch("/connections");
    const data = await readJSON(response);

    if (!response.ok) {
      throw new Error(data.error || "Unable to load connection status");
    }

    const googleConnected = Boolean(data.google?.connected);
    toggleConnectionCard(googleConnectCardEl, googleConnected);
    syncGoogleButtonEl.disabled = !googleConnected;
  } catch {
    toggleConnectionCard(googleConnectCardEl, false);
    syncGoogleButtonEl.disabled = true;
  }
}

async function syncGoogleToSystem() {
  syncGoogleButtonEl.disabled = true;
  statusEl.textContent = "Syncing Google Calendar into the system...";

  const params = new URLSearchParams({
    source: "google",
    from: currentRange.from.toISOString(),
    to: currentRange.to.toISOString(),
  });

  try {
    const response = await fetch(`/appointments/sync?${params.toString()}`, {
      method: "POST",
    });
    const data = await readJSON(response);

    if (!response.ok) {
      throw new Error(data.error || "Unable to sync Google Calendar");
    }

    statusEl.textContent = `System sync complete: ${data.created || 0} created, ${data.updated || 0} updated.`;
    await loadAppointments();
  } catch (error) {
    statusEl.textContent = error.message;
  } finally {
    await loadConnections();
  }
}

async function readJSON(response) {
  const text = await response.text();
  if (!text) {
    return {};
  }

  try {
    return JSON.parse(text);
  } catch {
    return { error: text };
  }
}

function normalizeEvents(payload) {
  return Array.isArray(payload?.events) ? payload.events : [];
}

function toggleConnectionCard(element, connected) {
  element.classList.toggle("hidden", connected);
}

function renderEvents(events) {
  renderedEvents = events;
  eventsEl.innerHTML = "";
  if (!events.length) {
    return;
  }

  for (const event of events) {
    const item = document.createElement("li");
    item.className = "eventCard";
    item.dataset.eventId = event.id;

    const header = document.createElement("div");
    header.className = "eventCardHeader";

    const title = document.createElement("strong");
    title.textContent = event.title;
    header.appendChild(title);

    const actions = document.createElement("div");
    actions.className = "eventActions";
    const deleteButton = document.createElement("button");
    deleteButton.className = "ghost dangerButton";
    deleteButton.type = "button";
    deleteButton.setAttribute("aria-label", "Delete appointment");
    deleteButton.innerHTML = `
      <svg class="buttonIcon" viewBox="0 0 24 24" aria-hidden="true">
        <path d="M9.5 4.75h5a1 1 0 0 1 1 1V7h3.25a.75.75 0 0 1 0 1.5h-1.08l-.74 9.02A2 2 0 0 1 14.94 19h-5.88a2 2 0 0 1-1.99-1.48L6.33 8.5H5.25a.75.75 0 0 1 0-1.5H8.5V5.75a1 1 0 0 1 1-1Zm4.5 2.25v-.75H10V7h4ZM7.84 8.5l.72 8.77a.5.5 0 0 0 .5.48h5.88a.5.5 0 0 0 .5-.48l.72-8.77H7.84Zm2.66 2a.75.75 0 0 1 .75.75v3.5a.75.75 0 0 1-1.5 0v-3.5a.75.75 0 0 1 .75-.75Zm3 0a.75.75 0 0 1 .75.75v3.5a.75.75 0 0 1-1.5 0v-3.5a.75.75 0 0 1 .75-.75Z"></path>
      </svg>
      <span>Delete</span>
    `;
    deleteButton.addEventListener("click", () => deleteAppointment(event.id, event.source));
    actions.appendChild(deleteButton);
    header.appendChild(actions);
    item.appendChild(header);

    const meta = document.createElement("div");
    meta.className = "meta";
    meta.innerHTML = `
      <span class="tag">${escapeHTML(event.sourceLabel || event.source)}</span>
      <span>${formatDate(new Date(event.startAt))}</span>
      <span>${formatTime(new Date(event.startAt))} - ${formatTime(new Date(event.endAt))}</span>
    `;
    item.appendChild(meta);

    const source = document.createElement("p");
    source.className = "eventSource";
    source.textContent = `Source: ${event.sourceLabel || event.source}${event.sourceDetail ? `, ${event.sourceDetail}` : ""}`;
    item.appendChild(source);

    eventsEl.appendChild(item);
  }

  evaluateReminders();
}

function openCreateModal() {
  createModalEl.classList.remove("hidden");
  document.body.classList.add("modalOpen");
  requestAnimationFrame(() => {
    appointmentFormEl.elements.title.focus();
  });
}

function closeCreateModal() {
  createModalEl.classList.add("hidden");
  updateBodyModalState();
}

function openDeleteModal() {
  deleteConfirmModalEl.classList.remove("hidden");
  updateBodyModalState();
}

function closeDeleteModal() {
  deleteConfirmModalEl.classList.add("hidden");
  pendingDelete = null;
  updateBodyModalState();
}

function updateBodyModalState() {
  const hasOpenModal = !createModalEl.classList.contains("hidden") || !deleteConfirmModalEl.classList.contains("hidden");
  document.body.classList.toggle("modalOpen", hasOpenModal);
}

function handleGlobalKeydown(event) {
  if (event.key === "Escape" && !createModalEl.classList.contains("hidden")) {
    closeCreateModal();
    return;
  }
  if (event.key === "Escape" && !deleteConfirmModalEl.classList.contains("hidden")) {
    closeDeleteModal();
  }
}

function initializeRangeControls() {
  syncCustomInputsFromRange();
  updateFilterSummary();
}

function applyCustomRange() {
  if (!rangeFromEl.value || !rangeToEl.value) {
    statusEl.textContent = "Select both from and to dates.";
    return;
  }

  const from = new Date(`${rangeFromEl.value}T00:00:00`);
  const to = new Date(`${rangeToEl.value}T23:59:59`);
  if (!(to > from)) {
    statusEl.textContent = "Range end must be after range start.";
    return;
  }

  currentRange = { from, to };
  updateFilterSummary();
  refreshDashboard();
}

function buildDefaultRange() {
  const from = new Date();
  const to = new Date(from);
  to.setDate(to.getDate() + 30);

  return { from, to };
}

function syncCustomInputsFromRange() {
  rangeFromEl.value = toDateInputValue(currentRange.from);
  rangeToEl.value = toDateInputValue(currentRange.to);
}

function toggleFiltersSection() {
  const willOpen = filtersSectionEl.classList.contains("hidden");
  filtersSectionEl.classList.toggle("hidden", !willOpen);
  toggleFiltersButtonEl.setAttribute("aria-expanded", String(willOpen));
}

function updateFilterSummary() {
  filterSummaryEl.textContent = `${formatDate(currentRange.from)} to ${formatDate(currentRange.to)}`;
}

function toDateInputValue(value) {
  const offsetMs = value.getTimezoneOffset() * 60 * 1000;
  return new Date(value.getTime() - offsetMs).toISOString().slice(0, 10);
}

function formatDate(value) {
  return value.toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" });
}

function formatTime(value) {
  return value.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
}

function escapeHTML(value) {
  return value.replace(/[&<>"']/g, (char) => ({
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
    '"': "&quot;",
    "'": "&#39;",
  }[char]));
}

function startReminderMonitor() {
  if (reminderTimerId !== null) {
    return;
  }

  reminderTimerId = window.setInterval(() => {
    evaluateReminders();
  }, 1000);
}

function evaluateReminders() {
  if (!renderedEvents.length) {
    return;
  }

  const now = Date.now();
  for (const event of renderedEvents) {
    const eventEl = eventsEl.querySelector(`[data-event-id="${CSS.escape(event.id)}"]`);
    if (!eventEl) {
      continue;
    }

    clearReminderClasses(eventEl);
    const reminder = getReminderLevel(event, now);
    if (!reminder) {
      continue;
    }

    eventEl.classList.add(reminder.className);
    const reminderKey = `${event.id}:${reminder.key}`;
    if (!deliveredReminderKeys.has(reminderKey)) {
      if (playReminderTone(reminder)) {
        deliveredReminderKeys.add(reminderKey);
      }
    }
  }
}

function getReminderLevel(event, nowMs) {
  const startMs = new Date(event.startAt).getTime();
  const diffMinutes = (startMs - nowMs) / 60000;
  if (diffMinutes < 0) {
    return null;
  }

  if (diffMinutes <= 5) {
    return reminderLevels[0];
  }
  if (diffMinutes <= 15) {
    return reminderLevels[1];
  }
  if (diffMinutes <= 30) {
    return reminderLevels[2];
  }

  return null;
}

function clearReminderClasses(element) {
  for (const level of reminderLevels) {
    element.classList.remove(level.className);
  }
}

function ensureAudioContext() {
  if (!window.AudioContext && !window.webkitAudioContext) {
    return null;
  }
  if (!audioContext) {
    const AudioContextCtor = window.AudioContext || window.webkitAudioContext;
    audioContext = new AudioContextCtor();
  }
  if (audioContext.state === "suspended") {
    audioContext.resume().catch(() => {});
  }
  return audioContext;
}

function playReminderTone(level) {
  const context = ensureAudioContext();
  if (!context || context.state !== "running") {
    return false;
  }

  const config = getToneConfig(level.key);
  const startAt = context.currentTime;

  for (let index = 0; index < config.pulses; index += 1) {
    const oscillator = context.createOscillator();
    const gain = context.createGain();

    oscillator.type = "triangle";
    oscillator.frequency.setValueAtTime(config.frequency, startAt);

    gain.gain.setValueAtTime(0.0001, startAt);
    gain.gain.exponentialRampToValueAtTime(config.volume, startAt + 0.03 + index * config.spacing);
    gain.gain.exponentialRampToValueAtTime(0.0001, startAt + config.duration + index * config.spacing);

    oscillator.connect(gain);
    gain.connect(context.destination);
    oscillator.start(startAt + index * config.spacing);
    oscillator.stop(startAt + index * config.spacing + config.duration + 0.04);
  }

  return true;
}

function getToneConfig(levelKey) {
  switch (levelKey) {
    case "5m":
      return { frequency: 1046.5, pulses: 6, spacing: 0.24, volume: 0.24, duration: 0.34 };
    case "15m":
      return { frequency: 880, pulses: 4, spacing: 0.28, volume: 0.19, duration: 0.3 };
    case "30m":
    default:
      return { frequency: 698.46, pulses: 3, spacing: 0.34, volume: 0.14, duration: 0.28 };
  }
}

function handleFirstInteraction() {
  ensureAudioContext();
  evaluateReminders();
}

refreshDashboard();
