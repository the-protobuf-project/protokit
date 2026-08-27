package buffers

// lock_test.go covers the two ways a ledger can be wrong without looking wrong:
// a version that was never declared, and an equality check that compares a cache
// instead of the records.

import "testing"

func TestParseLockRejectsALedgerWithNoVersion(t *testing.T) {
	// The version key is what lets a future change of shape be refused rather than
	// misread. A ledger omitting it must fail the check, not inherit the current
	// version from whatever the decoder was handed.
	_, err := ParseLock([]byte("messages:\n  - message: m\n    fields:\n      - number: 1\n        ordinal: 0\n"), "test")
	if err == nil {
		t.Fatal("a ledger with no version parsed successfully; the version check cannot fire")
	}
}

func TestParseLockAcceptsTheCurrentVersionAndIndexesIt(t *testing.T) {
	lock, err := ParseLock([]byte("version: 1\nmessages:\n  - message: m\n    fields:\n      - number: 3\n        ordinal: 7\n"), "test")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := lock.FieldSlots("m")[3]; got != 7 {
		t.Errorf("field 3 indexed to ordinal %d, want 7", got)
	}
}

func TestEqualComparesTheRecordsRatherThanTheCache(t *testing.T) {
	// Lock's record slices are exported, so a caller can assemble or append to one
	// without any lookup cache existing. Comparing caches would call these two
	// equal, because neither has been reindexed and both caches are empty.
	a := &Lock{Version: lockVersion, Messages: []MessageSlots{
		{Node: "m", Fields: []SlotEntry{{Number: 1, Ordinal: 0}}},
	}}
	b := &Lock{Version: lockVersion, Messages: []MessageSlots{
		{Node: "m", Fields: []SlotEntry{{Number: 1, Ordinal: 4}}},
	}}
	if a.Equal(b) {
		t.Error("two ledgers recording different ordinals compared equal")
	}

	same := &Lock{Version: lockVersion, Messages: []MessageSlots{
		{Node: "m", Fields: []SlotEntry{{Number: 1, Ordinal: 0, Name: "commentary"}}},
	}}
	if !a.Equal(same) {
		t.Error("two ledgers recording the same ordinals compared unequal; Name is commentary")
	}
}
