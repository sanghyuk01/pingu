"use strict";

const $ = (s, root = document) => root.querySelector(s);
const $$ = (s, root = document) => [...root.querySelectorAll(s)];
const paths = {
  moon: '<path d="M20.5 13A8.6 8.6 0 0 1 11 3.5 8.8 8.8 0 1 0 20.5 13Z"/>',
  sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M2 12h2M20 12h2M5 5l1.5 1.5M17.5 17.5 19 19M5 19l1.5-1.5M17.5 6.5 19 5"/>',
  dashboard:
    '<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/>',
  layers:
    '<rect x="4" y="3" width="16" height="13" rx="2"/><path d="M8 20h8M12 16v4M8 8h8M8 11h4"/>',
  book: '<path d="M12 5c-3-2-6-2-9-1v15c3-1 6-1 9 1 3-2 6-2 9-1V4c-3-1-6-1-9 1ZM12 5v15"/>',
  activity: '<path d="M2 12h5l3-8 4 16 3-8h5"/>',
  shield:
    '<path d="m12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6l8-3Z"/><path d="m8 12 3 3 5-6"/>',
  clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  help: '<circle cx="12" cy="12" r="9"/><path d="M9.5 9a2.5 2.5 0 0 1 5 .5c0 2-2.5 2-2.5 4M12 17h.01"/>',
  settings:
    '<path d="m9 3-.6 2-2 .9-2-.5-2 3.5 1.5 1.5v2.3L2.5 14l2 3.5 2-.5 2 .9L9 20h4l.6-2.1 2-.9 2 .5 2-3.5-1.5-1.3v-2.3l1.5-1.5-2-3.5-2 .5-2-.9L13 3H9Z"/><circle cx="11" cy="11.5" r="3"/>',
  search: '<circle cx="10" cy="10" r="6"/><path d="m15 15 5 5"/>',
  refresh:
    '<path d="M20 7v5h-5M4 17v-5h5"/><path d="M6 6a8 8 0 0 1 13 3M5 15a8 8 0 0 0 13 3"/>',
  sparkles:
    '<path d="m12 3 2.5 6.5L21 12l-6.5 2.5L12 21l-2.5-6.5L3 12l6.5-2.5L12 3ZM20 2v4M18 4h4"/>',
  feather:
    '<path d="M20 4c-7-3-17 4-15 11l3 3c7 1 14-8 12-14ZM3 21 15 9M8 16h6"/>',
  close: '<path d="m6 6 12 12M6 18 18 6"/>',
  send: '<path d="m21 3-7 18-4-7-7-4L21 3ZM10 14 21 3"/>',
  info: '<circle cx="12" cy="12" r="9"/><path d="M12 11v6M12 7h.01"/>',
  check: '<path d="m5 12 4 4L19 6"/>',
  globe:
    '<circle cx="12" cy="12" r="9"/><ellipse cx="12" cy="12" rx="4" ry="9"/><path d="M3 12h18"/>',
  stop: '<rect x="6" y="6" width="12" height="12" rx="2"/>',
  trash: '<path d="M3 6h18M9 6V3h6v3M5 6l1 15h12l1-15M10 10v7M14 10v7"/>',
  arrow: '<path d="M5 12h14m-6-6 6 6-6 6"/>',
};
function icon(name) {
  return `<svg class="ui-icon" viewBox="0 0 24 24" aria-hidden="true">${paths[name] || paths.activity}</svg>`;
}
function hydrate(root = document) {
  $$("[data-icon]", root).forEach((el) => {
    el.innerHTML = icon(el.dataset.icon);
  });
}
function esc(s) {
  return String(s ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
}
const n = (v) => Number(v || 0).toLocaleString("ko-KR");
const ms = (v) =>
  v < 10 ? v.toFixed(1) : Math.round(v).toLocaleString("ko-KR");
const labels = {
  running: "실행 중",
  completed: "완료",
  stopped: "중지됨",
  interrupted: "재시작으로 중단",
};
const state = {
  jobs: [],
  recent: [],
  filter: "all",
  search: "",
  online: false,
  status: null,
  detail: null,
  detailJob: null,
  detailPage: 0,
  token: sessionStorage.getItem("pingu-token") || "",
  timer: null,
  toastTimer: null,
  confirm: null,
  refreshing: false,
};
function statusBadge(s) {
  return `<span class="status ${esc(s)}"><i></i>${labels[s] || esc(s)}</span>`;
}
function aggregate(targets) {
  return targets.reduce(
    (a, t) => ({
      count: a.count + t.count,
      success: a.success + t.success,
      total: a.total + t.total_ms,
      skipped: a.skipped + t.skipped,
    }),
    { count: 0, success: 0, total: 0, skipped: 0 },
  );
}
function duration(seconds) {
  if (seconds < 60) return `${seconds}초`;
  if (seconds % 3600 === 0) return `${seconds / 3600}시간`;
  if (seconds % 60 === 0) return `${seconds / 60}분`;
  return `${seconds}초`;
}
function remaining(j) {
  if (j.status !== "running") return "—";
  const s = Math.max(0, Math.ceil((new Date(j.ends_at) - Date.now()) / 1000));
  const h = Math.floor(s / 3600),
    m = Math.floor((s % 3600) / 60);
  return h ? `${h}시간 ${m}분` : m ? `${m}분 ${s % 60}초` : `${s}초`;
}
function timeLabel(v) {
  return new Date(v).toLocaleTimeString("ko-KR", {
    hour12: false,
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}
function toast(message) {
  clearTimeout(state.toastTimer);
  $("#toast").textContent = message;
  $("#toast").classList.add("show");
  state.toastTimer = setTimeout(
    () => $("#toast").classList.remove("show"),
    3500,
  );
}

async function api(path, options = {}) {
  const headers = { ...options.headers };
  if (state.token) headers.Authorization = `Bearer ${state.token}`;
  if (options.body) headers["Content-Type"] = "application/json";
  let response;
  try {
    response = await fetch(path, {
      ...options,
      headers,
      signal: AbortSignal.timeout(12000),
    });
  } catch {
    throw new Error("서버와 연결할 수 없어요. 잠시 후 다시 시도해 주세요.");
  }
  if (response.status === 401) {
    state.online = false;
    connection();
    if (!$$("dialog[open]").length) openSettings();
    const error = new Error("서버 접속 토큰을 확인해 주세요.");
    error.auth = true;
    throw error;
  }
  if (!response.ok) {
    const data = await response.json().catch(() => ({}));
    throw new Error(data.error || "요청을 처리할 수 없어요.");
  }
  return response;
}
function connection() {
  const el = $("#connection");
  el.className = `connection ${state.online ? "online" : "offline"}`;
  el.innerHTML = `<i></i>${state.online ? "서버 연결됨" : "서버 연결 끊김"}`;
  $(".live-pill").textContent = state.online ? "LIVE" : "OFFLINE";
}
async function refresh() {
  if (state.refreshing || document.hidden) return;
  state.refreshing = true;
  clearTimeout(state.timer);
  try {
    const data = await (await api("/api/jobs")).json();
    state.jobs = data.jobs;
    state.recent = data.recent || [];
    state.online = true;
    if (!state.status) {
      try {
        state.status = await (await api("/api/status")).json();
      } catch {}
    }
    if ($("#detail-dialog").open && state.detail) {
      if (state.jobs.some((j) => j.id === state.detail)) {
        state.detailJob = await (await api(`/api/jobs/${state.detail}`)).json();
      } else {
        $("#detail-dialog").close();
        state.detailJob = null;
      }
    }
    render();
  } catch (error) {
    state.online = false;
    if (!error.auth && !state.jobs.length)
      $("#table-caption").textContent = error.message;
  } finally {
    state.refreshing = false;
    connection();
    state.timer = setTimeout(
      refresh,
      state.online && state.jobs.some((j) => j.status === "running")
        ? 2000
        : 10000,
    );
  }
}
function samples() {
  return state.recent;
}
function render() {
  const running = state.jobs.filter((j) => j.status === "running");
  const all = aggregate(state.jobs.flatMap((j) => j.targets));
  $("#stat-running").textContent = running.length;
  $("#stat-targets").textContent =
    `${n(running.reduce((a, j) => a + j.targets.length, 0))}개 대상의 안부를 확인해요`;
  $("#stat-checks").textContent = n(all.count);
  $("#stat-rate").textContent = all.count
    ? ((all.success / all.count) * 100).toFixed(1)
    : "—";
  $("#stat-latency").textContent = all.success
    ? ms(all.total / all.success)
    : "—";
  $("#stat-failed").textContent = all.count
    ? `실패 ${n(all.count - all.success)}회 · 검사 생략 ${n(all.skipped)}회`
    : "첫 응답을 기다리고 있어요";
  $("#job-count").textContent = state.jobs.length;
  $("#nav-count").textContent = state.jobs.length;
  $("#filter-all").textContent = state.jobs.length;
  $("#filter-running").textContent = running.length;
  $("#filter-finished").textContent = state.jobs.length - running.length;
  renderChart(samples());
  renderJournal();
  renderJobs();
  if ($("#detail-dialog").open) renderDetail();
}
function renderChart(results) {
  const points = results.filter((r) => r.ok).reverse();
  const width = 600,
    height = 160,
    left = 32,
    right = 8,
    top = 12,
    bottom = 25;
  const max = points.length
    ? Math.max(
        10,
        Math.ceil(Math.max(...points.map((r) => r.latency)) / 10) * 10,
      )
    : 100;
  const grid = [0, 1, 2, 3]
    .map((i) => {
      const y = top + ((height - top - bottom) * i) / 3;
      return `<line x1="${left}" x2="${width - right}" y1="${y}" y2="${y}" stroke="#edf2f5" stroke-dasharray="3 4"/><text x="0" y="${y + 3}">${Math.round((max * (3 - i)) / 3)}</text>`;
    })
    .join("");
  const coords = points.map((r, i) => [
    left +
      (width - left - right) *
        (points.length > 1 ? i / (points.length - 1) : 0.5),
    top + (height - top - bottom) * (1 - r.latency / max),
  ]);
  const line = coords
    .map(([x, y], i) => `${i ? "L" : "M"}${x.toFixed(1)},${y.toFixed(1)}`)
    .join(" ");
  const area =
    coords.length > 1
      ? `${line} L${coords.at(-1)[0]},${height - bottom} L${coords[0][0]},${height - bottom} Z`
      : "";
  const ticks = points.length
    ? `<text x="${left}" y="${height - 4}">${timeLabel(points[0].at)}</text><text text-anchor="end" x="${width - right}" y="${height - 4}">${timeLabel(points.at(-1).at)}</text>`
    : `<text x="${left}" y="${height - 4}">검사 시작</text><text text-anchor="end" x="${width - right}" y="${height - 4}">지금</text>`;
  $("#chart").innerHTML =
    `<svg viewBox="0 0 ${width} ${height}" preserveAspectRatio="none" role="img" aria-label="최근 성공한 검사 응답시간 그래프"><defs><linearGradient id="chart-fill" x1="0" y1="0" x2="0" y2="1"><stop stop-color="#9fc4d0" stop-opacity=".28"/><stop offset="1" stop-color="#9fc4d0" stop-opacity=".01"/></linearGradient></defs>${grid}${area ? `<path d="${area}" fill="url(#chart-fill)"/>` : ""}${line ? `<path d="${line}" fill="none" stroke="#82adbd" stroke-width="2" stroke-linejoin="round" vector-effect="non-scaling-stroke"/>${coords.map(([x, y], i) => (i === coords.length - 1 ? `<circle cx="${x}" cy="${y}" r="3" fill="#82adbd" stroke="white" stroke-width="2"/>` : "")).join("")}` : ""}${ticks}</svg>${points.length ? "" : `<div class="chart-empty"><span data-icon="activity"></span>${results.length ? "아직 성공한 응답이 없어요" : "첫 번째 핑을 기다리고 있어요"}</div>`}`;
  hydrate($("#chart"));
}
function renderJournal() {
  const results = samples().slice(0, 4);
  $("#journal").innerHTML = results.length
    ? `<div class="journal-list">${results.map((r) => `<div class="journal-item"><span class="result-icon ${r.ok ? "" : "bad"}">${icon(r.ok ? "check" : "info")}</span><div><strong title="${esc(r.address)}">${esc(r.address.replace(/^https?:\/\//, ""))}</strong><p>${r.ok ? `응답 도착 · ${ms(r.latency)} ms` : esc(r.error || "응답 없음")}</p></div><time>${timeLabel(r.at)}</time></div>`).join("")}</div>`
    : `<div class="journal-empty"><div class="empty-bubble">${icon("send")}</div>아직 도착한 소식이 없어요<p>첫 작업을 시작하면 연결 소식이 쌓여요.</p></div>`;
}
function renderJobs() {
  const query = state.search.toLowerCase();
  const jobs = state.jobs.filter(
    (j) =>
      (state.filter === "all" ||
        (state.filter === "running"
          ? j.status === "running"
          : j.status !== "running")) &&
      (!query ||
        j.spec.name.toLowerCase().includes(query) ||
        j.targets.some((t) => t.address.toLowerCase().includes(query))),
  );
  $("#jobs-body").innerHTML = jobs
    .map((j) => {
      const a = aggregate(j.targets),
        rate = a.count ? ((a.success / a.count) * 100).toFixed(1) : null;
      return `<tr><td><button class="job-name" data-detail="${j.id}"><span class="job-type-icon">${icon(j.targets.some((t) => t.kind === "http") ? "globe" : "activity")}</span><span><strong>${esc(j.spec.name)}</strong><small>${esc(j.targets[0].address)}${j.targets.length > 1 ? ` 외 ${j.targets.length - 1}개` : ""}</small></span></button></td><td>${duration(j.spec.interval)}마다</td><td class="${rate !== null && Number(rate) < 95 ? "failure-text" : "success-text"}">${rate === null ? "—" : `${rate}%`}<small class="cell-checks">${n(a.count)}회 검사</small></td><td>${remaining(j)}</td><td>${statusBadge(j.status)}</td><td><div class="row-actions">${j.status === "running" ? `<button class="icon-button" data-stop="${j.id}" title="작업 중지" aria-label="${esc(j.spec.name)} 중지">${icon("stop")}</button>` : `<button class="icon-button" data-delete="${j.id}" title="작업 삭제" aria-label="${esc(j.spec.name)} 삭제">${icon("trash")}</button>`}<button class="icon-button" data-detail="${j.id}" title="상세 결과" aria-label="${esc(j.spec.name)} 상세 결과">${icon("arrow")}</button></div></td></tr>`;
    })
    .join("");
  $("#jobs-empty").innerHTML = jobs.length
    ? ""
    : state.jobs.length
      ? '<div class="empty-jobs"><h3>조건에 맞는 작업이 없어요</h3><p>다른 검색어나 필터로 찾아보세요.</p></div>'
      : `<div class="empty-jobs"><img src="/penguin.svg" alt="첫 탐험을 기다리는 핑구"><h3>아직 등록된 작업이 없어요</h3><p>첫 작업을 만들고, 연결의 안부를 물어보세요.</p><button class="button subtle" data-create>${icon("plus")}첫 번째 작업 만들기</button></div>`;
  if (state.online)
    $("#table-caption").textContent =
      `${jobs.length}개 작업 표시 · 전체 ${state.jobs.reduce((a, j) => a + j.targets.length, 0)}개 대상`;
}
async function openDetail(id) {
  try {
    const job = await (await api(`/api/jobs/${id}`)).json();
    state.detail = id;
    state.detailJob = job;
    state.detailPage = 0;
    renderDetail();
    openModal("#detail-dialog");
  } catch (e) {
    toast(e.message);
  }
}
function renderDetail() {
  const j = state.detailJob;
  if (!j) {
    $("#detail-dialog").close();
    return;
  }
  const a = aggregate(j.targets);
  const visible = j.targets.slice(
    state.detailPage * 100,
    (state.detailPage + 1) * 100,
  );
  const root = $("#detail-content");
  const scroll = $(".detail-targets", root)?.scrollTop || 0;
  root.innerHTML = `<div class="modal-heading"><span class="eyebrow">A CLOSER LOOK</span><button class="icon-button" data-close aria-label="닫기">${icon("close")}</button></div><h2>${esc(j.spec.name)}</h2><div class="detail-status-line">${statusBadge(j.status)}<span>${duration(j.spec.interval)}마다 · ${duration(j.spec.duration)} 동안 · 제한시간 ${j.spec.timeout}초</span></div><div class="detail-summary"><div>검사 횟수<strong>${n(a.count)}회</strong></div><div>응답 성공률<strong>${a.count ? ((a.success / a.count) * 100).toFixed(1) + "%" : "—"}</strong></div><div>평균 응답시간<strong>${a.success ? ms(a.total / a.success) + " ms" : "—"}</strong></div><div>검사 생략<strong>${n(a.skipped)}회</strong></div></div><div class="detail-targets">${visible
    .map((t) => {
      const last = t.history.at(-1),
        max = Math.max(1, ...t.history.map((r) => r.latency));
      return `<article class="target-card"><div class="target-card-heading"><strong><span class="kind">${t.kind.toUpperCase()}</span>${esc(t.address)}</strong><span class="result-icon ${last && !last.ok ? "bad" : ""}">${icon(last ? (last.ok ? "check" : "info") : "clock")}</span></div><div class="target-metrics"><span>성공<b>${n(t.success)}/${n(t.count)}</b></span><span>최소<b>${t.success ? ms(t.min_ms) : "—"} ms</b></span><span>평균<b>${t.success ? ms(t.total_ms / t.success) : "—"} ms</b></span><span>최대<b>${t.success ? ms(t.max_ms) : "—"} ms</b></span><span>생략<b>${n(t.skipped)}</b></span></div><svg class="history-svg" viewBox="0 0 600 36" preserveAspectRatio="none" role="img" aria-label="최근 검사 결과, 주황색은 실패">${t.history
        .map((r, i) => {
          const h = r.ok ? Math.max(3, (r.latency / max) * 32) : 6;
          return `<rect x="${i * 10}" y="${36 - h}" width="7" height="${h}" rx="1.5" fill="${r.ok ? "#98bdc9" : "#e6af95"}"><title>${timeLabel(r.at)} · ${r.ok ? `${ms(r.latency)} ms` : esc(r.error)}</title></rect>`;
        })
        .join(
          "",
        )}</svg><div class="history-note"><span>최근 ${t.history.length}건 · 최대 60건 보관</span><span>${last ? timeLabel(last.at) : "첫 검사 대기 중"}</span></div>${last && last.error ? `<p class="target-error">${esc(last.error)}</p>` : ""}</article>`;
    })
    .join(
      "",
    )}</div>${j.targets.length > 100 ? `<p class="detail-page-info"></p>` : ""}<div class="modal-footer"><div class="detail-actions">${j.status === "running" ? `<button class="button subtle small" data-stop="${j.id}">${icon("stop")}중지</button>` : `<button class="button subtle small" data-delete="${j.id}">${icon("trash")}삭제</button>`}</div><button class="button dark small" data-close>닫기</button></div>`;
  if (j.targets.length > 100) {
    $(".detail-page-info", root).innerHTML =
      `<span>${state.detailPage * 100 + 1}–${Math.min(j.targets.length, (state.detailPage + 1) * 100)} / ${j.targets.length}개 대상</span><span><button class="button subtle small" data-page="-1" ${state.detailPage === 0 ? "disabled" : ""}>이전</button> <button class="button subtle small" data-page="1" ${(state.detailPage + 1) * 100 >= j.targets.length ? "disabled" : ""}>다음</button></span>`;
  }
  $(".detail-targets", root).scrollTop = scroll;
}
function openModal(id) {
  const d = $(id);
  if (!d.open) d.showModal();
}
function openSettings() {
  $("#token-input").value = state.token;
  $("#token-error").textContent = "";
  $("#server-info").innerHTML =
    `<strong>${esc(location.origin)}</strong><br>저장 방식: 메모리 + 작업 정의 JSON<br>ICMP IPv4: ${state.status ? (state.status.icmp.ipv4 ? "사용 가능" : "권한 확인 필요") : "연결 후 확인"} · IPv6: ${state.status ? (state.status.icmp.ipv6 ? "사용 가능" : "권한 확인 필요") : "연결 후 확인"}<br>버전: ${state.status ? esc(state.status.version) : "—"}`;
  openModal("#settings-dialog");
}
function formSpec() {
  const f = new FormData($("#create-form"));
  return {
    name: String(f.get("name")).trim(),
    addresses: [
      ...new Set(
        String(f.get("addresses"))
          .split(/\r?\n/)
          .map((v) => v.trim())
          .filter(Boolean),
      ),
    ],
    interval: Number(f.get("interval")) * Number(f.get("interval_unit")),
    duration: Number(f.get("duration")) * Number(f.get("duration_unit")),
    timeout: Number(f.get("timeout")),
  };
}
function formPreview() {
  const s = formSpec(),
    rate = s.addresses.length / s.interval,
    active = state.jobs
      .filter((j) => j.status === "running")
      .reduce((a, j) => a + j.targets.length / j.spec.interval, 0);
  $("#target-count").textContent = `${s.addresses.length}개 대상`;
  const invalid =
    rate + active > 20 ||
    s.addresses.length > 1000 ||
    s.duration > 86400 ||
    s.interval > 86400;
  $(".form-summary").classList.toggle("invalid", invalid);
  $("#rate-preview").textContent = invalid
    ? "설정을 조금 줄여 주세요. 서버 한도를 초과해요."
    : s.addresses.length
      ? `초당 약 ${Number.isFinite(rate) ? rate.toFixed(2) : "—"}회 · 실행 중인 작업 포함 ${(rate + active).toFixed(2)}회`
      : "가볍게 시작할 준비가 됐어요";
}
function confirmAction(id, remove) {
  const j = state.jobs.find((j) => j.id === id);
  if (!j) return;
  $("#confirm-title").textContent = remove
    ? "이 작업을 삭제할까요?"
    : "잠깐, 핑을 멈출까요?";
  $("#confirm-copy").textContent = remove
    ? `“${j.spec.name}”의 작업 정의와 메모리에 보관된 결과가 삭제돼요.`
    : `“${j.spec.name}”의 검사를 중지해요. 지금까지의 결과는 유지돼요.`;
  $("#confirm-action").textContent = remove ? "삭제하기" : "검사 중지";
  state.confirm = { id, remove };
  openModal("#confirm-dialog");
}

document.addEventListener("click", (event) => {
  const button = event.target.closest("button");
  if (!button) return;
  if (button.hasAttribute("data-create")) {
    $("#form-error").textContent = "";
    formPreview();
    openModal("#create-dialog");
  }
  if (button.hasAttribute("data-close")) button.closest("dialog").close();
  if (button.dataset.nav === "guide" || button.id === "help")
    openModal("#guide-dialog");
  if (button.id === "settings") openSettings();
  if (button.dataset.nav === "jobs")
    $("#jobs-section").scrollIntoView({ behavior: "smooth", block: "start" });
  if (button.dataset.nav === "dashboard")
    window.scrollTo({ top: 0, behavior: "smooth" });
  if (button.dataset.filter) {
    state.filter = button.dataset.filter;
    $$("[data-filter]").forEach((b) =>
      b.classList.toggle("selected", b === button),
    );
    renderJobs();
  }
  if (button.dataset.detail) {
    openDetail(button.dataset.detail);
  }
  if (button.dataset.stop) confirmAction(button.dataset.stop, false);
  if (button.dataset.delete) confirmAction(button.dataset.delete, true);
  if (button.dataset.page) {
    state.detailPage += Number(button.dataset.page);
    renderDetail();
    $(".detail-targets").scrollTop = 0;
  }
});
$("#search").addEventListener("input", (e) => {
  state.search = e.target.value;
  renderJobs();
});
$("#create-form").addEventListener("input", formPreview);
$("#create-form").addEventListener("change", formPreview);
$("#create-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const button = $("#submit-job");
  if (button.disabled) return;
  const spec = formSpec();
  $("#form-error").textContent = "";
  button.disabled = true;
  try {
    await api("/api/jobs", { method: "POST", body: JSON.stringify(spec) });
    $("#create-dialog").close();
    $("#create-form").reset();
    state.filter = "all";
    state.search = "";
    $("#search").value = "";
    $$("[data-filter]").forEach((b) =>
      b.classList.toggle("selected", b.dataset.filter === "all"),
    );
    await refresh();
    toast("핑구가 출발했어요! 연결을 살펴볼게요.");
  } catch (e) {
    $("#form-error").textContent = e.message;
    if (e.auth) {
      $("#create-dialog").close();
      openSettings();
    }
  } finally {
    button.disabled = false;
  }
});
$("#confirm-action").addEventListener("click", async () => {
  if (!state.confirm) return;
  const { id, remove } = state.confirm;
  const b = $("#confirm-action");
  b.disabled = true;
  try {
    await api(`/api/jobs/${id}${remove ? "" : "/stop"}`, {
      method: remove ? "DELETE" : "POST",
    });
    $("#confirm-dialog").close();
    state.confirm = null;
    await refresh();
    toast(
      remove ? "작업을 삭제했어요." : "검사를 멈췄어요. 결과는 보관할게요.",
    );
  } catch (e) {
    toast(e.message);
  } finally {
    b.disabled = false;
  }
});
$("#token-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const b = $('#token-form button[type="submit"]');
  b.disabled = true;
  state.token = $("#token-input").value.trim();
  $("#token-error").textContent = "";
  try {
    state.status = await (await api("/api/status")).json();
    sessionStorage.setItem("pingu-token", state.token);
    $("#settings-dialog").close();
    await refresh();
    toast("서버에 연결했어요.");
  } catch (e) {
    $("#token-error").textContent = e.message;
  } finally {
    b.disabled = false;
  }
});
document.addEventListener("visibilitychange", () => {
  if (!document.hidden) refresh();
  else clearTimeout(state.timer);
});
function updateThemeControl() {
  const dark = document.documentElement.dataset.theme === "dark";
  const button = $("#theme-toggle");
  const label = dark ? "라이트모드 켜기" : "다크모드 켜기";
  button.innerHTML = icon(dark ? "sun" : "moon");
  button.setAttribute("aria-label", label);
  button.setAttribute("aria-pressed", String(dark));
  button.title = label;
  $('meta[name="theme-color"]').content = dark ? "#111c26" : "#f5f8f9";
}
$("#theme-toggle").addEventListener("click", () => {
  const theme =
    document.documentElement.dataset.theme === "dark" ? "light" : "dark";
  document.documentElement.dataset.theme = theme;
  try {
    localStorage.setItem("pingu-theme", theme);
  } catch {}
  updateThemeControl();
});
window.addEventListener("storage", (event) => {
  if (event.key === "pingu-theme") {
    document.documentElement.dataset.theme =
      event.newValue === "dark" ||
      (!event.newValue && matchMedia("(prefers-color-scheme: dark)").matches)
        ? "dark"
        : "light";
    updateThemeControl();
  }
});
hydrate();
updateThemeControl();
render();
refresh();
