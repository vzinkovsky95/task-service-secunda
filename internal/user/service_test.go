package user

import (
	"errors"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestDummyHashIsAValidBcryptHashOfTheRealCost(t *testing.T) {
	cost, err := bcrypt.Cost([]byte(dummyHash))
	if err != nil {
		t.Fatalf("dummy hash does not parse: %v", err)
	}
	if cost != bcrypt.DefaultCost {
		t.Fatalf("dummy hash cost = %d, registration uses %d: the timing comparison is meaningless", cost, bcrypt.DefaultCost)
	}

	err = bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte("anything"))
	if !errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		t.Errorf("comparison against the dummy hash returned %v, want a plain mismatch", err)
	}
}
