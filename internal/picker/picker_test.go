package picker

import (
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func mkFiles(t *testing.T, root string, names ...string) {
	t.Helper()
	for _, n := range names {
		p := filepath.Join(root, n)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
}

func newRNG(seed uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed, seed))
}

func TestPick_SingleFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "only.jpg")
	mkFiles(t, dir, "only.jpg")
	res, err := Pick(f, false, []string{"jpg"}, nil, newRNG(1))
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if res.Path != f {
		t.Errorf("path = %s, want %s", res.Path, f)
	}
	if res.Source != f {
		t.Errorf("source = %s, want %s", res.Source, f)
	}
}

func TestPick_DirectoryFiltersByExt(t *testing.T) {
	dir := t.TempDir()
	mkFiles(t, dir, "a.jpg", "b.png", "c.txt", "d.webp")
	res, err := Pick(dir, false, []string{"jpg", "png"}, nil, newRNG(1))
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if res.Pool != 2 {
		t.Errorf("pool = %d, want 2", res.Pool)
	}
	base := filepath.Base(res.Path)
	if base != "a.jpg" && base != "b.png" {
		t.Errorf("picked %s, want a.jpg or b.png", base)
	}
}

func TestPick_Recursive(t *testing.T) {
	dir := t.TempDir()
	mkFiles(t, dir, "a.jpg", "sub/b.jpg", "sub/nested/c.jpg", "sub/d.txt")

	flat, err := Pick(dir, false, []string{"jpg"}, nil, newRNG(1))
	if err != nil {
		t.Fatal(err)
	}
	if flat.Pool != 1 {
		t.Errorf("flat pool = %d, want 1", flat.Pool)
	}

	rec, err := Pick(dir, true, []string{"jpg"}, nil, newRNG(1))
	if err != nil {
		t.Fatal(err)
	}
	if rec.Pool != 3 {
		t.Errorf("recursive pool = %d, want 3", rec.Pool)
	}
}

func TestPick_ExcludesRecent(t *testing.T) {
	dir := t.TempDir()
	mkFiles(t, dir, "a.jpg", "b.jpg", "c.jpg", "d.jpg", "e.jpg", "f.jpg", "g.jpg", "h.jpg", "i.jpg", "j.jpg")

	recent := []string{"a.jpg", "b.jpg", "c.jpg", "d.jpg", "e.jpg"}
	picks := map[string]int{}
	for i := 0; i < 50; i++ {
		res, err := Pick(dir, false, []string{"jpg"}, recent, newRNG(uint64(i+1)))
		if err != nil {
			t.Fatal(err)
		}
		picks[filepath.Base(res.Path)]++
	}
	for _, excluded := range recent {
		if picks[excluded] > 0 {
			t.Errorf("recent %s was picked %d times", excluded, picks[excluded])
		}
	}
	keys := make([]string, 0, len(picks))
	for k := range picks {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) < 3 {
		t.Errorf("too few distinct picks across 50 tries: %v", keys)
	}
}

func TestPick_ResetsWhenExclusionEmptiesPool(t *testing.T) {
	dir := t.TempDir()
	mkFiles(t, dir, "a.jpg", "b.jpg")
	// Pool of 2 → N = min(5, floor(2/2)) = 1. recent[0] = "a.jpg" → only "b.jpg"
	// is available. If recent covers ALL files, reset → full pool.
	recent := []string{"a.jpg", "b.jpg"}
	res, err := Pick(dir, false, []string{"jpg"}, recent, newRNG(1))
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	// N would be 1, so only "a.jpg" gets excluded; "b.jpg" remains.
	if filepath.Base(res.Path) != "b.jpg" {
		t.Errorf("with N=1 only a.jpg is excluded; got %s", res.Path)
	}
}

func TestPick_NoFilesIsError(t *testing.T) {
	dir := t.TempDir()
	if _, err := Pick(dir, false, []string{"jpg"}, nil, newRNG(1)); err == nil {
		t.Fatal("expected error for empty pool")
	}
}

func TestTrim(t *testing.T) {
	// Pool of 20 → limit = min(5, 10) = 5. Adds latest + prior history.
	got := Trim([]string{"old1", "old2", "old3", "old4", "old5", "old6"}, "new", 20)
	if len(got) != 5 || got[0] != "new" || got[4] != "old4" {
		t.Errorf("Trim cap = %v, want [new old1..old4]", got)
	}

	// Pool of 4 → limit = 2.
	got = Trim([]string{"a", "b", "c"}, "new", 4)
	if len(got) != 2 || got[0] != "new" || got[1] != "a" {
		t.Errorf("Trim(pool=4) = %v, want [new a]", got)
	}

	// Latest already in history is deduped (not double-listed).
	got = Trim([]string{"a", "b"}, "a", 20)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("Trim dedupe = %v, want [a b]", got)
	}
}

func TestRelKey(t *testing.T) {
	root := "/foo/bar"
	if got := RelKey(root, "/foo/bar/baz.jpg"); got != "baz.jpg" {
		t.Errorf("RelKey flat = %q", got)
	}
	if got := RelKey(root, "/foo/bar/sub/baz.jpg"); got != "sub/baz.jpg" {
		t.Errorf("RelKey nested = %q", got)
	}
}
