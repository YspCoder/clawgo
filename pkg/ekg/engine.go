package ekg

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Event struct {
	Time     string `json:"time"`
	TaskID   string `json:"task_id,omitempty"`
	Session  string `json:"session,omitempty"`
	Channel  string `json:"channel,omitempty"`
	Source   string `json:"source,omitempty"`
	Status   string `json:"status"` // success|error|suppressed
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	ErrSig   string `json:"errsig,omitempty"`
	Log      string `json:"log,omitempty"`
}

type SignalContext struct {
	TaskID  string
	ErrSig  string
	Source  string
	Channel string
}

type Advice struct {
	ShouldEscalate  bool     `json:"should_escalate"`
	RetryBackoffSec int      `json:"retry_backoff_sec"`
	Reason          []string `json:"reason"`
}

type Engine struct {
	workspace                 string
	path                      string
	snapshotPath              string
	recentLines               int
	consecutiveErrorThreshold int
}

func New(workspace string) *Engine {
	ws := strings.TrimSpace(workspace)
	p := filepath.Join(ws, "memory", "ekg-events.jsonl")
	sp := filepath.Join(ws, "memory", "ekg-snapshot.json")
	return &Engine{workspace: ws, path: p, snapshotPath: sp, recentLines: 2000, consecutiveErrorThreshold: 3}
}

func (e *Engine) SetConsecutiveErrorThreshold(v int) {
	if e == nil {
		return
	}
	if v <= 0 {
		v = 3
	}
	e.consecutiveErrorThreshold = v
}

func (e *Engine) Record(ev Event) {
	if e == nil || strings.TrimSpace(e.path) == "" {
		return
	}
	if strings.TrimSpace(ev.Time) == "" {
		ev.Time = time.Now().UTC().Format(time.RFC3339)
	}
	ev.TaskID = strings.TrimSpace(ev.TaskID)
	ev.Session = strings.TrimSpace(ev.Session)
	ev.Channel = strings.TrimSpace(ev.Channel)
	ev.Source = strings.TrimSpace(ev.Source)
	ev.Status = strings.TrimSpace(strings.ToLower(ev.Status))
	ev.Provider = strings.TrimSpace(ev.Provider)
	ev.Model = strings.TrimSpace(ev.Model)
	if ev.Status == "error" && ev.ErrSig == "" && ev.Log != "" {
		ev.ErrSig = NormalizeErrorSignature(ev.Log)
	}
	if ev.ErrSig != "" {
		ev.ErrSig = NormalizeErrorSignature(ev.ErrSig)
	}
	_ = os.MkdirAll(filepath.Dir(e.path), 0o755)
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	f, err := os.OpenFile(e.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

func (e *Engine) GetAdvice(ctx SignalContext) Advice {
	adv := Advice{ShouldEscalate: false, RetryBackoffSec: 30, Reason: []string{}}
	if e == nil {
		return adv
	}
	taskID := strings.TrimSpace(ctx.TaskID)
	errSig := NormalizeErrorSignature(ctx.ErrSig)
	if taskID == "" || errSig == "" {
		return adv
	}
	events := e.readRecentEvents()
	if len(events) == 0 {
		return adv
	}
	consecutive := 0
	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		if strings.TrimSpace(ev.TaskID) != taskID {
			continue
		}
		evErr := NormalizeErrorSignature(ev.ErrSig)
		if evErr == "" {
			evErr = NormalizeErrorSignature(ev.Log)
		}
		if evErr != errSig {
			continue
		}
		if strings.ToLower(strings.TrimSpace(ev.Status)) == "error" {
			consecutive++
			if consecutive >= e.consecutiveErrorThreshold {
				adv.ShouldEscalate = true
				adv.RetryBackoffSec = 300
				adv.Reason = append(adv.Reason, "repeated_error_signature")
				adv.Reason = append(adv.Reason, "same task and error signature exceeded threshold")
				return adv
			}
			// Memory-linked fast path: if this errsig was documented as incident, escalate one step earlier.
			if consecutive >= e.consecutiveErrorThreshold-1 && e.hasMemoryIncident(errSig) {
				adv.ShouldEscalate = true
				adv.RetryBackoffSec = 300
				adv.Reason = append(adv.Reason, "memory_linked_repeated_error_signature")
				adv.Reason = append(adv.Reason, "same errsig already recorded in memory incident")
				return adv
			}
			continue
		}
		// Same signature but success/suppressed encountered: reset chain.
		break
	}
	return adv
}

func (e *Engine) readRecentEvents() []Event {
	if strings.TrimSpace(e.path) == "" {
		return nil
	}
	snapshotEvents, snapshotLines := e.readSnapshot()
	f, err := os.Open(e.path)
	if err != nil {
		return snapshotEvents
	}
	defer f.Close()
	lines := make([]string, 0, e.recentLines)
	s := bufio.NewScanner(f)
	lineNo := 0
	for s.Scan() {
		lineNo++
		if lineNo <= snapshotLines {
			continue
		}
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		lines = append(lines, line)
		if len(lines) > e.recentLines {
			lines = lines[1:]
		}
	}
	out := append([]Event{}, snapshotEvents...)
	for _, l := range lines {
		var ev Event
		if json.Unmarshal([]byte(l), &ev) == nil {
			out = append(out, ev)
		}
	}
	if len(out) > e.recentLines {
		out = out[len(out)-e.recentLines:]
	}
	e.writeSnapshot(out, lineNo)
	return out
}

type snapshotFile struct {
	UpdatedAt string  `json:"updated_at"`
	LineCount int     `json:"line_count"`
	Events    []Event `json:"events"`
}

func (e *Engine) readSnapshot() ([]Event, int) {
	if e == nil || strings.TrimSpace(e.snapshotPath) == "" {
		return nil, 0
	}
	b, err := os.ReadFile(e.snapshotPath)
	if err != nil || len(b) == 0 {
		return nil, 0
	}
	var snap snapshotFile
	if json.Unmarshal(b, &snap) != nil {
		return nil, 0
	}
	if snap.LineCount < 0 {
		snap.LineCount = 0
	}
	if len(snap.Events) > e.recentLines {
		snap.Events = snap.Events[len(snap.Events)-e.recentLines:]
	}
	return snap.Events, snap.LineCount
}

func (e *Engine) writeSnapshot(events []Event, lineCount int) {
	if e == nil || strings.TrimSpace(e.snapshotPath) == "" || lineCount <= 0 {
		return
	}
	if len(events) > e.recentLines {
		events = events[len(events)-e.recentLines:]
	}
	snap := snapshotFile{UpdatedAt: time.Now().UTC().Format(time.RFC3339), LineCount: lineCount, Events: events}
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(e.snapshotPath), 0o755)
	tmp := e.snapshotPath + ".tmp"
	if os.WriteFile(tmp, append(b, '\n'), 0o644) == nil {
		_ = os.Rename(tmp, e.snapshotPath)
	}
}

