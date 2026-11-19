package registry

import (
	"context"
	"errors"
	"testing"
)

type loader struct {
	terms []Terminal
	err   error
}

func (l *loader) LoadTerminals(context.Context) ([]Terminal, error) { return l.terms, l.err }

func TestRefreshSwapsSnapshotAndKeepsItOnFailure(t *testing.T) {
	l := &loader{terms: []Terminal{{ID: "SAT-00001", Region: "Europe"}}}
	r := New(l)
	if r.Ready() {
		t.Fatal("not ready before first load")
	}
	if _, ok := r.Region("SAT-00001"); ok {
		t.Fatal("empty registry should know nothing")
	}
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if region, ok := r.Region("SAT-00001"); !ok || region != "Europe" || !r.Ready() || r.Size() != 1 {
		t.Fatalf("Region = %q, %v", region, ok)
	}

	l.terms = []Terminal{{ID: "SAT-00002", Region: "Oceania"}}
	_ = r.Refresh(context.Background())
	if _, ok := r.Region("SAT-00001"); ok {
		t.Fatal("decommissioned terminal still accepted")
	}

	l.err = errors.New("db down")
	if err := r.Refresh(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if _, ok := r.Region("SAT-00002"); !ok {
		t.Fatal("failed refresh must keep the previous snapshot")
	}
}
