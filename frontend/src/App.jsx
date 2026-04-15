import { useEffect, useMemo, useRef, useState } from "react";

const TIMELINE_START_MINUTES = 7 * 60 + 30;
const TIMELINE_END_MINUTES = 20 * 60 + 30;
const TIMELINE_TOTAL_MINUTES = TIMELINE_END_MINUTES - TIMELINE_START_MINUTES;
const APPRENTICE_START_MINUTES = 8 * 60;
const APPRENTICE_END_MINUTES = 20 * 60;
const TIMELINE_MARKERS = [
  { label: "08:00", minutes: 8 * 60, kind: "target" },
  { label: "20:00", minutes: 20 * 60, kind: "target" }
];
const TIMELINE_HOURS = Array.from({ length: 13 }, (_, index) => {
  const hour = 8 + index;
  return {
    label: `${String(hour).padStart(2, "0")}:00`,
    minutes: hour * 60
  };
});
const MANAGED_STATUS_OPTIONS = [
  { value: "apprentice", label: "Alternant", emoji: "👨‍🎓" },
  { value: "student", label: "Étudiant", emoji: "🐣" },
  { value: "pisciner", label: "Piscineux", emoji: "🏊‍♂️" },
  { value: "staff", label: "Staff", emoji: "🛠️" },
  { value: "extern", label: "Externe", emoji: "🌍" }
];
const CALENDAR_DAY_TYPES = [
  { value: "company", label: "Jour entreprise" },
  { value: "on_site_school", label: "Jour école sur site" },
  { value: "off_site_school", label: "Jour école à distance" },
  { value: "holiday", label: "Jour férié" },
  { value: "weekend", label: "Week-end" }
];

function isAdminUser(user) {
  if (!user) {
    return false;
  }
  return user.is_admin ?? (user.is_staff || user.ft_is_staff);
}

function formatDuration(seconds, fallback) {
  if (typeof seconds !== "number" || Number.isNaN(seconds)) {
    return fallback || "0s";
  }
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const secs = seconds % 60;
  const parts = [];
  if (hours > 0) {
    parts.push(`${hours}h`);
  }
  if (minutes > 0 || hours > 0) {
    parts.push(`${minutes}m`);
  }
  parts.push(`${secs}s`);
  return parts.join(" ");
}

function formatCompactDuration(seconds, fallback) {
  return formatDuration(seconds, fallback).replace(/\s+/g, "");
}

function getBadgeDelayVariant(seconds) {
  if (typeof seconds !== "number" || Number.isNaN(seconds)) {
    return "neutral";
  }
  if (seconds > 10 * 60) {
    return "danger";
  }
  if (seconds > 60) {
    return "warning";
  }
  return "neutral";
}

function formatClockTime(value, withSeconds = false) {
  if (!value) {
    return "--:--";
  }
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "--:--";
  }
  return new Intl.DateTimeFormat("fr-FR", {
    hour: "2-digit",
    minute: "2-digit",
    ...(withSeconds ? { second: "2-digit" } : {})
  }).format(date);
}

function formatDayKey(value) {
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "";
  }
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

function formatMonthKey(value) {
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "";
  }
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}`;
}

function parseMonthKey(monthKey) {
  const [year, month] = String(monthKey).split("-").map(Number);
  if (!year || !month) {
    return null;
  }
  return new Date(year, month - 1, 1);
}

function parseDayKey(dayKey) {
  const [year, month, day] = String(dayKey).split("-").map(Number);
  if (!year || !month || !day) {
    return null;
  }
  return new Date(year, month - 1, day);
}

function shiftMonth(date, delta) {
  return new Date(date.getFullYear(), date.getMonth() + delta, 1);
}

function formatMonthLabel(value) {
  const date = value instanceof Date ? value : parseMonthKey(value);
  if (!(date instanceof Date) || Number.isNaN(date.getTime())) {
    return "";
  }
  return new Intl.DateTimeFormat("fr-FR", {
    month: "long",
    year: "numeric"
  }).format(date);
}

function formatLongDayLabel(value) {
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "";
  }
  return new Intl.DateTimeFormat("fr-FR", {
    weekday: "long",
    day: "numeric",
    month: "long",
    year: "numeric"
  }).format(date);
}

function formatStatusLabel(status) {
  switch (status) {
    case "apprentice":
      return "Alternant";
    case "pisciner":
      return "Piscineux";
    case "staff":
      return "Staff";
    case "extern":
      return "Externe";
    default:
      return "Étudiant";
  }
}

function getCalendarDayMeta(dayType, fallbackLabel = "") {
  const normalized = String(dayType || "").trim().toLowerCase();
  const match = CALENDAR_DAY_TYPES.find((item) => item.value === normalized);
  if (match) {
    return match;
  }
  return {
    value: normalized || "on_site_school",
    label: fallbackLabel || "Jour école sur site"
  };
}

function formatRequiredAttendanceHours(hours) {
  if (typeof hours !== "number" || Number.isNaN(hours)) {
    return "Non défini";
  }
  return `${new Intl.NumberFormat("fr-FR", {
    minimumFractionDigits: Number.isInteger(hours) ? 0 : 1,
    maximumFractionDigits: 2
  }).format(hours)}h`;
}

function getDetectedStatus(user) {
  const raw = String(user?.status_42 || "").trim().toLowerCase();
  return raw || "student";
}

function getEffectiveStatus(user) {
  const raw = String(user?.status || "").trim().toLowerCase();
  return raw || getDetectedStatus(user);
}

function getStatusOption(status) {
  return MANAGED_STATUS_OPTIONS.find((option) => option.value === status) || MANAGED_STATUS_OPTIONS[1];
}

function getUserFlags(user) {
  return {
    isBlacklisted: Boolean(user?.is_blacklisted),
    blacklistReason: String(user?.blacklist_reason || "").trim()
  };
}

function getUserStateBadges(user) {
  const flags = getUserFlags(user);
  const badges = [];
  if (flags.isBlacklisted) {
    badges.push({ key: "blacklisted", label: "BLACKLISTED", tone: "danger" });
  }
  return badges;
}

function formatLastBadgeAt(value) {
  if (!value) {
    return "Jamais";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "Jamais";
  }
  return new Intl.DateTimeFormat("fr-FR", {
    dateStyle: "medium",
    timeStyle: "short"
  }).format(date);
}

function getReportLineMessage(user, isLiveDay = false) {
  if (isLiveDay && !isReportLineSuccess(user)) {
    return "Day is still on going";
  }
  const status = String(user?.post_result || "").trim();
  const errorMessage = String(user?.error_message || "").trim();

  if (status === "Skipped because user is blacklisted") {
    return "Apprentice is blacklisted";
  }
  if (errorMessage !== "") {
    return errorMessage;
  }
  return status || "Post not attempted";
}

function isReportLineSuccess(user) {
  const status = String(user?.post_result || "").trim();
  return status === "Posted" || status === "AUTOPOST is off";
}

function getReportLineTimes(user) {
  const first = user?.first_access ? formatClockTime(user.first_access, true) : "00:00:00";
  const last = user?.last_access ? formatClockTime(user.last_access, true) : "00:00:00";
  return { first, last };
}

function sortReportUsers(users) {
  const apprentices = [...users].filter((user) => Boolean(user?.is_apprentice));
  const compareLogin = (left, right) => String(left?.login_42 || "").localeCompare(String(right?.login_42 || ""));
  const successes = apprentices.filter(isReportLineSuccess).sort(compareLogin);
  const failures = apprentices.filter((user) => !isReportLineSuccess(user)).sort(compareLogin);
  return { successes, failures };
}

function padCenter(value, width) {
  const text = String(value || "");
  if (text.length >= width) {
    return text;
  }
  const total = width - text.length;
  const left = Math.floor(total / 2);
  const right = total - left;
  return `${" ".repeat(left)}${text}${" ".repeat(right)}`;
}

function getReportLineTone(user, isLiveDay = false) {
  if (isReportLineSuccess(user)) {
    return "success";
  }
  return isLiveDay ? "warning" : "danger";
}

function buildReportLine(user, isLiveDay = false) {
  const success = isReportLineSuccess(user);
  const message = getReportLineMessage(user, isLiveDay);
  const { first, last } = getReportLineTimes(user);
  const emoji = success ? "✅" : isLiveDay ? "⏳" : "❌";
  const login = String(user?.login_42 || "").padEnd(8, " ");
  const duration = padCenter(`(${formatCompactDuration(user?.duration_seconds, user?.duration_human)})`, 10);
  return `${emoji} ${login}: ${first}-${last}  ${duration}  — ${message}`;
}

function readAdminUserFiltersFromURL() {
  const query = new URLSearchParams(window.location.search);
  const statuses = new Set(query.getAll("status").map((value) => value.trim().toLowerCase()).filter(Boolean));
  return {
    search: query.get("search") || "",
    date: query.get("date") || "",
    statusFilters:
      statuses.size > 0
        ? {
            apprentice: statuses.has("apprentice"),
            student: statuses.has("student"),
            pisciner: statuses.has("pisciner"),
            staff: statuses.has("staff"),
            extern: statuses.has("extern")
          }
        : {
            apprentice: true,
            student: false,
            pisciner: false,
            staff: false,
            extern: false
          }
  };
}

function buildPresenceMonthKeys(days) {
  const todayMonthKey = formatMonthKey(new Date());
  const sourceMonthKeys = days.map((day) => String(day.day || "").slice(0, 7)).filter(Boolean).sort();
  const firstMonthKey = sourceMonthKeys[0] || todayMonthKey;
  const lastDataMonthKey = sourceMonthKeys[sourceMonthKeys.length - 1] || todayMonthKey;
  const lastMonthKey = lastDataMonthKey > todayMonthKey ? lastDataMonthKey : todayMonthKey;
  const monthKeys = [];
  let cursor = parseMonthKey(firstMonthKey);

  while (cursor && formatMonthKey(cursor) <= lastMonthKey) {
    monthKeys.push(formatMonthKey(cursor));
    cursor = shiftMonth(cursor, 1);
  }

  return monthKeys;
}

function getTimelinePosition(value) {
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) {
    return 0;
  }
  const totalMinutes = date.getHours() * 60 + date.getMinutes() + date.getSeconds() / 60;
  const ratio = (totalMinutes - TIMELINE_START_MINUTES) / TIMELINE_TOTAL_MINUTES;
  return Math.min(100, Math.max(0, ratio * 100));
}

function getTimelineMinutes(value) {
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) {
    return 0;
  }
  return date.getHours() * 60 + date.getMinutes() + date.getSeconds() / 60;
}

function isWithinStudentTimeline(value) {
  const totalMinutes = getTimelineMinutes(value);
  return totalMinutes >= TIMELINE_START_MINUTES && totalMinutes <= TIMELINE_END_MINUTES;
}

function overlapsStudentTimeline(startValue, endValue) {
  const startMinutes = getTimelineMinutes(startValue);
  const endMinutes = getTimelineMinutes(endValue);
  return startMinutes <= TIMELINE_END_MINUTES && endMinutes >= TIMELINE_START_MINUTES;
}

function getTimelineOffset(ratio) {
  const safeRatio = Math.min(1, Math.max(0, ratio));
  return `calc(var(--timeline-side-padding) + (100% - (var(--timeline-side-padding) * 2)) * ${safeRatio})`;
}

function getTimelineWidth(percent) {
  const safePercent = Math.max(0, percent);
  return `calc((100% - (var(--timeline-side-padding) * 2)) * ${safePercent / 100})`;
}

function isBeforeApprenticeStart(value) {
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) {
    return false;
  }
  const totalMinutes = date.getHours() * 60 + date.getMinutes() + date.getSeconds() / 60;
  return totalMinutes < APPRENTICE_START_MINUTES;
}

function getAdjustedStartClockTime() {
  return "08:00:00";
}

function isAfterApprenticeEnd(value) {
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) {
    return false;
  }
  const totalMinutes = date.getHours() * 60 + date.getMinutes() + date.getSeconds() / 60;
  return totalMinutes > APPRENTICE_END_MINUTES;
}

function getAdjustedEndClockTime() {
  return "20:00:00";
}

function getTimelineSpan(startValue, endValue) {
  const start = getTimelinePosition(startValue);
  const end = getTimelinePosition(endValue);
  return {
    start,
    end,
    width: Math.max(end - start, 0)
  };
}

async function requestJSON(url, options = {}) {
  const response = await fetch(url, {
    credentials: "include",
    ...options,
    headers: {
      ...(options.body ? { "Content-Type": "application/json" } : {}),
      ...(options.headers || {})
    }
  });

  const text = await response.text();
  let json = null;
  if (text.trim() !== "") {
    try {
      json = JSON.parse(text);
    } catch {
      json = null;
    }
  }

  return { response, text, json };
}

const liveUpdateListeners = new Set();
let liveUpdateSocket = null;
let liveUpdateReconnectTimer = 0;

function ensureLiveUpdatesSocket() {
  if (liveUpdateSocket && (liveUpdateSocket.readyState === WebSocket.OPEN || liveUpdateSocket.readyState === WebSocket.CONNECTING)) {
    return;
  }

  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  const url = `${protocol}//${window.location.host}/api/live`;
  liveUpdateSocket = new WebSocket(url);

  liveUpdateSocket.onmessage = (event) => {
    try {
      const payload = JSON.parse(event.data);
      for (const listener of liveUpdateListeners) {
        listener(payload);
      }
    } catch {
      // Ignore malformed live events.
    }
  };

  liveUpdateSocket.onerror = () => {
    liveUpdateSocket?.close();
  };

  liveUpdateSocket.onclose = () => {
    liveUpdateSocket = null;
    if (liveUpdateReconnectTimer) {
      window.clearTimeout(liveUpdateReconnectTimer);
      liveUpdateReconnectTimer = 0;
    }
    if (liveUpdateListeners.size === 0) {
      return;
    }
    liveUpdateReconnectTimer = window.setTimeout(() => {
      ensureLiveUpdatesSocket();
    }, 2000);
  };
}

