package activity

import "testing"

func TestRing(t *testing.T) {
	l := New(2)
	l.Add(Entry{Subject: "a"}, []byte{1})
	id2 := l.Add(Entry{Subject: "b"}, nil)
	id3 := l.Add(Entry{Subject: "c"}, []byte{3})
	r := l.Recent()
	if len(r) != 2 || r[0].ID != id3 || r[1].ID != id2 || !r[0].HasImage || r[1].HasImage {
		t.Fatalf("recent: %+v", r)
	}
	if _, ok := l.PNG(1); ok {
		t.Fatal("evicted image still returned")
	}
	if p, ok := l.PNG(id3); !ok || p[0] != 3 {
		t.Fatal("image missing")
	}
}
