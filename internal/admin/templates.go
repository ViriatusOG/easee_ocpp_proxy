package admin

import (
	"html/template"
	"strings"
)

var templateFuncs = template.FuncMap{
	// hasPrefixError styles a flash message as an error when it starts with "error".
	"hasPrefixError": func(s string) bool { return strings.HasPrefix(s, "error") },
}

func buildTemplates() map[string]*template.Template {
	pages := map[string]string{
		"dashboard":    dashboardContent,
		"chargepoints": chargepointsContent,
		"schedules":    schedulesContent,
		"remote":       remoteContent,
		"account":      accountContent,
		"login":        loginContent,
		"setup":        setupContent,
	}
	out := make(map[string]*template.Template, len(pages))
	for name, content := range pages {
		out[name] = template.Must(template.New(name).Funcs(templateFuncs).Parse(baseTmpl + content))
	}
	return out
}

const baseTmpl = `{{define "base"}}<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<link rel="icon" href="data:,">
<title>{{.Title}} · Easee OCPP Proxy</title>
{{if .Refresh}}<meta http-equiv="refresh" content="{{.Refresh}}">{{end}}
<style>
  :root { font-family: system-ui, sans-serif; }
  *, *::before, *::after { box-sizing: border-box; }
  body { margin: 0; color: #1a1a1a; background: #f6f7f9; overflow-x: hidden; }
  header { background: #1f2933; color: #fff; padding: .75rem 1rem; display: flex; gap: 1rem; align-items: center; flex-wrap: wrap; }
  header a { color: #cbd5e1; text-decoration: none; }
  header a:hover { color: #fff; }
  header .brand { font-weight: 600; color: #fff; }
  header .spacer { flex: 1; }
  main { max-width: 60rem; margin: 1.5rem auto; padding: 0 1rem; }
  h1 { font-size: 1.4rem; }
  table { border-collapse: collapse; width: 100%; background: #fff; box-shadow: 0 1px 2px rgba(0,0,0,.06); }
  th, td { text-align: left; padding: .5rem .6rem; border-bottom: 1px solid #eef0f3; font-size: .92rem; }
  th { background: #f0f2f5; }
  tr.proxied { background: #fff7e6; }
  .badge { font-size: .72rem; padding: .1rem .45rem; border-radius: .5rem; background: #e2e8f0; }
  .badge.proxied { background: #f59e0b; color: #fff; }
  button.badge { border: 1px solid transparent; background: #e2e8f0; color: #1a1a1a; cursor: pointer; }
  button.badge.proxied { background: #f59e0b; color: #fff; }
  button.badge:hover { outline: 1px solid #94a3b8; }
  .up { color: #16a34a; } .down { color: #cbd5e1; }
  .flash { max-width: 60rem; margin: 1rem auto 0; padding: .6rem 1rem; background: #ecfdf5; border: 1px solid #a7f3d0; border-radius: .4rem; }
  .flash.error { background: #fef2f2; border-color: #fecaca; }
  .warn { color: #b45309; }
  form.card, .card { background: #fff; padding: 1rem; border-radius: .5rem; box-shadow: 0 1px 2px rgba(0,0,0,.06); margin-bottom: 1rem; }
  label { display: block; margin: .5rem 0 .2rem; font-size: .9rem; }
  input[type=text], input[type=password], input[type=url] { width: 100%; max-width: 30rem; padding: .4rem; border: 1px solid #cbd5e1; border-radius: .3rem; }
  button { padding: .45rem .9rem; border: 0; border-radius: .3rem; background: #2563eb; color: #fff; cursor: pointer; }
  button.danger { background: #dc2626; }
  .radios label { display: flex; align-items: center; gap: .4rem; margin: .3rem 0; }
  label.checkbox { display: flex; align-items: center; gap: .4rem; font-size: 1rem; }
  label.checkbox input { width: auto; }
  .muted { color: #64748b; font-size: .85rem; }
  .table-scroll { overflow-x: auto; -webkit-overflow-scrolling: touch; }
  select, input[type=text], input[type=password], input[type=url] { font-size: 16px; } /* avoid iOS zoom-on-focus */
  .confirm-overlay { position: fixed; inset: 0; z-index: 50; display: flex; align-items: center; justify-content: center; padding: 1rem; background: rgba(15,23,42,.55); }
  .confirm-overlay[hidden] { display: none; }
  .confirm-dialog { background: #fff; border-radius: .6rem; box-shadow: 0 12px 40px rgba(0,0,0,.25); width: 100%; max-width: 26rem; padding: 1.1rem 1.2rem; }
  .confirm-dialog h2 { margin: 0 0 .4rem; font-size: 1.05rem; }
  .confirm-dialog p { margin: 0 0 1rem; color: #334155; font-size: .95rem; }
  .confirm-actions { display: flex; justify-content: flex-end; gap: .5rem; }
  button.secondary { background: #e2e8f0; color: #1a1a1a; }
  button.secondary:hover { background: #cbd5e1; }
  button.danger:hover { background: #b91c1c; }
  @media (max-width: 720px) {
    main { padding: 0 .6rem; margin: 1rem auto; }
    header { gap: .55rem; padding: .6rem .7rem; }
    h1 { font-size: 1.2rem; }
    .table-scroll { overflow-x: visible; } /* cards stack, no horizontal scroll needed */
    /* Dashboard: turn the wide table into stacked cards */
    table.cards { display: block; }
    table.cards thead { display: none; }
    table.cards tbody, table.cards tr, table.cards td { display: block; }
    table.cards tr { margin: 0 0 .8rem; border: 1px solid #e2e8f0; border-radius: .6rem; padding: .3rem .8rem; background: #fff; box-shadow: 0 1px 2px rgba(0,0,0,.05); }
    table.cards tr.proxied { background: #fff7e6; }
    table.cards td { display: flex; justify-content: space-between; align-items: baseline; gap: 1rem; border: 0; border-bottom: 1px solid #f1f5f9; padding: .55rem 0; text-align: right; min-width: 0; }
    table.cards td:last-child { border-bottom: 0; }
    table.cards td::before { content: attr(data-label); font-weight: 600; color: #64748b; text-align: left; white-space: nowrap; }
    table.cards td.name-cell { display: block; text-align: left; font-size: 1.1rem; font-weight: 600; padding: .35rem 0 .55rem; }
    table.cards td.name-cell::before { content: none; }
    table.cards select { max-width: 100%; }
  }
</style>
</head>
<body>
{{if .User}}
<header>
  <span class="brand">Easee OCPP Proxy</span>
  <a href="/admin">Dashboard</a>
  <a href="/admin/chargepoints">Chargepoints</a>
  <a href="/admin/schedules">Schedules</a>
  <a href="/admin/remote">Remote server</a>
  <a href="/admin/account">Account</a>
  <span class="spacer"></span>
  <span class="muted">{{.User}}</span>
  <a href="/admin/logout">Sign out</a>
</header>
{{end}}
{{if .Flash}}<div class="flash{{if hasPrefixError .Flash}} error{{end}}">{{.Flash}}</div>{{end}}
<main>{{block "content" .}}{{end}}</main>
<div id="confirm-overlay" class="confirm-overlay" hidden>
  <div class="confirm-dialog" role="dialog" aria-modal="true" aria-labelledby="confirm-title">
    <h2 id="confirm-title">Are you sure?</h2>
    <p id="confirm-message"></p>
    <div class="confirm-actions">
      <button type="button" id="confirm-cancel" class="secondary">Cancel</button>
      <button type="button" id="confirm-ok" class="danger">Confirm</button>
    </div>
  </div>
</div>
<script>
(function () {
  var overlay = document.getElementById('confirm-overlay');
  var msg = document.getElementById('confirm-message');
  var ok = document.getElementById('confirm-ok');
  var pending = null;

  function show(text, label) {
    msg.textContent = text;
    ok.textContent = label;
    overlay.hidden = false;
    ok.focus();
  }
  function hide() {
    overlay.hidden = true;
    pending = null;
  }
  document.addEventListener('submit', function (e) {
    var f = e.target;
    if (!f.hasAttribute('data-confirm')) return;
    e.preventDefault();
    pending = f;
    show(f.getAttribute('data-confirm'), f.getAttribute('data-confirm-label') || 'Confirm');
  }, true);
  ok.addEventListener('click', function () {
    var f = pending;
    hide();
    if (f) f.submit();
  });
  document.getElementById('confirm-cancel').addEventListener('click', hide);
  overlay.addEventListener('click', function (e) { if (e.target === overlay) hide(); });
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape' && !overlay.hidden) hide();
  });
})();
</script>
</body>
</html>{{end}}`