function subscribeToLiveUpdates(onEvent) {
  if (typeof onEvent !== "function") {
    return () => {};
  }
  liveUpdateListeners.add(onEvent);
  ensureLiveUpdatesSocket();

  return () => {
    liveUpdateListeners.delete(onEvent);
    if (liveUpdateListeners.size > 0) {
      return;
    }
    if (liveUpdateReconnectTimer) {
      window.clearTimeout(liveUpdateReconnectTimer);
      liveUpdateReconnectTimer = 0;
    }
    if (liveUpdateSocket && (liveUpdateSocket.readyState === WebSocket.OPEN || liveUpdateSocket.readyState === WebSocket.CONNECTING)) {
      liveUpdateSocket.close();
    }
  };
}

function mergeDaySummary(days, nextSummary) {
  const currentDays = Array.isArray(days) ? days : [];
  if (!nextSummary || !nextSummary.day) {
    return currentDays;
  }

  let found = false;
  const merged = currentDays.map((day) => {
    if (day?.day !== nextSummary.day) {
      return day;
    }
    found = true;
    return {
      ...day,
      ...nextSummary,
      loading: false,
      day_type: nextSummary.day_type || day.day_type,
      day_type_label: nextSummary.day_type_label || day.day_type_label,
      required_attendance_hours:
        nextSummary.required_attendance_hours ?? day.required_attendance_hours
    };
  });

  if (!found) {
    merged.push(nextSummary);
  }

  merged.sort((left, right) => String(right?.day || "").localeCompare(String(left?.day || "")));
  return merged;
}

function mergeLiveDetailPayload(payload, event) {
  if (!payload) {
    return payload;
  }
  if (event?.month_payload) {
    return event.month_payload;
  }
  if (!event?.day_summary) {
    return payload;
  }

  const nextPayload = {
    ...payload,
    days: mergeDaySummary(payload.days, event.day_summary)
  };

  if (event.last_badge_at) {
    nextPayload.last_badge_at = event.last_badge_at;
  }
  if (typeof event.last_badge_day_duration_seconds === "number") {
    nextPayload.last_badge_day_duration_seconds = event.last_badge_day_duration_seconds;
  }
  if (typeof event.last_badge_day_duration_human === "string" && event.last_badge_day_duration_human !== "") {
    nextPayload.last_badge_day_duration_human = event.last_badge_day_duration_human;
  }

  return nextPayload;
}

function shouldApplyLiveEvent(event, login, monthKey) {
  const eventLogin = String(event?.login || "").trim().toLowerCase();
  const targetLogin = String(login || "").trim().toLowerCase();
  const eventDay = String(event?.day || "").trim();
  const eventMonth = String(event?.month || "").trim();
  const targetMonth = String(monthKey || "").trim();
  return eventLogin !== "" && eventLogin === targetLogin && (
    eventMonth === targetMonth ||
    eventDay.startsWith(`${targetMonth}-`)
  );
}

function BadgeDelayChip({ seconds }) {
  const variant = getBadgeDelayVariant(seconds);
  const label =
    typeof seconds === "number" && !Number.isNaN(seconds)
      ? formatDuration(seconds, "0s")
      : "Aucun badge";

  return (
    <div className={`badge-delay-chip badge-delay-chip-${variant}`}>
      <strong>Delai badgeuse</strong>
      <span>{label}</span>
    </div>
  );
}

function UserAvatar({ user, className = "user-avatar" }) {
  const flags = getUserFlags(user);
  const avatarClassName = [className, flags.isBlacklisted ? "user-avatar-blacklisted" : ""].filter(Boolean).join(" ");

  return (
    <div className="user-avatar-shell">
      {user.photo_url ? (
        <img className={avatarClassName} src={user.photo_url} alt="" />
      ) : (
        <div className={`${avatarClassName} user-avatar-fallback`} aria-hidden>
          {String(user.login_42 || "").slice(0, 2).toUpperCase()}
        </div>
      )}
    </div>
  );
}

function UserStateBadges({ user }) {
  const badges = getUserStateBadges(user);
  if (badges.length === 0) {
    return null;
  }
  return (
    <span className="user-state-badges">
      {badges.map((badge) => (
        <span key={badge.key} className={`user-state-badge user-state-badge-${badge.tone}`}>
          {badge.label}
        </span>
      ))}
    </span>
  );
}

function AdminStatusField({ value, detectedValue, disabled, onChange }) {
  const [open, setOpen] = useState(false);
  const selectedOption =
    MANAGED_STATUS_OPTIONS.find((option) => option.value === value) || MANAGED_STATUS_OPTIONS[0];

  useEffect(() => {
    if (disabled) {
      setOpen(false);
    }
  }, [disabled]);

  return (
    <div className="admin-status-field">
      <button
        className={`admin-status-trigger${open ? " admin-status-trigger-open" : ""}`}
        type="button"
        disabled={disabled}
        onClick={() => setOpen((current) => !current)}
      >
        <span className="admin-status-trigger-value">
          <span aria-hidden>{selectedOption.emoji}</span>
          <span>{selectedOption.label}</span>
        </span>
        <span className="admin-status-trigger-chevron" aria-hidden>
          ▾
        </span>
      </button>
      {open ? (
        <div className="admin-status-menu" role="listbox" aria-label="Statut">
          {MANAGED_STATUS_OPTIONS.map((option) => {
            const isDetected = option.value === detectedValue;
            const isSelected = option.value === value;
            return (
              <button
                key={option.value}
                className={`admin-status-option${isSelected ? " admin-status-option-selected" : ""}`}
                type="button"
                onClick={() => {
                  setOpen(false);
                  onChange(option.value);
                }}
              >
                <span className="admin-status-option-main">
                  <span aria-hidden>{option.emoji}</span>
                  <span>{option.label}</span>
                </span>
                {isDetected ? <span className="admin-status-option-detected">42</span> : null}
              </button>
            );
          })}
        </div>
      ) : null}
    </div>
  );
}

function BlacklistActionButton({ blacklisted, disabled, onClick }) {
  return (
    <button
      className={`blacklist-action-button${blacklisted ? " blacklist-action-button-forgive" : ""}`}
      type="button"
      disabled={disabled}
      onClick={onClick}
    >
      <span className="blacklist-action-button__text">{blacklisted ? "Pardonner?" : "Blacklister"}</span>
      <span className="blacklist-action-button__icon" aria-hidden>
        <svg className="blacklist-action-button__svg" viewBox="0 0 512 512" xmlns="http://www.w3.org/2000/svg">
          <circle
            cx="256"
            cy="256"
            r="176"
            style={{ fill: "none", stroke: "#fff", strokeLinecap: "round", strokeLinejoin: "round", strokeWidth: 32 }}
          />
          <path
            d="M145 367L367 145"
            style={{ fill: "none", stroke: "#fff", strokeLinecap: "round", strokeLinejoin: "round", strokeWidth: 32 }}
          />
        </svg>
      </span>
    </button>
  );
}

