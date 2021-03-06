package s2

import (
	"bytes"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/mkevac/gos2/r1"
	"github.com/mkevac/gos2/r2"
	"github.com/mkevac/gos2/r3"
)

// CellID uniquely identifies a cell in the S2 cell decomposition.
// The most significant 3 bits encode the face number (0-5). The
// remaining 61 bits encode the position of the center of this cell
// along the Hilbert curve on that face. The zero value and the value
// (1<<64)-1 are invalid cell IDs. The first compares less than any
// valid cell ID, the second as greater than any valid cell ID.
type CellID uint64

// TODO(dsymonds): Some of these constants should probably be exported.
const (
	faceBits     = 3
	numFaces     = 6
	maxLevel     = 30
	posBits      = 2*maxLevel + 1
	maxSize      = 1 << maxLevel
	wrapOffset   = numFaces << posBits
	MaxCellLevel = maxLevel // export
)

// CellIDFromFacePosLevel returns a cell given its face in the range
// [0,5], the 61-bit Hilbert curve position pos within that face, and
// the level in the range [0,maxLevel]. The position in the cell ID
// will be truncated to correspond to the Hilbert curve position at
// the center of the returned cell.
func CellIDFromFacePosLevel(face int, pos uint64, level int) CellID {
	return CellID(uint64(face)<<posBits + pos | 1).Parent(level)
}

func CellIDBegin(level int) CellID {
	return CellIDFromFacePosLevel(0, 0, 0).ChildBeginAtLevel(level)
}

func CellIDEnd(level int) CellID {
	return CellIDFromFacePosLevel(5, 0, 0).ChildEndAtLevel(level)
}

// CellIDFromFace returns the cell corresponding to a given S2 cube face.
func CellIDFromFace(face int) CellID {
	return CellID((uint64(face) << posBits) + lsbForLevel(0))
}

// CellIDFromLatLng returns the leaf cell containing ll.
func CellIDFromLatLng(ll LatLng) CellID {
	return cellIDFromPoint(PointFromLatLng(ll))
}

// CellIDFromToken returns a cell given a hex-encoded string of its uint64 ID.
func CellIDFromToken(s string) CellID {
	if len(s) > 16 {
		return CellID(0)
	}
	n, err := strconv.ParseUint(s, 16, 64)
	if err != nil {
		return CellID(0)
	}
	// Equivalent to right-padding string with zeros to 16 characters.
	if len(s) < 16 {
		n = n << (4 * uint(16-len(s)))
	}
	return CellID(n)
}

func Sentinel() CellID {
	return CellID(^uint64(0))
}

func (ci CellID) Advance(steps int64) CellID {
	if steps == 0 {
		return ci
	}
	stepShift := uint64(2*(maxLevel-ci.Level()) + 1)
	if steps < 0 {
		minSteps := -int64(uint64(ci) >> stepShift)
		if steps < minSteps {
			steps = minSteps
		}
	} else {
		maxSteps := (wrapOffset + ci.lsb() - uint64(ci)) >> stepShift
		if uint64(steps) > maxSteps {
			steps = int64(maxSteps)
		}
	}
	return CellID(uint64(ci) + (uint64(steps) << stepShift))
}

// ToToken returns a hex-encoded string of the uint64 cell id, with leading
// zeros included but trailing zeros stripped.
func (ci CellID) ToToken() string {
	s := strings.TrimRight(fmt.Sprintf("%016x", uint64(ci)), "0")
	if len(s) == 0 {
		return "X"
	}
	return s
}

// IsValid reports whether ci represents a valid cell.
func (ci CellID) IsValid() bool {
	return ci.Face() < numFaces && (ci.lsb()&0x1555555555555555 != 0)
}

// Face returns the cube face for this cell ID, in the range [0,5].
func (ci CellID) Face() int { return int(uint64(ci) >> posBits) }

// Pos returns the position along the Hilbert curve of this cell ID, in the range [0,2^posBits-1].
func (ci CellID) Pos() uint64 { return uint64(ci) & (^uint64(0) >> faceBits) }

// Level returns the subdivision level of this cell ID, in the range [0, maxLevel].
func (ci CellID) Level() int {
	// Fast path for leaf cells.
	if ci.IsLeaf() {
		return maxLevel
	}
	x := uint32(ci)
	level := -1
	if x != 0 {
		level += 16
	} else {
		x = uint32(uint64(ci) >> 32)
	}
	// Only need to look at even-numbered bits for valid cell IDs.
	x &= -x // remove all but the LSB.
	if x&0x00005555 != 0 {
		level += 8
	}
	if x&0x00550055 != 0 {
		level += 4
	}
	if x&0x05050505 != 0 {
		level += 2
	}
	if x&0x11111111 != 0 {
		level += 1
	}
	return level
}

