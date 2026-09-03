"use strict";

const $ = (id) => document.getElementById(id);
const TOKEN_KEY = "ocn_admin_token";
let token = localStorage.getItem(TOKEN_KEY) || "";
let currentUser = "";

function show(id) {
  document.querySelectorAll("#app-view section").forEach((s) => s.classList.add("hidden"));
  $(id).classList.remove("hidden");
}

async function api(path, opts = {}) {
  const headers = { ...(opts.headers || {}) };
  if (token) headers["Authorization"] = "Bearer " + token;
  if (opts.body && !headers["Content-Type"]) headers["Content-Type"] = "application/json";
  const res = await fetch(path, { ...opts, headers });
  if (res.status === 401) {
    logout();
    throw new Error("Session expired");
  }
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || res.statusText);
  return data;
}

function logout() {
  if (token) api("/api/logout", { method: "POST" }).catch(() => {});
  token = "";
  localStorage.removeItem(TOKEN_KEY);
  $("app-view").classList.add("hidden");
  $("login-view").classList.remove("hidden");
}

function esc(s) {
  return String(s == null ? "" : s)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

function fmtTime(sec) {
  if (!sec) return "never";
  return new Date(sec * 1000).toLocaleString();
}

async function boot() {
  if (token) {
    try {
      const me = await api("/api/me");
      currentUser = me.username;
      $("nav-user").textContent = me.username;
      enterApp(me.must_change);
      return;
    } catch (e) {
      /* fall through to login */
    }
  }
  $("login-view").classList.remove("hidden");
}

function enterApp(mustChange) {
  token = token; // persisted above
  $("login-view").classList.add("hidden");
  $("app-view").classList.remove("hidden");
  $("nav-user").textContent = currentUser;
  $("must-change-banner").classList.toggle("hidden", !mustChange);
  if (mustChange) {
    show("page-account");
  } else {
    show("page-dashboard");
  }
  loadDashboard();
}

/* Navigation */
document.querySelectorAll(".sidebar nav a").forEach((a) => {
  a.addEventListener("click", () => {
    document.querySelectorAll(".sidebar nav a").forEach((x) => x.classList.remove("active"));
    a.classList.add("active");
    show("page-" + a.dataset.page);
    if (a.dataset.page === "dashboard") loadDashboard();
    if (a.dataset.page === "lines") loadLines();
    if (a.dataset.page === "provision") loadProvisions();
    if (a.dataset.page === "federation") loadFederation();
  });
});
$("logout-link").addEventListener("click", logout);

/* Login */
$("login-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  $("login-error").classList.add("hidden");
  try {
    const res = await fetch("/api/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        username: $("login-username").value.trim(),
        password: $("login-password").value,
      }),
    });
    const data = await res.json();
    if (!res.ok) throw new Error(data.error || "login failed");
    token = data.token;
    localStorage.setItem(TOKEN_KEY, token);
    currentUser = data.username;
    $("login-password").value = "";
    enterApp(data.must_change);
  } catch (err) {
    $("login-error").textContent = err.message;
    $("login-error").classList.remove("hidden");
  }
});

/* Dashboard */
async function loadDashboard() {
  try {
    const s = await api("/api/stats");
    const cards = [
      ["Lines", s.lines_total],
      ["Online now", s.lines_online],
      ["Free numbers (est.)", s.free_estimate],
      ["Codes issued", s.tokens_issued],
      ["Codes used", s.tokens_used],
    ];
    $("stat-cards").innerHTML = cards
      .map(([lbl, n]) => `<div class="card"><div class="num">${n}</div><div class="lbl">${esc(lbl)}</div></div>`)
      .join("");
  } catch (e) {
    /* token may be stale */
  }
}

