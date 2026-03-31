package circuitbreaker_test

import (
	"testing"

	cb "github.com/aqylsoft/circuitbreaker"
)

func TestRegistry_GetOrCreate(t *testing.T) {
	r := cb.NewRegistry()

	b1, err := r.Get("stripe", cb.WithConsecutiveFailures(3))
	if err != nil {
		t.Fatal(err)
	}
	b2, err := r.Get("stripe")
	if err != nil {
		t.Fatal(err)
	}

	if b1 != b2 {
		t.Fatal("expected same instance")
	}
}

func TestRegistry_Breakers(t *testing.T) {
	r := cb.NewRegistry()
	if _, err := r.Get("stripe"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get("paypal"); err != nil {
		t.Fatal(err)
	}

	all := r.Breakers()
	if len(all) != 2 {
		t.Fatalf("expected 2, got %d", len(all))
	}
}

func TestRegistry_Reset(t *testing.T) {
	r := cb.NewRegistry()
	b, err := r.Get("stripe", cb.WithConsecutiveFailures(2))
	if err != nil {
		t.Fatal(err)
	}

	_ = exec(b, alwaysFail)
	_ = exec(b, alwaysFail)

	r.Reset("stripe")

	if b.State() != cb.StateClosed {
		t.Fatal("expected closed after registry reset")
	}
}

func TestRegistry_Remove(t *testing.T) {
	r := cb.NewRegistry()
	if _, err := r.Get("stripe"); err != nil {
		t.Fatal(err)
	}
	r.Remove("stripe")

	if len(r.Breakers()) != 0 {
		t.Fatal("expected empty registry")
	}
}
