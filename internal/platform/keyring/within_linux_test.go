package keyring

import (
	"context"
	"errors"
	"testing"
	"time"
)

// An operation the caller gave up on finds its time over when it goes on: the bus library sends
// nothing for a context that is over, so what the operation had not yet sent, a write included,
// is never sent after the caller was told the time was up.
func TestAnOperationGivenUpOnFindsItsTimeOver(t *testing.T) {
	goOn := make(chan struct{})
	later := make(chan error, 1)
	start := time.Now()
	_, err := withinFor(50*time.Millisecond, func(ctx context.Context) (struct{}, error) {
		<-goOn // held up before it could send, as by a bus that does not answer
		later <- ctx.Err()
		return struct{}{}, nil
	})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("an operation held up past its time: %v, want ErrTimeout", err)
	}
	if took := time.Since(start); took > time.Second {
		t.Errorf("the caller waited %v for an operation of 50 ms", took)
	}
	close(goOn)
	select {
	case err := <-later:
		if err == nil {
			t.Error("the operation that was given up on still had time: it could have written after the caller was told it had not")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the held operation did not go on")
	}
	// One that finishes in time is its own result.
	got, err := withinFor(time.Second, func(ctx context.Context) (string, error) { return "done", ctx.Err() })
	if got != "done" || err != nil {
		t.Errorf("an operation in time: %q, %v", got, err)
	}
}