// IsLeaf returns whether this cell ID is at the deepest level;
// that is, the level at which the cells are smallest.
func (ci CellID) IsLeaf() bool { return uint64(ci)&1 != 0 }

// ChildPosition returns the child position (0..3) of this cell's
// ancestor at the given level, relative to its parent.  The argument
// should be in the range 1..kMaxLevel.  For example,
// ChildPosition(1) returns the position of this cell's level-1
// ancestor within its top-level face cell.
func (ci CellID) ChildPosition(level int) int {
	return int(uint64(ci)>>uint64(2*(maxLevel-level)+1)) & 3
}

// lsbForLevel returns the lowest-numbered bit that is on for cells at the given level.
func lsbForLevel(level int) uint64 { return 1 << uint64(2*(maxLevel-level)) }

// Parent returns the cell at the given level, which must be no greater than the current level.
func (ci CellID) Parent(level int) CellID {
	lsb := lsbForLevel(level)
	return CellID((uint64(ci) & -lsb) | lsb)
}

// immediateParent is cheaper than Parent, but assumes !ci.isFace().
func (ci CellID) immediateParent() CellID {
	nlsb := CellID(ci.lsb() << 2)
	return (ci & -nlsb) | nlsb
}

func (ci CellID) Child(pos int) CellID {
	lsb := ci.lsb() >> 2
	return CellID(uint64(ci) + uint64(2*pos+1-4)*lsb)
}

// isFace returns whether this is a top-level (face) cell.
func (ci CellID) isFace() bool { return uint64(ci)&(lsbForLevel(0)-1) == 0 }

// lsb returns the least significant bit that is set.
func (ci CellID) lsb() uint64 { return uint64(ci) & -uint64(ci) }

// Children returns the four immediate children of this cell.
// If ci is a leaf cell, it returns four identical cells that are not the children.
func (ci CellID) Children() [4]CellID {
	var ch [4]CellID
	lsb := CellID(ci.lsb())
	ch[0] = ci - lsb + lsb>>2
	lsb >>= 1
	ch[1] = ch[0] + lsb
	ch[2] = ch[1] + lsb
	ch[3] = ch[2] + lsb
	return ch
}

func sizeIJ(level int) int {
	return 1 << uint(maxLevel-level)
}

// EdgeNeighbors returns the four cells that are adjacent across the cell's four edges.
// Edges 0, 1, 2, 3 are in the down, right, up, left directions in the face space.
// All neighbors are guaranteed to be distinct.
func (ci CellID) EdgeNeighbors() [4]CellID {
	level := ci.Level()
	size := sizeIJ(level)
	f, i, j, _ := ci.faceIJOrientation()
	return [4]CellID{
		cellIDFromFaceIJWrap(f, i, j-size).Parent(level),
		cellIDFromFaceIJWrap(f, i+size, j).Parent(level),
		cellIDFromFaceIJWrap(f, i, j+size).Parent(level),
		cellIDFromFaceIJWrap(f, i-size, j).Parent(level),
	}
}

func (ci CellID) AppendVertexNeighbors(level int, out *[]CellID) {
	var isame, jsame bool
	var ioff, joff int
	// level must be strictly less than this cell's level so that we can
	// determine which vertex this cell is closest to.
	if level >= ci.Level() {
		return
	}
	face, i, j, _ := ci.faceIJOrientation()
	halfsize := sizeIJ(level + 1)
	size := halfsize << 1
	if i&halfsize != 0 {
		ioff = size
		isame = (i + size) < maxSize
	} else {
		ioff = -size
		isame = (i - size) >= 0
	}
	if j&halfsize != 0 {
		joff = size
		jsame = (j + size) < maxSize
	} else {
		joff = -size
		jsame = (j - size) >= 0
	}
	*out = append(*out, ci.Parent(level))
	*out = append(*out, cellIDFromFaceIJSame(face, i+ioff, j, isame).Parent(level))
	*out = append(*out, cellIDFromFaceIJSame(face, i, j+joff, jsame).Parent(level))
	// If i- and j- edge neighbors are *both* on a different face, then this
	// vertex only has three neighors (it is on of the 8 cube vertices)
	if isame || jsame {
		*out = append(*out, cellIDFromFaceIJSame(face, i+ioff, j+joff, isame && jsame).Parent(level))
	}
}

