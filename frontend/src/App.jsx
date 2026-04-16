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
const ADMIN_STATUS_TILES = [
  { key: "apprentice", label: "Alternant", emoji: "👨‍🎓" },
  { key: "student", label: "Étudiant", emoji: "👶" },
  { key: "pisciner", label: "Piscineux", emoji: "🏊‍♂️" },
  { key: "staff", label: "Staff", emoji: "🛠️" },
  { key: "extern", label: "Externe", emoji: "🌍" }
];
const STATS_WEEKDAYS = [
  { value: 0, label: "Lundi" },
  { value: 1, label: "Mardi" },
  { value: 2, label: "Mercredi" },
  { value: 3, label: "Jeudi" },
  { value: 4, label: "Vendredi" },
  { value: 5, label: "Samedi" },
  { value: 6, label: "Dimanche" }
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

function getPresenceTargetStatus(summary) {
  const requiredHours = Number(summary?.required_attendance_hours);
  const durationSeconds = Number(summary?.duration_seconds);
  if (!Number.isFinite(requiredHours) || requiredHours <= 0 || !Number.isFinite(durationSeconds)) {
    return "";
  }
  return durationSeconds >= Math.round(requiredHours * 3600) ? "success" : "warning";
}

function formatDurationPadded(seconds, fallback) {
  const raw = formatDuration(seconds, fallback);
  const match = String(raw).match(/^(?:(\d+)h\s*)?(?:(\d+)m\s*)?(\d+)s$/);
  if (!match) {
    return raw;
  }
  const hours = String(match[1] || 0).padStart(2, "0");
  const minutes = String(match[2] || 0).padStart(2, "0");
  const secs = String(match[3] || 0).padStart(2, "0");
  return `${hours}h ${minutes}m ${secs}s`;
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

function getReportExpectedSeconds(user) {
  const hours = Number(user?.required_attendance_hours);
  if (!Number.isFinite(hours)) {
    return null;
  }
  return Math.round(hours * 3600);
}

function getReportDurationTone(user) {
  const expectedSeconds = getReportExpectedSeconds(user);
  const totalSeconds = Number(user?.total_duration_seconds ?? user?.duration_seconds);
  if (!Number.isFinite(expectedSeconds) || !Number.isFinite(totalSeconds)) {
    return "neutral";
  }
  const delta = totalSeconds - expectedSeconds;
  if (delta >= 0) {
    return "success";
  }
  if (delta >= -30 * 60) {
    return "warning";
  }
  return "danger";
}

function getReportLineTone(user, isLiveDay = false) {
  if (!isLiveDay && !isReportLineSuccess(user)) {
    return "danger";
  }
  if (isLiveDay) {
    if (!user?.first_access) {
      return "warning";
    }
    return "success";
  }
  return "success";
}

function getReportRangeTone(user) {
  const first = user?.first_access ? new Date(user.first_access) : null;
  const last = user?.last_access ? new Date(user.last_access) : null;
  const isFirstWarning = first instanceof Date && !Number.isNaN(first.getTime()) && isBeforeApprenticeStart(first);
  const isLastWarning = last instanceof Date && !Number.isNaN(last.getTime()) && getTimelineMinutes(last) > APPRENTICE_END_MINUTES;
  return isFirstWarning || isLastWarning ? "warning" : "neutral";
}

function buildReportLine(user, isLiveDay = false, options = {}) {
  const { onLoginClick = null } = options;
  const message = getReportLineMessage(user, isLiveDay);
  const { first, last } = getReportLineTimes(user);
  const success = isReportLineSuccess(user);
  const emoji = success ? "✅" : isLiveDay ? "⏳" : "❌";
  const isErrorLine = !isLiveDay && !success;
  const rangeTone = isErrorLine ? "neutral" : getReportRangeTone(user);
  const totalTone = isErrorLine ? "neutral" : getReportDurationTone(user);
  const logtimeTone = isErrorLine ? "neutral" : "neutral";

  return (
    <>
      <span className="report-line-prefix">
        <span className="report-line-emoji">{emoji}</span>
        {typeof onLoginClick === "function" ? (
          <button type="button" className="report-line-login report-line-login-button" onClick={onLoginClick}>
            {String(user?.login_42 || "")}
          </button>
        ) : (
          <span className="report-line-login">{String(user?.login_42 || "")}</span>
        )}
        <span className="report-line-separator">:</span>
      </span>
      <span className="report-line-block">
        <span className="report-line-label">Badge:</span>
        <span className={`report-line-range${rangeTone !== "neutral" ? ` report-line-range-${rangeTone}` : ""}`}>
          {first}-{last}
        </span>
        <span className={`report-line-duration report-line-duration-${totalTone}`}>
          ({formatCompactDuration(user?.badge_duration_seconds, user?.badge_duration_human)})
        </span>
      </span>
      <span className="report-line-separator">|</span>
      <span className="report-line-block">
        <span className="report-line-label">Logtime:</span>
        <span className={`report-line-duration${logtimeTone !== "neutral" ? ` report-line-duration-${logtimeTone}` : ""}`}>
          ({formatCompactDuration(user?.logtime_duration_seconds, user?.logtime_duration_human)})
        </span>
      </span>
      <span className="report-line-separator">|</span>
      <span className="report-line-block">
        <span className="report-line-label">Total:</span>
        <span className={`report-line-duration report-line-duration-${totalTone}`}>
          ({formatCompactDuration(user?.total_duration_seconds, user?.total_duration_human)})
        </span>
      </span>
      <span className="report-line-separator">—</span>
      <span className="report-line-message">{message}</span>
    </>
  );
}

function buildStatusFilters(statuses) {
  const active = new Set((statuses || []).map((value) => String(value).trim().toLowerCase()).filter(Boolean));
  if (active.size > 0) {
    return {
      apprentice: active.has("apprentice"),
      student: active.has("student"),
      pisciner: active.has("pisciner"),
      staff: active.has("staff"),
      extern: active.has("extern")
    };
  }
  return {
    apprentice: true,
    student: false,
    pisciner: false,
    staff: false,
    extern: false
  };
}

function getActiveStatusKeys(statusFilters) {
  return Object.entries(statusFilters || {})
    .filter(([, checked]) => checked)
    .map(([status]) => status);
}

function readAdminUserFiltersFromURL() {
  const query = new URLSearchParams(window.location.search);
  return {
    search: query.get("search") || "",
    date: query.get("date") || "",
    statusFilters: buildStatusFilters(query.getAll("status"))
  };
}

function readAdminStatsFiltersFromURL() {
  const query = new URLSearchParams(window.location.search);
  return {
    statusFilters: buildStatusFilters(query.getAll("status")),
    restrictWindow: String(query.get("restrict_window") || "").trim().toLowerCase() !== "false"
  };
}

function readAdminUserDetailSelectionFromURL() {
  const query = new URLSearchParams(window.location.search);
  const today = new Date();
  const fallbackMonth = formatMonthKey(today);
  const fallbackDay = formatDayKey(today);
  const month = String(query.get("month") || "").trim();
  const date = String(query.get("date") || "").trim();

  return {
    monthKey: /^\d{4}-\d{2}$/.test(month) ? month : fallbackMonth,
    selectedDayKey: /^\d{4}-\d{2}-\d{2}$/.test(date) ? date : fallbackDay
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

const liveUpdateScopes = {
  user: {
    endpoint: "/api/live",
    listeners: new Set(),
    socket: null,
    reconnectTimer: 0
  },
  admin: {
    endpoint: "/api/live/admin",
    listeners: new Set(),
    socket: null,
    reconnectTimer: 0
  }
};

function ensureLiveUpdatesSocket(scope = "user") {
  const target = liveUpdateScopes[scope] || liveUpdateScopes.user;
  if (target.socket && (target.socket.readyState === WebSocket.OPEN || target.socket.readyState === WebSocket.CONNECTING)) {
    return;
  }

  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  const url = `${protocol}//${window.location.host}${target.endpoint}`;
  target.socket = new WebSocket(url);

  target.socket.onmessage = (event) => {
    try {
      const payload = JSON.parse(event.data);
      for (const listener of target.listeners) {
        listener(payload);
      }
    } catch {
      // Ignore malformed live events.
    }
  };

  target.socket.onerror = () => {
    target.socket?.close();
  };

  target.socket.onclose = () => {
    target.socket = null;
    if (target.reconnectTimer) {
      window.clearTimeout(target.reconnectTimer);
      target.reconnectTimer = 0;
    }
    if (target.listeners.size === 0) {
      return;
    }
    target.reconnectTimer = window.setTimeout(() => {
      ensureLiveUpdatesSocket(scope);
    }, 2000);
  };
}

function subscribeToLiveUpdates(onEvent, options = {}) {
  if (typeof onEvent !== "function") {
    return () => {};
  }
  const scope = options.scope === "admin" ? "admin" : "user";
  const target = liveUpdateScopes[scope];
  target.listeners.add(onEvent);
  ensureLiveUpdatesSocket(scope);

  return () => {
    target.listeners.delete(onEvent);
    if (target.listeners.size > 0) {
      return;
    }
    if (target.reconnectTimer) {
      window.clearTimeout(target.reconnectTimer);
      target.reconnectTimer = 0;
    }
    if (target.socket && (target.socket.readyState === WebSocket.OPEN || target.socket.readyState === WebSocket.CONNECTING)) {
      target.socket.close();
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
    { key: "students", label: "Étudiants", href: "/admin/students" },
    { key: "stats", label: "Stats", href: "/admin/stats" },
    { key: "reports", label: "Rapports", href: "/admin/reports" }
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
    }, { scope: "user" });
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

  const activeStatuses = useMemo(() => getActiveStatusKeys(statusFilters), [statusFilters]);
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
    const nextURL = query.toString() ? `/admin/students?${query.toString()}` : "/admin/students";
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
    }, { scope: "admin" });
  }, [searchInput, activeStatusKey, dateInput]);

  function toggleStatusFilter(status) {
    setStatusFilters((current) => ({
      ...current,
      [status]: !current[status]
    }));
  }

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
              {ADMIN_STATUS_TILES.map((statusTile) => (
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
                onClick={() => onNavigate(`/admin/students/${encodeURIComponent(currentUser.login_42)}`)}
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
  const [regeneratingDay, setRegeneratingDay] = useState("");

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
        `/api/admin/reports/${encodeURIComponent(dayKey)}`
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

  async function regenerateReportDay(dayKey) {
    setRegeneratingDay(dayKey);
    try {
      const { response, json, text } = await requestJSON(`/api/admin/reports/${encodeURIComponent(dayKey)}/regenerate`, {
        method: "POST"
      });
      if (!response.ok) {
        throw new Error((json && json.message) || text || "Unable to regenerate this report day.");
      }
      await loadReports();
      await loadReportDay(dayKey);
    } catch (regenError) {
      setDetailsByDay((current) => ({
        ...current,
        [dayKey]: {
          loading: false,
          error: regenError instanceof Error ? regenError.message : String(regenError),
          items: current[dayKey]?.items || []
        }
      }));
    } finally {
      setRegeneratingDay("");
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
                      {!report.live ? (
                        <button
                          type="button"
                          className="report-regenerate-button"
                          onClick={(event) => {
                            event.stopPropagation();
                            void regenerateReportDay(report.day);
                          }}
                          disabled={regeneratingDay === report.day}
                          aria-label={regeneratingDay === report.day ? "Regeneration en cours" : "Regenerer le rapport"}
                          title={regeneratingDay === report.day ? "Regeneration en cours" : "Regenerer le rapport"}
                        >
                          {regeneratingDay === report.day ? "⟳" : "🔄"}
                        </button>
                      ) : null}
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
                                  const reportMonthKey = String(report.day || "").slice(0, 7);
                                  const line = buildReportLine(student, report.live, {
                                    onLoginClick: () =>
                                      onNavigate(
                                        `/admin/students/${encodeURIComponent(student.login_42)}?month=${encodeURIComponent(reportMonthKey)}&date=${encodeURIComponent(report.day)}`
                                      )
                                  });

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

function formatDecimalValue(value, digits = 1) {
  const numeric = Number(value || 0);
  return new Intl.NumberFormat("fr-FR", {
    minimumFractionDigits: 0,
    maximumFractionDigits: digits
  }).format(numeric);
}

function getStatusChartMeta(status) {
  switch (String(status || "").trim().toLowerCase()) {
    case "apprentice":
      return { label: "Alternant", color: "#3fc6a8" };
    case "student":
      return { label: "Étudiant", color: "#62a4ff" };
    case "pisciner":
      return { label: "Piscineux", color: "#ffb347" };
    case "staff":
      return { label: "Staff", color: "#ff7d7d" };
    case "extern":
      return { label: "Externe", color: "#c8a6ff" };
    default:
      return { label: formatStatusLabel(status), color: "#a5b8b0" };
  }
}

function StatsCard({ title, subtitle = "", children }) {
  return (
    <section className="stats-card">
      <div className="stats-card-header">
        <div>
          <h3>{title}</h3>
          {subtitle ? <p>{subtitle}</p> : null}
        </div>
      </div>
      {children}
    </section>
  );
}

function VerticalBarChart({ data, formatValue = (value) => formatDecimalValue(value), valueSuffix = "", emptyLabel = "Aucune donnée." }) {
  const safeData = Array.isArray(data) ? data : [];
  const values = safeData.map((item) => Number(item?.value || 0));
  const maxValue = values.reduce((current, value) => Math.max(current, value), 0);

  if (safeData.length === 0) {
    return <p className="feedback">{emptyLabel}</p>;
  }

  const width = 680;
  const height = 280;
  const paddingLeft = 56;
  const paddingRight = 20;
  const paddingTop = 20;
  const paddingBottom = 48;
  const chartWidth = width - paddingLeft - paddingRight;
  const chartHeight = height - paddingTop - paddingBottom;
  const tickCount = 5;
  const roughStep = maxValue > 0 ? maxValue / tickCount : 1;
  const magnitude = 10 ** Math.max(0, Math.floor(Math.log10(roughStep)));
  const normalizedStep = roughStep / magnitude;
  let niceStep = magnitude;
  if (normalizedStep > 5) {
    niceStep = 10 * magnitude;
  } else if (normalizedStep > 2) {
    niceStep = 5 * magnitude;
  } else if (normalizedStep > 1) {
    niceStep = 2 * magnitude;
  }
  const axisMax = maxValue > 0 ? Math.ceil(maxValue / niceStep) * niceStep : 1;
  const ticks = Array.from({ length: tickCount + 1 }, (_, index) => {
    const value = (axisMax / tickCount) * index;
    const y = paddingTop + chartHeight - (value / axisMax) * chartHeight;
    return { value, y };
  });
  const bandWidth = chartWidth / safeData.length;
  const barWidth = Math.min(58, bandWidth * 0.7);

  return (
    <div className="chart-vertical-shell">
      <svg viewBox={`0 0 ${width} ${height}`} className="chart-vertical-svg" role="img" aria-label="Graphique de présence moyenne par jour">
        <defs>
          <linearGradient id="chart-bar-gradient" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="#56c8ad" />
            <stop offset="100%" stopColor="#245ff4" />
          </linearGradient>
        </defs>
        {ticks.map((tick) => (
          <g key={tick.value}>
            <line x1={paddingLeft} y1={tick.y} x2={width - paddingRight} y2={tick.y} className="chart-grid-line" />
            <text x={paddingLeft - 10} y={tick.y + 4} textAnchor="end" className="chart-grid-label">
              {formatValue(tick.value)}
            </text>
          </g>
        ))}
        <line x1={paddingLeft} y1={paddingTop} x2={paddingLeft} y2={paddingTop + chartHeight} className="chart-axis" />
        <line x1={paddingLeft} y1={paddingTop + chartHeight} x2={width - paddingRight} y2={paddingTop + chartHeight} className="chart-axis" />
        {safeData.map((item, index) => {
          const value = Number(item?.value || 0);
          const barHeight = axisMax > 0 ? (value / axisMax) * chartHeight : 0;
          const x = paddingLeft + bandWidth * index + (bandWidth - barWidth) / 2;
          const y = paddingTop + chartHeight - barHeight;
          return (
            <g key={item.label}>
              <text x={x + barWidth / 2} y={Math.max(y - 8, paddingTop + 12)} textAnchor="middle" className="chart-bar-value">
                {formatValue(value)}
                {valueSuffix}
              </text>
              <rect x={x} y={y} width={barWidth} height={Math.max(barHeight, 0)} rx="14" className="chart-bar-rect" />
              <text x={x + barWidth / 2} y={height - 14} textAnchor="middle" className="chart-bar-label">
                {item.label}
              </text>
            </g>
          );
        })}
      </svg>
    </div>
  );
}

function LineChart({ data, formatValue = (value) => formatDecimalValue(value), emptyLabel = "Aucune donnée." }) {
  const safeData = Array.isArray(data) ? data : [];
  const values = safeData.map((item) => Number(item?.value || 0));
  const maxValue = values.reduce((current, value) => Math.max(current, value), 0);

  if (safeData.length === 0) {
    return <p className="feedback">{emptyLabel}</p>;
  }

  const width = 680;
  const height = 260;
  const paddingX = 28;
  const paddingTop = 18;
  const paddingBottom = 46;
  const chartWidth = width - paddingX * 2;
  const chartHeight = height - paddingTop - paddingBottom;
  const points = safeData.map((item, index) => {
    const x = safeData.length === 1 ? paddingX + chartWidth / 2 : paddingX + (chartWidth * index) / (safeData.length - 1);
    const value = Number(item?.value || 0);
    const y = paddingTop + chartHeight - (maxValue > 0 ? (value / maxValue) * chartHeight : 0);
    return { x, y, value, label: item.label };
  });
  const path = points.map((point, index) => `${index === 0 ? "M" : "L"} ${point.x} ${point.y}`).join(" ");

  return (
    <div className="chart-line-shell">
      <svg viewBox={`0 0 ${width} ${height}`} className="chart-line-svg" role="img" aria-label="Graphique de présence moyenne">
        <line x1={paddingX} y1={paddingTop + chartHeight} x2={width - paddingX} y2={paddingTop + chartHeight} className="chart-axis" />
        <line x1={paddingX} y1={paddingTop} x2={paddingX} y2={paddingTop + chartHeight} className="chart-axis" />
        <path d={path} className="chart-line-path" />
        {points.map((point) => (
          <g key={point.label}>
            <circle cx={point.x} cy={point.y} r="4" className="chart-line-dot" />
            <text x={point.x} y={height - 16} textAnchor="middle" className="chart-line-label">
              {point.label}
            </text>
            <text x={point.x} y={point.y - 10} textAnchor="middle" className="chart-line-value">
              {formatValue(point.value)}
            </text>
          </g>
        ))}
      </svg>
    </div>
  );
}

const WEEKDAY_LINE_META = [
  { key: "lun", label: "Lundi", shortLabel: "Lun", color: "#56c8ad" },
  { key: "mar", label: "Mardi", shortLabel: "Mar", color: "#4f8df6" },
  { key: "mer", label: "Mercredi", shortLabel: "Mer", color: "#ffb347" },
  { key: "jeu", label: "Jeudi", shortLabel: "Jeu", color: "#ff7a7a" },
  { key: "ven", label: "Vendredi", shortLabel: "Ven", color: "#b388ff" },
  { key: "sam", label: "Samedi", shortLabel: "Sam", color: "#7ed957" },
  { key: "dim", label: "Dimanche", shortLabel: "Dim", color: "#f78fb3" }
];

function MultiWeekdayLineChart({ series, disabledKeys, onToggleSeries, formatValue = (value) => formatDecimalValue(value), emptyLabel = "Aucune donnée." }) {
  const safeSeries = (Array.isArray(series) ? series : [])
    .map((item, index) => {
      const fallbackMeta = WEEKDAY_LINE_META[index] || WEEKDAY_LINE_META[0];
      const key = String(item?.key || fallbackMeta.key || "").trim().toLowerCase();
      const meta = WEEKDAY_LINE_META.find((candidate) => candidate.key === key) || fallbackMeta;
      const values = Array.isArray(item?.values) ? item.values : [];
      return {
        key: meta.key,
        label: meta.label,
        shortLabel: meta.shortLabel,
        color: meta.color,
        values
      };
    })
    .filter((item) => item.values.length > 0);
  const visibleSeries = safeSeries.filter((item) => !disabledKeys.has(item.key));
  const xLabels = safeSeries[0]?.values?.map((item) => item?.label || "") || [];
  const allValues = visibleSeries.flatMap((item) => item.values.map((entry) => Number(entry?.value || 0)));
  const maxValue = allValues.reduce((current, value) => Math.max(current, value), 0);

  if (safeSeries.length === 0 || xLabels.length === 0) {
    return <p className="feedback">{emptyLabel}</p>;
  }

  const width = 760;
  const height = 320;
  const paddingLeft = 54;
  const paddingRight = 24;
  const paddingTop = 20;
  const paddingBottom = 54;
  const chartWidth = width - paddingLeft - paddingRight;
  const chartHeight = height - paddingTop - paddingBottom;
  const tickCount = 5;
  const axisMax = maxValue > 0 ? Math.ceil(maxValue / tickCount) * tickCount : 1;
  const yTicks = Array.from({ length: tickCount + 1 }, (_, index) => {
    const value = (axisMax / tickCount) * index;
    const y = paddingTop + chartHeight - (value / axisMax) * chartHeight;
    return { value, y };
  });

  return (
    <div className="chart-multi-line-panel">
      <div className="chart-multi-line-legend" role="group" aria-label="Jours affichés">
        {safeSeries.map((item) => {
          const disabled = disabledKeys.has(item.key);
          return (
            <button
              key={item.key}
              type="button"
              className={`chart-multi-line-legend-item${disabled ? " chart-multi-line-legend-item-disabled" : ""}`}
              onClick={() => onToggleSeries(item.key)}
              aria-pressed={!disabled}
            >
              <span className="chart-multi-line-legend-swatch" style={{ backgroundColor: item.color }} />
              <span>{item.label}</span>
            </button>
          );
        })}
      </div>
      <div className="chart-line-shell">
        <svg viewBox={`0 0 ${width} ${height}`} className="chart-line-svg" role="img" aria-label="Graphique de présence moyenne par heure et par jour">
          {yTicks.map((tick) => (
            <g key={tick.value}>
              <line x1={paddingLeft} y1={tick.y} x2={width - paddingRight} y2={tick.y} className="chart-grid-line" />
              <text x={paddingLeft - 10} y={tick.y + 4} textAnchor="end" className="chart-grid-label">
                {formatValue(tick.value)}
              </text>
            </g>
          ))}
          <line x1={paddingLeft} y1={paddingTop} x2={paddingLeft} y2={paddingTop + chartHeight} className="chart-axis" />
          <line x1={paddingLeft} y1={paddingTop + chartHeight} x2={width - paddingRight} y2={paddingTop + chartHeight} className="chart-axis" />
          {xLabels.map((label, index) => {
            const x = xLabels.length === 1 ? paddingLeft + chartWidth / 2 : paddingLeft + (chartWidth * index) / (xLabels.length - 1);
            return (
              <text key={label} x={x} y={height - 16} textAnchor="middle" className="chart-line-label">
                {label}
              </text>
            );
          })}
          {visibleSeries.map((item) => {
            const points = item.values.map((entry, index) => {
              const x = item.values.length === 1 ? paddingLeft + chartWidth / 2 : paddingLeft + (chartWidth * index) / (item.values.length - 1);
              const value = Number(entry?.value || 0);
              const y = paddingTop + chartHeight - (axisMax > 0 ? (value / axisMax) * chartHeight : 0);
              return { x, y, value };
            });
            const peakValue = points.reduce((current, point) => Math.max(current, point.value), 0);
            const path = points.map((point, index) => `${index === 0 ? "M" : "L"} ${point.x} ${point.y}`).join(" ");
            return (
              <g key={item.key}>
                <path d={path} className="chart-line-path" style={{ stroke: item.color }} />
                {points.map((point, index) => {
                  const isPeak = point.value === peakValue;
                  if (!isPeak) {
                    return <circle key={`${item.key}-${index}`} cx={point.x} cy={point.y} r="3.5" className="chart-line-dot" style={{ fill: item.color }} />;
                  }
                  return (
                    <g key={`${item.key}-${index}`}>
                      <circle cx={point.x} cy={point.y} r="4" className="chart-line-dot" style={{ fill: item.color }} />
                      <text x={point.x} y={point.y - 10} textAnchor="middle" className="chart-line-value">
                        {formatValue(point.value)}
                      </text>
                    </g>
                  );
                })}
              </g>
            );
          })}
        </svg>
      </div>
    </div>
  );
}

function PieChart({ data }) {
  const safeData = (Array.isArray(data) ? data : []).filter((item) => Number(item?.count || 0) > 0);
  const total = safeData.reduce((sum, item) => sum + Number(item.count || 0), 0);

  if (total <= 0) {
    return <p className="feedback">Aucune donnée sur la répartition.</p>;
  }

  let angleCursor = -Math.PI / 2;
  const radius = 90;
  const center = 110;
  const slices = safeData.map((item) => {
    const share = Number(item.count || 0) / total;
    const nextAngle = angleCursor + share * Math.PI * 2;
    const x1 = center + Math.cos(angleCursor) * radius;
    const y1 = center + Math.sin(angleCursor) * radius;
    const x2 = center + Math.cos(nextAngle) * radius;
    const y2 = center + Math.sin(nextAngle) * radius;
    const largeArc = nextAngle - angleCursor > Math.PI ? 1 : 0;
    const meta = getStatusChartMeta(item.status);
    const path = `M ${center} ${center} L ${x1} ${y1} A ${radius} ${radius} 0 ${largeArc} 1 ${x2} ${y2} Z`;
    const slice = {
      key: item.status,
      path,
      color: meta.color,
      label: meta.label,
      count: Number(item.count || 0),
      share
    };
    angleCursor = nextAngle;
    return slice;
  });

  return (
    <div className="pie-chart-layout">
      <svg viewBox="0 0 220 220" className="pie-chart-svg" role="img" aria-label="Répartition des types d'utilisateur">
        {slices.map((slice) => (
          <path key={slice.key} d={slice.path} fill={slice.color} stroke="rgba(9, 17, 27, 0.88)" strokeWidth="2" />
        ))}
        <circle cx="110" cy="110" r="42" className="pie-chart-hole" />
      </svg>
      <div className="pie-chart-legend">
        {slices.map((slice) => (
          <div key={slice.key} className="pie-chart-legend-item">
            <span className="pie-chart-legend-dot" style={{ backgroundColor: slice.color }} />
            <span>{slice.label}</span>
            <strong>{slice.count}</strong>
            <span>{formatDecimalValue(slice.share * 100)}%</span>
          </div>
        ))}
      </div>
    </div>
  );
}

function HorizontalUsageChart({ data }) {
  const safeData = Array.isArray(data) ? data : [];
  const maxValue = safeData.reduce((current, item) => Math.max(current, Number(item?.count || 0)), 0);

  if (safeData.length === 0) {
    return <p className="feedback">Aucune donnée sur les portes.</p>;
  }

  return (
    <div className="usage-chart">
      {safeData.map((item) => {
        const count = Number(item?.count || 0);
        const width = maxValue > 0 ? (count / maxValue) * 100 : 0;
        return (
          <div key={item.door_name} className="usage-chart-row">
            <div className="usage-chart-head">
              <strong>{item.door_name}</strong>
              <span>{count} badges</span>
            </div>
            <div className="usage-chart-track">
              <div className="usage-chart-fill" style={{ width: `${width}%` }} />
            </div>
          </div>
        );
      })}
    </div>
  );
}

function AdminStatsView({ user, badgeDelaySeconds, onLogout, onToggleView, onNavigate }) {
  const initialFilters = readAdminStatsFiltersFromURL();
  const [statusFilters, setStatusFilters] = useState(initialFilters.statusFilters);
  const [restrictWindow, setRestrictWindow] = useState(initialFilters.restrictWindow);
  const [state, setState] = useState({ loading: true, error: "", payload: null });
  const [disabledWeekdayKeys, setDisabledWeekdayKeys] = useState(() => new Set());
  const requestRef = useRef(0);
  const abortRef = useRef(null);

  const activeStatuses = useMemo(() => getActiveStatusKeys(statusFilters), [statusFilters]);
  const activeStatusKey = activeStatuses.join(",");

  async function loadStats(
    currentStatuses = activeStatuses,
    currentRestrictWindow = restrictWindow,
    options = {}
  ) {
    const { background = false } = options;
    abortRef.current?.abort();
    const controller = new AbortController();
    abortRef.current = controller;
    const requestId = requestRef.current + 1;
    requestRef.current = requestId;
    if (!background) {
      setState((current) => ({ ...current, loading: true, error: "" }));
    } else {
      setState((current) => ({ ...current, error: "" }));
    }
    try {
      const query = new URLSearchParams();
      if (currentStatuses.length === 0) {
        query.append("status", "");
      } else {
        currentStatuses.forEach((status) => query.append("status", status));
      }
      query.set("restrict_window", String(currentRestrictWindow));
      const { response, json, text } = await requestJSON(`/api/admin/stats?${query.toString()}`, {
        signal: controller.signal
      });
      if (requestId !== requestRef.current) {
        return;
      }
      if (!response.ok) {
        throw new Error((json && json.message) || text || "Unable to load statistics.");
      }
      setState({
        loading: false,
        error: "",
        payload: json
      });
    } catch (loadError) {
      if (loadError instanceof Error && loadError.name === "AbortError") {
        return;
      }
      if (requestId !== requestRef.current) {
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
    const timer = window.setTimeout(() => {
      void loadStats(activeStatuses, restrictWindow, {
        background: Boolean(state.payload)
      });
    }, 120);
    return () => window.clearTimeout(timer);
  }, [activeStatusKey, restrictWindow]);

  useEffect(() => {
    return () => {
      abortRef.current?.abort();
    };
  }, []);

  useEffect(() => {
    const query = new URLSearchParams();
    activeStatuses.forEach((status) => query.append("status", status));
    query.set("restrict_window", String(restrictWindow));
    window.history.replaceState(window.history.state, "", `/admin/stats?${query.toString()}`);
  }, [activeStatusKey, restrictWindow]);

  useEffect(() => {
    let reloadTimer = 0;
    const unsubscribe = subscribeToLiveUpdates((event) => {
      if (!event?.type || (event.type !== "badge_received" && event.type !== "location_sessions_updated" && event.type !== "month_updated")) {
        return;
      }
      if (reloadTimer) {
        return;
      }
      reloadTimer = window.setTimeout(() => {
        reloadTimer = 0;
        void loadStats(activeStatuses, restrictWindow, { background: true });
      }, 1000);
    }, { scope: "admin" });
    return () => {
      if (reloadTimer) {
        window.clearTimeout(reloadTimer);
      }
      unsubscribe();
    };
  }, [activeStatusKey, restrictWindow]);

  function toggleStatusFilter(status) {
    setStatusFilters((current) => ({
      ...current,
      [status]: !current[status]
    }));
  }

  function toggleWeekdaySeries(weekdayKey) {
    setDisabledWeekdayKeys((current) => {
      const next = new Set(current);
      if (next.has(weekdayKey)) {
        next.delete(weekdayKey);
      } else {
        next.add(weekdayKey);
      }
      return next;
    });
  }

  return (
    <main className="app-shell">
      <AdminHeader
        user={user}
        badgeDelaySeconds={badgeDelaySeconds}
        onLogout={onLogout}
        onToggleView={onToggleView}
        activeSection="stats"
        onNavigate={onNavigate}
      />

      <section className="panel">
        <div className="panel-header">
          <div>
            <h2>Statistiques de présence</h2>
            <p className="panel-subtitle">Vue globale sur les 30 derniers jours, par type d’utilisateur et par plage horaire.</p>
          </div>
        </div>

        <div className="admin-filters admin-filters-stats">
          <div className="admin-filter-group">
            <div className="status-tile-group" role="group" aria-label="Filtre par statut">
              {ADMIN_STATUS_TILES.map((statusTile) => (
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

          <label className="admin-toggle-field" htmlFor="restrict-window-toggle">
            <input
              id="restrict-window-toggle"
              type="checkbox"
              checked={restrictWindow}
              onChange={(event) => setRestrictWindow(event.target.checked)}
            />
            <span>
              Limiter à {state.payload?.presence_window_start || "07:30"}-{state.payload?.presence_window_end || "20:30"}
            </span>
          </label>
        </div>

        {state.loading ? (
          <StatsDashboardSkeleton />
        ) : state.error ? (
          <p className="feedback feedback-error">{state.error}</p>
        ) : state.payload ? (
          <>
            <div className="stats-dashboard-grid">
              <StatsCard
                title="Présence moyenne par jour"
                subtitle="Nombre moyen d’utilisateurs distincts vus selon le jour de semaine."
              >
                <VerticalBarChart data={state.payload.average_seen_by_weekday} />
              </StatsCard>

              <StatsCard
                title="Présence moyenne par heure"
                subtitle="Nombre moyen d'utilisateurs présents sur chaque créneau horaire, comparé sur les 7 jours."
              >
                <MultiWeekdayLineChart
                  series={state.payload.average_presence_by_weekday}
                  disabledKeys={disabledWeekdayKeys}
                  onToggleSeries={toggleWeekdaySeries}
                />
              </StatsCard>

              <StatsCard
                title="Répartition des profils"
                subtitle="Distribution des utilisateurs inclus par les filtres actuels."
              >
                <PieChart data={state.payload.user_type_distribution} />
              </StatsCard>

              <StatsCard
                title="Utilisation des portes"
                subtitle="Comparaison du volume de badges par porte."
              >
                <HorizontalUsageChart data={state.payload.door_usage} />
              </StatsCard>
            </div>

            <div className="stats-summary-grid">
              <div className="stats-summary-tile">
                <span>Temps de présence moyen</span>
                <strong>{formatDuration(Number(state.payload.average_daily_presence_seconds || 0), "0s")}</strong>
                <p>Les journées sans venue sont exclues.</p>
              </div>
              <div className="stats-summary-tile">
                <span>Utilisateurs filtrés</span>
                <strong>{state.payload.filtered_user_count || 0}</strong>
                <p>Population prise en compte pour la page.</p>
              </div>
              <div className="stats-summary-tile">
                <span>Jours observés</span>
                <strong>{state.payload.observed_day_count || 0}</strong>
                <p>Basé sur les données actuellement stockées dans Watchdog.</p>
              </div>
            </div>
          </>
        ) : (
          <p className="feedback">Aucune statistique disponible.</p>
        )}
      </section>
    </main>
  );
}

function StatsDashboardSkeleton() {
  return (
    <>
      <div className="stats-dashboard-grid">
        {[0, 1, 2, 3].map((item) => (
          <div key={item} className="stats-card stats-card-skeleton">
            <SkeletonBlock className="skeleton-line skeleton-line-title" />
            <SkeletonBlock className="skeleton-line skeleton-line-wide" />
            <SkeletonBlock className="skeleton-chart-block" />
          </div>
        ))}
      </div>
      <div className="stats-summary-grid">
        {[0, 1, 2].map((item) => (
          <div key={item} className="stats-summary-tile">
            <SkeletonBlock className="skeleton-line skeleton-line-title" />
            <SkeletonBlock className="skeleton-line skeleton-line-number" />
            <SkeletonBlock className="skeleton-line skeleton-line-wide" />
          </div>
        ))}
      </div>
    </>
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
                <span className="presence-day-number-row">
                  <span className="presence-day-number">{cell.dayNumber}</span>
                  {cell.summary ? (
                    (() => {
                      if (cell.isFuture) {
                        return null;
                      }
                      const targetStatus = getPresenceTargetStatus(cell.summary);
                      return targetStatus ? (
                        <span
                          className={`presence-target-pill presence-target-pill-${targetStatus}`}
                          aria-label={targetStatus === "success" ? "Objectif atteint" : "Objectif non atteint"}
                          title={targetStatus === "success" ? "Objectif atteint" : "Objectif non atteint"}
                        />
                      ) : null;
                    })()
                  ) : null}
                </span>
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
    }, { scope: dayEndpointBase.startsWith("/api/admin/") ? "admin" : "user" });
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
  const expectedSeconds = typeof selectedDaySummary?.required_attendance_hours === "number"
    ? Math.round(selectedDaySummary.required_attendance_hours * 3600)
    : null;
  const actualSeconds = state.payload?.tracked && state.payload?.user
    ? Number(state.payload.user.duration_seconds || 0)
    : 0;
  const actualPresenceLabel = formatDurationPadded(actualSeconds, state.payload?.user?.duration_human || "0s");
  const expectedPresenceLabel = expectedSeconds == null
    ? "Non défini"
    : formatDurationPadded(expectedSeconds, `${expectedSeconds}s`);
  const isPresenceBelowExpected = expectedSeconds != null && actualSeconds < expectedSeconds;

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
              <KeyValue
                label="Heures présence"
                value={
                  <strong className="presence-hours-value">
                    <span className={isPresenceBelowExpected ? "presence-hours-actual-warning" : "presence-hours-actual"}>
                      {actualPresenceLabel}
                    </span>
                    <span className="presence-hours-separator"> / </span>
                    <span className="presence-hours-expected">{expectedPresenceLabel}</span>
                  </strong>
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
  const initialSelection = readAdminUserDetailSelectionFromURL();
  const [state, setState] = useState({ loading: true, error: "", payload: null });
  const [selectedDayKey, setSelectedDayKey] = useState(() => initialSelection.selectedDayKey);
  const [selectedMonthKey, setSelectedMonthKey] = useState(() => initialSelection.monthKey);
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
    const nextSelection = readAdminUserDetailSelectionFromURL();
    setSelectedDayKey(nextSelection.selectedDayKey);
    setSelectedMonthKey(nextSelection.monthKey);
  }, [login]);

  useEffect(() => {
    const query = new URLSearchParams(window.location.search);
    if (selectedMonthKey) {
      query.set("month", selectedMonthKey);
    } else {
      query.delete("month");
    }
    if (selectedDayKey) {
      query.set("date", selectedDayKey);
    } else {
      query.delete("date");
    }
    const nextURL = query.toString()
      ? `/admin/students/${encodeURIComponent(login)}?${query.toString()}`
      : `/admin/students/${encodeURIComponent(login)}`;
    window.history.replaceState(window.history.state, "", nextURL);
  }, [login, selectedMonthKey, selectedDayKey]);

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
    }, { scope: "admin" });
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
          <button className="secondary-button" type="button" onClick={() => onNavigate("/admin/students")}>
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
  const adminUserMatch = path.match(/^\/admin\/students\/([^/]+)$/);
  const adminUserLogin = adminUserMatch ? decodeURIComponent(adminUserMatch[1]) : "";
  const isAdminStudentsPath = path === "/admin/students";
  const isAdminStatsPath = path === "/admin/stats";
  const isAdminReportsPath = path === "/admin/reports";
  const isAdminPath = path === "/admin" || path.startsWith("/admin/");

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
    }, { scope: isAdminUser(authState.user) && adminViewMode === "admin" ? "admin" : "user" });
  }, [adminViewMode, authState.loading, authState.user]);

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
      const nextPath = isAdminUser(authState.user) ? "/admin/students" : "/me";
      window.history.replaceState({}, "", nextPath);
      setPath(nextPath);
      return;
    }
    if (authState.user && !isAdminUser(authState.user) && path !== "/me") {
      window.history.replaceState({}, "", "/me");
      setPath("/me");
      return;
    }
    if (authState.user && isAdminUser(authState.user) && adminViewMode === "admin" && (path === "/" || path === "/admin")) {
      window.history.replaceState({}, "", "/admin/students");
      setPath("/admin/students");
      return;
    }
    if (authState.user && isAdminUser(authState.user) && adminViewMode === "admin") {
      if (path === "/stats") {
        window.history.replaceState({}, "", "/admin/stats");
        setPath("/admin/stats");
        return;
      }
      if (path === "/reports") {
        window.history.replaceState({}, "", "/admin/reports");
        setPath("/admin/reports");
        return;
      }
    }
  }, [adminViewMode, authState.loading, authState.user, path]);

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
    setAdminViewMode((current) => {
      const nextMode = current === "admin" ? "student" : "admin";
      const nextPath = nextMode === "admin" ? "/admin/students" : "/me";
      if (`${window.location.pathname}${window.location.search}` !== nextPath) {
        window.history.pushState({}, "", nextPath);
        setPath(window.location.pathname);
      }
      return nextMode;
    });
  }

  function navigateTo(nextPath) {
    if (!nextPath) {
      return;
    }
    const currentFullPath = `${window.location.pathname}${window.location.search}`;
    if (nextPath === currentFullPath) {
      return;
    }
    window.history.pushState({}, "", nextPath);
    setPath(window.location.pathname);
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

  if (!isAdmin && path !== "/me") {
    return null;
  }

  if (isAdmin && adminViewMode === "admin" && (path === "/" || path === "/admin" || path === "/stats" || path === "/reports")) {
    return null;
  }

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

    if (isAdminStatsPath) {
      return (
        <AdminStatsView
          user={authState.user}
          badgeDelaySeconds={badgeDelaySeconds}
          onLogout={handleLogout}
          onToggleView={handleToggleAdminView}
          onNavigate={navigateTo}
        />
      );
    }

    if (isAdminStudentsPath) {
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

    if (isAdminPath || path === "/") {
      return null;
    }
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