const dashboardContent = `{{define "content"}}
<h1>Dashboard</h1>
{{range .Warnings}}<p class="warn">⚠ {{.}}</p>{{end}}

<div class="table-scroll">
<table class="cards">
  <thead><tr>
    <th>Chargepoint</th><th>Role</th><th>Down</th><th>Up</th>
    <th>Connector</th><th>Schedule</th><th>Session</th><th>Energy</th><th>Power</th><th>Last HB</th>
  </tr></thead>
  <tbody>
  {{range .Rows}}
    <tr class="{{if .Proxied}}proxied{{end}}" data-cp-id="{{.ID}}">
      <td class="name-cell">{{.Name}}{{if ne .Name .ID}}<br><span class="muted">{{.ID}}</span>{{end}}</td>
      <td data-label="Role">
        <form method="post" action="/admin/mode">
          <input type="hidden" name="id" value="{{.ID}}">
          <select name="mode" onchange="this.form.submit()" title="Change how this chargepoint is controlled">
            <option value="proxied" {{if eq .Mode "proxied"}}selected{{end}}>proxied</option>
            <option value="always_on" {{if eq .Mode "always_on"}}selected{{end}}>always on</option>
            <option value="always_off" {{if eq .Mode "always_off"}}selected{{end}}>always off</option>
            {{if .HasSchedule}}<option value="scheduled" {{if eq .Mode "scheduled"}}selected{{end}}>scheduled</option>{{end}}
            {{if or .CanSync (eq .Mode "synchronised")}}<option value="synchronised" {{if eq .Mode "synchronised"}}selected{{end}}>synchronised</option>{{end}}
          </select>
        </form>
      </td>
      <td data-label="Down" data-field="down">{{if .Online}}<span class="up">●</span>{{else}}<span class="down">○</span>{{end}}</td>
      <td data-label="Up" data-field="up">{{if .ShowUpstream}}{{if .UpstreamUp}}<span class="up">●</span>{{else}}<span class="down">○</span>{{end}}{{else}}—{{end}}</td>
      <td data-label="Connector" data-field="status">{{.Status}}</td>
      <td data-label="Schedule" data-field="schedule">
        {{- if .Schedule}}{{.Schedule}}
          {{- if eq .ScheduleState "open"}} <span class="up">open</span>
          {{- else if eq .ScheduleState "closed"}} <span class="warn">closed</span>
          {{- else if eq .ScheduleState "paused"}} <span class="muted">paused</span>{{end}}
        {{- else}}<span class="muted">—</span>{{end}}
      </td>
      <td data-label="Session" data-field="session">{{.Session}}</td>
      <td data-label="Energy" data-field="energy">{{.Energy}}</td>
      <td data-label="Power" data-field="power">{{.Power}}</td>
      <td data-label="Last HB" data-field="lasthb">{{.LastHeartbeat}}</td>
    </tr>
  {{else}}
    <tr><td colspan="10" class="muted">No chargepoints configured. Add one under “Chargepoints”.</td></tr>
  {{end}}
  </tbody>
</table>
</div>

<p class="muted" style="margin-top:1rem">Tip: use a chargepoint's <strong>role</strong> selector to switch between <em>proxied</em> (remotely managed), <em>always on</em>, <em>always off</em> (charging disabled — for external control via the API/Home Assistant), <em>scheduled</em> (only when a schedule is assigned), and <em>synchronised</em> (only when another chargepoint is proxied — mirrors its charging windows locally). Only one chargepoint can be proxied; switching to proxied reconnects it.</p>

<script>
(function () {
  function dot(on) { return on ? '<span class="up">●</span>' : '<span class="down">○</span>'; }
  function upCell(r) { return r.ShowUpstream ? dot(r.UpstreamUp) : '—'; }
  function schedCell(r) {
    if (!r.Schedule) { return '<span class="muted">—</span>'; }
    if (r.ScheduleState === 'open') { return r.Schedule + ' <span class="up">open</span>'; }
    if (r.ScheduleState === 'closed') { return r.Schedule + ' <span class="warn">closed</span>'; }
    if (r.ScheduleState === 'paused') { return r.Schedule + ' <span class="muted">paused</span>'; }
    return r.Schedule;
  }
  function apply(rows) {
    rows.forEach(function (r) {
      var tr = document.querySelector('tr[data-cp-id="' + (window.CSS && CSS.escape ? CSS.escape(r.ID) : r.ID) + '"]');
      if (!tr) { return; }
      tr.classList.toggle('proxied', !!r.Proxied);
      function setHTML(f, v) { var el = tr.querySelector('[data-field="' + f + '"]'); if (el) { el.innerHTML = v; } }
      function setText(f, v) { var el = tr.querySelector('[data-field="' + f + '"]'); if (el) { el.textContent = v; } }
      setHTML('down', dot(r.Online));
      setHTML('up', upCell(r));
      setText('status', r.Status || '');
      setHTML('schedule', schedCell(r));
      setText('session', r.Session || '');
      setText('energy', r.Energy || '');
      setText('power', r.Power || '');
      setText('lasthb', r.LastHeartbeat || '');
      var sel = tr.querySelector('select[name="mode"]');
      if (sel && document.activeElement !== sel && sel.value !== r.Mode) { sel.value = r.Mode; }
    });
  }
  function tick() {
    fetch('/admin/state', { credentials: 'same-origin' })
      .then(function (res) { return res.ok ? res.json() : Promise.reject(); })
      .then(apply)
      .catch(function () {});
  }
  setInterval(tick, 5000);
})();
</script>
{{end}}`

