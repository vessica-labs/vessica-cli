package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/vessica-labs/vessica-cli/internal/id"
)

var SoftwareEpicPhases = []string{
	"preflight", "harness", "plan", "design", "ticketize",
	"code", "build", "validate", "preview", "pr", "receipt",
}

func (db *DB) CreateRun(ctx context.Context, epicID, ticketID, runner, model, reasoningEffort, sandbox string, concurrency int, preview bool, prMode, startPhase, stopAfter string) (*Run, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	var repository *Repository
	if epicID != "" {
		epic, epicErr := db.GetEpic(ctx, epicID)
		if epicErr != nil {
			return nil, epicErr
		}
		repository, err = db.GetRepository(ctx, epic.RepositoryID)
	} else {
		repository, err = db.GetRepository(ctx, "")
	}
	if err != nil {
		return nil, err
	}
	if concurrency <= 0 {
		concurrency = 3
	}
	if prMode == "" {
		prMode = "none"
	}
	now := Now()
	r := &Run{
		ID:              id.New(id.Run),
		WorkspaceID:     ws.ID,
		RepositoryID:    repository.ID,
		EpicID:          epicID,
		TicketID:        ticketID,
		Workflow:        "software_epic",
		Status:          "pending",
		StartPhase:      startPhase,
		StopAfter:       stopAfter,
		Concurrency:     concurrency,
		Runner:          runner,
		Model:           model,
		ReasoningEffort: reasoningEffort,
		SandboxBackend:  sandbox,
		Preview:         preview,
		PRMode:          prMode,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	previewInt := 0
	if preview {
		previewInt = 1
	}
	_, err = db.Exec(ctx, `INSERT INTO runs(id, workspace_id, repository_id, epic_id, ticket_id, workflow, status, start_phase, stop_after, concurrency, runner, model, reasoning_effort, sandbox_backend, preview, pr_mode, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.WorkspaceID, r.RepositoryID, nullStr(r.EpicID), nullStr(r.TicketID), r.Workflow, r.Status, nullStr(r.StartPhase), nullStr(r.StopAfter),
		r.Concurrency, nullStr(r.Runner), nullStr(r.Model), nullStr(r.ReasoningEffort), nullStr(r.SandboxBackend), previewInt, r.PRMode, r.CreatedAt, r.UpdatedAt)
	if err != nil {
		return nil, err
	}
	for _, p := range SoftwareEpicPhases {
		_, _ = db.Exec(ctx, `INSERT INTO run_phases(run_id, phase, status) VALUES (?,?,?)`, r.ID, p, "pending")
	}
	return r, nil
}

func (db *DB) GetRun(ctx context.Context, runID string) (*Run, error) {
	var r Run
	var epicID, ticketID, cur, start, stop, runner, model, reasoningEffort, sb, previewURL, prURL, receiptID, aset, errStr, started, finished sql.NullString
	var preview int
	err := db.QueryRow(ctx, `SELECT id, workspace_id, repository_id, epic_id, ticket_id, workflow, status, current_phase, start_phase, stop_after, concurrency, runner, model, reasoning_effort, sandbox_backend, preview, pr_mode, preview_url, pr_url, receipt_id, artifact_set_id, error, created_at, updated_at, started_at, finished_at
		FROM runs WHERE id=?`, runID).
		Scan(&r.ID, &r.WorkspaceID, &r.RepositoryID, &epicID, &ticketID, &r.Workflow, &r.Status, &cur, &start, &stop, &r.Concurrency, &runner, &model, &reasoningEffort, &sb, &preview, &r.PRMode, &previewURL, &prURL, &receiptID, &aset, &errStr, &r.CreatedAt, &r.UpdatedAt, &started, &finished)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("run not found: %s", runID)
	}
	if err != nil {
		return nil, err
	}
	r.EpicID = epicID.String
	r.TicketID = ticketID.String
	r.CurrentPhase = cur.String
	r.StartPhase = start.String
	r.StopAfter = stop.String
	r.Runner = runner.String
	r.Model = model.String
	r.ReasoningEffort = reasoningEffort.String
	r.SandboxBackend = sb.String
	r.Preview = preview != 0
	r.PreviewURL = previewURL.String
	r.PRURL = prURL.String
	r.ReceiptID = receiptID.String
	r.ArtifactSetID = aset.String
	r.Error = errStr.String
	r.StartedAt = started.String
	r.FinishedAt = finished.String
	return &r, nil
}

func (db *DB) ListRuns(ctx context.Context) ([]Run, error) {
	rows, err := db.Query(ctx, `SELECT id FROM runs ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		r, err := db.GetRun(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

func (db *DB) ListRunsForRepository(ctx context.Context, repositoryID string) ([]Run, error) {
	rows, err := db.Query(ctx, `SELECT id FROM runs WHERE repository_id=? ORDER BY created_at DESC`, repositoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		var runID string
		if err := rows.Scan(&runID); err != nil {
			return nil, err
		}
		runRecord, err := db.GetRun(ctx, runID)
		if err != nil {
			return nil, err
		}
		out = append(out, *runRecord)
	}
	return out, rows.Err()
}

func (db *DB) UpdateRun(ctx context.Context, r *Run) error {
	r.UpdatedAt = Now()
	previewInt := 0
	if r.Preview {
		previewInt = 1
	}
	_, err := db.Exec(ctx, `UPDATE runs SET status=?, current_phase=?, model=?, reasoning_effort=?, preview_url=?, pr_url=?, pr_mode=?, receipt_id=?, artifact_set_id=?, error=?, started_at=?, finished_at=?, updated_at=?, preview=? WHERE id=?`,
		r.Status, nullStr(r.CurrentPhase), nullStr(r.Model), nullStr(r.ReasoningEffort), nullStr(r.PreviewURL), nullStr(r.PRURL), r.PRMode, nullStr(r.ReceiptID), nullStr(r.ArtifactSetID), nullStr(r.Error), nullStr(r.StartedAt), nullStr(r.FinishedAt), r.UpdatedAt, previewInt, r.ID)
	return err
}

func (db *DB) SetPhaseStatus(ctx context.Context, runID, phase, status, errMsg string) error {
	now := Now()
	switch status {
	case "running":
		_, err := db.Exec(ctx, `UPDATE run_phases SET status=?, started_at=?, error=NULL WHERE run_id=? AND phase=?`, status, now, runID, phase)
		return err
	case "completed", "failed", "skipped":
		_, err := db.Exec(ctx, `UPDATE run_phases SET status=?, finished_at=?, error=? WHERE run_id=? AND phase=?`, status, now, nullStr(errMsg), runID, phase)
		return err
	default:
		_, err := db.Exec(ctx, `UPDATE run_phases SET status=? WHERE run_id=? AND phase=?`, status, runID, phase)
		return err
	}
}

func (db *DB) ListPhases(ctx context.Context, runID string) ([]RunPhase, error) {
	rows, err := db.Query(ctx, `SELECT run_id, phase, status, COALESCE(started_at,''), COALESCE(finished_at,''), COALESCE(error,'') FROM run_phases WHERE run_id=?`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// preserve order
	byPhase := map[string]RunPhase{}
	for rows.Next() {
		var p RunPhase
		if err := rows.Scan(&p.RunID, &p.Phase, &p.Status, &p.StartedAt, &p.FinishedAt, &p.Error); err != nil {
			return nil, err
		}
		byPhase[p.Phase] = p
	}
	var out []RunPhase
	for _, name := range SoftwareEpicPhases {
		if p, ok := byPhase[name]; ok {
			out = append(out, p)
		}
	}
	return out, rows.Err()
}

func (db *DB) CreateSandbox(ctx context.Context, runID, backend, branch string) (*Sandbox, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	now := Now()
	s := &Sandbox{
		ID:          id.New(id.Sandbox),
		RunID:       runID,
		WorkspaceID: ws.ID,
		Backend:     backend,
		Status:      "pending",
		Branch:      branch,
		MetaJSON:    "{}",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, err = db.Exec(ctx, `INSERT INTO sandboxes(id, run_id, workspace_id, backend, status, branch, meta_json, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		s.ID, nullStr(s.RunID), s.WorkspaceID, s.Backend, s.Status, nullStr(s.Branch), s.MetaJSON, s.CreatedAt, s.UpdatedAt)
	return s, err
}

func (db *DB) GetSandbox(ctx context.Context, sandboxID string) (*Sandbox, error) {
	var s Sandbox
	var runID, containerID, branch, previewURL, lastAccessed, expires, retained, destroyed sql.NullString
	var port sql.NullInt64
	err := db.QueryRow(ctx, `SELECT id, run_id, workspace_id, backend, container_id, status, branch, preview_port, preview_url, meta_json, last_accessed_at, expires_at, retained_until, created_at, updated_at, destroyed_at FROM sandboxes WHERE id=?`, sandboxID).
		Scan(&s.ID, &runID, &s.WorkspaceID, &s.Backend, &containerID, &s.Status, &branch, &port, &previewURL, &s.MetaJSON, &lastAccessed, &expires, &retained, &s.CreatedAt, &s.UpdatedAt, &destroyed)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("sandbox not found: %s", sandboxID)
	}
	if err != nil {
		return nil, err
	}
	s.RunID = runID.String
	s.ContainerID = containerID.String
	s.Branch = branch.String
	s.PreviewPort = int(port.Int64)
	s.PreviewURL = previewURL.String
	s.LastAccessedAt = lastAccessed.String
	s.ExpiresAt = expires.String
	s.RetainedUntil = retained.String
	s.DestroyedAt = destroyed.String
	return &s, nil
}

func (db *DB) UpdateSandbox(ctx context.Context, s *Sandbox) error {
	s.UpdatedAt = Now()
	_, err := db.Exec(ctx, `UPDATE sandboxes SET container_id=?, status=?, preview_port=?, preview_url=?, meta_json=?, last_accessed_at=?, expires_at=?, retained_until=?, destroyed_at=?, updated_at=? WHERE id=?`,
		nullStr(s.ContainerID), s.Status, nullInt(s.PreviewPort), nullStr(s.PreviewURL), s.MetaJSON, nullStr(s.LastAccessedAt), nullStr(s.ExpiresAt), nullStr(s.RetainedUntil), nullStr(s.DestroyedAt), s.UpdatedAt, s.ID)
	return err
}

func (db *DB) ListSandboxes(ctx context.Context) ([]Sandbox, error) {
	rows, err := db.Query(ctx, `SELECT id FROM sandboxes ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Sandbox
	for rows.Next() {
		var sid string
		if err := rows.Scan(&sid); err != nil {
			return nil, err
		}
		s, err := db.GetSandbox(ctx, sid)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

func (db *DB) GetSandboxForRun(ctx context.Context, runID string) (*Sandbox, error) {
	var id string
	err := db.QueryRow(ctx, `SELECT id FROM sandboxes WHERE run_id=? ORDER BY created_at DESC LIMIT 1`, runID).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("sandbox not found for run: %s", runID)
	}
	if err != nil {
		return nil, err
	}
	return db.GetSandbox(ctx, id)
}

func (db *DB) ListSandboxesForRun(ctx context.Context, runID string) ([]Sandbox, error) {
	rows, err := db.Query(ctx, `SELECT id FROM sandboxes WHERE run_id=? ORDER BY created_at DESC`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Sandbox
	for rows.Next() {
		var sid string
		if err := rows.Scan(&sid); err != nil {
			return nil, err
		}
		s, err := db.GetSandbox(ctx, sid)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

func (db *DB) AppendEvent(ctx context.Context, runID, sandboxID, typ string, payload any) (*Event, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	seq, err := db.nextSequenceTx(ctx, tx, runID)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(payload)
	e := &Event{
		ID:          id.New(id.Event),
		RunID:       runID,
		SandboxID:   sandboxID,
		Seq:         seq,
		Type:        typ,
		PayloadJSON: string(b),
		CreatedAt:   Now(),
	}
	_, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO events(id, run_id, sandbox_id, seq, type, payload_json, created_at) VALUES (?,?,?,?,?,?,?)`),
		e.ID, nullStr(e.RunID), nullStr(e.SandboxID), e.Seq, e.Type, e.PayloadJSON, e.CreatedAt)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return e, nil
}

func (db *DB) nextSequenceTx(ctx context.Context, tx *sql.Tx, scope string) (int64, error) {
	var seq int64
	query := db.Rebind(`INSERT INTO event_sequences(scope,last_seq) VALUES(?,1)
		ON CONFLICT(scope) DO UPDATE SET last_seq=event_sequences.last_seq+1
		RETURNING last_seq`)
	if err := tx.QueryRowContext(ctx, query, scope).Scan(&seq); err != nil {
		return 0, fmt.Errorf("allocate event sequence for %q: %w", scope, err)
	}
	return seq, nil
}

func (db *DB) ListEvents(ctx context.Context, runID string, afterSeq int64) ([]Event, error) {
	rows, err := db.Query(ctx, `SELECT id, COALESCE(run_id,''), COALESCE(sandbox_id,''), seq, type, payload_json, created_at FROM events WHERE run_id=? AND seq>? ORDER BY seq`, runID, afterSeq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.RunID, &e.SandboxID, &e.Seq, &e.Type, &e.PayloadJSON, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (db *DB) LatestEventSeq(ctx context.Context, runID string) (int64, error) {
	var seq int64
	err := db.QueryRow(ctx, `SELECT COALESCE(MAX(seq),0) FROM events WHERE run_id=?`, runID).Scan(&seq)
	return seq, err
}

func (db *DB) GetEvent(ctx context.Context, eventID string) (*Event, error) {
	var e Event
	err := db.QueryRow(ctx, `SELECT id, COALESCE(run_id,''), COALESCE(sandbox_id,''), seq, type, payload_json, created_at FROM events WHERE id=?`, eventID).
		Scan(&e.ID, &e.RunID, &e.SandboxID, &e.Seq, &e.Type, &e.PayloadJSON, &e.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("event not found: %s", eventID)
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (db *DB) CreateReceipt(ctx context.Context, runID, epicID, status string, body any) (*Receipt, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(body)
	r := &Receipt{
		ID:          id.New(id.Receipt),
		RunID:       runID,
		WorkspaceID: ws.ID,
		EpicID:      epicID,
		Status:      status,
		BodyJSON:    string(b),
		CreatedAt:   Now(),
	}
	_, err = db.Exec(ctx, `INSERT INTO receipts(id, run_id, workspace_id, epic_id, status, body_json, created_at) VALUES (?,?,?,?,?,?,?)`,
		r.ID, r.RunID, r.WorkspaceID, nullStr(r.EpicID), r.Status, r.BodyJSON, r.CreatedAt)
	return r, err
}

func (db *DB) GetReceipt(ctx context.Context, receiptID string) (*Receipt, error) {
	var r Receipt
	var epic sql.NullString
	err := db.QueryRow(ctx, `SELECT id, run_id, workspace_id, epic_id, status, body_json, created_at FROM receipts WHERE id=?`, receiptID).
		Scan(&r.ID, &r.RunID, &r.WorkspaceID, &epic, &r.Status, &r.BodyJSON, &r.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("receipt not found: %s", receiptID)
	}
	r.EpicID = epic.String
	return &r, err
}

func (db *DB) ListReceipts(ctx context.Context) ([]Receipt, error) {
	rows, err := db.Query(ctx, `SELECT id, run_id, workspace_id, COALESCE(epic_id,''), status, body_json, created_at FROM receipts ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Receipt
	for rows.Next() {
		var r Receipt
		if err := rows.Scan(&r.ID, &r.RunID, &r.WorkspaceID, &r.EpicID, &r.Status, &r.BodyJSON, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (db *DB) CreateTrace(ctx context.Context, runID, summary string, body any) (*Trace, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(body)
	t := &Trace{
		ID:          id.New(id.Trace),
		RunID:       runID,
		WorkspaceID: ws.ID,
		Summary:     summary,
		BodyJSON:    string(b),
		CreatedAt:   Now(),
	}
	_, err = db.Exec(ctx, `INSERT INTO traces(id, run_id, workspace_id, summary, body_json, created_at) VALUES (?,?,?,?,?,?)`,
		t.ID, t.RunID, t.WorkspaceID, t.Summary, t.BodyJSON, t.CreatedAt)
	return t, err
}

func (db *DB) GetTrace(ctx context.Context, traceID string) (*Trace, error) {
	var t Trace
	err := db.QueryRow(ctx, `SELECT id, run_id, workspace_id, summary, body_json, created_at FROM traces WHERE id=?`, traceID).
		Scan(&t.ID, &t.RunID, &t.WorkspaceID, &t.Summary, &t.BodyJSON, &t.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("trace not found: %s", traceID)
	}
	return &t, err
}

func (db *DB) ListTraces(ctx context.Context) ([]Trace, error) {
	rows, err := db.Query(ctx, `SELECT id, run_id, workspace_id, summary, body_json, created_at FROM traces ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Trace
	for rows.Next() {
		var t Trace
		if err := rows.Scan(&t.ID, &t.RunID, &t.WorkspaceID, &t.Summary, &t.BodyJSON, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func nullInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}
