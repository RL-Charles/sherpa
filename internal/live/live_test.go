package live

import (
	"testing"
	"time"

	"github.com/RL-Charles/sherpa/internal/store"
)

func TestTouchReadClear(t *testing.T) {
	s, err := store.Init(t.TempDir(), "TST", "test")
	if err != nil {
		t.Fatal(err)
	}
	if got := Read(s.Dir, time.Now()); len(got) != 0 {
		t.Fatalf("no heartbeats yet, got %v", got)
	}
	_ = Touch(s, "claude", "T-0001")
	_ = Touch(s, "claude", "") // a tool call: keeps the ticket
	got := Read(s.Dir, time.Now())
	if len(got) != 1 || got[0].Agent != "claude" || got[0].Ticket != "T-0001" {
		t.Fatalf("want claude on T-0001, got %+v", got)
	}
	if got := Read(s.Dir, time.Now().Add(Stale+time.Second)); len(got) != 0 {
		t.Fatalf("stale heartbeat should be ignored, got %v", got)
	}
	_ = Clear(s)
	if got := Read(s.Dir, time.Now()); len(got) != 0 {
		t.Fatalf("cleared, got %v", got)
	}
}