func (ci CellID) AppendAllNeighbors(nbrLevel int, out *[]CellID) {
	face, i, j, _ := ci.faceIJOrientation()
	size := sizeIJ(ci.Level())
	i &= -size
	j &= -size
	nbrSize := sizeIJ(nbrLevel)

	for k := -nbrSize; ; k += nbrSize {
		var sameFace bool
		if k < 0 {
			sameFace = (j+k >= 0)
		} else if k >= size {
			sameFace = (j+k < maxSize)
		} else {
			sameFace = true
			// North and South neighbors.
			*out = append(*out, cellIDFromFaceIJSame(face, i+k, j-nbrSize,
				j-size >= 0).Parent(nbrLevel))
			*out = append(*out, cellIDFromFaceIJSame(face, i+k, j+size,
				j+size < maxSize).Parent(nbrLevel))
		}
		// East, West, and Diagonal neighbors.
		*out = append(*out, cellIDFromFaceIJSame(face, i-nbrSize, j+k,
			sameFace && i-size >= 0).Parent(nbrLevel))
		*out = append(*out, cellIDFromFaceIJSame(face, i+size, j+k,
			sameFace && i+size < maxSize).Parent(nbrLevel))
		if k >= size {
			break
		}
	}
}

// RangeMin returns the minimum CellID that is contained within this cell.
func (ci CellID) RangeMin() CellID { return CellID(uint64(ci) - (ci.lsb() - 1)) }

// RangeMax returns the maximum CellID that is contained within this cell.
func (ci CellID) RangeMax() CellID { return CellID(uint64(ci) + (ci.lsb() - 1)) }

// Contains returns true iff the CellID contains oci.
func (ci CellID) Contains(oci CellID) bool {
	return uint64(ci.RangeMin()) <= uint64(oci) && uint64(oci) <= uint64(ci.RangeMax())
}

// Intersects returns true iff the CellID intersects oci.
func (ci CellID) Intersects(oci CellID) bool {
	return uint64(oci.RangeMin()) <= uint64(ci.RangeMax()) && uint64(oci.RangeMax()) >= uint64(ci.RangeMin())
}

// String returns the string representation of the cell ID in the form "1/3210".
func (ci CellID) String() string {
	if !ci.IsValid() {
		return "Invalid: " + strconv.FormatInt(int64(ci), 16)
	}
	var b bytes.Buffer
	b.WriteByte("012345"[ci.Face()]) // values > 5 will have been picked off by !IsValid above
	b.WriteByte('/')
	for level := 1; level <= ci.Level(); level++ {
		b.WriteByte("0123"[ci.ChildPosition(level)])
	}
	return b.String()
}

// Point returns the center of the s2 cell on the sphere as a Point.
func (ci CellID) Point() Point { return Point{ci.rawPoint().Normalize()} }

// LatLng returns the center of the s2 cell on the sphere as a LatLng.
func (ci CellID) LatLng() LatLng { return LatLngFromPoint(Point{ci.rawPoint()}) }

// ChildBegin returns the first child in a traversal of the children of this cell, in Hilbert curve order.
//
//    for ci := c.ChildBegin(); ci != c.ChildEnd(); ci = ci.Next() {
//        ...
//    }
func (ci CellID) ChildBegin() CellID {
	ol := ci.lsb()
	return CellID(uint64(ci) - ol + ol>>2)
}

// ChildBeginAtLevel returns the first cell in a traversal of children a given level deeper than this cell, in
// Hilbert curve order. The given level must be no smaller than the cell's level.
func (ci CellID) ChildBeginAtLevel(level int) CellID {
	return CellID(uint64(ci) - ci.lsb() + lsbForLevel(level))
}

// ChildEnd returns the first cell after a traversal of the children of this cell in Hilbert curve order.
// The returned cell may be invalid.
func (ci CellID) ChildEnd() CellID {
	ol := ci.lsb()
	return CellID(uint64(ci) + ol + ol>>2)
}

// ChildEndAtLevel returns the first cell after the last child in a traversal of children a given level deeper
// than this cell, in Hilbert curve order.
// The given level must be no smaller than the cell's level.
// The returned cell may be invalid.
func (ci CellID) ChildEndAtLevel(level int) CellID {
	return CellID(uint64(ci) + ci.lsb() + lsbForLevel(level))
}

// Next returns the next cell along the Hilbert curve.
// This is expected to be used with ChildStart and ChildEnd.
func (ci CellID) Next() CellID {
	return CellID(uint64(ci) + ci.lsb()<<1)
}

// TODO: the methods below are not exported yet.  Settle on the entire API design
// before doing this.  Do we want to mirror the C++ one as closely as possible?