/* Lines */
async function loadLines() {
  const search = encodeURIComponent($("lines-search").value.trim());
  try {
    const d = await api(`/api/lines?search=${search}&limit=200`);
    const body = $("lines-body");
    $("lines-empty").classList.toggle("hidden", d.lines.length > 0);
    body.innerHTML = d.lines
      .map((l) => {
        const status =
          l.online
            ? '<span class="badge online">online</span>'
            : '<span class="badge offline">offline</span>';
        return `<tr>
          <td>${esc(l.display_number)}</td>
          <td>${esc(l.display_name) || "—"}</td>
          <td>${status}</td>
          <td>${l.fcm ? "yes" : "no"}</td>
          <td>${fmtTime(l.last_seen)}</td>
          <td>
            <button class="ghost" data-act="edit" data-number="${esc(l.number)}" data-name="${esc(l.display_name)}">Edit</button>
            <button class="ghost danger" data-act="release" data-number="${esc(l.number)}">Release</button>
          </td>
        </tr>`;
      })
      .join("");
    body.querySelectorAll("[data-act='edit']").forEach((b) =>
      b.addEventListener("click", () => editLine(b.dataset.number, b.dataset.name))
    );
    body.querySelectorAll("[data-act='release']").forEach((b) =>
      b.addEventListener("click", () => releaseLine(b.dataset.number))
    );
  } catch (e) {
    /* ignore */
  }
}
$("lines-refresh").addEventListener("click", loadLines);
$("lines-search").addEventListener("keydown", (e) => { if (e.key === "Enter") loadLines(); });

async function editLine(number, name) {
  const newName = prompt(`Display name for ${number}`, name || "");
  if (newName === null) return;
  const changeNum = prompt("Change number too? Leave as-is or enter a new free number:", "");
  if (changeNum === null) return;
  try {
    await api(`/api/lines/${encodeURIComponent(number)}`, {
      method: "PUT",
      body: JSON.stringify({
        display_name: newName,
        number: changeNum && changeNum.trim() ? changeNum.trim().replace(/-/g, "") : undefined,
      }),
    });
    loadLines();
  } catch (e) {
    alert(e.message);
  }
}

async function releaseLine(number) {
  if (!confirm(`Release line ${number}? The number will become available again.`)) return;
  try {
    await api(`/api/lines/${encodeURIComponent(number)}`, { method: "DELETE" });
    loadLines();
  } catch (e) {
    alert(e.message);
  }
}

/* Provision */
$("prov-suggest").addEventListener("click", async () => {
  try {
    const d = await api("/api/numbers/free?count=8");
    $("prov-number").value = d.numbers[0] || "";
  } catch (e) { /* ignore */ }
});

$("provision-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  $("prov-error").classList.add("hidden");
  $("prov-result").classList.add("hidden");
  const number = $("prov-number").value.trim().replace(/-/g, "");
  const hours = parseInt($("prov-hours").value, 10) || 24;
  try {
    const d = await api("/api/provisions", {
      method: "POST",
      body: JSON.stringify({
        number: number || "",
        display_name: $("prov-name").value.trim(),
        notes: $("prov-notes").value.trim(),
        valid_hours: hours,
      }),
    });
    $("prov-qr").src = d.qr_data;
    $("prov-num").textContent = d.number || "(auto-assign on first connect)";
    $("prov-expires").textContent = new Date(d.expires_at).toLocaleString();
    $("prov-url").value = d.url;
    $("prov-result").classList.remove("hidden");
    $("prov-name").value = "";
    $("prov-notes").value = "";
    $("prov-number").value = "";
    loadProvisions();
  } catch (err) {
    $("prov-error").textContent = err.message;
    $("prov-error").classList.remove("hidden");
  }
});

$("prov-copy").addEventListener("click", async () => {
  try {
    await navigator.clipboard.writeText($("prov-url").value);
    $("prov-copy").textContent = "Copied!";
    setTimeout(() => ($("prov-copy").textContent = "Copy URL"), 1500);
  } catch (e) {
    $("prov-url").select();
    document.execCommand("copy");
  }
});
$("prov-close").addEventListener("click", () => $("prov-result").classList.add("hidden"));