const chargepointsContent = `{{define "content"}}
<h1>Chargepoints</h1>
<div class="card table-scroll">
  <table>
    <thead><tr><th>ID</th><th>Name</th><th>Schedule</th><th></th></tr></thead>
    <tbody>
    {{range $e := .Entries}}
      <tr>
        <td>{{$e.ID}}{{if $e.TooLong}} <span class="warn">(over {{$.MaxLen}} chars — Easee may reject)</span>{{end}}</td>
        <td>
          <form method="post" action="/admin/chargepoints" style="display:flex;gap:.3rem">
            <input type="hidden" name="action" value="alias">
            <input type="hidden" name="id" value="{{$e.ID}}">
            <input type="text" name="alias" value="{{$e.Alias}}" placeholder="e.g. Garage Left" style="max-width:12rem">
            <button type="submit">Save</button>
          </form>
        </td>
        <td>
          <form method="post" action="/admin/chargepoints" style="display:flex;gap:.3rem">
            <input type="hidden" name="action" value="assign">
            <input type="hidden" name="id" value="{{$e.ID}}">
            <select name="schedule">
              <option value="">— none —</option>
              {{range $.Schedules}}<option value="{{.}}" {{if eq . $e.Schedule}}selected{{end}}>{{.}}</option>{{end}}
            </select>
            <button type="submit">Set</button>
          </form>
        </td>
        <td>
          <form method="post" action="/admin/chargepoints" data-confirm="Remove {{ $e.ID }}?" data-confirm-label="Remove">
            <input type="hidden" name="action" value="remove">
            <input type="hidden" name="id" value="{{$e.ID}}">
            <button class="danger" type="submit">Remove</button>
          </form>
        </td>
      </tr>
    {{else}}
      <tr><td colspan="4" class="muted">No chargepoints yet.</td></tr>
    {{end}}
    </tbody>
  </table>
</div>
<form class="card" method="post" action="/admin/chargepoints">
  <input type="hidden" name="action" value="add">
  <label for="id">Add chargepoint ID <span class="muted">(Easee accepts up to {{.MaxLen}} characters)</span></label>
  <input id="id" name="id" type="text" required maxlength="64" autofocus>
  <p><button type="submit">Add</button></p>
</form>
{{end}}`