var (
	rePathNum = regexp.MustCompile(`\b\d+\b`)
	rePathHex = regexp.MustCompile(`\b0x[0-9a-fA-F]+\b`)
	rePathWin = regexp.MustCompile(`[a-zA-Z]:\\[^\s]+`)
	rePathNix = regexp.MustCompile(`/[^\s]+`)
	reSpace   = regexp.MustCompile(`\s+`)
)

func (e *Engine) RankProviders(candidates []string) []string {
	return e.RankProvidersForError(candidates, "")
}

func (e *Engine) RankProvidersForError(candidates []string, errSig string) []string {
	if len(candidates) <= 1 || e == nil {
		return append([]string(nil), candidates...)
	}
	errSig = NormalizeErrorSignature(errSig)
	events := e.readRecentEvents()
	score := map[string]float64{}
	for _, c := range candidates {
		score[c] = 0
	}
	for _, ev := range events {
		p := strings.TrimSpace(ev.Provider)
		if p == "" {
			continue
		}
		if _, ok := score[p]; !ok {
			continue
		}
		weight := 1.0
		evSig := NormalizeErrorSignature(ev.ErrSig)
		if errSig != "" {
			if evSig == errSig {
				weight = 2.5
			} else if evSig != "" {
				weight = 0.4
			}
		}
		switch strings.ToLower(strings.TrimSpace(ev.Status)) {
		case "success":
			score[p] += 1.0 * weight
		case "suppressed":
			score[p] += 0.2 * weight
		case "error":
			score[p] -= 1.0 * weight
		}
	}
	ordered := append([]string(nil), candidates...)
	sort.SliceStable(ordered, func(i, j int) bool {
		si, sj := score[ordered[i]], score[ordered[j]]
		if si == sj {
			return ordered[i] < ordered[j]
		}
		return si > sj
	})
	return ordered
}

func (e *Engine) hasMemoryIncident(errSig string) bool {
	if e == nil || strings.TrimSpace(e.workspace) == "" {
		return false
	}
	errSig = NormalizeErrorSignature(errSig)
	if errSig == "" {
		return false
	}
	needle := "[EKG_INCIDENT]"
	candidates := []string{
		filepath.Join(e.workspace, "MEMORY.md"),
		filepath.Join(e.workspace, "memory", time.Now().UTC().Format("2006-01-02")+".md"),
		filepath.Join(e.workspace, "memory", time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")+".md"),
	}
	for _, p := range candidates {
		b, err := os.ReadFile(p)
		if err != nil || len(b) == 0 {
			continue
		}
		txt := strings.ToLower(string(b))
		if strings.Contains(txt, strings.ToLower(needle)) && strings.Contains(txt, errSig) {
			return true
		}
	}
	return false
}

func NormalizeErrorSignature(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}
	s = rePathWin.ReplaceAllString(s, "<path>")
	s = rePathNix.ReplaceAllString(s, "<path>")
	s = rePathHex.ReplaceAllString(s, "<hex>")
	s = rePathNum.ReplaceAllString(s, "<n>")
	s = reSpace.ReplaceAllString(s, " ")
	if len(s) > 240 {
		s = s[:240]
	}
	return s
}