// rawPoint returns an unnormalized r3 vector from the origin through the center
// of the s2 cell on the sphere.
func (ci CellID) rawPoint() r3.Vector {
	face, si, ti := ci.faceSiTi()
	return faceUVToXYZ(face, stToUV((0.5/maxSize)*float64(si)), stToUV((0.5/maxSize)*float64(ti)))
}

// faceSiTi returns the Face/Si/Ti coordinates of the center of the cell.
func (ci CellID) faceSiTi() (face, si, ti int) {
	face, i, j, _ := ci.faceIJOrientation()
	delta := 0
	if ci.IsLeaf() {
		delta = 1
	} else {
		if (i^(int(ci)>>2))&1 != 0 {
			delta = 2
		}
	}
	return face, 2*i + delta, 2*j + delta
}

func (ci CellID) centerUV() (u, v float64) {
	_, si, ti := ci.faceSiTi()
	u = (0.5 / maxSize) * float64(si)
	v = (0.5 / maxSize) * float64(ti)
	return
}

// faceIJOrientation uses the global lookupIJ table to unfiddle the bits of ci.
func (ci CellID) faceIJOrientation() (f, i, j, bits int) {
	f = ci.Face()
	bits = f & swapMask
	nbits := maxLevel - 7*lookupBits // first iteration

	for k := 7; k >= 0; k-- {
		bits += (int(uint64(ci)>>uint64(k*2*lookupBits+1)) & ((1 << uint((2 * nbits))) - 1)) << 2
		bits = lookupIJ[bits]
		i += (bits >> (lookupBits + 2)) << uint(k*lookupBits)
		j += ((bits >> 2) & ((1 << lookupBits) - 1)) << uint(k*lookupBits)
		bits &= (swapMask | invertMask)
		nbits = lookupBits // following iterations
	}

	if ci.lsb()&0x1111111111111110 != 0 {
		bits ^= swapMask
	}

	return
}

// cellIDFromFaceIJ returns a leaf cell given its cube face (range 0..5) and IJ coordinates.
func cellIDFromFaceIJ(f, i, j int) CellID {
	// Note that this value gets shifted one bit to the left at the end
	// of the function.
	n := uint64(f) << (posBits - 1)
	// Alternating faces have opposite Hilbert curve orientations; this
	// is necessary in order for all faces to have a right-handed
	// coordinate system.
	bits := f & swapMask
	// Each iteration maps 4 bits of "i" and "j" into 8 bits of the Hilbert
	// curve position.  The lookup table transforms a 10-bit key of the form
	// "iiiijjjjoo" to a 10-bit value of the form "ppppppppoo", where the
	// letters [ijpo] denote bits of "i", "j", Hilbert curve position, and
	// Hilbert curve orientation respectively.
	for k := 7; k >= 0; k-- {
		mask := (1 << lookupBits) - 1
		bits += int((i>>uint(k*lookupBits))&mask) << (lookupBits + 2)
		bits += int((j>>uint(k*lookupBits))&mask) << 2
		bits = lookupPos[bits]
		n |= uint64(bits>>2) << (uint(k) * 2 * lookupBits)
		bits &= (swapMask | invertMask)
	}
	return CellID(n*2 + 1)
}

func cellIDFromFaceIJWrap(f, i, j int) CellID {
	// Convert i and j to the coordinates of a leaf cell just beyond the
	// boundary of this face.  This prevents 32-bit overflow in the case
	// of finding the neighbors of a face cell.
	i = clamp(i, -1, maxSize)
	j = clamp(j, -1, maxSize)

	// We want to wrap these coordinates onto the appropriate adjacent face.
	// The easiest way to do this is to convert the (i,j) coordinates to (x,y,z)
	// (which yields a point outside the normal face boundary), and then call
	// xyzToFaceUV to project back onto the correct face.
	//
	// The code below converts (i,j) to (si,ti), and then (si,ti) to (u,v) using
	// the linear projection (u=2*s-1 and v=2*t-1).  (The code further below
	// converts back using the inverse projection, s=0.5*(u+1) and t=0.5*(v+1).
	// Any projection would work here, so we use the simplest.)  We also clamp
	// the (u,v) coordinates so that the point is barely outside the
	// [-1,1]x[-1,1] face rectangle, since otherwise the reprojection step
	// (which divides by the new z coordinate) might change the other
	// coordinates enough so that we end up in the wrong leaf cell.
	const scale = 1.0 / maxSize
	limit := math.Nextafter(1, 2)
	u := math.Max(-limit, math.Min(limit, scale*float64((i<<1)+1-maxSize)))
	v := math.Max(-limit, math.Min(limit, scale*float64((j<<1)+1-maxSize)))

	// Find the leaf cell coordinates on the adjacent face, and convert
	// them to a cell id at the appropriate level.
	f, u, v = xyzToFaceUV(faceUVToXYZ(f, u, v))
	return cellIDFromFaceIJ(f, stToIJ(0.5*(u+1)), stToIJ(0.5*(v+1)))
}