const schedulesContent = `{{define "content"}}
<h1>Schedules</h1>
<p class="muted">Charging windows in local time ({{if .Timezone}}{{.Timezone}}{{else}}server local{{end}}, currently {{.LocalTime}}), applied per chargepoint. A window may cross midnight (e.g. 23:30 → 05:30).</p>

<div class="card table-scroll">
  <table>
    <thead><tr><th>Name</th><th>Start</th><th>Stop</th><th></th></tr></thead>
    <tbody>
    {{range .Schedules}}
      <tr>
        <td>{{.Name}}</td><td>{{.Start}}</td><td>{{.Stop}}</td>
        <td>
          <form method="post" action="/admin/schedules" data-confirm="Remove schedule {{ .Name }}?" data-confirm-label="Remove">
            <input type="hidden" name="action" value="remove">
            <input type="hidden" name="name" value="{{.Name}}">
            <button class="danger" type="submit">Remove</button>
          </form>
        </td>
      </tr>
    {{else}}
      <tr><td colspan="4" class="muted">No schedules defined.</td></tr>
    {{end}}
    </tbody>
  </table>
</div>

<form class="card" method="post" action="/admin/schedules">
  <input type="hidden" name="action" value="add">
  <strong>Add / update schedule</strong>
  <label for="name">Name</label>
  <input id="name" name="name" type="text" required placeholder="overnight">
  <label for="start">Start (HH:MM)</label>
  <input id="start" name="start" type="text" required placeholder="23:30" pattern="[0-2][0-9]:[0-5][0-9]">
  <label for="stop">Stop (HH:MM)</label>
  <input id="stop" name="stop" type="text" required placeholder="05:30" pattern="[0-2][0-9]:[0-5][0-9]">
  <p><button type="submit">Save schedule</button></p>
</form>

<form class="card" method="post" action="/admin/schedules">
  <input type="hidden" name="action" value="timezone">
  <label for="timezone">Timezone <span class="muted">(IANA name, e.g. Europe/London; blank = server local)</span></label>
  <input id="timezone" name="timezone" type="text" value="{{.Timezone}}" placeholder="Europe/London">
  <p><button type="submit">Save timezone</button></p>
</form>
{{end}}`

