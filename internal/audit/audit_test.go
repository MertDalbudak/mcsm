package audit

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func newLogger(t *testing.T) (*Logger, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "audit")
	l, err := New(dir, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return l, dir
}

func TestAppend_AssignsMonotonicIDs(t *testing.T) {
	l, _ := newLogger(t)
	for i := 0; i < 5; i++ {
		got := l.Append(context.Background(), Entry{Kind: "x"})
		want := uint64(i + 1)
		if got != want {
			t.Errorf("ID %d: got %d want %d", i, got, want)
		}
	}
}

func TestList_NewestFirst(t *testing.T) {
	l, _ := newLogger(t)
	for i := 0; i < 5; i++ {
		l.Append(context.Background(), Entry{Kind: "k", At: time.Now().Add(time.Duration(i) * time.Second)})
	}
	got, _ := l.List(Query{})
	if len(got) != 5 {
		t.Fatalf("len=%d", len(got))
	}
	for i := 0; i < len(got)-1; i++ {
		if got[i].ID < got[i+1].ID {
			t.Errorf("expected newest first; got[%d].ID=%d got[%d].ID=%d", i, got[i].ID, i+1, got[i+1].ID)
		}
	}
}

func TestList_Filters(t *testing.T) {
	l, _ := newLogger(t)
	now := time.Now()
	l.Append(context.Background(), Entry{Kind: "slot.start", Actor: Actor{Name: "alice"}, At: now})
	l.Append(context.Background(), Entry{Kind: "slot.stop", Actor: Actor{Name: "bob"}, At: now.Add(time.Second)})
	l.Append(context.Background(), Entry{Kind: "slot.start", Actor: Actor{Name: "alice"}, At: now.Add(2 * time.Second)})

	t.Run("by_kind", func(t *testing.T) {
		got, _ := l.List(Query{Kind: "slot.start"})
		if len(got) != 2 {
			t.Fatalf("len=%d, %+v", len(got), got)
		}
	})
	t.Run("by_actor", func(t *testing.T) {
		got, _ := l.List(Query{Actor: "bob"})
		if len(got) != 1 || got[0].Actor.Name != "bob" {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("since", func(t *testing.T) {
		got, _ := l.List(Query{Since: now.Add(time.Second)})
		// since is "at >= since"; we have entries at now+1s and now+2s
		if len(got) != 2 {
			t.Fatalf("len=%d, want 2", len(got))
		}
	})
}

func TestList_CursorPagination(t *testing.T) {
	l, _ := newLogger(t)
	for i := 0; i < 10; i++ {
		l.Append(context.Background(), Entry{Kind: "k"})
	}
	page1, cursor := l.List(Query{Limit: 4})
	if len(page1) != 4 {
		t.Fatalf("page1 len=%d", len(page1))
	}
	if cursor == "" {
		t.Fatal("cursor empty")
	}
	page2, cursor2 := l.List(Query{Limit: 4, Cursor: ""})
	_ = page2
	_ = cursor2
	// Pagination: cursor is the *last* (oldest) returned id; passing it
	// should return entries with id > cursor's id... wait no, this
	// implementation uses cursor as "id <= cursor break" — meaning
	// passing a cursor returns OLDER entries. Let's verify behavior.
	page3, _ := l.List(Query{Limit: 4, Cursor: cursor})
	// All page3 ids should be < min id in page1.
	if len(page3) == 0 {
		t.Fatal("expected more entries past cursor")
	}
	cursorID, _ := strconv.ParseUint(cursor, 10, 64)
	for _, e := range page3 {
		if e.ID >= cursorID {
			t.Errorf("entry %d should be < cursor %d", e.ID, cursorID)
		}
	}
}

func TestList_RingEvictsOldest(t *testing.T) {
	l, _ := newLogger(t)
	l.ringCap = 3
	l.ring = make([]Entry, 0, 3)
	for i := 0; i < 10; i++ {
		l.Append(context.Background(), Entry{Kind: "k"})
	}
	got, _ := l.List(Query{})
	if len(got) > 3 {
		t.Errorf("ring should cap at 3, got %d", len(got))
	}
	// Newest IDs should be present (8, 9, 10)
	ids := map[uint64]bool{}
	for _, e := range got {
		ids[e.ID] = true
	}
	for _, want := range []uint64{8, 9, 10} {
		if !ids[want] {
			t.Errorf("expected ID %d in ring; got %v", want, ids)
		}
	}
}

func TestPersistence_IDSurvivesRestart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "audit")
	l1, err := New(dir, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	last := l1.Append(context.Background(), Entry{Kind: "x"})
	for i := 0; i < 3; i++ {
		last = l1.Append(context.Background(), Entry{Kind: "y"})
	}
	// Reopen — ID should continue from last+1.
	l2, err := New(dir, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	next := l2.Append(context.Background(), Entry{Kind: "z"})
	if next != last+1 {
		t.Errorf("next ID after restart: got %d want %d", next, last+1)
	}
}

func TestPersistence_RingBackfilledFromFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "audit")
	l1, _ := New(dir, time.Hour)
	for i := 0; i < 5; i++ {
		l1.Append(context.Background(), Entry{Kind: "k"})
	}
	l2, _ := New(dir, time.Hour)
	got, _ := l2.List(Query{})
	if len(got) != 5 {
		t.Errorf("ring backfill: got %d entries, want 5", len(got))
	}
}
