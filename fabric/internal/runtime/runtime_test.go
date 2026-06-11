package runtime_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"otherworld/fabric/internal/protocol"
	"otherworld/fabric/internal/runtime"
)

func TestDeliverInvokesHandler(t *testing.T) {
	var handled atomic.Int32
	r := runtime.New()
	r.Spawn(context.Background(), "voice:lamp", func(ctx context.Context, env protocol.Envelope) {
		handled.Add(1)
	})
	r.Deliver("voice:lamp", protocol.Envelope{ID: "utt_1"})
	deadline := time.Now().Add(2 * time.Second)
	for handled.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if handled.Load() != 1 {
		t.Fatal("handler not invoked")
	}
}

func TestPanicRestartsVoiceAndReportsCrash(t *testing.T) {
	type crash struct {
		voice string
		cause any
	}
	crashed := make(chan crash, 1)
	r := runtime.NewWithCrashHook(func(voice string, cause any) {
		crashed <- crash{voice: voice, cause: cause}
	})
	first := true
	var handled atomic.Int32
	r.Spawn(context.Background(), "voice:door", func(ctx context.Context, env protocol.Envelope) {
		if first {
			first = false
			panic("brain melted")
		}
		handled.Add(1)
	})
	r.Deliver("voice:door", protocol.Envelope{ID: "utt_1"}) // panics
	select {
	case c := <-crashed:
		if c.voice != "voice:door" {
			t.Fatalf("crash hook got voice %q", c.voice)
		}
		if c.cause != "brain melted" {
			t.Fatalf("crash hook got cause %v, want %q", c.cause, "brain melted")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("crash hook never called")
	}
	r.Deliver("voice:door", protocol.Envelope{ID: "utt_2"}) // voice must still work
	deadline := time.Now().Add(2 * time.Second)
	for handled.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if handled.Load() == 0 {
		t.Fatal("voice did not survive its own death")
	}
}

// TestDeliverToUnknownVoiceReturnsFalse verifies that delivering to a
// never-spawned voice is a no-op that signals the caller with false.
func TestDeliverToUnknownVoiceReturnsFalse(t *testing.T) {
	r := runtime.New()
	if r.Deliver("voice:ghost", protocol.Envelope{ID: "utt_1"}) {
		t.Fatal("expected false for unregistered voice, got true")
	}
}

// TestDespawnStopsDelivery verifies that after Despawn, the mailbox is removed
// and further Deliver calls return false.
func TestDespawnStopsDelivery(t *testing.T) {
	r := runtime.New()
	r.Spawn(context.Background(), "voice:chest", func(ctx context.Context, env protocol.Envelope) {})
	if !r.Deliver("voice:chest", protocol.Envelope{ID: "utt_1"}) {
		t.Fatal("expected true before despawn")
	}
	r.Despawn("voice:chest")
	if r.Deliver("voice:chest", protocol.Envelope{ID: "utt_2"}) {
		t.Fatal("expected false after despawn")
	}
}

// TestContextCancelStopsLoop verifies that after a voice's context is canceled,
// its loop goroutine exits and no further handler invocations occur.
//
// Timing caveat: we sleep 100 ms after cancel to give the goroutine scheduler
// time to observe ctx.Done() and exit the select. This is a best-effort
// observable rather than a guaranteed synchronization point — on a heavily
// loaded machine the goroutine may not have exited yet when we deliver.
// The 300 ms wait after delivery is the observable window; any spurious
// invocation (handler running after cancel) would surface here as a flake.
// In practice the 100 ms pause is generous for a goroutine that only does
// a channel select, so this test is expected to be stable on all CI tiers.
func TestContextCancelStopsLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var handled atomic.Int32
	r := runtime.New()
	r.Spawn(ctx, "voice:mirror", func(ctx context.Context, env protocol.Envelope) {
		handled.Add(1)
	})

	// Let the goroutine start and drain any spurious state.
	time.Sleep(10 * time.Millisecond)

	cancel()

	// Give the goroutine time to observe cancellation and exit.
	time.Sleep(100 * time.Millisecond)

	baseline := handled.Load()

	// Deliver returns true: the mailbox still exists in the map (Despawn was
	// not called). The loop goroutine has exited, so nobody drains this message.
	r.Deliver("voice:mirror", protocol.Envelope{ID: "utt_post_cancel"})

	// Wait generously; if the loop were still alive it would handle the message
	// well within this window.
	time.Sleep(300 * time.Millisecond)

	if handled.Load() != baseline {
		t.Fatalf("handler was invoked after context cancel (count went from %d to %d)",
			baseline, handled.Load())
	}
}