const remoteContent = `{{define "content"}}
<h1>Remote server</h1>
<form class="card" method="post" action="/admin/remote">
  <label for="url">CSMS base URL <span class="muted">(scheme + host [+ path]; the upstream ID is appended)</span></label>
  <input id="url" name="url" type="text" value="{{.Remote.URL}}" placeholder="wss://csms.example.com">

  <label for="upstream_id">Upstream chargepoint ID <span class="muted">(may exceed 25 chars)</span></label>
  <input id="upstream_id" name="upstream_id" type="text" value="{{.Remote.UpstreamID}}">

  <label for="username">Username <span class="muted">(optional — defaults to the upstream chargepoint ID, per OCPP Basic auth)</span></label>
  <input id="username" name="username" type="text" value="{{.Remote.Username}}" placeholder="(defaults to upstream ID)">

  <label for="password">Password <span class="muted">({{if .PasswordSet}}set — leave blank to keep{{else}}not set{{end}})</span></label>
  <input id="password" name="password" type="password" autocomplete="new-password">

  <hr>
  <strong>BootNotification anonymisation</strong>
  <p class="muted">Values sent upstream in place of the real manufacturer identity.</p>
  <label for="vendor">Charge point vendor</label>
  <input id="vendor" name="vendor" type="text" value="{{.Boot.ChargePointVendor}}" placeholder="Wallbox">
  <label for="model">Charge point model</label>
  <input id="model" name="model" type="text" value="{{.Boot.ChargePointModel}}">
  <label for="firmware">Firmware version <span class="muted">(blank = pass through real value)</span></label>
  <input id="firmware" name="firmware" type="text" value="{{.Boot.FirmwareVersion}}">
  <label for="serial">Serial number <span class="muted">(blank = pass through real value)</span></label>
  <input id="serial" name="serial" type="text" value="{{.Boot.SerialNumber}}">

  <hr>
  <strong>Provisioning</strong>
  <p class="muted">Applies on the next upstream connect; the current session is not interrupted.</p>
  <label class="checkbox"><input name="force_boot" type="checkbox" value="1"{{if .ForceBoot}} checked{{end}}> Request a BootNotification on every upstream connect</label>
  <p class="muted">Makes the CSMS re-provision the charger on connect/switchover (fixes "session open, 0&nbsp;kWh"). Once the CSMS has configured the charger this is redundant and can be turned off.</p>

  <hr>
  <strong>Metering (multi-unit)</strong>
  <p class="muted">Only relevant when you rotate more than one physical charger through this one remote identity.</p>
  <label class="checkbox"><input name="normalise_meter" type="checkbox" value="1"{{if .NormaliseMeter}} checked{{end}}> Present one continuous meter to the CSMS</label>
  <p class="muted">Each physical charger has its own lifetime meter, so switching units makes the CSMS see the reading jump — a backwards jump can break its session history. This rewrites the register sent upstream onto a single, never-decreasing virtual meter. Per-session energy is unchanged. Enable it while the highest-reading unit is proxied, then leave it on.</p>

  <p><button type="submit">Save</button></p>
</form>
{{end}}`

