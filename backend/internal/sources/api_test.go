package sources_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ilhenyco/ai-buddy/backend/internal/apptest"
	"github.com/ilhenyco/ai-buddy/backend/internal/sources"
)

const base = "/api/v1"

var keyN atomic.Int64

func key() string { return fmt.Sprintf("k-%d-%d", time.Now().UnixNano(), keyN.Add(1)) }

func hdr(etag string) map[string]string {
	h := map[string]string{"Idempotency-Key": key()}
	if etag != "" {
		h["If-Match"] = `"` + etag + `"`
	}
	return h
}

// fake ingestion queue of the knowledge service
type fake struct {
	puts    []sources.Document
	removes []string
	status  map[string]string
	fail    error
}

func (f *fake) Put(_ context.Context, k string, d sources.Document) (string, error) {
	if f.fail != nil {
		return "", f.fail
	}
	f.puts = append(f.puts, d)
	id := "job-" + k
	f.status[id] = "pending"
	return id, nil
}

func (f *fake) Remove(_ context.Context, k, id string) (string, error) {
	f.removes = append(f.removes, id)
	j := "job-" + k
	f.status[j] = "pending"
	return j, nil
}

func (f *fake) Job(_ context.Context, id string) (string, int, error) {
	s, ok := f.status[id]
	if !ok {
		return "", 0, errors.New("unknown job")
	}
	if s == "failed" {
		return s, 3, nil
	}
	return s, 1, nil
}

func (f *fake) completeAll() {
	for k := range f.status {
		f.status[k] = "completed"
	}
}

func setup(t *testing.T) (*apptest.Env, *fake) {
	env := apptest.New(t)
	f := &fake{status: map[string]string{}}
	env.App.Sources.Index = f
	env.App.Sources.Poll = time.Millisecond
	return env, f
}

func drain(t *testing.T, env *apptest.Env) {
	t.Helper()
	for i := 0; i < 20; i++ {
		env.Clock.Set(env.Clock.Now().Add(time.Second))
		ok, err := env.App.Sources.Step(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			return
		}
	}
}

var input = map[string]any{"title": "Доступы", "body": "Список доступов уточни у бадди.", "category": "Инструменты",
	"acl": map[string]any{"teamIds": []string{"platform"}, "allowedRoleKinds": []string{"intern"}}}

