package ui

const indexTemplate = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>SkillGate Experiments</title>
<style>
:root {
  --bg: #0a0a0a;
  --surface: #1a1a1a;
  --surface-hover: #2a2a2a;
  --text-primary: #fafafa;
  --text-secondary: #a0a0a0;
  --cyan: #06b6d4;
  --emerald: #10b981;
  --red: #ef4444;
  --yellow: #eab308;
}
* { margin: 0; padding: 0; box-sizing: border-box; }
body {
  background: var(--bg);
  color: var(--text-primary);
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
  font-size: clamp(14px, 1vw + 12px, 18px);
  line-height: 1.6;
  padding: 24px 16px;
}
.container {
  max-width: 1200px;
  margin: 0 auto;
}
h1 {
  font-size: clamp(24px, 2vw + 16px, 36px);
  margin-bottom: 32px;
  color: var(--cyan);
}
.experiments {
  display: grid;
  gap: 16px;
}
.experiment-card {
  background: var(--surface);
  border-radius: 8px;
  padding: 20px;
  transition: background 0.2s;
  text-decoration: none;
  color: inherit;
  display: block;
}
.experiment-card:hover {
  background: var(--surface-hover);
}
.experiment-id {
  font-size: 1.1em;
  font-weight: 600;
  color: var(--cyan);
  margin-bottom: 8px;
}
.experiment-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
  font-size: 0.9em;
  color: var(--text-secondary);
}
.status {
  padding: 4px 12px;
  border-radius: 4px;
  font-weight: 500;
  font-size: 0.85em;
  text-transform: uppercase;
}
.status-COMPLETED { background: var(--emerald); color: #000; }
.status-RUNNING, .status-QUEUED { background: var(--cyan); color: #000; }
.status-FAILED, .status-CANCELLED { background: var(--red); color: #fff; }
.status-GRADING { background: var(--yellow); color: #000; }
.progress {
  color: var(--text-primary);
}
.empty {
  text-align: center;
  padding: 48px 16px;
  color: var(--text-secondary);
}
@media (min-width: 768px) {
  .experiment-card { padding: 24px; }
}
</style>
</head>
<body>
<div class="container">
  <h1>SkillGate Experiments</h1>
  {{if .}}
  <div class="experiments">
    {{range .}}
    <a href="/experiments/{{.ID}}" class="experiment-card">
      <div class="experiment-id">{{.ID}}</div>
      <div class="experiment-meta">
        <span class="status status-{{.Status}}">{{.Status}}</span>
        <span class="progress">{{.TerminalCount}} / {{.TotalTrials}} trials</span>
        <span>{{.CreatedAt}}</span>
      </div>
    </a>
    {{end}}
  </div>
  {{else}}
  <div class="empty">No experiments found</div>
  {{end}}
</div>
</body>
</html>`

const detailTemplate = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>{{.Experiment.ExperimentID}} - SkillGate</title>
<style>
:root {
  --bg: #0a0a0a;
  --surface: #1a1a1a;
  --surface-hover: #2a2a2a;
  --text-primary: #fafafa;
  --text-secondary: #a0a0a0;
  --cyan: #06b6d4;
  --emerald: #10b981;
  --red: #ef4444;
  --yellow: #eab308;
  --border: #333;
}
* { margin: 0; padding: 0; box-sizing: border-box; }
body {
  background: var(--bg);
  color: var(--text-primary);
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
  font-size: clamp(14px, 1vw + 12px, 18px);
  line-height: 1.6;
  padding: 24px 16px;
}
.container {
  max-width: 1200px;
  margin: 0 auto;
}
.back {
  color: var(--cyan);
  text-decoration: none;
  margin-bottom: 24px;
  display: inline-block;
}
.back:hover { text-decoration: underline; }
h1 {
  font-size: clamp(20px, 2vw + 14px, 32px);
  margin-bottom: 24px;
  color: var(--cyan);
}
h2 {
  font-size: 1.3em;
  margin: 32px 0 16px;
  color: var(--emerald);
}
.card {
  background: var(--surface);
  border-radius: 8px;
  padding: 20px;
  margin-bottom: 24px;
}
.meta-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 16px;
}
.meta-item label {
  display: block;
  color: var(--text-secondary);
  font-size: 0.85em;
  margin-bottom: 4px;
}
.meta-item value {
  display: block;
  font-weight: 500;
}
.status {
  padding: 4px 12px;
  border-radius: 4px;
  font-weight: 500;
  font-size: 0.85em;
  text-transform: uppercase;
  display: inline-block;
}
.status-COMPLETED { background: var(--emerald); color: #000; }
.status-RUNNING, .status-QUEUED { background: var(--cyan); color: #000; }
.status-FAILED, .status-CANCELLED { background: var(--red); color: #fff; }
.status-GRADING { background: var(--yellow); color: #000; }
.decision {
  padding: 20px;
  border-radius: 8px;
  margin-top: 16px;
}
.decision-PROMOTE { background: rgba(16, 185, 129, 0.1); border: 2px solid var(--emerald); }
.decision-HOLD { background: rgba(234, 179, 8, 0.1); border: 2px solid var(--yellow); }
.decision-REJECT { background: rgba(239, 68, 68, 0.1); border: 2px solid var(--red); }
.decision-label {
  font-size: 1.5em;
  font-weight: 700;
  margin-bottom: 12px;
}
.decision-PROMOTE .decision-label { color: var(--emerald); }
.decision-HOLD .decision-label { color: var(--yellow); }
.decision-REJECT .decision-label { color: var(--red); }
.stats-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 16px;
  margin: 16px 0;
}
.stat-card {
  background: rgba(255, 255, 255, 0.05);
  padding: 16px;
  border-radius: 8px;
  text-align: center;
}
.stat-value {
  font-size: 1.8em;
  font-weight: 700;
  color: var(--cyan);
}
.stat-label {
  font-size: 0.85em;
  color: var(--text-secondary);
  margin-top: 4px;
}
table {
  width: 100%;
  border-collapse: collapse;
  margin-top: 16px;
  overflow-x: auto;
  display: block;
}
thead {
  background: rgba(255, 255, 255, 0.05);
}
th, td {
  padding: 12px;
  text-align: left;
  border-bottom: 1px solid var(--border);
}
th {
  font-weight: 600;
  color: var(--cyan);
}
.evidence-link {
  color: var(--cyan);
  text-decoration: none;
  display: block;
  margin: 4px 0;
  word-break: break-all;
}
.evidence-link:hover { text-decoration: underline; }
.no-report {
  color: var(--text-secondary);
  font-style: italic;
  padding: 32px;
  text-align: center;
}
@media (max-width: 768px) {
  table { font-size: 0.85em; }
  th, td { padding: 8px; }
}
</style>
</head>
<body>
<div class="container">
  <a href="/" class="back">← Back to experiments</a>
  <h1>{{.Experiment.ExperimentID}}</h1>

  <div class="card">
    <div class="meta-grid">
      <div class="meta-item">
        <label>Status</label>
        <value><span class="status status-{{.Experiment.Status}}">{{.Experiment.Status}}</span></value>
      </div>
      <div class="meta-item">
        <label>Created At</label>
        <value>{{.Experiment.CreatedAt}}</value>
      </div>
      <div class="meta-item">
        <label>Total Trials</label>
        <value>{{.Experiment.TotalTrials}}</value>
      </div>
      <div class="meta-item">
        <label>Terminal Count</label>
        <value>{{.Experiment.TerminalCount}}</value>
      </div>
    </div>

    <div class="stats-grid">
      <div class="stat-card">
        <div class="stat-value">{{.Experiment.SucceededCount}}</div>
        <div class="stat-label">Succeeded</div>
      </div>
      <div class="stat-card">
        <div class="stat-value">{{.Experiment.FailedCount}}</div>
        <div class="stat-label">Failed</div>
      </div>
      <div class="stat-card">
        <div class="stat-value">{{.Experiment.TimedOutCount}}</div>
        <div class="stat-label">Timed Out</div>
      </div>
      <div class="stat-card">
        <div class="stat-value">{{.Experiment.CancelledCount}}</div>
        <div class="stat-label">Cancelled</div>
      </div>
    </div>
  </div>

  {{if .HasReport}}
  <h2>Statistical Summary</h2>
  <div class="card">
    <div class="meta-grid">
      <div class="meta-item">
        <label>Valid Pairs</label>
        <value>{{.Report.ValidPairs}}</value>
      </div>
      <div class="meta-item">
        <label>Invalid Pairs</label>
        <value>{{.Report.InvalidPairs}}</value>
      </div>
      {{if .Report.MeanLift}}
      <div class="meta-item">
        <label>Mean Lift</label>
        <value>{{printf "%.2f%%" (index . "Report" | index "MeanLift" | printf "%v")}}</value>
      </div>
      {{end}}
      {{if .Report.CILower}}
      <div class="meta-item">
        <label>95% CI</label>
        <value>[{{printf "%.2f" .Report.CILower}}, {{printf "%.2f" .Report.CIUpper}}]</value>
      </div>
      {{end}}
      {{if .Report.StatisticallySignificant}}
      <div class="meta-item">
        <label>Statistically Significant</label>
        <value>{{if .Report.StatisticallySignificant}}Yes{{else}}No{{end}}</value>
      </div>
      {{end}}
    </div>
  </div>

  {{if .CaseResults}}
  <h2>Case Results</h2>
  <div class="card">
    <div style="overflow-x: auto;">
      <table>
        <thead>
          <tr>
            <th>Case ID</th>
            <th>Without Skill</th>
            <th>With Skill</th>
            <th>Lift</th>
          </tr>
        </thead>
        <tbody>
          {{range .CaseResults}}
          <tr>
            <td>{{index . "case_id"}}</td>
            <td>{{index . "without_skill"}}</td>
            <td>{{index . "with_skill"}}</td>
            <td>{{index . "lift"}}</td>
          </tr>
          {{end}}
        </tbody>
      </table>
    </div>
  </div>
  {{end}}
  {{else}}
  <div class="card">
    <div class="no-report">Report not available</div>
  </div>
  {{end}}

  {{if .HasDecision}}
  <h2>Release Decision</h2>
  <div class="decision decision-{{.Decision.Result}}">
    <div class="decision-label">{{.Decision.Result}}</div>
    <div class="meta-grid">
      <div class="meta-item">
        <label>Policy</label>
        <value>{{.Decision.PolicyID}} ({{.Decision.PolicyVersion}})</value>
      </div>
      <div class="meta-item">
        <label>Created At</label>
        <value>{{.Decision.CreatedAt}}</value>
      </div>
    </div>
    <div style="margin-top: 16px;">
      <label style="display: block; color: var(--text-secondary); font-size: 0.85em; margin-bottom: 8px;">Explanation</label>
      <div>{{.Decision.Explanation}}</div>
    </div>
    {{if .Decision.EvidenceLinks}}
    <div style="margin-top: 16px;">
      <label style="display: block; color: var(--text-secondary); font-size: 0.85em; margin-bottom: 8px;">Evidence Links</label>
      {{range .Decision.EvidenceLinks}}
      <a href="{{.}}" class="evidence-link">{{.}}</a>
      {{end}}
    </div>
    {{end}}
  </div>
  {{end}}
</div>
</body>
</html>`

const notFoundTemplate = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>404 - Not Found</title>
<style>
body {
  background: #0a0a0a;
  color: #fafafa;
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100vh;
  margin: 0;
  text-align: center;
}
h1 { color: #06b6d4; font-size: 3em; margin-bottom: 16px; }
p { color: #a0a0a0; margin-bottom: 24px; }
a { color: #06b6d4; text-decoration: none; }
a:hover { text-decoration: underline; }
</style>
</head>
<body>
<div>
  <h1>404</h1>
  <p>Experiment not found</p>
  <a href="/">← Back to experiments</a>
</div>
</body>
</html>`

const errorTemplate = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>500 - Internal Server Error</title>
<style>
body {
  background: #0a0a0a;
  color: #fafafa;
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100vh;
  margin: 0;
  text-align: center;
  padding: 24px;
}
h1 { color: #ef4444; font-size: 3em; margin-bottom: 16px; }
p { color: #a0a0a0; margin-bottom: 8px; }
.error { color: #ef4444; font-family: monospace; font-size: 0.9em; margin: 16px 0; }
a { color: #06b6d4; text-decoration: none; }
a:hover { text-decoration: underline; }
</style>
</head>
<body>
<div>
  <h1>500</h1>
  <p>Internal server error</p>
  <div class="error">{{.Message}}</div>
  <a href="/">← Back to experiments</a>
</div>
</body>
</html>`