async function loadProvisions() {
  try {
    const d = await api("/api/provisions?limit=100");
    const body = $("prov-list");
    body.innerHTML = d.tokens
      .map((t) => {
        const badge = `<span class="badge ${esc(t.status)}">${esc(t.status)}</span>`;
        const revoke =
          t.status === "issued"
            ? `<button class="ghost danger" data-id="${esc(t.token_hash)}">Revoke</button>`
            : "";
        return `<tr>
          <td>${t.number || "auto"}</td>
          <td>${esc(t.display_name) || "—"}</td>
          <td>${badge}</td>
          <td>${fmtTime(t.created_at)}</td>
          <td>${t.expires_at ? fmtTime(Math.floor(new Date(t.expires_at).getTime() / 1000)) : "—"}</td>
          <td>${revoke}</td>
        </tr>`;
      })
      .join("");
    body.querySelectorAll("[data-id]").forEach((b) =>
      b.addEventListener("click", async () => {
        if (!confirm("Revoke this provisioning code? It will stop working.")) return;
        try {
          await api(`/api/provisions/${encodeURIComponent(b.dataset.id)}/revoke`, { method: "POST" });
          loadProvisions();
        } catch (e) { alert(e.message); }
      })
    );
  } catch (e) { /* ignore */ }
}

/* Federation */
async function loadFederation() {
  try {
    const d = await api("/api/federation/status");
    const area = d.area_code || d.server_area_code || "—";
    const rows = d.configured
      ? [
          ["Registry", d.registry_address],
          ["Insecure", d.registry_insecure ? "yes (dev)" : "no"],
          ["Requested area code", d.requested_area_code || "(auto)"],
          ["Federation public address", d.federation_public_address || "—"],
          ["Assigned area code", area],
        ]
      : [["Status", "Standalone — not federated"], ["Assigned area code", area]];
    $("fed-status").innerHTML =
      "<h3>Current status</h3>" +
      rows.map(([k, v]) => `<p><strong>${esc(k)}:</strong> ${esc(v)}</p>`).join("") +
      (d.configured
        ? '<p class="muted small">Restart the server to apply. Settings are stored and re-applied on startup.</p>'
        : '<p class="muted small">Register below to join the network and receive an area code.</p>');
  } catch (e) {
    $("fed-status").innerHTML = '<p class="error">' + esc(e.message) + "</p>";
  }
}

$("fed-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  $("fed-error").classList.add("hidden");
  $("fed-ok").classList.add("hidden");
  try {
    const d = await api("/api/federation/register", {
      method: "POST",
      body: JSON.stringify({
        registry_address: $("fed-address").value.trim(),
        federation_public_address: $("fed-public").value.trim(),
        requested_area_code: $("fed-area").value.trim(),
        registry_insecure: $("fed-insecure").checked,
      }),
    });
    $("fed-ok").textContent =
      "Registered with area code " + d.area_code + ". " + d.message;
    $("fed-ok").classList.remove("hidden");
    loadFederation();
  } catch (err) {
    $("fed-error").textContent = err.message;
    $("fed-error").classList.remove("hidden");
  }
});

$("fed-clear").addEventListener("click", async () => {
  if (!confirm("Clear federation settings? The server will run standalone after restart.")) return;
  try {
    const d = await api("/api/federation/clear", { method: "POST" });
    $("fed-ok").textContent = d.message;
    $("fed-ok").classList.remove("hidden");
    $("fed-error").classList.add("hidden");
    loadFederation();
  } catch (e) {
    $("fed-error").textContent = e.message;
    $("fed-error").classList.remove("hidden");
  }
});

/* Account */
$("password-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  $("pw-error").classList.add("hidden");
  $("pw-ok").classList.add("hidden");
  const oldpw = $("pw-old").value;
  const newpw = $("pw-new").value;
  const confirmpw = $("pw-confirm").value;
  if (newpw !== confirmpw) {
    $("pw-error").textContent = "New passwords do not match";
    $("pw-error").classList.remove("hidden");
    return;
  }
  try {
    await api("/api/password", {
      method: "POST",
      body: JSON.stringify({ old_password: oldpw, new_password: newpw }),
    });
    $("pw-old").value = $("pw-new").value = $("pw-confirm").value = "";
    $("must-change-banner").classList.add("hidden");
    $("pw-ok").textContent = "Password updated.";
    $("pw-ok").classList.remove("hidden");
  } catch (err) {
    $("pw-error").textContent = err.message;
    $("pw-error").classList.remove("hidden");
  }
});

boot();