func TestLifecycle(t *testing.T) {
	env, f := setup(t)
	alex := env.Login(t, "u-alex")
	r := alex.Do(t, "POST", base+"/sources", input, hdr(""))
	if r.Status != 202 || r.JSON["status"] != "queued" || r.JSON["activeVersionId"] != nil {
		t.Fatalf("create: %d %s", r.Status, r.Raw)
	}
	id, job := r.JSON["sourceId"].(string), r.JSON["id"].(string)
	v1 := r.JSON["candidateVersionId"].(string)

	var src sources.Source
	alex.Do(t, "GET", base+"/sources/"+id, nil, nil).Decode(t, &src)
	if src.Status != "indexing" || src.VersionID != nil || src.ETag != "1" {
		t.Fatalf("before indexing: %+v", src)
	}
	drain(t, env) // submitted, still pending in the service
	if len(f.puts) != 1 || f.puts[0].DocumentID != id || f.puts[0].Revision != v1 || *f.puts[0].ScopeID != "platform" ||
		f.puts[0].URL != "https://homych.invalid/sources/"+id || f.puts[0].Text != "Список доступов уточни у бадди." {
		t.Fatalf("document: %+v", f.puts)
	}
	f.completeAll()
	drain(t, env)
	alex = env.Login(t, "u-alex")
	alex.Do(t, "GET", base+"/sources/"+id, nil, nil).Decode(t, &src)
	if src.Status != "ready" || src.VersionID == nil || *src.VersionID != v1 {
		t.Fatalf("indexed: %+v", src)
	}
	if j := alex.Do(t, "GET", base+"/source-jobs/"+job, nil, nil); j.JSON["status"] != "ready" || j.JSON["activeVersionId"] != v1 {
		t.Fatalf("job: %s", j.Raw)
	}
	if got := env.App.Metrics.JobCount("index", "completed"); got != 1 {
		t.Fatalf("completed index jobs = %v, want 1", got)
	}

	// edit: a new version; answers keep the old one until it is indexed
	edited := map[string]any{"title": "Доступы v2", "body": "Новый текст.", "category": "Инструменты", "acl": map[string]any{"teamIds": []string{"platform"}}}
	if r := alex.Do(t, "PUT", base+"/sources/"+id, edited, hdr("7")); r.Status != 412 {
		t.Fatalf("stale etag: %d", r.Status)
	}
	if r := alex.Do(t, "PUT", base+"/sources/"+id, edited, map[string]string{"Idempotency-Key": key()}); r.Status != 422 {
		t.Fatalf("no If-Match: %d", r.Status)
	}
	u := alex.Do(t, "PUT", base+"/sources/"+id, edited, hdr("1"))
	if u.Status != 202 {
		t.Fatalf("update: %d %s", u.Status, u.Raw)
	}
	v2 := u.JSON["candidateVersionId"].(string)
	alex.Do(t, "GET", base+"/sources/"+id, nil, nil).Decode(t, &src)
	if src.Title != "Доступы v2" || src.Status != "indexing" || *src.VersionID != v1 || src.ETag != "2" {
		t.Fatalf("while indexing v2: %+v", src)
	}
	// the index fails: the old version stays active, the job reports an error
	drain(t, env)
	for k := range f.status {
		if f.status[k] == "pending" {
			f.status[k] = "failed"
		}
	}
	drain(t, env)
	alex = env.Login(t, "u-alex")
	alex.Do(t, "GET", base+"/sources/"+id, nil, nil).Decode(t, &src)
	if src.Status != "error" || *src.VersionID != v1 {
		t.Fatalf("failed v2: %+v", src)
	}
	j := alex.Do(t, "GET", base+"/source-jobs/"+u.JSON["id"].(string), nil, nil)
	if j.JSON["status"] != "failed" || j.JSON["error"].(map[string]any)["code"] != "INDEX_FAILED" {
		t.Fatalf("failed job: %s", j.Raw)
	}
	// history: the first version is still readable
	alex.Do(t, "GET", base+"/sources/"+id+"?versionId="+v1, nil, nil).Decode(t, &src)
	if src.Title != "Доступы" {
		t.Fatalf("version 1: %+v", src)
	}
	if r := alex.Do(t, "GET", base+"/sources/"+id+"?versionId=sv-none", nil, nil); r.Status != 404 {
		t.Fatalf("unknown version: %d", r.Status)
	}
	_ = v2

	// exclude: removed from the index, out of answers
	ex := alex.Do(t, "POST", base+"/sources/"+id+"/exclude", nil, hdr("2"))
	if ex.Status != 200 || ex.JSON["status"] != "excluded" || ex.JSON["etag"] != "3" {
		t.Fatalf("exclude: %d %s", ex.Status, ex.Raw)
	}
	drain(t, env)
	f.completeAll()
	drain(t, env)
	if len(f.removes) != 1 || f.removes[0] != id {
		t.Fatalf("removes: %v", f.removes)
	}
	alex = env.Login(t, "u-alex")
	alex.Do(t, "GET", base+"/sources/"+id, nil, nil).Decode(t, &src)
	if src.Status != "excluded" || src.VersionID != nil {
		t.Fatalf("excluded: %+v", src)
	}
	// list and search
	var page sources.Page
	alex.Do(t, "GET", base+"/sources?q=v2", nil, nil).Decode(t, &page)
	if len(page.Items) != 1 {
		t.Fatalf("search: %+v", page)
	}
	alex.Do(t, "GET", base+"/sources?q=нет-такого", nil, nil).Decode(t, &page)
	if len(page.Items) != 0 {
		t.Fatal("search finds nothing")
	}
	// a new version brings it back
	if r := alex.Do(t, "PUT", base+"/sources/"+id, edited, hdr("3")); r.Status != 202 {
		t.Fatalf("bring back: %d", r.Status)
	}
	alex.Do(t, "GET", base+"/sources/"+id, nil, nil).Decode(t, &src)
	if src.Status != "indexing" {
		t.Fatalf("back: %+v", src)
	}
}

