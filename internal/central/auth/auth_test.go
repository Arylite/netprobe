package auth

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func cheap(t *testing.T) {
	t.Helper()
	old := hashParams
	hashParams = argonParams{memory: 8, time: 1, threads: 1}
	t.Cleanup(func() { hashParams = old })
}

func TestHashAndVerify(t *testing.T) {
	cheap(t)
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$") || strings.Contains(h, "correct") {
		t.Fatalf("hash %q", h)
	}
	if ok, err := VerifyPassword("correct horse battery", h); !ok || err != nil {
		t.Fatalf("right password: %v, %v", ok, err)
	}
	if ok, err := VerifyPassword("wrong horse battery", h); ok || err != nil {
		t.Fatalf("wrong password: %v, %v", ok, err)
	}
	again, _ := HashPassword("correct horse battery")
	if again == h {
		t.Fatal("two hashes of one password are identical: the salt is not random")
	}
}

func TestHashesKeepTheirOwnCosts(t *testing.T) {
	cheap(t)
	old, _ := HashPassword("correct horse battery")
	hashParams = argonParams{memory: 16, time: 2, threads: 1}
	if ok, err := VerifyPassword("correct horse battery", old); !ok || err != nil {
		t.Fatalf("an older hash stopped verifying after the costs changed: %v, %v", ok, err)
	}
}

func TestVerifyRejectsUnusableHashes(t *testing.T) {
	for _, bad := range []string{
		"", "plain", "$argon2i$v=19$m=8,t=1,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=18$m=8,t=1,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=0,t=1,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=99999999,t=1,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=8,t=1,p=1$!!$aGFzaA",
		"$argon2id$v=19$m=8,t=1,p=1$c2FsdA$",
	} {
		if ok, err := VerifyPassword("x", bad); ok || err == nil {
			t.Errorf("VerifyPassword(%q) = %v, %v", bad, ok, err)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	tests := map[string]struct {
		user, password string
		valid          bool
	}{
		"fine":           {"alice", "a long enough one", true},
		"too short":      {"alice", "short", false},
		"just short":     {"alice", strings.Repeat("a", MinPasswordLength-1), false},
		"minimum":        {"alice", "abcdeabcdeab", true},
		"too long":       {"alice", strings.Repeat("a", MaxPasswordLength+1), false},
		"maximum":        {"alice", strings.Repeat("abcde", 26)[:MaxPasswordLength], true},
		"the username":   {"alice-the-admin", "ALICE-THE-ADMIN", false},
		"null character": {"alice", "abcdefghijkl\x00", false},
	}
	for name, tt := range tests {
		if err := ValidatePassword(tt.user, tt.password); (err == nil) != tt.valid {
			t.Errorf("%s: ValidatePassword() = %v, want valid=%v", name, err, tt.valid)
		}
	}
}

func TestSpendHashingTimeDoesNotPanic(t *testing.T) {
	cheap(t)
	SpendHashingTime("anything")
}

func fakeClock(start time.Time) (func() time.Time, func(time.Duration)) {
	now := start
	return func() time.Time { return now }, func(d time.Duration) { now = now.Add(d) }
}

func TestLimiterBlocksAfterTooManyFailuresThenForgets(t *testing.T) {
	l := NewLimiter(3, time.Minute)
	now, advance := fakeClock(time.Now())
	l.now = now

	for i := 0; i < 2; i++ {
		l.Fail("alice")
	}
	if l.Blocked("alice") {
		t.Fatal("blocked before the limit")
	}
	l.Fail("alice")
	if !l.Blocked("alice") || l.Blocked("bob") {
		t.Fatal("the limit applies to one key only")
	}
	advance(time.Minute)
	if l.Blocked("alice") {
		t.Fatal("still blocked after the window")
	}
	l.Fail("alice")
	if l.Blocked("alice") {
		t.Fatal("the count did not restart with the window")
	}
}

func TestLimiterResetAfterASuccess(t *testing.T) {
	l := NewLimiter(1, time.Minute)
	l.Fail("alice")
	if !l.Blocked("alice") {
		t.Fatal("not blocked")
	}
	l.Reset("alice")
	if l.Blocked("alice") {
		t.Fatal("still blocked after a reset")
	}
}

func TestLimiterMemoryIsBounded(t *testing.T) {
	l := NewLimiter(1, time.Hour)
	for i := 0; i < maxTrackedKeys+500; i++ {
		l.Fail(fmt.Sprint("key-", i))
	}
	if len(l.entries) > maxTrackedKeys {
		t.Fatalf("%d entries tracked", len(l.entries))
	}
}

func TestValidatePasswordRefusesWhatIsEasyToGuess(t *testing.T) {
	for name, password := range map[string]string{
		"a common password":           "passwordpassword",
		"a common one with digits":    "Password123456!",
		"a common one in French":      "azertyuiopqsdf",
		"the same character":          "aaaaaaaaaaaa",
		"a few characters repeated":   "abababababab",
		"digits in sequence":          "123456789012",
		"digits in sequence, wrapped": "345678901234",
		"letters in sequence":         "abcdefghijkl",
		"letters in reverse":          "lkjihgfedcba",
	} {
		if err := ValidatePassword("alice", password); err == nil {
			t.Errorf("%s: %q was accepted", name, password)
		}
	}
	for name, password := range map[string]string{
		"a sentence":                     "correct horse battery staple",
		"a password with a common word":  "welcome to the jungle, baby",
		"mixed characters":               "k9#Xp2-vQm7!zT",
		"a short username may be inside": "bobsled-race-champion",
	} {
		if err := ValidatePassword("bob", password); err != nil {
			t.Errorf("%s: %q was refused: %v", name, password, err)
		}
	}
}

func TestHashingIsCappedSoThatItCannotUseAllTheMemory(t *testing.T) {
	// Take every turn, as a burst of logins would.
	var releases []func()
	for range maxConcurrentHashes {
		release, err := acquire()
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, release)
	}
	// The next one waits for its turn and is refused when none comes.
	done := make(chan error, 1)
	go func() {
		_, err := acquire()
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("a request got a turn although all were taken: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	// A turn given back goes to the one that waits.
	releases[0]()
	if err := <-done; err != nil {
		t.Fatalf("the waiting request was refused: %v", err)
	}
	// Leave things as they were: the waiter's turn and the others'.
	for _, release := range releases[1:] {
		release()
	}
	<-hashSlots
	if len(hashSlots) != 0 {
		t.Fatalf("%d turns were not given back", len(hashSlots))
	}
}
