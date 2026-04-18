package state

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestHasState(t *testing.T) {
	dir := t.TempDir()
	if HasState(dir) {
		t.Fatal("HasState should return false for empty dir")
	}

	// Write a state file, then check again.
	st := &State{Status: StatusRunning}
	if err := st.Save(dir); err != nil {
		t.Fatal(err)
	}
	if !HasState(dir) {
		t.Fatal("HasState should return true after Save")
	}
}

func TestIsAuditDir(t *testing.T) {
	t.Run("empty dir is not an audit dir", func(t *testing.T) {
		if IsAuditDir(t.TempDir()) {
			t.Fatal("empty dir should not be an audit dir")
		}
	})
	for _, sentinel := range []string{"timing.json", "costs.json", "state.json"} {
		t.Run("detected by "+sentinel, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, sentinel), []byte("{}"), 0644); err != nil {
				t.Fatal(err)
			}
			if !IsAuditDir(dir) {
				t.Fatalf("dir with %s should be an audit dir", sentinel)
			}
		})
	}
	t.Run("unrelated file is not an audit dir", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "readme.md"), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		if IsAuditDir(dir) {
			t.Fatal("dir without sentinel files should not be an audit dir")
		}
	})
}

func TestLoad_NoExistingState(t *testing.T) {
	dir := t.TempDir()
	st, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.PhaseIndex != 0 {
		t.Fatalf("PhaseIndex = %d, want 0", st.PhaseIndex)
	}
	if st.GetStatus() != StatusRunning {
		t.Fatalf("Status = %q, want running", st.GetStatus())
	}
}

func TestSaveAndLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	original := &State{
		PhaseIndex: 3,
		Ticket:     "ABC-123",
		Status:     StatusCompleted,
	}
	if err := original.Save(dir); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PhaseIndex != 3 {
		t.Fatalf("PhaseIndex = %d, want 3", loaded.PhaseIndex)
	}
	if loaded.GetTicket() != "ABC-123" {
		t.Fatalf("Ticket = %q", loaded.GetTicket())
	}
	if loaded.Status != StatusCompleted {
		t.Fatalf("Status = %q", loaded.Status)
	}
}

func TestAdvance(t *testing.T) {
	s := &State{PhaseIndex: 2}
	s.Advance()
	if s.GetPhaseIndex() != 3 {
		t.Fatalf("PhaseIndex = %d, want 3", s.GetPhaseIndex())
	}
}

func TestSetPhase(t *testing.T) {
	s := &State{PhaseIndex: 5}
	s.SetPhase(1)
	if s.GetPhaseIndex() != 1 {
		t.Fatalf("PhaseIndex = %d, want 1", s.GetPhaseIndex())
	}
}

func TestSaveAndLoad_RoundTrip_WithSessionID(t *testing.T) {
	dir := t.TempDir()
	original := &State{
		PhaseIndex:     2,
		Ticket:         "ABC-123",
		Status:         StatusInterrupted,
		PhaseSessionID: "session-uuid-1234",
	}
	if err := original.Save(dir); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.GetSessionID() != "session-uuid-1234" {
		t.Fatalf("PhaseSessionID = %q, want %q", loaded.GetSessionID(), "session-uuid-1234")
	}
}

func TestSaveAndLoad_RoundTrip_WithFailure(t *testing.T) {
	dir := t.TempDir()
	original := &State{
		PhaseIndex:      1,
		Ticket:          "T-1",
		Status:          StatusFailed,
		FailureCategory: FailCategoryAgentError,
		FailureDetail:   "non-zero exit",
	}
	if err := original.Save(dir); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.GetFailureCategory() != FailCategoryAgentError {
		t.Fatalf("FailureCategory = %q, want %q", loaded.GetFailureCategory(), FailCategoryAgentError)
	}
	if loaded.GetFailureDetail() != "non-zero exit" {
		t.Fatalf("FailureDetail = %q, want %q", loaded.GetFailureDetail(), "non-zero exit")
	}
}

func TestSaveAndLoad_RoundTrip_WithWorkflow(t *testing.T) {
	dir := t.TempDir()
	original := &State{
		PhaseIndex: 1,
		Ticket:     "T-001",
		Status:     StatusRunning,
		Workflow:   "bugfix",
	}
	if err := original.Save(dir); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.GetWorkflow() != "bugfix" {
		t.Fatalf("Workflow = %q, want %q", loaded.GetWorkflow(), "bugfix")
	}
}

