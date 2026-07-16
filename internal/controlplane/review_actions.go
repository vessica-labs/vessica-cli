package controlplane

import (
	"context"
	"fmt"
	"html/template"
	"strings"

	"github.com/vessica-labs/vessica-cli/internal/repo"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

func (s *Server) approveRun(ctx context.Context, runID string) (map[string]any, error) {
	runRecord, err := s.DB.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if runRecord.PRMode == "merged" {
		return map[string]any{"run_id": runID, "merged": true, "merge_commit_sha": "already-merged", "pr_url": runRecord.PRURL}, nil
	}
	if runRecord.Status != "completed" || runRecord.PRURL == "" || runRecord.PRMode == "rolled_back" {
		return nil, fmt.Errorf("run is not ready for approval")
	}
	approvalBody := fmt.Sprintf("<!-- vessica:review-approve:%s -->\nVessica review decision: **Accept and Merge**.\n\nThe user approved this preview and requested that the draft PR be merged.\n%s", runID, runRecord.PRURL)
	if err := s.enqueueLinearReviewComment(ctx, runRecord, "approval_comment", runID, "linear:run:approved:"+runID, approvalBody); err != nil {
		return nil, fmt.Errorf("record approval in Linear: %w", err)
	}
	number, err := repo.ParsePRNumber(runRecord.PRURL)
	if err != nil {
		return nil, err
	}
	remote, err := s.repositoryRemote(ctx, runRecord)
	if err != nil {
		return nil, err
	}
	details, err := repo.GetPullRequest(ctx, remote, number)
	if err != nil {
		return nil, err
	}
	if details.Draft {
		if err := repo.MarkPullRequestReady(ctx, details.NodeID); err != nil {
			return nil, err
		}
	}
	merged, err := repo.MergePullRequest(ctx, remote, number, "squash", details.Head.SHA)
	if err != nil {
		return nil, err
	}
	runRecord.PRMode = "merged"
	_ = s.DB.UpdateRun(ctx, runRecord)
	_, _ = s.DB.CreateRunEvidence(ctx, runID, "approve", "pr_merge", "", "passed", map[string]any{"merge_commit_sha": merged.SHA, "approved_at": state.Now(), "hosted": true})
	if mapping, err := s.DB.GetExternalMapping(ctx, "linear", "epic", runRecord.EpicID); err == nil && s.Config.Tracker.DoneStateID != "" {
		if integration, _ := s.DB.GetTrackerIntegration(ctx, "linear"); integration != nil {
			_, _ = s.DB.EnqueueOutbox(ctx, integration.ID, "linear.issue_state", "linear:epic:done:"+runID, map[string]any{"issue_id": mapping.ExternalID, "state_id": s.Config.Tracker.DoneStateID})
		}
	}
	if sandboxRecord, err := s.DB.GetSandboxForRun(ctx, runID); err == nil && s.Launcher != nil {
		_ = s.Launcher.Destroy(ctx, sandboxRecord)
	}
	return map[string]any{"run_id": runID, "merged": true, "merge_commit_sha": merged.SHA, "pr_url": runRecord.PRURL}, nil
}

func (s *Server) rollbackRun(ctx context.Context, runID string) error {
	runRecord, err := s.DB.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if runRecord.PRMode == "rolled_back" {
		return nil
	}
	if runRecord.PRMode == "merged" || runRecord.PRURL == "" {
		return fmt.Errorf("merged runs cannot be rolled back from the review panel")
	}
	rollbackBody := fmt.Sprintf("<!-- vessica:review-rollback:%s -->\nVessica review decision: **Rollback**.\n\nThe user rejected this preview. Vessica will close the draft PR and stop the preview sandbox.\n%s", runID, runRecord.PRURL)
	if err := s.enqueueLinearReviewComment(ctx, runRecord, "rollback_comment", runID, "linear:run:rollback:"+runID, rollbackBody); err != nil {
		return fmt.Errorf("record rollback in Linear: %w", err)
	}
	number, err := repo.ParsePRNumber(runRecord.PRURL)
	if err != nil {
		return err
	}
	remote, err := s.repositoryRemote(ctx, runRecord)
	if err != nil {
		return err
	}
	comment := fmt.Sprintf("Rolled back through Vessica review controls for run `%s`. The retained preview sandbox was stopped.", runID)
	if err := repo.CommentPullRequest(ctx, remote, number, comment); err != nil {
		return err
	}
	if err := repo.ClosePullRequest(ctx, remote, number); err != nil {
		return err
	}
	runRecord.PRMode = "rolled_back"
	_ = s.DB.UpdateRun(ctx, runRecord)
	_, _ = s.DB.CreateRunEvidence(ctx, runID, "rollback", "pr_close", "", "passed", map[string]any{"rolled_back_at": state.Now(), "hosted": true})
	if sandboxRecord, err := s.DB.GetSandboxForRun(ctx, runID); err == nil && s.Launcher != nil {
		return s.Launcher.Destroy(ctx, sandboxRecord)
	}
	return nil
}

func (s *Server) enqueueLinearReviewComment(ctx context.Context, runRecord *state.Run, entityType, localID, idempotencyKey, body string) error {
	if runRecord == nil || strings.TrimSpace(runRecord.EpicID) == "" {
		return nil
	}
	mapping, err := s.DB.GetExternalMapping(ctx, "linear", "epic", runRecord.EpicID)
	if err != nil {
		return nil
	}
	integration, err := s.DB.GetTrackerIntegration(ctx, "linear")
	if err != nil || integration == nil || integration.Status != "connected" {
		return nil
	}
	_, err = s.DB.EnqueueOutbox(ctx, integration.ID, "linear.comment", idempotencyKey, map[string]any{
		"issue_id": mapping.ExternalID, "entity_type": entityType, "local_id": localID, "body": body,
	})
	return err
}

var reviewPageTemplate = template.Must(template.New("review").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Vessica review</title>
<style>
:root{--ink:#171c26;--ink-2:#242b38;--paper:#ffffff;--canvas:#edf0f3;--line:#d8dde4;--muted:#697483;--teal:#0b8f8f;--teal-soft:#e5f5f3;--amber:#f5ad36;--amber-hover:#ffc15b;--red:#a63d43;--red-soft:#fff0f1;--green:#35c98b}
*{box-sizing:border-box;letter-spacing:0}
html,body{min-height:100%}
body{margin:0;color:var(--ink);font:14px/1.48 ui-sans-serif,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:var(--canvas)}
body.is-panel{background:transparent}
button,textarea,a{font:inherit}
button,a{touch-action:manipulation}
.shell{width:min(720px,calc(100% - 32px));margin:40px auto;background:var(--paper);border:1px solid #cbd2db;border-radius:8px;box-shadow:0 18px 55px rgba(20,27,38,.2);overflow:hidden}
.is-panel .shell,.is-window .shell{width:100%;margin:0;border-radius:8px}
.is-panel .workspace{max-height:596px}
.is-window{background:var(--paper)}
.is-window .shell{min-height:100vh;border:0;box-shadow:none;border-radius:0}
.topbar{height:68px;padding:0 14px;display:flex;align-items:center;justify-content:space-between;gap:12px;background:var(--ink);color:#fff;border-bottom:1px solid #333c4b}
.identity{min-width:0;display:flex;align-items:center;gap:10px}
.identity>span:last-child{min-width:0}
.mark{width:34px;height:34px;display:grid;place-items:center;flex:0 0 auto;background:var(--amber);border:1px solid #ffd17f;border-radius:6px;color:var(--ink);font-weight:850;font-size:16px}
.eyebrow{display:block;color:#9ca7b6;font-size:10px;font-weight:750;text-transform:uppercase}
.title{display:block;font-size:15px;font-weight:760;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.tools{display:flex;align-items:center;gap:6px;flex:0 0 auto}
.tool{min-height:34px;padding:7px 10px;border:1px solid #4a5566;border-radius:5px;background:transparent;color:#e9edf2;font-size:12px;font-weight:700;cursor:pointer}
.tool:hover,.tool:focus-visible{border-color:#96a2b2;background:var(--ink-2);outline:0}
.tool.primary{border-color:var(--amber);background:var(--amber);color:var(--ink)}
.tool.primary:hover{background:var(--amber-hover)}
.workspace{max-height:calc(100vh - 84px);overflow:auto;background:var(--paper);{{if and .Panel (not .Open)}}display:none{{end}}}
.is-window .workspace{max-height:none}
.run-strip{min-height:52px;padding:10px 16px;display:flex;align-items:center;gap:10px;border-bottom:1px solid var(--line);background:#f7f8fa}
.live{display:inline-flex;align-items:center;gap:6px;color:#2d6251;font-size:12px;font-weight:700}
.live-dot{width:7px;height:7px;border-radius:50%;background:var(--green);box-shadow:0 0 0 3px rgba(53,201,139,.14)}
.run-id{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:#596474;font:11px/1.2 ui-monospace,SFMono-Regular,Menlo,monospace}
.badge{padding:3px 7px;border:1px solid #c9d0da;border-radius:4px;background:#fff;color:#505b69;font-size:10px;font-weight:750;text-transform:uppercase}
.pr-link{margin-left:auto;color:#176d73;font-size:12px;font-weight:750;text-decoration:none}
.pr-link:hover{text-decoration:underline}
.conversation{position:relative;min-height:96px;padding:16px;border-bottom:1px solid var(--line);background:#fbfcfd}
.transcript{display:grid;gap:14px}
.empty{margin:0;color:var(--muted)}
.message{margin:0 0 10px;padding:11px 12px;border-left:3px solid var(--teal);border-radius:0 6px 6px 0;background:var(--teal-soft);color:#1d5b57}
.message.error{border-left-color:var(--red);background:var(--red-soft);color:#842e34}
.agent-output{margin:0;max-height:220px;overflow:auto;white-space:pre-wrap;padding:12px;border:1px solid #d5dae1;border-radius:6px;background:#f4f6f8;color:#35404d;font:12px/1.55 ui-monospace,SFMono-Regular,Menlo,monospace}
.turn{min-width:0}
.user-turn{margin-left:38px;padding:11px 12px;border:1px solid #cfd5dd;border-radius:7px;background:#fff;color:#313a47;white-space:pre-wrap}
.turn-label{display:block;margin-bottom:5px;color:#7a8491;font-size:10px;font-weight:800;text-transform:uppercase}
.codex-turn{border:1px solid #d6dbe2;border-radius:7px;background:#fff;overflow:hidden}
.codex-head{min-height:46px;padding:9px 11px;display:flex;align-items:center;gap:9px;border-bottom:1px solid #e1e5ea;background:#f7f8fa}
.codex-glyph{width:27px;height:27px;display:grid;place-items:center;flex:0 0 auto;border-radius:5px;background:var(--ink);color:#fff;font:800 12px/1 ui-monospace,SFMono-Regular,Menlo,monospace}
.codex-name{font-weight:800}.codex-meta{margin-left:auto;display:flex;align-items:center;gap:7px;color:#6e7885;font:11px/1.2 ui-monospace,SFMono-Regular,Menlo,monospace}
.stream-dot{width:7px;height:7px;border-radius:50%;background:var(--teal)}
.is-streaming .stream-dot{animation:blink 1.15s ease-in-out infinite}
@keyframes blink{0%,100%{opacity:.35}50%{opacity:1}}
.codex-body{padding:4px 11px 11px}
.activity-list{display:grid}
.activity{border-bottom:1px solid #edf0f3}.activity:last-child{border-bottom:0}
.activity summary{min-height:39px;padding:8px 0;display:grid;grid-template-columns:46px minmax(0,1fr) 8px 18px;align-items:center;gap:8px;list-style:none;cursor:pointer;color:#3c4653}
.activity summary::-webkit-details-marker{display:none}
.activity summary:after{content:"+";width:18px;color:#8a94a0;text-align:center;font:15px/1 ui-monospace,SFMono-Regular,Menlo,monospace}
.activity[open] summary:after{content:"-"}
.activity-kind{color:#788390;font:700 9px/1.2 ui-monospace,SFMono-Regular,Menlo,monospace;text-transform:uppercase}
.activity-title{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font:12px/1.35 ui-monospace,SFMono-Regular,Menlo,monospace}
.activity-state{grid-column:3;grid-row:1;width:7px;height:7px;border-radius:50%;background:#aab2bd}
.activity-state.running{background:var(--teal);animation:blink 1.15s ease-in-out infinite}.activity-state.passed{background:var(--green)}.activity-state.failed{background:var(--red)}
.activity-detail{margin:0 0 10px;padding:10px;max-height:210px;overflow:auto;border:1px solid #dce1e7;border-radius:5px;background:#f4f6f8;color:#3e4855;white-space:pre-wrap;overflow-wrap:anywhere;font:11px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace}
.agent-message{margin:9px 0 2px;padding:8px 0;color:#28313d;white-space:pre-wrap;overflow-wrap:anywhere;line-height:1.55}
.stream-note{margin:8px 0 0;padding:8px 10px;border-left:2px solid #c8ced6;color:#626d7a;font-size:12px}
.stream-note.error{border-color:var(--red);color:#842e34}
.usage{margin-top:8px;color:#7b8592;font:10px/1.3 ui-monospace,SFMono-Regular,Menlo,monospace}
.jump{position:sticky;left:100%;bottom:6px;margin-top:8px;padding:6px 9px;border:1px solid #aeb7c3;border-radius:5px;background:#fff;color:#44505e;font-size:11px;font-weight:750;box-shadow:0 3px 12px rgba(20,27,38,.12);cursor:pointer}
.jump[hidden]{display:none}
.refresh{margin-top:10px;border-color:#aeb7c3;background:#fff;color:#35404d}.refresh:hover{background:#f1f4f6}
.composer{padding:16px;border-bottom:1px solid var(--line)}
.composer-label{display:block;margin-bottom:8px;font-weight:760}
textarea{display:block;width:100%;min-height:116px;max-height:260px;resize:vertical;padding:11px 12px;border:1px solid #aeb7c3;border-radius:6px;background:#fff;color:var(--ink);line-height:1.5}
textarea::placeholder{color:#9099a5}
textarea:focus{border-color:var(--teal);outline:3px solid rgba(11,143,143,.15)}
.compose-row{min-height:44px;margin-top:10px;display:flex;align-items:center;justify-content:space-between;gap:12px}
.count{color:#7b8592;font:11px/1.2 ui-monospace,SFMono-Regular,Menlo,monospace}
.button{min-height:38px;padding:9px 14px;border:1px solid transparent;border-radius:5px;font-weight:760;cursor:pointer;text-decoration:none;text-align:center}
.send{background:var(--teal);color:#fff}
.send:hover{background:#087a7a}
.busy{display:none;align-items:center;gap:8px;color:#596474;font-size:12px}
.loading .busy{display:flex}.loading .compose-row{display:none}.loading textarea{opacity:.55;pointer-events:none}
.pulse{display:flex;gap:3px}.pulse i{width:4px;height:12px;background:var(--teal);animation:work 1s ease-in-out infinite}.pulse i:nth-child(2){animation-delay:.15s}.pulse i:nth-child(3){animation-delay:.3s}
@keyframes work{0%,100%{opacity:.3;transform:scaleY(.55)}50%{opacity:1;transform:scaleY(1)}}
.decision{padding:14px 16px 16px;background:#f7f8fa}
.decision-title{margin:0 0 10px;color:#5b6674;font-size:11px;font-weight:780;text-transform:uppercase}
.decision-grid{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:8px}
.decision form{margin:0}.decision button{width:100%}
.accept{background:var(--amber);color:var(--ink);border-color:#e59b24}.accept:hover{background:var(--amber-hover)}
.rollback{background:#fff;color:var(--red);border-color:#d6a8ac}.rollback:hover{background:var(--red-soft)}
.result-actions{display:flex;gap:8px;flex-wrap:wrap}
.terminal-note{padding:18px;color:var(--muted)}
button:focus-visible,a:focus-visible{outline:3px solid rgba(11,143,143,.25);outline-offset:2px}
@media(max-width:520px){.shell{width:100%;margin:0;border-radius:0}.topbar{padding:0 10px}.mark{width:32px;height:32px}.eyebrow{display:none}.title{font-size:14px}.tool{padding:7px 8px}.run-strip{padding:9px 12px}.conversation,.composer{padding:14px}.decision-grid{grid-template-columns:1fr}.decision-grid form{width:100%}.workspace{max-height:calc(100vh - 68px)}.is-panel .workspace{max-height:596px}}
@media(prefers-reduced-motion:reduce){*{animation:none!important;transition:none!important}}
</style>
</head>
<body class="{{if .Panel}}is-panel{{else if .Standalone}}is-window{{else}}is-page{{end}}">
<main class="shell">
  <header class="topbar">
    <div class="identity"><span class="mark" aria-hidden="true">V</span><span><span class="eyebrow">Vessica / Review</span><span class="title">Review workspace</span></span></div>
    <div class="tools">
      {{if .Panel}}<button class="tool" type="button" data-popout data-window="{{.WindowURL}}">Pop out</button><button class="tool primary" type="button" data-toggle aria-expanded="{{if .Open}}true{{else}}false{{end}}">{{if .Open}}Hide{{else}}Review{{end}}</button>{{else if .Standalone}}<button class="tool" type="button" data-close>Close</button>{{end}}
    </div>
  </header>
  <div class="workspace" data-content>
    <section class="run-strip" aria-label="Run status"><span class="live"><span class="live-dot"></span>Preview live</span><span class="run-id">{{.RunID}}</span><span class="badge">Draft PR</span>{{if .PRURL}}<a class="pr-link" href="{{.PRURL}}" target="_blank" rel="noreferrer">View PR</a>{{end}}</section>
    <section class="conversation" data-result aria-live="polite">
      <div class="transcript" data-transcript>
        {{if .Message}}<div class="turn codex-turn"><div class="codex-head"><span class="codex-glyph">C</span><span class="codex-name">Codex</span></div><div class="codex-body"><p class="agent-message">{{.Message}}</p>{{if .Output}}<pre class="agent-output">{{.Output}}</pre>{{end}}{{if and (or .Panel .Standalone) .Refresh}}<div class="result-actions"><button class="button refresh" type="button" data-reload>Refresh preview</button></div>{{end}}</div></div>{{else if .Error}}<p class="message error">{{.Error}}</p>{{else}}<p class="empty">No revisions requested yet.</p>{{end}}
      </div>
      <button class="jump" type="button" data-jump hidden>Jump to latest</button>
    </section>
    {{if .CanPrompt}}<form class="composer" method="post" action="/review/runs/{{.RunID}}/prompt" data-review-control>
      <input type="hidden" name="token" value="{{.Token}}"><input type="hidden" name="panel" value="{{if .Panel}}1{{else}}0{{end}}"><input type="hidden" name="standalone" value="{{if .Standalone}}1{{else}}0{{end}}">
      <label class="composer-label" for="prompt">Request a revision</label>
      <textarea id="prompt" name="prompt" maxlength="4000" required placeholder="Tighten the mobile CTA copy and keep the current layout..."></textarea>
      <div class="compose-row"><span class="count" data-count>0 / 4000</span><button class="button send" type="submit">Send to Codex</button></div>
      <div class="busy" data-busy><span class="pulse" aria-hidden="true"><i></i><i></i><i></i></span><span>Live Codex activity is shown above.</span></div>
    </form>{{end}}
    {{if .CanReview}}<section class="decision" data-review-control><p class="decision-title">Decision</p><div class="decision-grid"><form method="post" action="/review/runs/{{.RunID}}/approve"><input type="hidden" name="token" value="{{.Token}}"><input type="hidden" name="panel" value="{{if .Panel}}1{{else}}0{{end}}"><input type="hidden" name="standalone" value="{{if .Standalone}}1{{else}}0{{end}}"><button class="button accept" type="submit">Accept and Merge</button></form><form method="post" action="/review/runs/{{.RunID}}/rollback" data-rollback><input type="hidden" name="token" value="{{.Token}}"><input type="hidden" name="panel" value="{{if .Panel}}1{{else}}0{{end}}"><input type="hidden" name="standalone" value="{{if .Standalone}}1{{else}}0{{end}}"><button class="button rollback" type="submit">Rollback</button></form></div></section>{{end}}
  </div>
</main>
<script>
(function(){
  var content=document.querySelector('[data-content]'),toggle=document.querySelector('[data-toggle]'),result=document.querySelector('[data-result]'),transcript=document.querySelector('[data-transcript]'),textarea=document.querySelector('textarea'),count=document.querySelector('[data-count]'),jump=document.querySelector('[data-jump]'),isPanel={{if .Panel}}true{{else}}false{{end}};
  var activities=new Map(),messages=new Map(),streamSource=null,streamTimer=null,streamStarted=0,streamState=null,streamElapsed=null,activityList=null,lastAgentMessage='',streamFinished=false;
  function resize(open){if(content)content.style.display=open?'block':'none';if(toggle){toggle.textContent=open?'Hide':'Review';toggle.setAttribute('aria-expanded',String(open))}if(window.parent!==window)window.parent.postMessage({scope:'vessica.review',type:'resize',height:open?680:68},'*')}
  function requestReload(){if(window.parent!==window){window.parent.postMessage({scope:'vessica.review',type:'reload'},'*')}else if(window.opener){window.opener.postMessage({scope:'vessica.review',type:'reload'},'*')}}
  function bindReload(){var reload=document.querySelector('[data-reload]');if(reload)reload.addEventListener('click',requestReload)}
  function make(tag,className,text){var node=document.createElement(tag);if(className)node.className=className;if(text!==undefined)node.textContent=text;return node}
  function nearBottom(){var contentNear=content.scrollHeight-content.scrollTop-content.clientHeight<72;var pageNear=document.documentElement.scrollHeight-window.scrollY-window.innerHeight<72;return contentNear||pageNear}
  function scrollLatest(force){if(!force&&!nearBottom()){jump.hidden=false;return}requestAnimationFrame(function(){content.scrollTop=content.scrollHeight;window.scrollTo(0,document.documentElement.scrollHeight);jump.hidden=true})}
  function append(node){var follow=nearBottom();transcript.appendChild(node);scrollLatest(follow);resize(true)}
  function setStreamState(label,running){if(!streamState)return;streamState.textContent=label;var turn=streamState.closest('.codex-turn');if(turn)turn.classList.toggle('is-streaming',running)}
  function elapsed(){if(!streamElapsed||!streamStarted)return;var seconds=Math.max(0,Math.floor((Date.now()-streamStarted)/1000));streamElapsed.textContent=seconds<60?seconds+'s':Math.floor(seconds/60)+'m '+String(seconds%60).padStart(2,'0')+'s'}
  function beginTranscript(prompt,keepSource){
    if(streamSource&&!keepSource)streamSource.close();if(streamTimer)clearInterval(streamTimer);activities.clear();messages.clear();lastAgentMessage='';streamFinished=false;transcript.replaceChildren();
    var user=make('div','turn user-turn');user.appendChild(make('span','turn-label','You'));user.appendChild(document.createTextNode(prompt));transcript.appendChild(user);
    var turn=make('section','turn codex-turn is-streaming'),head=make('header','codex-head'),body=make('div','codex-body');
    head.appendChild(make('span','codex-glyph','C'));head.appendChild(make('span','codex-name','Codex'));var meta=make('span','codex-meta');meta.appendChild(make('span','stream-dot'));streamState=make('span','','Connecting');streamElapsed=make('span','','0s');meta.appendChild(streamState);meta.appendChild(streamElapsed);head.appendChild(meta);
    activityList=make('div','activity-list');body.appendChild(activityList);turn.appendChild(head);turn.appendChild(body);transcript.appendChild(turn);streamStarted=Date.now();elapsed();streamTimer=setInterval(elapsed,1000);scrollLatest(true);resize(true)
  }
  function eventPayload(event){try{return JSON.parse(event.payload_json||'{}')}catch(_){return {}}}
  function statusClass(status,exitCode){if(Number(exitCode)>0||status==='failed'||status==='error')return'failed';if(status==='completed'||status==='passed'||status==='ok')return'passed';return'running'}
  function kindLabel(kind){return({command:'RUN',file_change:'EDIT',file_read:'READ',search:'FIND',tool:'TOOL'})[kind]||String(kind||'ACT').replaceAll('_',' ').slice(0,7).toUpperCase()}
  function activityTitle(kind,message,status){var done=statusClass(status) !== 'running';var verbs={command:done?'Ran':'Running',file_change:done?'Updated':'Updating',file_read:done?'Read':'Reading',search:done?'Searched':'Searching',tool:done?'Called':'Calling'};return ((verbs[kind]||'Activity')+' '+String(message||'')).trim()}
  function detailText(payload){var detail=payload.detail;if(detail===undefined||detail===null||detail==='')detail=payload.command||'';if(typeof detail==='string')return detail;try{return JSON.stringify(detail,null,2)}catch(_){return String(detail)}}
  function upsertActivity(event,payload){
    var kind=payload.kind||'activity',key=payload.activity_id||'event-'+event.seq,status=statusClass(payload.status,payload.exit_code),row=activities.get(key),detail=detailText(payload);
    if(!row){var details=make('details','activity'),summary=make('summary'),kindNode=make('span','activity-kind'),titleNode=make('span','activity-title'),stateNode=make('span','activity-state');summary.appendChild(kindNode);summary.appendChild(titleNode);summary.appendChild(stateNode);details.appendChild(summary);var detailNode=make('pre','activity-detail');details.appendChild(detailNode);row={details:details,kind:kindNode,title:titleNode,state:stateNode,detail:detailNode};activities.set(key,row);activityList.appendChild(details)}
    row.kind.textContent=kindLabel(kind);row.title.textContent=activityTitle(kind,payload.message,status);row.state.className='activity-state '+status;row.detail.textContent=detail||'No additional output.';row.detail.hidden=!detail;if(status==='failed')row.details.open=true;scrollLatest(false)
  }
  function addAgentMessage(text,event){text=String(text||'').trim();if(!text||text==='codex completed'||!activityList)return;var payload=event?eventPayload(event):{},key=payload.activity_id||'';if(key&&messages.has(key)){messages.get(key).textContent=text;lastAgentMessage=text;scrollLatest(false);return}if(!key&&text===lastAgentMessage)return;lastAgentMessage=text;var node=make('div','agent-message',text);if(key)messages.set(key,node);activityList.appendChild(node);scrollLatest(false)}
  function addNote(text,error){if(!text)return;var node=make('div','stream-note'+(error?' error':''),text);activityList.appendChild(node);scrollLatest(false)}
  function renderEvent(event){
    var payload=eventPayload(event),type=event.type||'';payload.message=payload.message||'';
    if(type==='sandbox.prompt.started'){if(!activityList)beginTranscript(payload.prompt||'Revision request',true);setStreamState('Working',true);return}
    if(type==='agent.activity'){if(payload.kind==='session'){setStreamState('Session started',true);return}if(payload.kind==='turn'||payload.kind==='codex_event'||payload.kind==='prompt')return;upsertActivity(event,payload);return}
    if(type==='agent.message'){addAgentMessage(payload.message,event);return}
    if(type==='agent.usage'){var input=payload.input_tokens||0,output=payload.output_tokens||0;if(input||output)activityList.appendChild(make('div','usage',Number(input).toLocaleString()+' tokens in / '+Number(output).toLocaleString()+' out'));scrollLatest(false);return}
    if(type==='agent.error'||type==='agent.warning'){if(type==='agent.warning'&&!/error|failed/i.test(payload.message))return;addNote(payload.message||'Codex reported an error.',type==='agent.error');return}
    if(type==='repo.branch.updated'){upsertActivity(event,{kind:'file_change',status:'completed',message:'preview branch',detail:payload.commit||''});return}
    if(type==='sandbox.prompt.completed'){streamFinished=true;setStreamState('Completed',false);if(streamSource)streamSource.close();return}
    if(type==='sandbox.prompt.failed'){streamFinished=true;setStreamState('Failed',false);addNote(payload.message||'The refinement failed.',true);if(streamSource)streamSource.close()}
  }
  function startEventStream(token,after){
    streamSource=new EventSource('/review/runs/{{.RunID}}/events?token='+encodeURIComponent(token)+'&after='+encodeURIComponent(after));
    streamSource.onopen=function(){setStreamState('Working',true)};
    streamSource.onmessage=function(message){try{renderEvent(JSON.parse(message.data))}catch(_){}};
    streamSource.onerror=function(){if(!streamFinished)setStreamState('Reconnecting',true)}
  }
  async function resumeEventStream(token){try{var response=await fetch('/review/runs/{{.RunID}}/events?token='+encodeURIComponent(token)+'&session=1'),session=await response.json();if(!response.ok)throw new Error(session.error||'Unable to resume live activity');if(session.found){beginTranscript(session.prompt||'Revision request');if(isPanel)resize(false)}startEventStream(token,session.after||0)}catch(_){}}
  function finishStream(data){
    streamFinished=true;if(streamTimer){clearInterval(streamTimer);streamTimer=null}setStreamState(data.ok?'Completed':'Failed',false);
    if(data.output)addAgentMessage(data.output);addNote(data.ok?data.message:(data.error||'The request failed.'),!data.ok);if(data.ok&&textarea){textarea.value='';textarea.dispatchEvent(new Event('input'))}
    if(data.refresh){var actions=make('div','result-actions'),refresh=make('button','button refresh','Refresh preview');refresh.type='button';refresh.setAttribute('data-reload','');actions.appendChild(refresh);activityList.appendChild(actions);bindReload()}
    if(data.terminal)document.querySelectorAll('[data-review-control]').forEach(function(el){el.remove()});scrollLatest(true);resize(true)
  }
  async function submitPrompt(form){
    var body=new URLSearchParams(new FormData(form)),prompt=body.get('prompt')||'',token=body.get('token')||'';body.set('async','1');beginTranscript(prompt);form.classList.add('loading');form.querySelectorAll('button,textarea').forEach(function(el){el.disabled=true});
    try{var latest=await fetch('/review/runs/{{.RunID}}/events?token='+encodeURIComponent(token)+'&latest=1'),snapshot=await latest.json();if(!latest.ok)throw new Error(snapshot.error||'Unable to start live activity');startEventStream(token,snapshot.seq||0);var response=await fetch(form.action,{method:'POST',body:body}),data=await response.json();finishStream(data)}catch(error){finishStream({ok:false,error:String(error)})}finally{form.classList.remove('loading');form.querySelectorAll('button,textarea').forEach(function(el){el.disabled=false});if(streamSource)setTimeout(function(){if(streamSource)streamSource.close()},700)}
  }
  async function submitAction(form){var body=new URLSearchParams(new FormData(form));body.set('async','1');form.querySelectorAll('button').forEach(function(el){el.disabled=true});try{var response=await fetch(form.action,{method:'POST',body:body}),data=await response.json();if(!activityList)beginTranscript('Review decision');finishStream(data)}catch(error){if(!activityList)beginTranscript('Review decision');finishStream({ok:false,error:String(error)})}finally{form.querySelectorAll('button').forEach(function(el){el.disabled=false})}}
  if(toggle){var open={{if .Open}}true{{else}}false{{end}};toggle.addEventListener('click',function(){open=!open;resize(open)});resize(open)}
  var popout=document.querySelector('[data-popout]'),detachedWindow,detachedPoll;if(popout)popout.addEventListener('click',function(){detachedWindow=window.open(popout.getAttribute('data-window'),'vessica-review-{{.RunID}}','popup=yes,width=480,height=760,resizable=yes,scrollbars=yes');if(!detachedWindow)return;window.parent.postMessage({scope:'vessica.review',type:'detach'},'*');if(detachedPoll)clearInterval(detachedPoll);detachedPoll=setInterval(function(){if(!detachedWindow||detachedWindow.closed){clearInterval(detachedPoll);detachedPoll=null;detachedWindow=null;window.parent.postMessage({scope:'vessica.review',type:'attach'},'*')}},400)});
  var close=document.querySelector('[data-close]');if(close)close.addEventListener('click',function(){window.close()});
  if(textarea&&count){var updateCount=function(){count.textContent=textarea.value.length+' / 4000'};textarea.addEventListener('input',updateCount);updateCount()}
  if(jump)jump.addEventListener('click',function(){scrollLatest(true)});content.addEventListener('scroll',function(){if(!nearBottom())jump.hidden=false});window.addEventListener('scroll',function(){if(!nearBottom())jump.hidden=false});bindReload();
  resumeEventStream({{.Token}});
  window.addEventListener('message',function(event){if(event.data&&event.data.scope==='vessica.review'&&event.data.type==='reload'&&window.parent!==window)window.parent.postMessage(event.data,'*')});
  document.querySelectorAll('form').forEach(function(form){form.addEventListener('submit',function(event){if(form.hasAttribute('data-rollback')&&!confirm('Close the draft PR and stop this preview sandbox?')){event.preventDefault();return}{{if or .Panel .Standalone}}event.preventDefault();if(form.classList.contains('composer'))submitPrompt(form);else submitAction(form);{{else}}form.classList.add('loading');{{end}}})});
})();
</script>
</body>
</html>`))
