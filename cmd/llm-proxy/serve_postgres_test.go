package main

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/mtanzi/llm-proxy/internal/core"
	"github.com/mtanzi/llm-proxy/internal/store/postgres"
	"github.com/mtanzi/llm-proxy/internal/store/postgres/pgtest"
)

// The real proxy, recorder and store against a test Postgres (ADR 0018), with a
// fake upstream: a request through the data plane ends up as a row, and the
// admin plane reads it back.
func TestServeRecordsCallInPostgres(t *testing.T) {
	url := pgtest.NewDatabase(t)
	ctx := context.Background()

	st, err := openStore(ctx, postgres.Config{URL: url}, testLogger(), defaultStartupRetry())
	if err != nil {
		t.Fatal(err)
	}
	// openStore migrated: a second store on the same database finds the schema.
	reader, err := postgres.Open(ctx, postgres.Config{URL: url})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reader.Close)

	upstream := fakeUpstream(t)
	a, err := buildApp(loadedHolder(t, upstream.URL), st, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	r := start(t, a)

	code, id := post(t, r.data)
	if code != http.StatusOK {
		t.Fatalf("forwarded call = %d, want 200", code)
	}
	callID, err := core.ParseID(id)
	if err != nil {
		t.Fatalf("x-request-id %q: %v", id, err)
	}
	if err := r.stop(t); err != nil { // the flush writes the row
		t.Fatalf("run: %v", err)
	}

	var got core.CallDetail
	deadline := time.Now().Add(10 * time.Second)
	for {
		got, err = reader.GetCall(ctx, callID)
		if err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("GetCall(%s): %v", callID, err)
	}
	if got.KeyName != "alice" || got.Status != 200 || got.Usage.InputUncached != 3 || got.Usage.Output != 5 || got.Model != "claude-test" {
		t.Errorf("row = key %q status %d usage %+v model %q; want alice, 200, 3 in / 5 out, claude-test",
			got.KeyName, got.Status, got.Usage, got.Model)
	}
}

// With Postgres up, the admin plane serves the stored call log through the
// real reader.
func TestServeAdminReadsFromPostgres(t *testing.T) {
	url := pgtest.NewDatabase(t)
	st, err := openStore(context.Background(), postgres.Config{URL: url}, testLogger(), defaultStartupRetry())
	if err != nil {
		t.Fatal(err)
	}
	a, err := buildApp(loadedHolder(t, fakeUpstream(t).URL), st, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	r := start(t, a)
	if code, _ := get(t, r.admin+"/readyz"); code != http.StatusOK {
		t.Errorf("admin /readyz = %d, want 200 with the store up", code)
	}
	if code, _ := get(t, r.admin+"/admin/calls"); code != http.StatusOK {
		t.Errorf("/admin/calls = %d, want 200", code)
	}
	if err := r.stop(t); err != nil {
		t.Fatal(err)
	}
}

// A database that never answers fails the start after the retry bound, with
// the cause in the message.
func TestOpenStoreGivesUpWhenDatabaseIsDown(t *testing.T) {
	pc := postgres.Config{URL: "postgres://nobody@127.0.0.1:1/none?connect_timeout=1&sslmode=disable"}
	retry := startupRetry{Timeout: 300 * time.Millisecond, Initial: 50 * time.Millisecond, Max: 100 * time.Millisecond, Attempt: time.Second}
	_, err := openStore(context.Background(), pc, testLogger(), retry)
	if err == nil {
		t.Fatal("openStore succeeded against a refused database")
	}
}
