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
  body { margin: 0; color: #1a1a1a; background: #f6f7f9; }
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
  .muted { color: #64748b; font-size: .85rem; }
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
</body>
</html>{{end}}`

const dashboardContent = `{{define "content"}}
<h1>Dashboard</h1>
{{range .Warnings}}<p class="warn">⚠ {{.}}</p>{{end}}

<table>
  <thead><tr>
    <th>Chargepoint</th><th>Role</th><th>Down</th><th>Up</th>
    <th>Connector</th><th>Schedule</th><th>Session</th><th>Energy</th><th>Power</th><th>Last HB</th>
  </tr></thead>
  <tbody>
  {{range .Rows}}
    <tr class="{{if .Proxied}}proxied{{end}}">
      <td>{{.Name}}{{if ne .Name .ID}}<br><span class="muted">{{.ID}}</span>{{end}}</td>
      <td>
        <form method="post" action="/admin/mode">
          <input type="hidden" name="id" value="{{.ID}}">
          <select name="mode" onchange="this.form.submit()" title="Change how this chargepoint is controlled">
            <option value="proxied" {{if eq .Mode "proxied"}}selected{{end}}>proxied</option>
            <option value="always_on" {{if eq .Mode "always_on"}}selected{{end}}>always on</option>
            {{if .HasSchedule}}<option value="scheduled" {{if eq .Mode "scheduled"}}selected{{end}}>scheduled</option>{{end}}
          </select>
        </form>
      </td>
      <td>{{if .Online}}<span class="up">●</span>{{else}}<span class="down">○</span>{{end}}</td>
      <td>{{if .ShowUpstream}}{{if .UpstreamUp}}<span class="up">●</span>{{else}}<span class="down">○</span>{{end}}{{else}}—{{end}}</td>
      <td>{{.Status}}</td>
      <td>
        {{- if .Schedule}}{{.Schedule}}
          {{- if eq .ScheduleState "open"}} <span class="up">open</span>
          {{- else if eq .ScheduleState "closed"}} <span class="warn">closed</span>
          {{- else if eq .ScheduleState "paused"}} <span class="muted">paused</span>{{end}}
        {{- else}}<span class="muted">—</span>{{end}}
      </td>
      <td>{{.Session}}</td>
      <td>{{.Energy}}</td>
      <td>{{.Power}}</td>
      <td>{{.LastHeartbeat}}</td>
    </tr>
  {{else}}
    <tr><td colspan="10" class="muted">No chargepoints configured. Add one under “Chargepoints”.</td></tr>
  {{end}}
  </tbody>
</table>

<p class="muted" style="margin-top:1rem">Tip: use a chargepoint's <strong>role</strong> selector to switch between <em>proxied</em> (remotely managed), <em>always on</em>, and <em>scheduled</em> (only when a schedule is assigned). Only one chargepoint can be proxied; switching to proxied reconnects it.</p>
{{end}}`

const chargepointsContent = `{{define "content"}}
<h1>Chargepoints</h1>
<div class="card">
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
          <form method="post" action="/admin/chargepoints" onsubmit="return confirm('Remove {{$e.ID}}?')">
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

<div class="card">
  <table>
    <thead><tr><th>Name</th><th>Start</th><th>Stop</th><th></th></tr></thead>
    <tbody>
    {{range .Schedules}}
      <tr>
        <td>{{.Name}}</td><td>{{.Start}}</td><td>{{.Stop}}</td>
        <td>
          <form method="post" action="/admin/schedules" onsubmit="return confirm('Remove {{.Name}}?')">
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

  <p><button type="submit">Save</button></p>
</form>
{{end}}`

const accountContent = `{{define "content"}}
<h1>Admin account</h1>
<form class="card" method="post" action="/admin/account">
  <label for="username">Username</label>
  <input id="username" name="username" type="text" value="{{.Username}}">
  <label for="password">New password <span class="muted">(min 8 characters)</span></label>
  <input id="password" name="password" type="password" autocomplete="new-password" required>
  <label for="confirm">Confirm password</label>
  <input id="confirm" name="confirm" type="password" autocomplete="new-password" required>
  <p><button type="submit">Update</button></p>
</form>
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
