package metadata

import (
	"context"
	"database/sql"
	"fmt"
	internaltypes "github.com/goccy/bigquery-emulator/internal/types"
	"path/filepath"
	"strings"
	"testing"

	bigqueryv2 "google.golang.org/api/bigquery/v2"
)

func TestRequestProjectDoesNotDecodeHistoricJobResults(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("googlesqlite", "file:"+filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo, err := NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := repo.getConnection(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	project := NewProject(repo, "fixture", nil, nil)
	if err = project.Insert(ctx, tx); err != nil {
		t.Fatal(err)
	}
	job := NewJob(repo, "fixture", "historic", &bigqueryv2.Job{Id: "historic", JobReference: &bigqueryv2.JobReference{JobId: "historic", ProjectId: "fixture"}}, nil, nil)
	if err = project.AddJob(ctx, tx, job); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	// Real job lookup still materializes the expected metadata.
	full, err := repo.FindProject(ctx, "fixture")
	if err != nil || full.Job("historic").Content().Id != "historic" {
		t.Fatalf("full=%v err=%v", full, err)
	}
	// An unrelated, undecodable saved response must not be touched in the
	// request-scope lookup. This also prevents loading large historic payloads.
	if _, err = db.ExecContext(ctx, `UPDATE jobs SET result='not-json' WHERE id='historic'`); err != nil {
		t.Fatal(err)
	}
	scoped, err := repo.FindProjectForRequest(ctx, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if scoped == nil || len(scoped.JobIDs()) != 1 || scoped.Job("historic") == nil {
		t.Fatal("job reference lost")
	}
	if scoped.Job("historic").Content() != nil {
		t.Fatal("request lookup loaded job content")
	}
	if _, err = repo.FindProject(ctx, "fixture"); err == nil {
		t.Fatal("full lookup no longer validates job result")
	}
	if _, err = repo.FindJob(ctx, "fixture", "historic"); err == nil {
		t.Fatal("explicit job lookup no longer validates job result")
	}
	missing, err := repo.FindProjectForRequest(ctx, "missing")
	if err != nil || missing != nil {
		t.Fatalf("missing project=%v err=%v", missing, err)
	}
}

// Historical result bodies stay on disk unless a job API explicitly requests
// them. This benchmark makes the allocation cost of that boundary visible.
func BenchmarkProjectLookupWithHistory(b *testing.B) {
	ctx := context.Background()
	db, err := sql.Open("googlesqlite", "file:"+filepath.Join(b.TempDir(), "history.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	repo, err := NewRepository(db)
	if err != nil {
		b.Fatal(err)
	}
	conn, err := repo.getConnection(ctx)
	if err != nil {
		b.Fatal(err)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		b.Fatal(err)
	}
	project := NewProject(repo, "history", nil, nil)
	if err = project.Insert(ctx, tx); err != nil {
		b.Fatal(err)
	}
	payload := strings.Repeat("x", 64<<10)
	for i := 0; i < 32; i++ {
		id := fmt.Sprintf("query-%d", i)
		content := &bigqueryv2.Job{Id: id, JobReference: &bigqueryv2.JobReference{JobId: id, ProjectId: "history"}}
		response := &internaltypes.QueryResponse{JobComplete: true, Rows: []*internaltypes.TableRow{{F: []*internaltypes.TableCell{{V: payload}}}}}
		if err = project.AddJob(ctx, tx, NewJob(repo, "history", id, content, response, nil)); err != nil {
			b.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		b.Fatal(err)
	}
	conn.Close()
	for _, full := range []bool{true, false} {
		name := "request"
		if full {
			name = "full-job-results"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var project *Project
				var err error
				if full {
					project, err = repo.FindProject(ctx, "history")
				} else {
					project, err = repo.FindProjectForRequest(ctx, "history")
				}
				if err != nil || project == nil || len(project.JobIDs()) != 32 {
					b.Fatalf("project lookup: %v", err)
				}
			}
		})
	}
}