func cellIDFromFaceIJSame(f, i, j int, same_face bool) CellID {
	if same_face {
		return cellIDFromFaceIJ(f, i, j)
	} else {
		return cellIDFromFaceIJWrap(f, i, j)
	}
}

// clamp returns number closest to x within the range min..max.
func clamp(x, min, max int) int {
	if x < min {
		return min
	}
	if x > max {
		return max
	}
	return x
}

// ijToSTMin converts the i- or j-index of a leaf cell to the minimum corresponding
// s- or t-value contained by that cell. The argument must be in the range
// [0..2**30], i.e. up to one position beyond the normal range of valid leaf
// cell indices.
func ijToSTMin(i int) float64 {
	return float64(i) / float64(maxSize)
}

// stToIJ converts value in ST coordinates to a value in IJ coordinates.
func stToIJ(s float64) int {
	return clamp(int(math.Floor(maxSize*s)), 0, maxSize-1)
}

// cellIDFromPoint returns the leaf cell containing point p.
func cellIDFromPoint(p Point) CellID {
	f, u, v := xyzToFaceUV(r3.Vector{p.X, p.Y, p.Z})
	i := stToIJ(uvToST(u))
	j := stToIJ(uvToST(v))
	return cellIDFromFaceIJ(f, i, j)
}

func CellIDFromPoint(p Point) CellID { return cellIDFromPoint(p) }

// ijLevelToBoundUV returns the bounds in (u,v)-space for the cell at the given
// level containing the leaf cell with the given (i,j)-coordinates.
func ijLevelToBoundUV(i, j, level int) r2.Rect {
	cellSize := sizeIJ(level)
	xLo := i & -cellSize
	yLo := j & -cellSize

	return r2.Rect{
		X: r1.Interval{
			Lo: stToUV(ijToSTMin(xLo)),
			Hi: stToUV(ijToSTMin(xLo + cellSize)),
		},
		Y: r1.Interval{
			Lo: stToUV(ijToSTMin(yLo)),
			Hi: stToUV(ijToSTMin(yLo + cellSize)),
		},
	}
}

// Constants related to the bit mangling in the Cell ID.
const (
	lookupBits = 4
	swapMask   = 0x01
	invertMask = 0x02
)

var (
	posToIJ = [4][4]int{
		{0, 1, 3, 2}, // canonical order:    (0,0), (0,1), (1,1), (1,0)
		{0, 2, 3, 1}, // axes swapped:       (0,0), (1,0), (1,1), (0,1)
		{3, 2, 0, 1}, // bits inverted:      (1,1), (1,0), (0,0), (0,1)
		{3, 1, 0, 2}, // swapped & inverted: (1,1), (0,1), (0,0), (1,0)
	}
	posToOrientation = [4]int{swapMask, 0, 0, invertMask | swapMask}
	lookupIJ         [1 << (2*lookupBits + 2)]int
	lookupPos        [1 << (2*lookupBits + 2)]int
)

func init() {
	initLookupCell(0, 0, 0, 0, 0, 0)
	initLookupCell(0, 0, 0, swapMask, 0, swapMask)
	initLookupCell(0, 0, 0, invertMask, 0, invertMask)
	initLookupCell(0, 0, 0, swapMask|invertMask, 0, swapMask|invertMask)
}

// initLookupCell initializes the lookupIJ table at init time.
func initLookupCell(level, i, j, origOrientation, pos, orientation int) {
	if level == lookupBits {
		ij := (i << lookupBits) + j
		lookupPos[(ij<<2)+origOrientation] = (pos << 2) + orientation
		lookupIJ[(pos<<2)+origOrientation] = (ij << 2) + orientation
		return
	}

	level++
	i <<= 1
	j <<= 1
	pos <<= 2
	r := posToIJ[orientation]
	initLookupCell(level, i+(r[0]>>1), j+(r[0]&1), origOrientation, pos, orientation^posToOrientation[0])
	initLookupCell(level, i+(r[1]>>1), j+(r[1]&1), origOrientation, pos+1, orientation^posToOrientation[1])
	initLookupCell(level, i+(r[2]>>1), j+(r[2]&1), origOrientation, pos+2, orientation^posToOrientation[2])
	initLookupCell(level, i+(r[3]>>1), j+(r[3]&1), origOrientation, pos+3, orientation^posToOrientation[3])
}

// BUG(dsymonds): The major differences from the C++ version is that barely anything is implemented.
