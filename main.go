// trim-1-demo: a local page for seeing what trim-1 does with a repository.
//
// Every question runs trim-1 over the repository's files joined into one
// context (one "==> path <==" header per file). The page shows each line's
// score, which files those scores rank highest, and the compressed text that
// trim-1 hands back. Every run is saved as a JSON trace.
//
// Usage: go run . [-port 8010] [-ext .py] /path/to/repo
package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	trim "github.com/caveman-ai/trim-1"
)

//go:embed index.html
var page []byte

const (
	traceVersion = 1
	topFiles     = 20 // files listed in a trace, by best line score
	topLines     = 30 // lines listed in a trace, by score
)

var (
	repo    string
	exts    []string // empty means every text file
	traces  = "traces"
	runLock sync.Mutex // one question at a time, so timings aren't shared with another run
)

type file struct {
	Path  string
	Lines []string
}

type Trace struct {
	Version    int       `json:"trace_version"`
	ID         string    `json:"id"`
	StartedAt  string    `json:"started_at"`
	Query      string    `json:"query"`
	Point      string    `json:"point"`
	Policy     any       `json:"policy"`
	Model      string    `json:"model"`
	Repo       RepoInfo  `json:"repo"`
	Input      Input     `json:"input"`
	Timings    Timings   `json:"timings"`
	Files      []FileHit `json:"files"`
	Lines      []LineHit `json:"lines"`
	Output     Output    `json:"output"`
	ReviewNote string    `json:"review_notes"`
}

type RepoInfo struct {
	Path        string   `json:"path"`
	Commit      string   `json:"commit,omitempty"`
	Uncommitted bool     `json:"uncommitted_changes"`
	Extensions  []string `json:"extensions"`
}

type Input struct {
	Files int `json:"files"`
	Lines int `json:"lines"`
	Bytes int `json:"bytes"`
}

type Timings struct {
	LoadMs     float64 `json:"load_ms"`     // reading the files from disk
	ScoreMs    float64 `json:"score_ms"`    // trim.Score: a probability for every line
	CompressMs float64 `json:"compress_ms"` // trim.Compress: Score plus choosing and rendering lines
}

type FileHit struct {
	Rank      int     `json:"rank"`
	Path      string  `json:"path"`
	BestScore float64 `json:"best_score"`
	BestLine  int     `json:"best_line"`
	Lines     int     `json:"lines"`
	Kept      int     `json:"kept_lines"`
}

type LineHit struct {
	Rank  int     `json:"rank"`
	Path  string  `json:"path"`
	Line  int     `json:"line"`
	Score float64 `json:"score"`
	Text  string  `json:"text"`
}

type Output struct {
	Unchanged    bool   `json:"unchanged"`
	Reason       string `json:"unchanged_reason,omitempty"`
	KeptLines    int    `json:"kept_lines"`
	RemovedLines int    `json:"removed_lines"`
	Bytes        int    `json:"bytes"`
	Text         string `json:"text"`
}

