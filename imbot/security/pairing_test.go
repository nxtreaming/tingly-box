package security

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPairingManager_MintProducesDistinctCodes(t *testing.T) {
	p := NewPairingManager(nil, WithPairingCodeLength(8))
	a, _ := p.Mint("bot-1")
	b, _ := p.Mint("bot-1") // re-mint replaces previous
	if a == b {
		t.Fatalf("expected distinct codes across re-mint, got %q twice", a)
	}
	c, _ := p.Mint("bot-2")
	if c == a {
		t.Fatalf("expected distinct codes across bots")
	}
	if !strings.Contains(a, "-") {
		t.Fatalf("expected codeLen >= 8 to include separator, got %q", a)
	}
}

func TestPairingManager_VerifySuccessIsSingleUse(t *testing.T) {
	p := NewPairingManager(nil)
	code, _ := p.Mint("bot-1")

	if err := p.Verify("bot-1", code); err != nil {
		t.Fatalf("first Verify should succeed, got %v", err)
	}
	// Single-use: code is consumed.
	if err := p.Verify("bot-1", code); !errors.Is(err, ErrPairCodeMissing) {
		t.Fatalf("expected ErrPairCodeMissing on replay, got %v", err)
	}
}

func TestPairingManager_VerifyExpired(t *testing.T) {
	current := time.Now().UTC()
	p := NewPairingManager(nil,
		WithPairingTTL(time.Minute),
		WithPairingClock(func() time.Time { return current }),
	)
	code, _ := p.Mint("bot-1")

	current = current.Add(2 * time.Minute)

	if err := p.Verify("bot-1", code); !errors.Is(err, ErrPairCodeExpired) {
		t.Fatalf("expected ErrPairCodeExpired, got %v", err)
	}
	// Entry should now be cleared.
	if _, _, ok := p.Current("bot-1"); ok {
		t.Fatalf("expired code should be evicted")
	}
}

func TestPairingManager_VerifyMismatchAndLockout(t *testing.T) {
	current := time.Now().UTC()
	p := NewPairingManager(nil,
		WithPairingTTL(time.Hour),
		WithPairingMaxFails(3),
		WithPairingLockout(15*time.Minute),
		WithPairingClock(func() time.Time { return current }),
	)
	code, _ := p.Mint("bot-1")

	for i := 0; i < 3; i++ {
		if err := p.Verify("bot-1", "WRONG-WRONG"); !errors.Is(err, ErrPairCodeMismatch) {
			t.Fatalf("attempt %d: expected ErrPairCodeMismatch, got %v", i, err)
		}
	}

	// Threshold reached → next attempt (correct or not) is locked.
	if err := p.Verify("bot-1", code); !errors.Is(err, ErrPairLocked) {
		t.Fatalf("expected ErrPairLocked after threshold, got %v", err)
	}

	// Lockout still active before window passes.
	current = current.Add(10 * time.Minute)
	if err := p.Verify("bot-1", code); !errors.Is(err, ErrPairLocked) {
		t.Fatalf("expected ErrPairLocked while locked, got %v", err)
	}

	// Past the lockout, a stale code returns missing/expired (not locked).
	current = current.Add(20 * time.Minute)
	err := p.Verify("bot-1", code)
	if errors.Is(err, ErrPairLocked) {
		t.Fatalf("lockout should have ended, got %v", err)
	}
}

func TestPairingManager_RevokeClearsState(t *testing.T) {
	p := NewPairingManager(nil)
	code, _ := p.Mint("bot-1")
	p.Revoke("bot-1")

	if _, _, ok := p.Current("bot-1"); ok {
		t.Fatalf("Current should be empty after Revoke")
	}
	if err := p.Verify("bot-1", code); !errors.Is(err, ErrPairCodeMissing) {
		t.Fatalf("expected ErrPairCodeMissing after revoke, got %v", err)
	}
}

func TestPairingManager_ConcurrentMintAndVerify(t *testing.T) {
	p := NewPairingManager(nil)
	const workers = 16
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, _ := p.Mint("bot-1")
			_ = p.Verify("bot-1", code) // may succeed or be replaced; never panic
		}()
	}
	wg.Wait()
}

func TestPairingManager_MintNoBotUUID(t *testing.T) {
	p := NewPairingManager(nil)
	code, exp := p.Mint("")
	if code != "" || !exp.IsZero() {
		t.Fatalf("Mint(\"\") should return empty values")
	}
}
