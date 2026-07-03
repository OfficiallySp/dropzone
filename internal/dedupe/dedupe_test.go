package dedupe

import "testing"

func TestSeenOrAdd(t *testing.T) {
	l := New(3)
	a, b := HashBytes([]byte("a")), HashBytes([]byte("b"))

	if l.SeenOrAdd(a) {
		t.Fatal("first insert of a reported as seen")
	}
	if !l.SeenOrAdd(a) {
		t.Fatal("second insert of a reported as new")
	}
	if l.SeenOrAdd(b) {
		t.Fatal("first insert of b reported as seen")
	}
}

func TestLRUEviction(t *testing.T) {
	l := New(2)
	h := func(s string) string { return HashBytes([]byte(s)) }

	l.SeenOrAdd(h("1")) // {1}
	l.SeenOrAdd(h("2")) // {1,2}
	l.SeenOrAdd(h("3")) // evicts 1 -> {2,3}

	if l.SeenOrAdd(h("1")) {
		t.Fatal("hash 1 should have been evicted (reported as still seen)")
	}
}

func TestHashStable(t *testing.T) {
	if HashBytes([]byte("hello")) != HashBytes([]byte("hello")) {
		t.Fatal("hash not deterministic")
	}
	if HashBytes([]byte("a")) == HashBytes([]byte("b")) {
		t.Fatal("distinct inputs collided")
	}
}