func main() {
	defaultPort := 8010
	if p, err := strconv.Atoi(os.Getenv("PORT")); err == nil {
		defaultPort = p
	}
	port := flag.Int("port", defaultPort, "port for the page (or set PORT)")
	ext := flag.String("ext", "", "comma-separated file extensions to search, such as .py,.cpp (default: every text file)")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: go run . [-port 8010] [-ext .py] /path/to/repo")
		os.Exit(2)
	}
	repo, _ = filepath.Abs(flag.Arg(0))
	for _, e := range strings.Split(*ext, ",") {
		if e = strings.TrimSpace(e); e != "" {
			exts = append(exts, e)
		}
	}
	files := loadRepo()
	log.Printf("%s: %d files", repo, len(files))

	http.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(page)
	})
	http.HandleFunc("GET /info", func(w http.ResponseWriter, r *http.Request) {
		info, _ := trim.ModelInfo()
		points := []string{}
		for p := range trim.Points {
			points = append(points, p)
		}
		sort.Strings(points)
		sendJSON(w, 200, map[string]any{"repo": repo, "extensions": exts, "files": len(loadRepo()), "model": info, "points": points, "default_point": trim.DefaultPoint})
	})
	http.HandleFunc("GET /traces", func(w http.ResponseWriter, r *http.Request) {
		sendJSON(w, 200, listTraces())
	})
	http.HandleFunc("GET /traces/{day}/{name}", func(w http.ResponseWriter, r *http.Request) {
		day, name := r.PathValue("day"), r.PathValue("name")
		if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`).MatchString(day) || !regexp.MustCompile(`^[a-z0-9-]+\.json$`).MatchString(name) {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(traces, day, name))
	})
	http.HandleFunc("POST /ask", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Query, Point string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Query) == "" {
			sendJSON(w, 400, map[string]string{"error": "send {\"query\": \"...\"}"})
			return
		}
		if _, ok := trim.Points[req.Point]; !ok {
			req.Point = trim.DefaultPoint
		}
		runLock.Lock()
		t, err := run(strings.TrimSpace(req.Query), req.Point)
		runLock.Unlock()
		if err != nil {
			sendJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		sendJSON(w, 200, t)
	})

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	log.Printf("open http://%s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

// run answers one question and saves its trace.
func run(query, point string) (*Trace, error) {
	start := time.Now()
	t := &Trace{Version: traceVersion, StartedAt: start.Format(time.RFC3339), Query: query, Point: point, Policy: trim.Points[point]}
	t.Model, _ = trim.ModelInfo()
	t.Repo = RepoInfo{Path: repo, Extensions: exts}
	t.Repo.Commit, t.Repo.Uncommitted = gitInfo()

	files := loadRepo()
	ctx, lineFile, lineNo := joinFiles(files)
	t.Timings.LoadMs = ms(time.Since(start))
	t.Input = Input{Files: len(files), Lines: len(lineFile), Bytes: len(ctx)}

	s := time.Now()
	_, scores, err := trim.Score(ctx, query)
	if err != nil {
		return nil, err
	}
	t.Timings.ScoreMs = ms(time.Since(s))
	s = time.Now()
	res := trim.Compress(ctx, query, trim.Options{Point: point})
	t.Timings.CompressMs = ms(time.Since(s))
	keep := keptMask(res, len(lineFile))

	// Files, ranked by their best line.
	hits := make([]FileHit, len(files))
	for i, f := range files {
		hits[i] = FileHit{Path: f.Path, Lines: len(f.Lines)}
	}
	for i, fi := range lineFile {
		h := &hits[fi]
		if scores[i] > h.BestScore {
			h.BestScore, h.BestLine = scores[i], lineNo[i]
		}
		if keep[i] && lineNo[i] > 0 {
			h.Kept++
		}
	}
	sort.SliceStable(hits, func(a, b int) bool { return hits[a].BestScore > hits[b].BestScore })
	for i := range hits {
		hits[i].Rank = i + 1
	}
	t.Files = hits[:min(topFiles, len(hits))]

	// Lines, ranked by score. Header lines ("==> path <==") are left out.
	order := make([]int, 0, len(scores))
	for i := range scores {
		if scores[i] > 0 && lineNo[i] > 0 {
			order = append(order, i)
		}
	}
	sort.SliceStable(order, func(a, b int) bool { return scores[order[a]] > scores[order[b]] })
	for r, i := range order[:min(topLines, len(order))] {
		t.Lines = append(t.Lines, LineHit{Rank: r + 1, Path: files[lineFile[i]].Path, Line: lineNo[i], Score: scores[i], Text: files[lineFile[i]].Lines[lineNo[i]-1]})
	}

	t.Output = Output{Unchanged: res.Unchanged, Reason: res.Reason, KeptLines: res.KeptLines, RemovedLines: res.RemovedLines, Bytes: len(res.Output), Text: res.Output}
	return t, saveTrace(t)
}

// loadRepo reads every git-tracked text file (no NUL bytes, not empty) that
// matches the extensions, at its repo-relative path.
func loadRepo() []file {
	out, err := exec.Command("git", "-C", repo, "ls-files").Output()
	if err != nil {
		log.Fatalf("%s is not a git repository: %v", repo, err)
	}
	var files []file
	for _, p := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if len(exts) > 0 && !hasExt(p) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(repo, p))
		if err != nil || len(bytes.TrimSpace(b)) == 0 || bytes.IndexByte(b, 0) >= 0 {
			continue
		}
		files = append(files, file{p, strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")})
	}
	return files
}

func hasExt(p string) bool {
	for _, e := range exts {
		if strings.HasSuffix(p, e) {
			return true
		}
	}
	return false
}

// joinFiles makes one context with a "==> path <==" line before each file,
// a header trim-1 treats as the start of a section. lineFile and lineNo map
// each context line back to its file and 1-based line (0 for a header).
func joinFiles(files []file) (string, []int, []int) {
	var b strings.Builder
	var lineFile, lineNo []int
	for fi, f := range files {
		b.WriteString("==> " + f.Path + " <==\n")
		lineFile, lineNo = append(lineFile, fi), append(lineNo, 0)
		for i, l := range f.Lines {
			b.WriteString(l + "\n")
			lineFile, lineNo = append(lineFile, fi), append(lineNo, i+1)
		}
	}
	return b.String(), lineFile, lineNo
}

var markerRe = regexp.MustCompile(`^[ \t]*… (\d+) lines? …[ \t\r]*$`)

// keptMask rebuilds which context lines Compress kept from its output, where
// each removed run is one "… N lines …" marker.
func keptMask(res trim.Result, n int) []bool {
	keep := make([]bool, n)
	if res.Unchanged {
		for i := range keep {
			keep[i] = true
		}
		return keep
	}
	i := 0
	for _, l := range strings.Split(strings.TrimSuffix(res.Output, "\n"), "\n") {
		if m := markerRe.FindStringSubmatch(l); m != nil {
			c, _ := strconv.Atoi(m[1])
			i += c
			continue
		}
		if i < n {
			keep[i] = true
		}
		i++
	}
	return keep
}

func gitInfo() (string, bool) {
	commit, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", false
	}
	status, _ := exec.Command("git", "-C", repo, "status", "--porcelain").Output()
	return strings.TrimSpace(string(commit)), len(bytes.TrimSpace(status)) > 0
}

var nonWord = regexp.MustCompile(`[^a-z0-9]+`)

// saveTrace writes traces/2026-10-14/153012-what-is-sample-heldout.json.
func saveTrace(t *Trace) error {
	now := time.Now()
	day := filepath.Join(traces, now.Format("2006-01-02"))
	if err := os.MkdirAll(day, 0o755); err != nil {
		return err
	}
	slug := strings.Trim(nonWord.ReplaceAllString(strings.ToLower(t.Query), "-"), "-")
	if len(slug) > 48 {
		slug = strings.TrimRight(slug[:48], "-")
	}
	if slug == "" {
		slug = "question"
	}
	stem := now.Format("150405") + "-" + slug
	path := filepath.Join(day, stem+".json")
	for n := 2; fileExists(path); n++ {
		path = filepath.Join(day, fmt.Sprintf("%s-%d.json", stem, n))
	}
	t.ID = strings.TrimSuffix(filepath.Base(path), ".json")
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// listTraces returns the saved traces, newest first.
func listTraces() []map[string]string {
	paths, _ := filepath.Glob(filepath.Join(traces, "*", "*.json"))
	sort.Sort(sort.Reverse(sort.StringSlice(paths)))
	out := []map[string]string{}
	for _, p := range paths {
		var t struct {
			Query, Point string
			StartedAt    string `json:"started_at"`
		}
		b, err := os.ReadFile(p)
		if err != nil || json.Unmarshal(b, &t) != nil {
			continue
		}
		out = append(out, map[string]string{"url": "/" + filepath.ToSlash(p), "query": t.Query, "point": t.Point, "started_at": t.StartedAt})
	}
	return out
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func sendJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
