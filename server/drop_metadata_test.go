package server

import (
	"context"
	"github.com/goccy/bigquery-emulator/types"
	"github.com/goccy/googlesqlite"
	"testing"
)

// A successful SQL no-op or a previously completed drop may leave no metadata
// record to remove. Catalog synchronization must remain idempotent in that case.
func TestDeleteAlreadyAbsentTableMetadata(t *testing.T) {
	ctx := context.Background()
	emulator, err := New(TempStorage)
	if err != nil {
		t.Fatal(err)
	}
	defer emulator.Stop(ctx)
	if err = emulator.Load(StructSource(types.NewProject("test", types.NewDataset("dataset1")))); err != nil {
		t.Fatal(err)
	}
	spec := &googlesqlite.TableSpec{NamePath: []string{"test", "dataset1", "missing"}}
	if err = deleteTableMetadata(ctx, emulator, spec); err != nil {
		t.Fatal(err)
	}
	if err = deleteTableMetadata(ctx, emulator, spec); err != nil {
		t.Fatal(err)
	}
}