func TestRightsAndValidation(t *testing.T) {
	env, _ := setup(t)
	alex, dasha, oleg, sasha := env.Login(t, "u-alex"), env.Login(t, "u-dasha"), env.Login(t, "u-oleg"), env.Login(t, "u-sasha")
	h := hdr("")
	r := alex.Do(t, "POST", base+"/sources", input, h)
	id := r.JSON["sourceId"].(string)
	if again := alex.Do(t, "POST", base+"/sources", input, h); again.JSON["id"] != r.JSON["id"] {
		t.Fatal("idempotent create")
	}
	// buddy reads, does not write; other team and interns see nothing
	if g := dasha.Do(t, "GET", base+"/sources/"+id, nil, nil); g.Status != 200 {
		t.Fatalf("buddy reads: %d", g.Status)
	}
	if g := dasha.Do(t, "PUT", base+"/sources/"+id, input, hdr("1")); g.Status != 403 {
		t.Fatalf("buddy writes: %d", g.Status)
	}
	if g := dasha.Do(t, "POST", base+"/sources", input, hdr("")); g.Status != 403 {
		t.Fatalf("buddy creates: %d", g.Status)
	}
	if g := oleg.Do(t, "GET", base+"/sources/"+id, nil, nil); g.Status != 404 {
		t.Fatalf("other team: %d", g.Status)
	}
	if g := oleg.Do(t, "GET", base+"/source-jobs/"+r.JSON["id"].(string), nil, nil); g.Status != 404 {
		t.Fatalf("other team job: %d", g.Status)
	}
	if g := sasha.Do(t, "GET", base+"/sources", nil, nil); g.Status != 403 {
		t.Fatalf("intern lists: %d", g.Status)
	}
	// the intern opens a material an answer cites, but not the materials page and not excluded ones
	if g := sasha.Do(t, "GET", base+"/sources/"+id, nil, nil); g.Status != 200 {
		t.Fatalf("intern reads a cited material: %d", g.Status)
	}
	if g := env.Login(t, "u-kirill").Do(t, "GET", base+"/sources/"+id, nil, nil); g.Status != 404 {
		t.Fatalf("intern of another team: %d", g.Status)
	}
	alex.Do(t, "POST", base+"/sources/"+id+"/exclude", nil, hdr("1"))
	if g := sasha.Do(t, "GET", base+"/sources/"+id, nil, nil); g.Status != 404 {
		t.Fatalf("intern reads an excluded material: %d", g.Status)
	}
	if g := dasha.Do(t, "GET", base+"/sources/"+id, nil, nil); g.Status != 200 {
		t.Fatalf("buddy reads an excluded material: %d", g.Status)
	}
	if g := alex.Do(t, "GET", base+"/source-jobs/idx-none", nil, nil); g.Status != 404 {
		t.Fatalf("unknown job: %d", g.Status)
	}
	if g := alex.Do(t, "GET", base+"/sources?cursor=x", nil, nil); g.Status != 422 {
		t.Fatalf("bad cursor: %d", g.Status)
	}
	bad := []map[string]any{
		{"title": "", "body": "x", "category": "c", "acl": map[string]any{"teamIds": []string{"platform"}}},
		{"title": "t", "body": " ", "category": "c", "acl": map[string]any{"teamIds": []string{"platform"}}},
		{"title": "t", "body": strings.Repeat("я", sources.MaxBody/2+1), "category": "c", "acl": map[string]any{"teamIds": []string{"platform"}}},
		{"title": "t", "body": "x", "category": "", "acl": map[string]any{"teamIds": []string{"platform"}}},
		{"title": "t", "body": "x", "category": "c", "acl": map[string]any{"teamIds": []string{"platform", "mobile"}}},
		{"title": "t", "body": "x", "category": "c", "acl": map[string]any{"teamIds": []string{"platform"}, "allowedRoleKinds": []string{"hr"}}},
	}
	for i, b := range bad {
		if g := alex.Do(t, "POST", base+"/sources", b, hdr("")); g.Status != 422 {
			t.Errorf("bad %d: %d", i, g.Status)
		}
	}
	if g := alex.Do(t, "POST", base+"/sources", map[string]any{"title": "t", "body": "x", "category": "c", "acl": map[string]any{"teamIds": []string{"mobile"}}}, hdr("")); g.Status != 403 {
		t.Errorf("other team's material: %d", g.Status)
	}
	if g := alex.Do(t, "POST", base+"/sources/"+id+"/exclude", nil, hdr("5")); g.Status != 412 {
		t.Errorf("exclude stale: %d", g.Status)
	}
	if g := dasha.Do(t, "POST", base+"/sources/"+id+"/exclude", nil, hdr("2")); g.Status != 403 {
		t.Errorf("buddy excludes: %d", g.Status)
	}
	// pagination
	for i := 0; i < 3; i++ {
		alex.Do(t, "POST", base+"/sources", map[string]any{"title": fmt.Sprintf("M%d", i), "body": "x", "category": "c", "acl": map[string]any{"teamIds": []string{"platform"}}}, hdr(""))
	}
	var page sources.Page
	alex.Do(t, "GET", base+"/sources?limit=2", nil, nil).Decode(t, &page)
	if len(page.Items) != 2 || page.NextCursor == nil {
		t.Fatalf("page 1: %+v", page)
	}
	alex.Do(t, "GET", base+"/sources?limit=2&cursor="+*page.NextCursor, nil, nil).Decode(t, &page)
	if len(page.Items) != 2 || page.NextCursor != nil {
		t.Fatalf("page 2: %+v", page)
	}
}

