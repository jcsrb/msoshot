package render

import (
	"bytes"
	"hash/maphash"
	"image"
)

// overlapEdge is how many rows at each end of an overlap may differ. Window
// edges (a border, a static margin) stay put while the content scrolls, so the
// very first and last rows of the overlapping band don't move with it.
const overlapEdge = 8

// minOverlapRows is the smallest band that counts as a real overlap.
const minOverlapRows = 8

// TrimOverlap finds where next continues prev, which overlap when a scroll
// moves less than one screen. It returns next with its repeated top rows
// removed, how many rows were removed from next, and how many rows to cut
// from the bottom of prev. Those bottom rows are static window edge (such as
// the pane's border) that would otherwise show as a line at the join.
// Joining prev minus cutPrev rows with the returned image gives continuous
// content. removed is 0 if no overlap was found. prev and next must be raw
// captures of the same area.
//
// It looks for the shortest scroll distance d where every row of next lines up
// with row i+d of prev, ignoring overlapEdge rows at each end of the band. A
// band made only of single-colour rows (blank space) doesn't count, so blank
// margins are never mistaken for an overlap.
func TrimOverlap(prev, next *image.RGBA) (trimmed *image.RGBA, removed, cutPrev int) {
	pb, nb := prev.Bounds(), next.Bounds()
	if pb.Dx() != nb.Dx() || pb.Dy() != nb.Dy() || pb.Dy() < 2*overlapEdge+minOverlapRows {
		return next, 0, 0
	}
	h := pb.Dy()
	seed := maphash.MakeSeed()
	ph, pu := rowHashes(prev, seed)
	nh, _ := rowHashes(next, seed)
	same := func(i, d int) bool { return ph[i+d] == nh[i] && rowsEqual(prev, next, i+d, i) }

	for d := 1; h-d-2*overlapEdge >= minOverlapRows; d++ {
		k := h - d // rows of next that would overlap prev
		match, content := true, false
		for i := overlapEdge; i < k-overlapEdge; i++ {
			if !same(i, d) {
				match = false
				break
			}
			content = content || !pu[i+d]
		}
		if !match || !content {
			continue
		}
		// Rows at the very bottom of prev that don't follow the scroll are
		// window edge. Drop them from prev and take those rows from next.
		for cutPrev < overlapEdge && !same(k-1-cutPrev, d) {
			cutPrev++
		}
		removed = k - cutPrev
		return Crop(next, image.Rect(nb.Min.X, nb.Min.Y+removed, nb.Max.X, nb.Max.Y)), removed, cutPrev
	}
	return next, 0, 0
}

// rowHashes returns a hash per row and whether each row is a single colour.
func rowHashes(img *image.RGBA, seed maphash.Seed) ([]uint64, []bool) {
	b := img.Bounds()
	hashes := make([]uint64, b.Dy())
	uniform := make([]bool, b.Dy())
	for y := 0; y < b.Dy(); y++ {
		row := rowBytes(img, y)
		hashes[y] = maphash.Bytes(seed, row)
		uniform[y] = true
		for x := 4; x < len(row); x += 4 {
			if !bytes.Equal(row[x:x+4], row[:4]) {
				uniform[y] = false
				break
			}
		}
	}
	return hashes, uniform
}

func rowBytes(img *image.RGBA, y int) []byte {
	b := img.Bounds()
	off := img.PixOffset(b.Min.X, b.Min.Y+y)
	return img.Pix[off : off+4*b.Dx()]
}

// rowsEqual confirms a hash match byte for byte.
func rowsEqual(a, b *image.RGBA, ya, yb int) bool {
	return bytes.Equal(rowBytes(a, ya), rowBytes(b, yb))
}