const accountContent = `{{define "content"}}
<h1>Admin account</h1>
<form class="card" method="post" action="/admin/account">
  <input type="hidden" name="action" value="password">
  <strong>Change admin password</strong>
  <label for="username">Username</label>
  <input id="username" name="username" type="text" value="{{.Username}}">
  <label for="password">New password <span class="muted">(min 8 characters)</span></label>
  <input id="password" name="password" type="password" autocomplete="new-password" required>
  <label for="confirm">Confirm password</label>
  <input id="confirm" name="confirm" type="password" autocomplete="new-password" required>
  <p><button type="submit">Update</button></p>
</form>

<div class="card">
  <strong>API token</strong>
  <p class="muted">Read-only + role-change JSON API for integrations (e.g. Home Assistant). Requests send <code>Authorization: Bearer &lt;token&gt;</code> to <code>GET /api/chargepoints</code> and <code>POST /api/chargepoints/&lt;id&gt;/mode</code>. No token = API disabled.</p>
  {{if .APIToken}}
    <p>Current token: <code style="word-break:break-all">{{.APIToken}}</code></p>
    <form method="post" action="/admin/account" style="display:inline-block;margin-right:.4rem">
      <input type="hidden" name="action" value="gentoken">
      <button type="submit">Regenerate</button>
    </form>
    <form method="post" action="/admin/account" style="display:inline-block" data-confirm="Clear the API token? This disables the API and breaks any integration using it." data-confirm-label="Clear">
      <input type="hidden" name="action" value="cleartoken">
      <button type="submit" class="danger">Clear</button>
    </form>
  {{else}}
    <p class="muted">No token set — the API is disabled.</p>
    <form method="post" action="/admin/account">
      <input type="hidden" name="action" value="gentoken">
      <button type="submit">Generate API token</button>
    </form>
  {{end}}
</div>
{{end}}`

const loginContent = `{{define "content"}}
<h1>Sign in</h1>
{{if .Error}}<p class="warn">{{.Error}}</p>{{end}}
<form class="card" method="post" action="/admin/login" style="max-width:24rem">
  <label for="username">Username</label>
  <input id="username" name="username" type="text" required autofocus>
  <label for="password">Password</label>
  <input id="password" name="password" type="password" required>
  <p><button type="submit">Sign in</button></p>
</form>
{{end}}`

const setupContent = `{{define "content"}}
<h1>First-run setup</h1>
<p class="muted">No admin password is set. Create one to secure the management interface.</p>
{{if .Error}}<p class="warn">{{.Error}}</p>{{end}}
<form class="card" method="post" action="/admin/login" style="max-width:24rem">
  <label for="username">Username <span class="muted">(default: admin)</span></label>
  <input id="username" name="username" type="text" placeholder="admin" autofocus>
  <label for="password">Password <span class="muted">(min 8 characters)</span></label>
  <input id="password" name="password" type="password" required>
  <label for="confirm">Confirm password</label>
  <input id="confirm" name="confirm" type="password" required>
  <p><button type="submit">Create</button></p>
</form>
{{end}}`