func TestWithoutIndexAndSubmitFailures(t *testing.T) {
	env, f := setup(t)
	alex := env.Login(t, "u-alex")
	f.fail = errors.New("down")
	r := alex.Do(t, "POST", base+"/sources", input, hdr(""))
	drain(t, env)
	alex = env.Login(t, "u-alex")
	if j := alex.Do(t, "GET", base+"/source-jobs/"+r.JSON["id"].(string), nil, nil); j.JSON["status"] != "failed" {
		t.Fatalf("submit keeps failing: %s", j.Raw)
	}
	env.App.Sources.Index = nil
	r = alex.Do(t, "POST", base+"/sources", map[string]any{"title": "Без поиска", "body": "x", "category": "c", "acl": map[string]any{"teamIds": []string{"platform"}}}, hdr(""))
	drain(t, env)
	alex = env.Login(t, "u-alex")
	j := alex.Do(t, "GET", base+"/source-jobs/"+r.JSON["id"].(string), nil, nil)
	if j.JSON["error"].(map[string]any)["code"] != "INDEX_DISABLED" || !strings.Contains(j.JSON["error"].(map[string]any)["message"].(string), "не подключён") {
		t.Fatalf("no index: %s", j.Raw)
	}
	// metrics: the first job was submitted 5 times (4 retries) and failed, the second failed at once
	mt := env.App.Metrics
	if got := [2]float64{mt.JobCount("index", "retry"), mt.JobCount("index", "failed")}; got != [2]float64{4, 2} {
		t.Fatalf("index job metrics retry/failed = %v, want [4 2]", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { env.App.Sources.Run(ctx, time.Millisecond); close(done) }()
	env.App.Sources.Trigger()
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done
}

func TestHTTPIndex(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer writer-key-writer-key-0000" {
			w.WriteHeader(401)
			return
		}
		seen = append(seen, r.Method+" "+r.URL.Path+" "+r.Header.Get("Idempotency-Key"))
		switch {
		case r.URL.Path == "/v1/jobs/bad":
			w.WriteHeader(404)
		case strings.HasPrefix(r.URL.Path, "/v1/jobs/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "completed", "attempts": 1})
		default:
			w.WriteHeader(202)
			_ = json.NewEncoder(w).Encode(map[string]any{"jobId": "job-1", "status": "pending"})
		}
	}))
	defer srv.Close()
	x := sources.NewHTTPIndex(srv.URL+"/", "writer-key-writer-key-0000", time.Second)
	ctx := context.Background()
	if id, err := x.Put(ctx, "k1", sources.Document{DocumentID: "src-1"}); err != nil || id != "job-1" {
		t.Fatalf("put: %s %v", id, err)
	}
	if id, err := x.Remove(ctx, "k2", "src-1"); err != nil || id != "job-1" {
		t.Fatalf("remove: %s %v", id, err)
	}
	if st, n, err := x.Job(ctx, "job-1"); err != nil || st != "completed" || n != 1 {
		t.Fatalf("job: %s %d %v", st, n, err)
	}
	if _, _, err := x.Job(ctx, "bad"); err == nil {
		t.Fatal("404 is an error")
	}
	if strings.Join(seen, "|") != "PUT /v1/documents/src-1 k1|DELETE /v1/documents/src-1 k2|GET /v1/jobs/job-1 |GET /v1/jobs/bad " {
		t.Fatalf("requests: %v", seen)
	}
	if _, err := sources.NewHTTPIndex("http://127.0.0.1:1", "k", time.Second).Put(ctx, "k", sources.Document{DocumentID: "x"}); err == nil {
		t.Fatal("no server")
	}
}