func TestAdvance_ClearsSessionID(t *testing.T) {
	s := &State{PhaseIndex: 2, PhaseSessionID: "session-123"}
	s.Advance()
	if s.GetSessionID() != "" {
		t.Fatalf("PhaseSessionID = %q after Advance, want empty", s.GetSessionID())
	}
	if s.GetPhaseIndex() != 3 {
		t.Fatalf("PhaseIndex = %d, want 3", s.GetPhaseIndex())
	}
}

func TestSetPhase_ClearsSessionID(t *testing.T) {
	s := &State{PhaseIndex: 5, PhaseSessionID: "session-456"}
	s.SetPhase(1)
	if s.GetSessionID() != "" {
		t.Fatalf("PhaseSessionID = %q after SetPhase, want empty", s.GetSessionID())
	}
	if s.GetPhaseIndex() != 1 {
		t.Fatalf("PhaseIndex = %d, want 1", s.GetPhaseIndex())
	}
}

func TestLoad_BackwardsCompatible_NoSessionID(t *testing.T) {
	dir := t.TempDir()
	// Write a state.json without phase_session_id (old format)
	data := []byte(`{"phase_index":1,"ticket":"T-1","status":"interrupted"}`)
	if err := os.WriteFile(filepath.Join(dir, "state.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	st, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.PhaseSessionID != "" {
		t.Fatalf("PhaseSessionID = %q, want empty for old format", st.PhaseSessionID)
	}
}

func TestLoad_BackwardsCompatible_NoFailureCategory(t *testing.T) {
	dir := t.TempDir()
	// Write a state.json without failure fields (old format)
	data := []byte(`{"phase_index":1,"ticket":"T-1","status":"failed"}`)
	if err := os.WriteFile(filepath.Join(dir, "state.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	st, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.GetFailureCategory() != "" {
		t.Fatalf("FailureCategory = %q, want empty for old format", st.GetFailureCategory())
	}
	if st.GetFailureDetail() != "" {
		t.Fatalf("FailureDetail = %q, want empty for old format", st.GetFailureDetail())
	}
}

func TestListTickets_Empty(t *testing.T) {
	dir := t.TempDir()
	tickets, err := ListTickets(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 0 {
		t.Fatalf("expected 0 tickets, got %d", len(tickets))
	}
}

func TestListTickets_NoDir(t *testing.T) {
	tickets, err := ListTickets(filepath.Join(t.TempDir(), "nonexistent"), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 0 {
		t.Fatalf("expected 0 tickets, got %d", len(tickets))
	}
}

func TestListTickets_MultipleTickets(t *testing.T) {
	dir := t.TempDir()

	for _, ticket := range []string{"T-001", "T-002"} {
		ad := filepath.Join(dir, ticket)
		os.MkdirAll(ad, 0755)
		st := &State{PhaseIndex: 2, Ticket: ticket, Status: StatusCompleted}
		st.Save(ad)
	}

	tickets, err := ListTickets(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 2 {
		t.Fatalf("expected 2 tickets, got %d", len(tickets))
	}
	if tickets[0].Ticket != "T-001" {
		t.Fatalf("tickets[0].Ticket = %q, want T-001", tickets[0].Ticket)
	}
	if tickets[1].Ticket != "T-002" {
		t.Fatalf("tickets[1].Ticket = %q, want T-002", tickets[1].Ticket)
	}
}

func TestListTickets_SkipDirWithoutState(t *testing.T) {
	dir := t.TempDir()

	ad := filepath.Join(dir, "T-001")
	os.MkdirAll(ad, 0755)
	st := &State{PhaseIndex: 1, Ticket: "T-001", Status: StatusRunning}
	st.Save(ad)

	os.MkdirAll(filepath.Join(dir, "empty-dir"), 0755)

	tickets, err := ListTickets(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 {
		t.Fatalf("expected 1 ticket, got %d", len(tickets))
	}
	if tickets[0].Ticket != "T-001" {
		t.Fatalf("tickets[0].Ticket = %q, want T-001", tickets[0].Ticket)
	}
}

// --- New tests for multi-workflow support ---

func TestListTickets_WorkflowNamespaced(t *testing.T) {
	base := t.TempDir()

	// Create artifacts/bugfix/T-001/state.json
	ticketDir := filepath.Join(base, "bugfix", "T-001")
	os.MkdirAll(ticketDir, 0755)
	st := &State{PhaseIndex: 1, Ticket: "T-001", Workflow: "bugfix", Status: StatusCompleted}
	st.Save(ticketDir)

	tickets, err := ListTickets(base, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 {
		t.Fatalf("expected 1 ticket, got %d", len(tickets))
	}
	if tickets[0].Ticket != "T-001" {
		t.Fatalf("Ticket = %q, want T-001", tickets[0].Ticket)
	}
	if tickets[0].State.Workflow != "bugfix" {
		t.Fatalf("Workflow = %q, want bugfix", tickets[0].State.Workflow)
	}
}

func TestListTickets_WorkflowNamespaced_InfersWorkflow(t *testing.T) {
	// State file has no workflow field — ListTickets infers it from the dir name
	base := t.TempDir()

	ticketDir := filepath.Join(base, "refactor", "T-002")
	os.MkdirAll(ticketDir, 0755)
	// Save state WITHOUT workflow field set
	st := &State{PhaseIndex: 0, Ticket: "T-002", Status: StatusRunning}
	st.Save(ticketDir)

	tickets, err := ListTickets(base, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 {
		t.Fatalf("expected 1 ticket, got %d", len(tickets))
	}
	if tickets[0].State.Workflow != "refactor" {
		t.Fatalf("Workflow = %q, want refactor (inferred from dir name)", tickets[0].State.Workflow)
	}
}

func TestListTickets_Mixed(t *testing.T) {
	// Flat ticket T-001 and workflow-namespaced bugfix/T-002 coexist
	base := t.TempDir()

	// Flat
	flatDir := filepath.Join(base, "T-001")
	os.MkdirAll(flatDir, 0755)
	stFlat := &State{PhaseIndex: 2, Ticket: "T-001", Status: StatusCompleted}
	stFlat.Save(flatDir)

	// Namespaced
	nsDir := filepath.Join(base, "bugfix", "T-002")
	os.MkdirAll(nsDir, 0755)
	stNS := &State{PhaseIndex: 1, Ticket: "T-002", Workflow: "bugfix", Status: StatusRunning}
	stNS.Save(nsDir)

	tickets, err := ListTickets(base, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 2 {
		t.Fatalf("expected 2 tickets, got %d", len(tickets))
	}

	// Find each by ticket name (order may vary)
	byName := make(map[string]TicketSummary)
	for _, ts := range tickets {
		byName[ts.Ticket] = ts
	}

	t1, ok := byName["T-001"]
	if !ok {
		t.Fatal("T-001 not found in results")
	}
	if t1.State.Workflow != "" {
		t.Fatalf("T-001 Workflow = %q, want empty (flat layout)", t1.State.Workflow)
	}

	t2, ok := byName["T-002"]
	if !ok {
		t.Fatal("T-002 not found in results")
	}
	if t2.State.Workflow != "bugfix" {
		t.Fatalf("T-002 Workflow = %q, want bugfix", t2.State.Workflow)
	}
}

func TestListTickets_HistoryLayout(t *testing.T) {
	base := t.TempDir()

	// Ticket with state only in history/ (no top-level state.json)
	histDir := filepath.Join(base, "T-001", "history", "2026-03-22T10-00-00.000")
	os.MkdirAll(histDir, 0755)
	st := &State{PhaseIndex: 5, Ticket: "T-001", Status: StatusCompleted}
	st.Save(histDir)

	tickets, err := ListTickets(base, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 {
		t.Fatalf("expected 1 ticket, got %d", len(tickets))
	}
	if tickets[0].Ticket != "T-001" {
		t.Fatalf("tickets[0].Ticket = %q, want T-001", tickets[0].Ticket)
	}
}

func TestListTickets_WorkflowNamespaced_History(t *testing.T) {
	base := t.TempDir()

	// Workflow-namespaced ticket with state in history/
	histDir := filepath.Join(base, "bugfix", "T-001", "history", "2026-03-22T10-00-00.000")
	os.MkdirAll(histDir, 0755)
	st := &State{PhaseIndex: 3, Ticket: "T-001", Workflow: "bugfix", Status: StatusCompleted}
	st.Save(histDir)

	tickets, err := ListTickets(base, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 {
		t.Fatalf("expected 1 ticket, got %d", len(tickets))
	}
	if tickets[0].Ticket != "T-001" {
		t.Fatalf("tickets[0].Ticket = %q, want T-001", tickets[0].Ticket)
	}
	if tickets[0].State.Workflow != "bugfix" {
		t.Fatalf("Workflow = %q, want bugfix", tickets[0].State.Workflow)
	}
}

func TestArtifactsDirForWorkflow_Empty(t *testing.T) {
	got := ArtifactsDirForWorkflow("/proj", "", "T-001")
	want := "/proj/.orc/artifacts/T-001"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestArtifactsDirForWorkflow_Named(t *testing.T) {
	got := ArtifactsDirForWorkflow("/proj", "bugfix", "T-001")
	want := "/proj/.orc/artifacts/bugfix/T-001"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAuditDirForWorkflow_Empty(t *testing.T) {
	got := AuditDirForWorkflow("/proj", "", "T-001")
	want := "/proj/.orc/audit/T-001"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAuditDirForWorkflow_Named(t *testing.T) {
	got := AuditDirForWorkflow("/proj", "bugfix", "T-001")
	want := "/proj/.orc/audit/bugfix/T-001"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAuditBaseDirForWorkflow_Empty(t *testing.T) {
	got := AuditBaseDirForWorkflow("/proj", "")
	want := "/proj/.orc/audit"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAuditBaseDirForWorkflow_Named(t *testing.T) {
	got := AuditBaseDirForWorkflow("/proj", "bugfix")
	want := "/proj/.orc/audit/bugfix"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestState_ConcurrentAccess(t *testing.T) {
	s := &State{PhaseIndex: 0, Ticket: "T-1", Status: StatusRunning}
	dir := t.TempDir()
	var wg sync.WaitGroup
	wg.Add(8)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			s.SetStatus(StatusRunning)
			s.GetStatus()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			s.Advance()
			s.GetPhaseIndex()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			s.SetSessionID("test")
			s.GetSessionID()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_ = s.Save(dir)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			s.SetTicket("T-concurrent")
			s.GetTicket()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			s.SetWorkflow("test-workflow")
			s.GetWorkflow()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			s.SetPhase(i % 10)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			s.SetFailure(FailCategoryAgentError, "test detail")
			s.GetFailureCategory()
			s.GetFailureDetail()
		}
	}()
	wg.Wait()
}

func writeStateJSON(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(`{"status":"completed"}`), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveStateDir_LiveArtifacts(t *testing.T) {
	dir := t.TempDir()
	writeStateJSON(t, dir)

	got, err := ResolveStateDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != dir {
		t.Fatalf("expected %q, got %q", dir, got)
	}
}

func TestResolveStateDir_ArchivedRun(t *testing.T) {
	dir := t.TempDir()
	histEntry := filepath.Join(dir, "history", "20260322T120000")
	if err := os.MkdirAll(histEntry, 0755); err != nil {
		t.Fatal(err)
	}
	writeStateJSON(t, histEntry)

	got, err := ResolveStateDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != histEntry {
		t.Fatalf("expected %q, got %q", histEntry, got)
	}
}

func TestResolveStateDir_NoRun(t *testing.T) {
	dir := t.TempDir()

	_, err := ResolveStateDir(dir)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestResolveStateDir_PartialArchive(t *testing.T) {
	dir := t.TempDir()
	// Create history dir with an entry but NO state.json inside it
	histEntry := filepath.Join(dir, "history", "20260322T120000")
	if err := os.MkdirAll(histEntry, 0755); err != nil {
		t.Fatal(err)
	}
	// Do NOT write state.json — this simulates a partial archive

	_, err := ResolveStateDir(dir)
	if err == nil {
		t.Fatal("expected error for partial archive (history dir without state.json), got nil")
	}
}

func TestResolveStateDir_PreferLiveOverHistory(t *testing.T) {
	dir := t.TempDir()
	writeStateJSON(t, dir)
	histEntry := filepath.Join(dir, "history", "20260322T120000")
	if err := os.MkdirAll(histEntry, 0755); err != nil {
		t.Fatal(err)
	}
	writeStateJSON(t, histEntry)

	got, err := ResolveStateDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != dir {
		t.Fatalf("expected live dir %q, got %q", dir, got)
	}
}