function ConfirmationModal({
  open,
  title,
  children,
  confirmLabel,
  cancelLabel = "Annuler",
  tone = "danger",
  confirmDisabled = false,
  onConfirm,
  onClose
}) {
  if (!open) {
    return null;
  }

  return (
    <div className="modal-backdrop" role="presentation" onClick={onClose}>
      <div
        className="modal-card"
        role="dialog"
        aria-modal="true"
        aria-label={title}
        onClick={(event) => event.stopPropagation()}
      >
        <h2>{title}</h2>
        <div className="modal-copy">{children}</div>
        <div className="modal-actions">
          <button className="secondary-button" type="button" onClick={onClose}>
            {cancelLabel}
          </button>
          <button
            className={tone === "danger" ? "danger-button" : "primary-button"}
            type="button"
            disabled={confirmDisabled}
            onClick={onConfirm}
          >
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}

function LoginPage() {
  const nextTarget = useMemo(() => {
    const params = new URLSearchParams(window.location.search);
    return params.get("next") || "/";
  }, []);

  function handleLogin() {
    window.location.assign(`/auth/42/login?next=${encodeURIComponent(nextTarget)}`);
  }

  return (
    <main className="login-page">
      <div className="login-orb login-orb-one" aria-hidden />
      <div className="login-orb login-orb-two" aria-hidden />
      <div className="login-layout">
        <div className="logo-stack">
          <p className="logo-kicker">42 Nice</p>
          <h1>Watchdog</h1>
          <p>Suivi des badges et des heures d&apos;apprentissage.</p>
        </div>

        <section className="login-card" aria-label="Connexion">
          <span className="card-glow" aria-hidden />
          <div className="card-body">
            <div className="card-meta">
              <h2>Connexion</h2>
              <p className="card-subtitle">
                Bienvenue sur la plateforme Watchdog. Connectez-vous pour acceder a votre espace.
              </p>
            </div>

            <button className="oauth-button" type="button" onClick={handleLogin}>
              <img src="/icons/42.svg" alt="" aria-hidden />
              <span>Se connecter avec OAuth 42</span>
            </button>

            <div className="card-divider">
              <span>ou</span>
            </div>

            <div className="credential-placeholder">
              <p>
                Cette interface utilise l&apos;authentification centralisee configuree pour Watchdog.
              </p>
            </div>
          </div>
        </section>
      </div>
    </main>
  );
}

function KeyValue({ label, value }) {
  return (
    <div className="kv-row">
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

function SkeletonBlock({ className = "" }) {
  return <span className={`skeleton-block${className ? ` ${className}` : ""}`} aria-hidden />;
}

function CalendarDayLegend() {
  return (
    <div className="calendar-day-legend" aria-label="Légende du calendrier">
      {CALENDAR_DAY_TYPES.map((item) => (
        <div key={item.value} className="calendar-day-legend-item">
          <span className={`calendar-day-legend-swatch calendar-day-legend-swatch-${item.value}`} aria-hidden />
          <span>{item.label}</span>
        </div>
      ))}
    </div>
  );
}

function SessionLoadingSkeleton() {
  return (
    <main className="app-shell">
      <section className="panel loading-panel loading-panel-skeleton">
        <SkeletonBlock className="skeleton-line skeleton-line-title" />
        <SkeletonBlock className="skeleton-line skeleton-line-medium" />
        <div className="loading-panel-skeleton-grid">
          <SkeletonBlock className="skeleton-block-card" />
          <SkeletonBlock className="skeleton-block-card" />
          <SkeletonBlock className="skeleton-block-card" />
        </div>
      </section>
    </main>
  );
}

function UserListSkeleton({ count = 7 }) {
  return (
    <div className="user-list" aria-hidden>
      {Array.from({ length: count }, (_, index) => (
        <div key={`user-skeleton-${index}`} className="user-list-row user-list-row-skeleton">
          <SkeletonBlock className="skeleton-circle skeleton-avatar" />
          <div className="user-list-main">
            <SkeletonBlock className="skeleton-line skeleton-line-medium" />
            <SkeletonBlock className="skeleton-line skeleton-line-short" />
          </div>
          <div className="user-list-side">
            <SkeletonBlock className="skeleton-line skeleton-line-short" />
            <SkeletonBlock className="skeleton-line skeleton-line-medium" />
          </div>
        </div>
      ))}
    </div>
  );
}

function ReportDetailSkeleton({ lines = 5 }) {
  return (
    <div className="report-mail" aria-hidden>
      {Array.from({ length: lines }, (_, index) => (
        <SkeletonBlock
          key={`report-detail-skeleton-${index}`}
          className={`skeleton-line ${index % 3 === 0 ? "skeleton-line-long" : index % 3 === 1 ? "skeleton-line-medium" : "skeleton-line-short"}`}
        />
      ))}
    </div>
  );
}

function ReportsListSkeleton({ count = 4 }) {
  return (
    <div className="report-day-list" aria-hidden>
      {Array.from({ length: count }, (_, index) => (
        <section key={`report-skeleton-${index}`} className="report-day-card">
          <div className="report-day-toggle report-day-toggle-skeleton">
            <div className="report-day-main">
              <SkeletonBlock className="skeleton-line skeleton-line-medium" />
              <SkeletonBlock className="skeleton-line skeleton-line-short" />
            </div>
            <div className="report-day-stats">
              <SkeletonBlock className="skeleton-line skeleton-line-short" />
              <SkeletonBlock className="skeleton-line skeleton-line-short" />
            </div>
            <SkeletonBlock className="skeleton-line skeleton-line-tiny" />
          </div>
        </section>
      ))}
    </div>
  );
}

function PresenceCalendarSkeleton({ monthKey }) {
  const weekdayLabels = ["Lun", "Mar", "Mer", "Jeu", "Ven", "Sam", "Dim"];
  const activeMonthKey = monthKey || formatMonthKey(new Date());
  const activeMonthDate = parseMonthKey(activeMonthKey);
  const monthStart = activeMonthDate instanceof Date ? new Date(activeMonthDate.getFullYear(), activeMonthDate.getMonth(), 1) : null;
  const daysInMonth = monthStart ? new Date(monthStart.getFullYear(), monthStart.getMonth() + 1, 0).getDate() : 35;
  const firstWeekday = monthStart ? (monthStart.getDay() + 6) % 7 : 0;
  const cells = [];

  for (let index = 0; index < firstWeekday; index += 1) {
    cells.push(null);
  }
  for (let index = 1; index <= daysInMonth; index += 1) {
    cells.push(index);
  }

  return (
    <section className="presence-calendar-shell" aria-hidden>
      <div className="presence-calendar-toolbar">
        <button className="secondary-button" type="button" disabled>
          Mois précédent
        </button>
        <strong className="presence-calendar-label">{formatMonthLabel(activeMonthDate)}</strong>
        <button className="secondary-button" type="button" disabled>
          Mois suivant
        </button>
      </div>
      <CalendarDayLegend />
      <section className="presence-month-card presence-month-card-compact">
        <div className="presence-weekdays">
          {weekdayLabels.map((label) => (
            <span key={`${activeMonthKey}-weekday-skeleton-${label}`}>{label}</span>
          ))}
        </div>
        <div className="presence-day-grid presence-day-grid-compact">
          {cells.map((cell, index) =>
            cell ? (
              <div key={`${activeMonthKey}-day-skeleton-${index}`} className="presence-day-cell presence-day-cell-skeleton">
                <SkeletonBlock className="skeleton-line skeleton-line-tiny" />
                <SkeletonBlock className="skeleton-line skeleton-line-short" />
              </div>
            ) : (
              <div key={`${activeMonthKey}-day-empty-${index}`} className="presence-day-cell presence-day-empty" aria-hidden />
            )
          )}
        </div>
      </section>
    </section>
  );
}

function AdminUserDayDetailSkeleton({ dayKey }) {
  return (
    <>
      <section className="admin-day-summary-slot" aria-hidden>
        <div className="admin-day-heading">
          <h2>{formatLongDayLabel(dayKey)}</h2>
        </div>
        <div className="student-day-summary admin-day-summary-grid">
          {Array.from({ length: 6 }, (_, index) => (
            <div key={`day-summary-skeleton-${index}`} className="kv-row kv-row-skeleton">
              <SkeletonBlock className="skeleton-line skeleton-line-short" />
              <SkeletonBlock className="skeleton-line skeleton-line-medium" />
            </div>
          ))}
        </div>
      </section>
      <section className="admin-day-timeline-slot" aria-hidden>
        <div className="timeline-frame timeline-frame-skeleton">
          {Array.from({ length: 4 }, (_, index) => (
            <SkeletonBlock key={`timeline-skeleton-${index}`} className={`timeline-skeleton-bar timeline-skeleton-bar-${index + 1}`} />
          ))}
        </div>
      </section>
    </>
  );
}

function UserPresencePanelSkeleton({ selectedDayKey, selectedMonthKey, showAdminActions = false }) {
  const dayKey = selectedDayKey || formatDayKey(new Date());
  return (
    <section className="panel" aria-hidden>
      <section className="user-detail-dashboard">
        <div className="user-detail-main">
          <div className="user-detail-main-header">
            <div className="user-detail-hero">
              <SkeletonBlock className="skeleton-circle skeleton-avatar-large" />
              <div className="user-detail-meta">
                <SkeletonBlock className="skeleton-line skeleton-line-title" />
                <SkeletonBlock className="skeleton-status-field" />
                <SkeletonBlock className="skeleton-line skeleton-line-short" />
              </div>
            </div>
            <div className="user-detail-main-header-skeleton-actions">
              {showAdminActions ? <SkeletonBlock className="skeleton-action-button" /> : null}
              <div className="badge-delay-chip badge-delay-chip-skeleton">
                <SkeletonBlock className="skeleton-line skeleton-line-tiny" />
                <SkeletonBlock className="skeleton-line skeleton-line-short" />
              </div>
            </div>
          </div>
          <div className="warning-callout warning-callout-skeleton">
            <SkeletonBlock className="skeleton-line skeleton-line-short" />
            <SkeletonBlock className="skeleton-line skeleton-line-long" />
            <SkeletonBlock className="skeleton-line skeleton-line-medium" />
          </div>
        </div>
        <AdminUserDayDetailSkeleton dayKey={dayKey} />
        <div className="user-detail-calendar-column">
          <PresenceCalendarSkeleton monthKey={selectedMonthKey} />
        </div>
      </section>
    </section>
  );
}

function StudentDayTimeline({ badgeEvents, locationSessions, currentTime, showNowMarker = true, locationsLoading = false }) {
  const firstEvent = badgeEvents[0];
  const lastEvent = badgeEvents[badgeEvents.length - 1];
  const rangeStart = firstEvent ? getTimelinePosition(firstEvent.timestamp) : 0;
  const rangeEnd = lastEvent ? getTimelinePosition(lastEvent.timestamp) : 0;
  const apprenticeStart = ((APPRENTICE_START_MINUTES - TIMELINE_START_MINUTES) / TIMELINE_TOTAL_MINUTES) * 100;
  const apprenticeEnd = ((APPRENTICE_END_MINUTES - TIMELINE_START_MINUTES) / TIMELINE_TOTAL_MINUTES) * 100;
  const hasRange =
    badgeEvents.length > 1 &&
    firstEvent &&
    lastEvent &&
    new Date(firstEvent.timestamp).getTime() !== new Date(lastEvent.timestamp).getTime();
  const cutoffWidth = hasRange ? Math.max(Math.min(rangeEnd, apprenticeStart) - rangeStart, 0) : 0;
  const validRangeStart = hasRange ? Math.max(rangeStart, apprenticeStart) : 0;
  const validRangeEnd = hasRange ? Math.min(rangeEnd, apprenticeEnd) : 0;
  const validRangeWidth = hasRange ? Math.max(validRangeEnd - validRangeStart, 0) : 0;
  const trailingCutoffStart = hasRange ? Math.max(rangeStart, apprenticeEnd) : 0;
  const trailingCutoffWidth = hasRange ? Math.max(rangeEnd - trailingCutoffStart, 0) : 0;
  const visibleLocationSessions = locationSessions
    .map((session, index) => ({
      ...session,
      index,
      ...getTimelineSpan(session.begin_at, session.end_at)
    }))
    .filter((session) => session.width > 0);

  return (
    <div className="student-day-timeline">
      <div className="timeline-frame">
        {TIMELINE_HOURS.map((hour) => (
          <div
            key={hour.label}
            className="timeline-hour-guide"
            style={{ left: getTimelineOffset((hour.minutes - TIMELINE_START_MINUTES) / TIMELINE_TOTAL_MINUTES) }}
            aria-hidden
          />
        ))}
        {TIMELINE_MARKERS.map((marker) => (
          <div
            key={marker.label}
            className={`timeline-reference timeline-reference-${marker.kind} ${
              marker.minutes === 8 * 60
                ? "timeline-reference-edge-start"
                : marker.minutes === 20 * 60
                  ? "timeline-reference-edge-end"
                  : ""
            }`}
            style={{ left: getTimelineOffset((marker.minutes - TIMELINE_START_MINUTES) / TIMELINE_TOTAL_MINUTES) }}
          >
            <span>{marker.label}</span>
          </div>
        ))}
        {showNowMarker ? (
          <div className="timeline-reference timeline-reference-now" style={{ left: getTimelineOffset(getTimelinePosition(currentTime) / 100) }}>
            <span>Maintenant</span>
          </div>
        ) : null}
        <div className="timeline-lane timeline-lane-badges">
          <span className="timeline-lane-name">Badge</span>
          <div className="timeline-track" aria-hidden />
          {hasRange && cutoffWidth > 0 ? (
            <div
              className="timeline-range timeline-range-cutoff"
              style={{
                left: getTimelineOffset(rangeStart / 100),
                width: getTimelineWidth(cutoffWidth)
              }}
              aria-label={`Plage hors alternance de ${formatClockTime(firstEvent.timestamp, true)} a ${formatClockTime(
                new Date(firstEvent.timestamp).setHours(8, 0, 0, 0)
              )}`}
            />
          ) : null}
          {hasRange && validRangeWidth > 0 ? (
            <div
              className="timeline-range timeline-range-valid"
              style={{
                left: getTimelineOffset(validRangeStart / 100),
                width: getTimelineWidth(validRangeWidth)
              }}
              aria-label={`Plage badgee de ${formatClockTime(firstEvent.timestamp, true)} a ${formatClockTime(lastEvent.timestamp, true)}`}
            />
          ) : null}
          {hasRange && trailingCutoffWidth > 0 ? (
            <div
              className="timeline-range timeline-range-cutoff"
              style={{
                left: getTimelineOffset(trailingCutoffStart / 100),
                width: getTimelineWidth(trailingCutoffWidth)
              }}
              aria-label={`Plage hors alternance de ${formatClockTime(
                new Date(lastEvent.timestamp).setHours(20, 0, 0, 0)
              )} a ${formatClockTime(lastEvent.timestamp, true)}`}
            />
          ) : null}
          {badgeEvents.map((event, index) => (
            <button
              key={`${event.timestamp}-${index}`}
              className="timeline-event-cut"
              type="button"
              style={{ left: getTimelineOffset(getTimelinePosition(event.timestamp) / 100) }}
              title={`${formatClockTime(event.timestamp, true)} · ${event.door_name || "Porte inconnue"}`}
              aria-label={`Badge à ${formatClockTime(event.timestamp, true)} sur ${event.door_name || "porte inconnue"}`}
            />
          ))}
        </div>
        <div className="timeline-lane timeline-lane-logs">
          <span className="timeline-lane-name">
            {locationsLoading ? "Logtime (loading...)" : "Logtime"}
          </span>
          <div className="timeline-track" aria-hidden />
          {visibleLocationSessions.map((session) => (
            <div
              key={`${session.begin_at}-${session.end_at}-${session.index}`}
              className={`timeline-session ${
                session.ongoing
                  ? "timeline-session-ongoing"
                  : session.counted
                    ? "timeline-session-valid"
                    : "timeline-session-invalid"
              }`}
              style={{
                left: getTimelineOffset(session.start / 100),
                width: getTimelineWidth(session.width)
              }}
              title={`${formatClockTime(session.begin_at, true)} -> ${formatClockTime(session.end_at, true)} · ${
                session.host || "Host inconnu"
              }${session.ongoing ? " - ONGOING" : ""}`}
              aria-label={`Session log ${session.host || "host inconnu"} de ${formatClockTime(
                session.begin_at,
                true
              )} a ${formatClockTime(session.end_at, true)}${session.ongoing ? ", en cours" : ""}`}
            >
              <span className="timeline-session-host">
                {session.host || "Host inconnu"}
                {session.ongoing ? " - ONGOING" : ""}
              </span>
            </div>
          ))}
        </div>
      </div>
      <div className="timeline-labels" aria-hidden>
        {TIMELINE_HOURS.map((label) => (
          <span
            key={label.label}
            className={`timeline-label ${
              label.minutes === 8 * 60 || label.minutes === 20 * 60 ? "timeline-label-target" : ""
            }`}
            style={{ left: getTimelineOffset((label.minutes - TIMELINE_START_MINUTES) / TIMELINE_TOTAL_MINUTES) }}
          >
            {label.label}
          </span>
        ))}
      </div>
    </div>
  );
}

function Header({ user, badgeDelaySeconds, onLogout, subtitle, viewMode, onToggleView }) {
  const isAdmin = isAdminUser(user);
  const roleLabel = isAdmin
    ? viewMode === "student"
      ? "Admin connecte · Vue étudiant"
      : "Admin"
    : "Étudiant";

  return (
    <header className="topbar">
      <div>
        <p className="logo-kicker">42 Watchdog</p>
        <h1>{subtitle}</h1>
      </div>
      <div className="topbar-actions">
        <BadgeDelayChip seconds={badgeDelaySeconds} />
        <div className="user-chip">
          <strong>{user.ft_login}</strong>
          <span>{roleLabel}</span>
        </div>
        {isAdmin && typeof onToggleView === "function" ? (
          <button className="secondary-button" type="button" onClick={onToggleView}>
            {viewMode === "student" ? "Retour admin" : "Vue étudiant"}
          </button>
        ) : null}
        <button className="secondary-button" type="button" onClick={onLogout}>
          Deconnexion
        </button>
      </div>
    </header>
  );
}

function AdminHeader({ user, badgeDelaySeconds, onLogout, onToggleView, activeSection, onNavigate }) {
  const [menuOpen, setMenuOpen] = useState(false);
  const menuRef = useRef(null);
  const sections = [
    { key: "students", label: "Étudiants", href: "/" },
    { key: "reports", label: "Rapports", href: "/reports" }
  ];

  useEffect(() => {
    function handlePointerDown(event) {
      if (!menuRef.current || menuRef.current.contains(event.target)) {
        return;
      }
      setMenuOpen(false);
    }

    function handleEscape(event) {
      if (event.key === "Escape") {
        setMenuOpen(false);
      }
    }

    document.addEventListener("mousedown", handlePointerDown);
    document.addEventListener("keydown", handleEscape);
    return () => {
      document.removeEventListener("mousedown", handlePointerDown);
      document.removeEventListener("keydown", handleEscape);
    };
  }, []);

  return (
    <header className="admin-header">
      <div className="admin-header-bar">
        <div className="admin-header-brand">
          <span className="admin-header-kicker">Espace administration</span>
          <nav className="admin-section-nav" aria-label="Navigation administration">
            {sections.map((section) => (
              <button
                key={section.key}
                type="button"
                className={`admin-section-tab${activeSection === section.key ? " admin-section-tab-active" : ""}`}
                onClick={() => onNavigate(section.href)}
              >
                {section.label}
              </button>
            ))}
          </nav>
        </div>
        <div className="admin-header-actions">
          <BadgeDelayChip seconds={badgeDelaySeconds} />
          <div className="admin-header-profile" ref={menuRef}>
            <button
              type="button"
              className={`admin-profile-button${menuOpen ? " admin-profile-button-open" : ""}`}
              onClick={() => setMenuOpen((current) => !current)}
              aria-haspopup="menu"
              aria-expanded={menuOpen}
            >
              <UserAvatar user={{ login_42: user.ft_login, photo_url: user.ft_photo }} className="user-avatar admin-profile-avatar" />
              <span className="admin-profile-login">{user.ft_login}</span>
            </button>
            {menuOpen ? (
              <div className="admin-profile-menu" role="menu">
                <button
                  type="button"
                  className="admin-profile-menu-item"
                  onClick={() => {
                    setMenuOpen(false);
                    onToggleView();
                  }}
                >
                  Vue étudiant
                </button>
                <button
                  type="button"
                  className="admin-profile-menu-item admin-profile-menu-item-danger"
                  onClick={() => {
                    setMenuOpen(false);
                    onLogout();
                  }}
                >
                  Déconnexion
                </button>
              </div>
            ) : null}
          </div>
        </div>
      </div>
    </header>
  );
}

function StudentView({ user, badgeDelaySeconds, onLogout, onToggleView }) {
  const [state, setState] = useState({ loading: true, error: "", payload: null });
  const [selectedDayKey, setSelectedDayKey] = useState(() => formatDayKey(new Date()));
  const [selectedMonthKey, setSelectedMonthKey] = useState(() => formatMonthKey(new Date()));
  const monthRequestRef = useRef(0);
  const monthAbortRef = useRef(null);

  async function loadSelfDetail(options = {}) {
    const { monthKey = selectedMonthKey || formatMonthKey(new Date()), background = false } = options;
    monthAbortRef.current?.abort();
    const controller = new AbortController();
    monthAbortRef.current = controller;
    const requestId = monthRequestRef.current + 1;
    monthRequestRef.current = requestId;
    if (!background) {
      setState((current) => ({ ...current, loading: true, error: "" }));
    }
    try {
      const query = new URLSearchParams();
      query.set("month", monthKey);
      const { response, json, text } = await requestJSON(`/api/student/detail?${query.toString()}`, {
        signal: controller.signal
      });
      if (requestId !== monthRequestRef.current) {
        return;
      }
      if (!response.ok) {
        throw new Error((json && json.message) || text || "Unable to load your profile.");
      }
      setState({ loading: false, error: "", payload: json });
    } catch (loadError) {
      if (loadError instanceof Error && loadError.name === "AbortError") {
        return;
      }
      if (requestId !== monthRequestRef.current) {
        return;
      }
      setState((current) => ({
        loading: false,
        error: loadError instanceof Error ? loadError.message : String(loadError),
        payload: background ? current.payload : null
      }));
    }
  }

  useEffect(() => {
    void loadSelfDetail({ monthKey: selectedMonthKey });
  }, [selectedMonthKey]);

  useEffect(() => {
    return () => {
      monthAbortRef.current?.abort();
    };
  }, []);

  useEffect(() => {
    if (!state.payload) {
      return;
    }
    const todayKey = formatDayKey(new Date());
    const todayMonthKey = formatMonthKey(new Date());
    setSelectedDayKey((current) => {
      if (selectedMonthKey === todayMonthKey) {
        if (current && current.startsWith(`${selectedMonthKey}-`)) {
          return current;
        }
        return todayKey;
      }
      if (current && current.startsWith(`${selectedMonthKey}-`)) {
        return current;
      }
      return null;
    });
  }, [state.payload, selectedMonthKey]);

  useEffect(() => {
    return subscribeToLiveUpdates((event) => {
      if (!shouldApplyLiveEvent(event, user.ft_login, selectedMonthKey)) {
        return;
      }
      if (event?.month_payload || event?.day_summary) {
        setState((current) => ({
          ...current,
          payload: mergeLiveDetailPayload(current.payload, event)
        }));
      }
    });
  }, [selectedMonthKey, user.ft_login]);

  function handleChangeMonth(delta) {
    setSelectedMonthKey((current) => {
      const parsed = parseMonthKey(current || formatMonthKey(new Date()));
      if (!(parsed instanceof Date) || Number.isNaN(parsed.getTime())) {
        return current;
      }
      const nextMonthKey = formatMonthKey(shiftMonth(parsed, delta));
      const todayMonthKey = formatMonthKey(new Date());
      setSelectedDayKey(nextMonthKey === todayMonthKey ? formatDayKey(new Date()) : null);
      return nextMonthKey;
    });
  }

  return (
    <main className="app-shell detail-shell">
      <UserPresencePanel
        loading={state.loading}
        error={state.error}
        payload={state.payload}
        login={user.ft_login}
        badgeDelaySeconds={badgeDelaySeconds}
        selectedDayKey={selectedDayKey}
        selectedMonthKey={selectedMonthKey}
        onChangeMonth={handleChangeMonth}
        onSelectDay={setSelectedDayKey}
        dayEndpointBase="/api/student/me"
      />
    </main>
  );
}

function AdminUsersIndexView({ user, badgeDelaySeconds, onLogout, onToggleView, onNavigate }) {
  const initialFilters = readAdminUserFiltersFromURL();
  const [searchInput, setSearchInput] = useState(initialFilters.search);
  const [dateInput, setDateInput] = useState(initialFilters.date);
  const [statusFilters, setStatusFilters] = useState(initialFilters.statusFilters);
  const [users, setUsers] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const activeStatuses = useMemo(
    () => Object.entries(statusFilters).filter(([, checked]) => checked).map(([status]) => status),
    [statusFilters]
  );
  const activeStatusKey = activeStatuses.join(",");

  async function loadUsers(currentSearch = searchInput, currentStatuses = activeStatuses, currentDate = dateInput) {
    setLoading(true);
    setError("");
    try {
      const query = new URLSearchParams();
      const normalizedSearch = currentSearch.trim().toLowerCase();
      if (normalizedSearch !== "") {
        query.set("search", normalizedSearch);
      }
      const normalizedDate = currentDate.trim();
      if (normalizedDate !== "") {
        query.set("date", normalizedDate);
      }
      if (currentStatuses.length === 0) {
        query.append("status", "");
      } else {
        currentStatuses.forEach((status) => query.append("status", status));
      }
      const suffix = query.toString() ? `?${query.toString()}` : "";
      const { response, json, text } = await requestJSON(`/api/admin/users${suffix}`);
      if (!response.ok) {
        throw new Error((json && json.message) || text || "Unable to load users.");
      }
      setUsers(Array.isArray(json) ? json : []);
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : String(loadError));
      setUsers([]);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    const timer = window.setTimeout(() => {
      void loadUsers(searchInput, activeStatuses, dateInput);
    }, 180);
    return () => window.clearTimeout(timer);
  }, [searchInput, activeStatusKey, dateInput]);

  useEffect(() => {
    const query = new URLSearchParams();
    const normalizedSearch = searchInput.trim().toLowerCase();
    if (normalizedSearch !== "") {
      query.set("search", normalizedSearch);
    }
    const normalizedDate = dateInput.trim();
    if (normalizedDate !== "") {
      query.set("date", normalizedDate);
    }
    activeStatuses.forEach((status) => query.append("status", status));
    const nextURL = query.toString() ? `/?${query.toString()}` : "/";
    window.history.replaceState(window.history.state, "", nextURL);
  }, [searchInput, activeStatusKey, dateInput]);

  useEffect(() => {
    return subscribeToLiveUpdates((event) => {
      const eventLogin = String(event?.login || "").trim().toLowerCase();
      const todayKey = formatDayKey(new Date());
      if (!eventLogin) {
        return;
      }
      if (event?.type === "badge_received") {
        setUsers((current) =>
          current.map((item) =>
            String(item?.login_42 || "").trim().toLowerCase() !== eventLogin
              ? item
              : {
                  ...item,
                  ...(event.last_badge_at ? { last_badge_at: event.last_badge_at } : {}),
                  ...(typeof event.last_badge_day_duration_seconds === "number"
                    ? { last_badge_day_duration_seconds: event.last_badge_day_duration_seconds }
                    : {}),
                  ...(typeof event.last_badge_day_duration_human === "string" && event.last_badge_day_duration_human !== ""
                    ? { last_badge_day_duration_human: event.last_badge_day_duration_human }
                    : {})
                }
          )
        );
        return;
      }
      if (event?.type === "location_sessions_updated" && event?.day === todayKey) {
        setUsers((current) =>
          current.map((item) =>
            String(item?.login_42 || "").trim().toLowerCase() !== eventLogin
              ? item
              : {
                  ...item,
                  ...(typeof event.last_badge_day_duration_seconds === "number"
                    ? { last_badge_day_duration_seconds: event.last_badge_day_duration_seconds }
                    : {}),
                  ...(typeof event.last_badge_day_duration_human === "string" && event.last_badge_day_duration_human !== ""
                    ? { last_badge_day_duration_human: event.last_badge_day_duration_human }
                    : {})
                }
          )
        );
      }
    });
  }, [searchInput, activeStatusKey, dateInput]);

  function toggleStatusFilter(status) {
    setStatusFilters((current) => ({
      ...current,
      [status]: !current[status]
    }));
  }

  const statusTiles = [
    { key: "apprentice", label: "Alternant", emoji: "👨‍🎓" },
    { key: "student", label: "Étudiant", emoji: "👶" },
    { key: "pisciner", label: "Piscineux", emoji: "🏊‍♂️" },
    { key: "staff", label: "Staff", emoji: "🛠️" },
    { key: "extern", label: "Externe", emoji: "🌍" }
  ];

  return (
    <main className="app-shell">
      <AdminHeader
        user={user}
        badgeDelaySeconds={badgeDelaySeconds}
        onLogout={onLogout}
        onToggleView={onToggleView}
        activeSection="students"
        onNavigate={onNavigate}
      />

      <section className="panel">
        <div className="panel-header">
          <div>
            <h2>Étudiants suivis</h2>
            <p className="panel-subtitle">Liste globale, independante des jours, avec dernier passage connu.</p>
          </div>
        </div>
        <div className="admin-filters">
          <label className="field admin-search-field">
            <span>Recherche par login</span>
            <input
              value={searchInput}
              onChange={(event) => setSearchInput(event.target.value)}
              placeholder="heinz"
            />
          </label>
          <label className="field admin-date-field">
            <span>Jour</span>
            <input
              type="date"
              value={dateInput}
              onChange={(event) => setDateInput(event.target.value)}
            />
          </label>
          <div className="admin-filter-group">
            <div className="status-tile-group" role="group" aria-label="Filtre par statut">
              {statusTiles.map((statusTile) => (
                <button
                  key={statusTile.key}
                  type="button"
                  className={`status-tile${statusFilters[statusTile.key] ? " status-tile-active" : ""}`}
                  aria-pressed={statusFilters[statusTile.key]}
                  onClick={() => toggleStatusFilter(statusTile.key)}
                >
                  <span className="status-tile-emoji" aria-hidden>
                    {statusTile.emoji}
                  </span>
                  <span className="status-tile-label">{statusTile.label}</span>
                </button>
              ))}
            </div>
          </div>
        </div>

        {loading ? (
          <UserListSkeleton />
        ) : error ? (
          <p className="feedback feedback-error">{error}</p>
        ) : users.length === 0 ? (
          <p className="feedback">Aucun utilisateur pour ces filtres.</p>
        ) : (
          <div className="user-list">
            {users.map((currentUser) => (
              <button
                key={currentUser.login_42}
                className="user-list-row"
                type="button"
                onClick={() => onNavigate(`/admin/${encodeURIComponent(currentUser.login_42)}`)}
              >
                <UserAvatar user={currentUser} />
                <div className="user-list-main">
                  <strong>
                    {currentUser.login_42} <UserStateBadges user={currentUser} />
                  </strong>
                  <span>{formatStatusLabel(getEffectiveStatus(currentUser))}</span>
                </div>
                <div className="user-list-side">
                  <span>Last badge</span>
                  <strong>{formatLastBadgeAt(currentUser.last_badge_at)}</strong>
                  {currentUser.last_badge_at ? (
                    <span>
                      Durée journée:{" "}
                      {formatDuration(
                        currentUser.last_badge_day_duration_seconds,
                        currentUser.last_badge_day_duration_human
                      )}
                    </span>
                  ) : null}
                </div>
              </button>
            ))}
          </div>
        )}
      </section>
    </main>
  );
}

function AdminReportsView({ user, badgeDelaySeconds, onLogout, onToggleView, onNavigate }) {
  const [reportsState, setReportsState] = useState({ loading: true, error: "", items: [] });
  const [expandedDays, setExpandedDays] = useState(() => new Set());
  const [detailsByDay, setDetailsByDay] = useState({});

  async function loadReports() {
    setReportsState((current) => ({ ...current, loading: true, error: "" }));
    try {
      const { response, json, text } = await requestJSON("/api/admin/reports");
      if (!response.ok) {
        throw new Error((json && json.message) || text || "Unable to load daily reports.");
      }
      setReportsState({
        loading: false,
        error: "",
        items: Array.isArray(json) ? json : []
      });
    } catch (loadError) {
      setReportsState({
        loading: false,
        error: loadError instanceof Error ? loadError.message : String(loadError),
        items: []
      });
    }
  }

  async function loadReportDay(dayKey) {
    setDetailsByDay((current) => ({
      ...current,
      [dayKey]: {
        loading: true,
        error: "",
        items: current[dayKey]?.items || []
      }
    }));

    try {
      const { response, json, text } = await requestJSON(
        `/api/admin/students?date=${encodeURIComponent(dayKey)}&apprentices_only=true`
      );
      if (!response.ok) {
        throw new Error((json && json.message) || text || "Unable to load this report day.");
      }
      const items = Array.isArray(json) ? json : [];
      setDetailsByDay((current) => ({
        ...current,
        [dayKey]: {
          loading: false,
          error: "",
          items
        }
      }));
    } catch (loadError) {
      setDetailsByDay((current) => ({
        ...current,
        [dayKey]: {
          loading: false,
          error: loadError instanceof Error ? loadError.message : String(loadError),
          items: current[dayKey]?.items || []
        }
      }));
    }
  }

  useEffect(() => {
    void loadReports();
  }, []);

  function toggleDay(dayKey) {
    setExpandedDays((current) => {
      const next = new Set(current);
      if (next.has(dayKey)) {
        next.delete(dayKey);
        return next;
      }
      next.add(dayKey);
      return next;
    });

    if (!detailsByDay[dayKey]) {
      void loadReportDay(dayKey);
    }
  }

  return (
    <main className="app-shell">
      <AdminHeader
        user={user}
        badgeDelaySeconds={badgeDelaySeconds}
        onLogout={onLogout}
        onToggleView={onToggleView}
        activeSection="reports"
        onNavigate={onNavigate}
      />

      <section className="panel">
        <div className="panel-header">
          <div>
            <h2>Rapports journaliers</h2>
            <p className="panel-subtitle">
              Un jour par item. Ouvrez une ligne pour retrouver le détail équivalent au mail de résumé.
            </p>
          </div>
        </div>
        {reportsState.loading ? (
          <ReportsListSkeleton />
        ) : reportsState.error ? (
          <p className="feedback feedback-error">{reportsState.error}</p>
        ) : reportsState.items.length === 0 ? (
          <p className="feedback">Aucun rapport journalier disponible.</p>
        ) : (
          <div className="report-day-list">
            {reportsState.items.map((report) => {
              const isExpanded = expandedDays.has(report.day);
              const detailState = detailsByDay[report.day];

              return (
                <section key={report.day} className={`report-day-card${isExpanded ? " report-day-card-open" : ""}`}>
                  <button
                    type="button"
                    className="report-day-toggle"
                    onClick={() => toggleDay(report.day)}
                    aria-expanded={isExpanded}
                  >
                    <div className="report-day-main">
                      <strong>{formatLongDayLabel(report.day)}</strong>
                      <span>{report.day}</span>
                    </div>
                    <div className="report-day-stats">
                      <span>{report.student_count} alternants</span>
                      <span>{report.posted_count} posts</span>
                      {report.live ? (
                        <span className="report-stat-warning">Journée en cours</span>
                      ) : report.failed_count > 0 ? (
                        <span className="report-stat-danger">{report.failed_count} échecs</span>
                      ) : null}
                    </div>
                    <span className={`report-day-chevron${isExpanded ? " report-day-chevron-open" : ""}`} aria-hidden>
                      ▾
                    </span>
                  </button>

                  {isExpanded ? (
                    <div className="report-day-body">
                      {detailState?.loading ? (
                        <ReportDetailSkeleton />
                      ) : detailState?.error ? (
                        <p className="feedback feedback-error">{detailState.error}</p>
                      ) : detailState?.items?.length ? (
                        (() => {
                          const { successes, failures } = sortReportUsers(detailState.items);
                          const orderedStudents = [...successes, ...(successes.length > 0 && failures.length > 0 ? [{ __separator: true }] : []), ...failures];

                          return (
                            <div className="report-mail">
                              {orderedStudents.length === 0 ? (
                                <p className="feedback">Aucun alternant pour cette journée.</p>
                              ) : (
                                orderedStudents.map((student, index) => {
                                  if (student.__separator) {
                                    return <div key={`${report.day}-separator-${index}`} className="report-line-gap" aria-hidden />;
                                  }

                                  const tone = getReportLineTone(student, report.live);
                                  const line = buildReportLine(student, report.live);

                                  return (
                                    <div
                                      key={`${report.day}-${student.login_42}`}
                                      className={`report-line report-line-${tone}`}
                                    >
                                      {line}
                                    </div>
                                  );
                                })
                              )}
                            </div>
                          );
                        })()
                      ) : (
                        <p className="feedback">Aucune donnée détaillée pour cette journée.</p>
                      )}
                    </div>
                  ) : null}
                </section>
              );
            })}
          </div>
        )}
      </section>
    </main>
  );
}

function AdminUserPresenceCalendar({ days, monthKey, selectedDayKey, onChangeMonth, onSelectDay }) {
  const weekdayLabels = ["Lun", "Mar", "Mer", "Jeu", "Ven", "Sam", "Dim"];
  const dayMap = useMemo(() => new Map(days.map((day) => [day.day, day])), [days]);
  const activeMonthKey = monthKey || formatMonthKey(new Date());
  const activeMonthDate = parseMonthKey(activeMonthKey);
  const todayKey = formatDayKey(new Date());
  const monthStart = activeMonthDate instanceof Date ? new Date(activeMonthDate.getFullYear(), activeMonthDate.getMonth(), 1) : null;
  const daysInMonth = monthStart ? new Date(monthStart.getFullYear(), monthStart.getMonth() + 1, 0).getDate() : 0;
  const firstWeekday = monthStart ? (monthStart.getDay() + 6) % 7 : 0;
  const cells = [];

  for (let index = 0; index < firstWeekday; index += 1) {
    cells.push(null);
  }

  for (let dayNumber = 1; dayNumber <= daysInMonth; dayNumber += 1) {
    const dayDate = new Date(monthStart.getFullYear(), monthStart.getMonth(), dayNumber);
    const dayKey = formatDayKey(dayDate);
    cells.push({
      dayKey,
      dayNumber,
      summary: dayMap.get(dayKey) || null,
      isToday: dayKey === todayKey,
      isFuture: dayKey > todayKey
    });
  }

  return (
    <section className="presence-calendar-shell">
      <div className="presence-calendar-toolbar">
        <button
          className="secondary-button"
          type="button"
          onClick={() => onChangeMonth(-1)}
        >
          Mois précédent
        </button>
        <strong className="presence-calendar-label">{formatMonthLabel(activeMonthDate)}</strong>
        <button
          className="secondary-button"
          type="button"
          onClick={() => onChangeMonth(1)}
        >
          Mois suivant
        </button>
      </div>

      <CalendarDayLegend />

      <section className="presence-month-card presence-month-card-compact">
        <div className="presence-weekdays">
          {weekdayLabels.map((label) => (
            <span key={`${activeMonthKey}-${label}`}>{label}</span>
          ))}
        </div>
        <div className="presence-day-grid presence-day-grid-compact">
          {cells.map((cell, index) => {
            if (!cell) {
              return <div key={`${activeMonthKey}-empty-${index}`} className="presence-day-cell presence-day-empty" aria-hidden />;
            }

            const content = (
              <>
                <span className="presence-day-number">{cell.dayNumber}</span>
                {cell.summary ? (
                  <>
                    {cell.summary.live ? <span className="calendar-live-pill">live</span> : null}
                    <strong>{cell.summary.loading ? "loading..." : formatDuration(cell.summary.duration_seconds, cell.summary.duration_human)}</strong>
                  </>
                ) : (
                  <span className="presence-day-muted">-</span>
                )}
              </>
            );
            const dayMeta = cell.summary ? getCalendarDayMeta(cell.summary.day_type, cell.summary.day_type_label) : null;

            return (
              <button
                key={cell.dayKey}
                type="button"
                className={`presence-day-cell presence-day-button${
                  cell.summary ? " presence-day-has-data" : " presence-day-no-data"
                }${
                  dayMeta ? ` presence-day-type-${dayMeta.value}` : ""
                }${
                  selectedDayKey === cell.dayKey ? " presence-day-selected" : ""
                }${
                  cell.isToday ? " presence-day-today" : ""
                }${
                  cell.isFuture ? " presence-day-disabled" : ""
                }`}
                onClick={() => {
                  if (!cell.isFuture) {
                    onSelectDay(cell.dayKey);
                  }
                }}
                disabled={cell.isFuture}
              >
                {content}
              </button>
            );
          })}
        </div>
      </section>
    </section>
  );
}

function AdminUserDayDetail({ login, dayKey, dayEndpointBase, selectedDaySummary = null }) {
  const [state, setState] = useState({ loading: true, error: "", payload: null });
  const [currentTime, setCurrentTime] = useState(() => new Date());
  const dayRequestRef = useRef(0);
  const dayAbortRef = useRef(null);
  const isToday = dayKey === formatDayKey(new Date());
  const dayMeta = getCalendarDayMeta(selectedDaySummary?.day_type, selectedDaySummary?.day_type_label);
  const requiredAttendanceLabel = formatRequiredAttendanceHours(selectedDaySummary?.required_attendance_hours);

  async function loadDay(targetDay = dayKey, options = {}) {
    const { background = false } = options;
    dayAbortRef.current?.abort();
    const controller = new AbortController();
    dayAbortRef.current = controller;
    const requestId = dayRequestRef.current + 1;
    dayRequestRef.current = requestId;
    if (!background) {
      setState((current) => ({ ...current, loading: true, error: "" }));
    }
    try {
      const { response, json, text } = await requestJSON(
        `${dayEndpointBase}?date=${encodeURIComponent(targetDay)}`,
        { signal: controller.signal }
      );
      if (requestId !== dayRequestRef.current) {
        return;
      }
      if (!response.ok) {
        if (response.status === 404) {
          setState({
            loading: false,
            error: "",
            payload: {
              day: targetDay,
              live: false,
              login,
              tracked: false,
              badge_events: [],
              location_sessions: [],
              attendance_posts: []
            }
          });
          return;
        }
        throw new Error((json && json.message) || text || "Unable to load this day.");
      }
      setState({ loading: false, error: "", payload: json });
    } catch (loadError) {
      if (loadError instanceof Error && loadError.name === "AbortError") {
        return;
      }
      if (requestId !== dayRequestRef.current) {
        return;
      }
      setState((current) => ({
        loading: false,
        error: loadError instanceof Error ? loadError.message : String(loadError),
        payload: background ? current.payload : null
      }));
    }
  }

  useEffect(() => {
    void loadDay(dayKey);
  }, [dayEndpointBase, dayKey, login]);

  useEffect(() => {
    return () => {
      dayAbortRef.current?.abort();
    };
  }, []);

  useEffect(() => {
    const intervalId = window.setInterval(() => {
      setCurrentTime(new Date());
    }, 30000);
    return () => window.clearInterval(intervalId);
  }, []);

  useEffect(() => {
    return subscribeToLiveUpdates((event) => {
      const eventLogin = String(event?.login || "").trim().toLowerCase();
      if (eventLogin !== login.trim().toLowerCase() || String(event?.day || "").trim() !== dayKey) {
        return;
      }
      if (event?.day_payload) {
        setState((current) => ({
          ...current,
          loading: false,
          error: "",
          payload: event.day_payload
        }));
      }
    });
  }, [dayEndpointBase, dayKey, login]);

  const badgeEvents = useMemo(() => {
    const events = state.payload?.badge_events || [];
    return [...events]
      .filter((event) => isWithinStudentTimeline(event.timestamp))
      .sort((left, right) => new Date(left.timestamp) - new Date(right.timestamp));
  }, [state.payload]);
  const locationSessions = useMemo(() => {
    const sessions = state.payload?.location_sessions || [];
    return [...sessions]
      .filter((session) => overlapsStudentTimeline(session.begin_at, session.end_at))
      .sort((left, right) => new Date(left.begin_at) - new Date(right.begin_at));
  }, [state.payload]);
  const firstBadge = badgeEvents.length > 0 ? badgeEvents[0] : null;
  const lastBadge = badgeEvents.length > 0 ? badgeEvents[badgeEvents.length - 1] : null;
  const firstBadgeValue = firstBadge ? formatClockTime(firstBadge.timestamp, true) : "Aucun";
  const lastBadgeValue = lastBadge ? formatClockTime(lastBadge.timestamp, true) : "Aucun";

  if (state.loading) {
    return <AdminUserDayDetailSkeleton dayKey={dayKey} />;
  }

  return (
    <>
      <section className="admin-day-summary-slot">
        <div className="admin-day-heading">
          <h2>{formatLongDayLabel(dayKey)}</h2>
        </div>
        {state.error ? (
          <p className="feedback feedback-error">{state.error}</p>
        ) : state.payload ? (
          <>
            <div className="student-day-summary admin-day-summary-grid">
              <KeyValue label="Badges" value={String(badgeEvents.length)} />
              <KeyValue label="Premier badge" value={firstBadgeValue} />
              <KeyValue label="Dernier badge" value={lastBadgeValue} />
              <KeyValue label="Type" value={dayMeta.label} />
              <KeyValue label="Heures attendues" value={requiredAttendanceLabel} />
              <KeyValue
                label="Durée"
                value={
                  state.payload.tracked && state.payload.user
                    ? formatDuration(state.payload.user.duration_seconds, state.payload.user.duration_human)
                    : "0s"
                }
              />
            </div>
          </>
        ) : (
          <p className="feedback">Aucune donnee disponible pour cette journée.</p>
        )}
      </section>

      <section className="admin-day-timeline-slot">
        {state.error ? null : state.payload ? (
          <StudentDayTimeline
            badgeEvents={badgeEvents}
            locationSessions={locationSessions}
            currentTime={currentTime}
            showNowMarker={isToday}
            locationsLoading={Boolean(state.payload.locations_loading)}
          />
        ) : null}
      </section>
    </>
  );
}

function AdminUserDetailView({ login, user, badgeDelaySeconds, onLogout, onToggleView, onNavigate }) {
  const [state, setState] = useState({ loading: true, error: "", payload: null });
  const [selectedDayKey, setSelectedDayKey] = useState(() => formatDayKey(new Date()));
  const [selectedMonthKey, setSelectedMonthKey] = useState(() => formatMonthKey(new Date()));
  const monthRequestRef = useRef(0);
  const monthAbortRef = useRef(null);
  const [settingsSaving, setSettingsSaving] = useState(false);
  const [settingsError, setSettingsError] = useState("");
  const [blacklistModalOpen, setBlacklistModalOpen] = useState(false);
  const [blacklistReasonInput, setBlacklistReasonInput] = useState("");

  async function loadUserDetail(targetLogin = login, options = {}) {
    const { monthKey = selectedMonthKey || formatMonthKey(new Date()), background = false } = options;
    monthAbortRef.current?.abort();
    const controller = new AbortController();
    monthAbortRef.current = controller;
    const requestId = monthRequestRef.current + 1;
    monthRequestRef.current = requestId;
    if (!background) {
      setState((current) => ({ ...current, loading: true, error: "" }));
    }
    try {
      const query = new URLSearchParams();
      query.set("month", monthKey);
      const { response, json, text } = await requestJSON(`/api/admin/users/${encodeURIComponent(targetLogin)}?${query.toString()}`, {
        signal: controller.signal
      });
      if (requestId !== monthRequestRef.current) {
        return;
      }
      if (!response.ok) {
        throw new Error((json && json.message) || text || "Unable to load this user.");
      }
      setState({ loading: false, error: "", payload: json });
    } catch (loadError) {
      if (loadError instanceof Error && loadError.name === "AbortError") {
        return;
      }
      if (requestId !== monthRequestRef.current) {
        return;
      }
      setState((current) => ({
        loading: false,
        error: loadError instanceof Error ? loadError.message : String(loadError),
        payload: background ? current.payload : null
      }));
    }
  }

  useEffect(() => {
    void loadUserDetail(login, { monthKey: selectedMonthKey });
  }, [login, selectedMonthKey]);

  useEffect(() => {
    return () => {
      monthAbortRef.current?.abort();
    };
  }, []);

  useEffect(() => {
    setSelectedDayKey(formatDayKey(new Date()));
    setSelectedMonthKey(formatMonthKey(new Date()));
  }, [login]);

  useEffect(() => {
    if (!state.payload) {
      return;
    }
    const todayKey = formatDayKey(new Date());
    const todayMonthKey = formatMonthKey(new Date());
    setSelectedDayKey((current) => {
      if (selectedMonthKey === todayMonthKey) {
        if (current && current.startsWith(`${selectedMonthKey}-`)) {
          return current;
        }
        return todayKey;
      }
      if (current && current.startsWith(`${selectedMonthKey}-`)) {
        return current;
      }
      return null;
    });
  }, [state.payload, selectedMonthKey]);

  useEffect(() => {
    setBlacklistReasonInput(String(state.payload?.blacklist_reason || ""));
  }, [state.payload?.blacklist_reason, login]);

  useEffect(() => {
    return subscribeToLiveUpdates((event) => {
      if (!shouldApplyLiveEvent(event, login, selectedMonthKey)) {
        return;
      }
      if (event?.month_payload || event?.day_summary) {
        setState((current) => ({
          ...current,
          payload: mergeLiveDetailPayload(current.payload, event)
        }));
      }
    });
  }, [login, selectedMonthKey]);

  function handleChangeMonth(delta) {
    setSelectedMonthKey((current) => {
      const parsed = parseMonthKey(current || formatMonthKey(new Date()));
      if (!(parsed instanceof Date) || Number.isNaN(parsed.getTime())) {
        return current;
      }
      const nextMonthKey = formatMonthKey(shiftMonth(parsed, delta));
      const todayMonthKey = formatMonthKey(new Date());
      setSelectedDayKey(nextMonthKey === todayMonthKey ? formatDayKey(new Date()) : null);
      return nextMonthKey;
    });
  }

  async function patchAdminSettings(partial) {
    setSettingsSaving(true);
    setSettingsError("");
    try {
      const { response, json, text } = await requestJSON(`/api/admin/users/${encodeURIComponent(login)}`, {
        method: "PATCH",
        body: JSON.stringify(partial)
      });
      if (!response.ok) {
        throw new Error((json && json.message) || text || "Impossible de mettre à jour cet étudiant.");
      }
      await loadUserDetail(login, { monthKey: selectedMonthKey, background: false });
      return true;
    } catch (updateError) {
      setSettingsError(updateError instanceof Error ? updateError.message : String(updateError));
      return false;
    } finally {
      setSettingsSaving(false);
    }
  }

  async function handleConfirmBlacklist() {
    const success = await patchAdminSettings({
      is_blacklisted: true,
      blacklist_reason: blacklistReasonInput
    });
    if (success) {
      setBlacklistModalOpen(false);
    }
  }

  async function handleConfirmForgive() {
    const success = await patchAdminSettings({
      is_blacklisted: false
    });
    if (success) {
      setBlacklistModalOpen(false);
    }
  }

  const isBlacklisted = Boolean(state.payload?.is_blacklisted);

  async function handleStatusChange(nextStatus) {
    const detectedStatus = getDetectedStatus(state.payload);
    const normalizedStatus = String(nextStatus || "").trim().toLowerCase();
    const shouldOverride = normalizedStatus !== detectedStatus;
    await patchAdminSettings({
      status: normalizedStatus,
      status_overridden: shouldOverride
    });
  }

  return (
    <>
      <main className="app-shell detail-shell">
        <AdminHeader
          user={user}
          badgeDelaySeconds={badgeDelaySeconds}
          onLogout={onLogout}
          onToggleView={onToggleView}
          activeSection="students"
          onNavigate={onNavigate}
        />
        <div className="action-row">
          <button className="secondary-button" type="button" onClick={() => onNavigate("/")}>
            Retour aux étudiants
          </button>
        </div>

        <UserPresencePanel
          loading={state.loading}
          error={state.error || settingsError}
          payload={state.payload}
          login={login}
          badgeDelaySeconds={badgeDelaySeconds}
          selectedDayKey={selectedDayKey}
          selectedMonthKey={selectedMonthKey}
          onChangeMonth={handleChangeMonth}
          onSelectDay={setSelectedDayKey}
          dayEndpointBase={`/api/admin/students/${encodeURIComponent(login)}`}
          adminControls={{
            isBlacklisted,
            status: getEffectiveStatus(state.payload),
            status42: getDetectedStatus(state.payload),
            statusOverridden: Boolean(state.payload?.status_overridden),
            saving: settingsSaving,
            onOpenBlacklistModal: () => {
              setBlacklistReasonInput(String(state.payload?.blacklist_reason || ""));
              setBlacklistModalOpen(true);
            },
            onStatusChange: handleStatusChange
          }}
        />
      </main>
      <ConfirmationModal
        open={blacklistModalOpen}
        title={isBlacklisted ? "Pardonner l’étudiant" : "Blacklister l’étudiant"}
        confirmLabel={isBlacklisted ? "Pardonner" : "Blacklister"}
        tone="danger"
        onClose={() => setBlacklistModalOpen(false)}
        onConfirm={isBlacklisted ? handleConfirmForgive : handleConfirmBlacklist}
      >
        {isBlacklisted ? (
          <>
            <p>Cette action retire l’étudiant de la blacklist.</p>
            {state.payload?.blacklist_reason ? <p>Le motif existant sera conservé.</p> : null}
          </>
        ) : (
          <>
            <p>Cette action empêche la badgeuse de traiter cet étudiant.</p>
            <label className="field modal-field">
              <span>Raison</span>
              <textarea
                rows="4"
                value={blacklistReasonInput}
                onChange={(event) => setBlacklistReasonInput(event.target.value)}
                placeholder="Raison de la blacklist"
              />
            </label>
          </>
        )}
      </ConfirmationModal>
    </>
  );
}

function UserPresencePanel({
  loading,
  error,
  payload,
  login,
  badgeDelaySeconds,
  selectedDayKey,
  selectedMonthKey,
  onChangeMonth,
  onSelectDay,
  dayEndpointBase,
  adminControls = null
}) {
  const selectedDaySummary = selectedDayKey
    ? (payload?.days || []).find((day) => day.day === selectedDayKey) || null
    : null;

  if (loading) {
    return (
      <UserPresencePanelSkeleton
        selectedDayKey={selectedDayKey}
        selectedMonthKey={selectedMonthKey}
        showAdminActions={Boolean(adminControls)}
      />
    );
  }

  return (
    <section className="panel">
      {error ? (
        <p className="feedback feedback-error">{error}</p>
      ) : payload ? (
        <section className="user-detail-dashboard">
          <div className="user-detail-main">
            <div className="user-detail-main-header">
              <div className="user-detail-hero">
                <UserAvatar user={payload} className="user-detail-avatar" />
                <div className="user-detail-meta">
                  <h2>
                    {payload.login_42} <UserStateBadges user={payload} />
                  </h2>
                  {adminControls ? (
                    <AdminStatusField
                      value={adminControls.status}
                      detectedValue={adminControls.status42}
                      disabled={adminControls.saving}
                      onChange={adminControls.onStatusChange}
                    />
                  ) : (
                    <p>
                      {getStatusOption(getEffectiveStatus(payload)).emoji} {formatStatusLabel(getEffectiveStatus(payload))}
                    </p>
                  )}
                  <span>Last badge: {formatLastBadgeAt(payload.last_badge_at)}</span>
                </div>
              </div>
              {adminControls ? (
                <div className="admin-actions-row admin-actions-row-header">
                  <BlacklistActionButton
                    blacklisted={adminControls.isBlacklisted}
                    disabled={adminControls.saving}
                    onClick={adminControls.onOpenBlacklistModal}
                  />
                </div>
              ) : null}
              <BadgeDelayChip seconds={badgeDelaySeconds} />
            </div>
            {payload.is_blacklisted ? (
              <div className="warning-callout blacklist-callout" role="alert" aria-label="Blacklist">
                <strong>Vous êtes blacklisté</strong>
                <p>
                  Le bocal a pris la décision de désactiver la badgeuse pour justifier votre présence.
                  <br />
                  Uniquement le logtime sera communiqué au CFA.
                </p>
                {adminControls && payload.blacklist_reason ? <p>Motif: {payload.blacklist_reason}</p> : null}
              </div>
            ) : null}
            <div className="warning-callout" role="note" aria-label="Avertissement">
              <strong>Attention</strong>
              <p>
                Les calendriers et durées affichés sur Watchdog sont fournis à titre indicatif et peuvent évoluer
                au cours de la journée.
                <br />
                En fin de journée, seule la présence affichée sur{" "}
                <a href="https://cfa.42.fr" target="_blank" rel="noreferrer">
                  cfa.42.fr
                </a>{" "}
                fait foi.
              </p>
            </div>
          </div>
          {selectedDayKey ? (
            <AdminUserDayDetail
              login={login}
              dayKey={selectedDayKey}
              dayEndpointBase={dayEndpointBase}
              selectedDaySummary={selectedDaySummary}
            />
          ) : (
            <section className="admin-day-summary-slot admin-day-summary-empty">
              <p className="feedback">Choisissez une journée dans le calendrier pour afficher la timeline.</p>
            </section>
          )}
          <div className="user-detail-calendar-column">
            <AdminUserPresenceCalendar
              days={payload.days || []}
              monthKey={selectedMonthKey}
              selectedDayKey={selectedDayKey}
              onChangeMonth={onChangeMonth}
              onSelectDay={onSelectDay}
            />
          </div>
        </section>
      ) : (
        <p className="feedback">Aucune donnee disponible pour cet utilisateur.</p>
      )}
    </section>
  );
}

function App() {
  const [authState, setAuthState] = useState({
    loading: true,
    user: null,
    error: ""
  });
  const [adminViewMode, setAdminViewMode] = useState("admin");
  const [badgeDelaySeconds, setBadgeDelaySeconds] = useState(null);
  const [path, setPath] = useState(() => window.location.pathname);
  const adminUserMatch = path.match(/^\/admin\/([^/]+)$/);
  const adminUserLogin = adminUserMatch ? decodeURIComponent(adminUserMatch[1]) : "";
  const isAdminReportsPath = path === "/reports";

  useEffect(() => {
    function handlePopState() {
      setPath(window.location.pathname);
    }

    window.addEventListener("popstate", handlePopState);
    return () => window.removeEventListener("popstate", handlePopState);
  }, []);

  async function loadCurrentUser() {
    setAuthState({ loading: true, user: null, error: "" });
    try {
      const { response, json, text } = await requestJSON("/api/auth/me");
      if (!response.ok) {
        if (response.status === 401) {
          setAuthState({ loading: false, user: null, error: "" });
          return;
        }
        throw new Error((json && json.message) || text || "Unable to load session.");
      }
      setBadgeDelaySeconds(
        typeof json?.badge_delay_seconds === "number" ? json.badge_delay_seconds : null
      );
      setAuthState({ loading: false, user: json, error: "" });
    } catch (error) {
      setAuthState({
        loading: false,
        user: null,
        error: error instanceof Error ? error.message : String(error)
      });
    }
  }

  useEffect(() => {
    void loadCurrentUser();
  }, []);

  useEffect(() => {
    if (authState.loading || !authState.user) {
      return undefined;
    }

    return subscribeToLiveUpdates((event) => {
      if (event?.type !== "badge_received") {
        return;
      }
      if (typeof event.badge_delay_seconds === "number") {
        setBadgeDelaySeconds(event.badge_delay_seconds);
      }
    });
  }, [authState.loading, authState.user]);

  useEffect(() => {
    if (authState.loading) {
      return;
    }
    if (!authState.user && path !== "/login") {
      const next = `${window.location.pathname}${window.location.search}`;
      window.location.replace(`/login?next=${encodeURIComponent(next)}`);
      return;
    }
    if (authState.user && path === "/login") {
      const nextPath = isAdminUser(authState.user) ? "/" : "/me";
      window.history.replaceState({}, "", nextPath);
      setPath(nextPath);
      return;
    }
    if (authState.user && !isAdminUser(authState.user) && path !== "/me") {
      window.history.replaceState({}, "", "/me");
      setPath("/me");
    }
  }, [authState.loading, authState.user, path]);

  async function handleLogout() {
    try {
      await fetch("/auth/logout", {
        method: "POST",
        credentials: "include"
      });
    } finally {
      window.location.assign("/login");
    }
  }

  function handleToggleAdminView() {
    setAdminViewMode((current) => (current === "admin" ? "student" : "admin"));
  }

  function navigateTo(nextPath) {
    if (!nextPath || nextPath === path) {
      return;
    }
    window.history.pushState({}, "", nextPath);
    setPath(nextPath);
  }

  if (path === "/login") {
    return <LoginPage />;
  }

  if (authState.loading) {
    return <SessionLoadingSkeleton />;
  }

  if (authState.error) {
    return (
      <main className="app-shell">
        <section className="panel loading-panel">
          <h1>Erreur d&apos;authentification</h1>
          <p className="feedback feedback-error">{authState.error}</p>
        </section>
      </main>
    );
  }

  if (!authState.user) {
    return null;
  }

  const isAdmin = isAdminUser(authState.user);

  if (isAdmin && adminViewMode === "admin") {
    if (adminUserLogin !== "") {
      return (
        <AdminUserDetailView
          login={adminUserLogin}
          user={authState.user}
          badgeDelaySeconds={badgeDelaySeconds}
          onLogout={handleLogout}
          onToggleView={handleToggleAdminView}
          onNavigate={navigateTo}
        />
      );
    }

    if (isAdminReportsPath) {
      return (
        <AdminReportsView
          user={authState.user}
          badgeDelaySeconds={badgeDelaySeconds}
          onLogout={handleLogout}
          onToggleView={handleToggleAdminView}
          onNavigate={navigateTo}
        />
      );
    }

    return (
      <AdminUsersIndexView
        user={authState.user}
        badgeDelaySeconds={badgeDelaySeconds}
        onLogout={handleLogout}
        onToggleView={handleToggleAdminView}
        onNavigate={navigateTo}
      />
    );
  }

  return (
    <StudentView
      user={authState.user}
      badgeDelaySeconds={badgeDelaySeconds}
      onLogout={handleLogout}
      onToggleView={isAdmin ? handleToggleAdminView : undefined}
    />
  );
}

export default App;
