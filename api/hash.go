package api

import (
	"encoding/binary"
	"math/big"
)

type Uint128 [2]uint64 // { high, low }

const hashSeed uint32 = 0x61247FBF
const BlockSize = 128

/* IOS 1.13.x */
var magicTable = [16]uint64{
	0x95C05F4D1512959E, 0xE4F3C46EEF0DCF07,
	0x6238DC228F980AD2, 0x53F3E3BC49607092,
	0x4E7BE7069078D625, 0x1016D709D1AD25FC,
	0x044E89B8AC76E045, 0xE0B684DDA364BFA1,
	0x90C533B835E89E5F, 0x3DAF462A74FA874F,
	0xFEA54965DD3EF5A0, 0x287A5D7CCB31B970,
	0xAE681046800752F8, 0x121C2D6EAF66EC6E,
	0xEE8F8CA7E090FB20, 0xCE1AE25F48FE0A52,
}
var magicRound = Uint128{0x78F32468CD48D6DE, 0x14C983660183C0AE}
var magicFinal = Uint128{0xBDB31B10864F3F87, 0x5B7E9E828A9B8ABD}

/************************************************************/

func hash(input []byte) uint64 {
	numBlocks := len(input) / BlockSize
	tailLen := len(input) % BlockSize

	// copy tail, pad with zeroes to multiple of 16
	tail := make([]byte, 16*((tailLen+15)/16))
	copy(tail, input[len(input)-tailLen:])

	var hash Uint128
	if numBlocks > 0 {
		hash = hashBlock(input[0:BlockSize])
	} else {
		hash = hashBlock(tail)
	}

	hash = hash.Add(magicRound)

	if numBlocks > 0 {
		for offset := BlockSize; numBlocks > 1; offset += BlockSize {
			hash = hashMulAdd(hash, magicRound,
				hashBlock(input[offset:offset+BlockSize]))
			numBlocks--
		}

		if tailLen > 0 {
			hash = hashMulAdd(hash, magicRound, hashBlock(tail))
		}
	}

	// Note: 0x7fffffffffffffffffffffffffffffff
	u7fff := Uint128{^uint64(1 << 63), ^uint64(0)}

	hash = hash.Add(Uint128{uint64(tailLen * 8), 0})
	if hash.Cmp(u7fff) >= 0 {
		hash = hash.Add(Uint128{0, 1})
	}
	hash = hash.And(u7fff)

	X := hash[0] + (hash[1] >> 32)
	X = ((X + (X >> 32) + 1) >> 32) + hash[0]
	Y := (X << 32) + hash[1]

	A := X + magicFinal[0]
	if A < X {
		A += 0x101
	}

	B := Y + magicFinal[1]
	if B < Y {
		B += 0x101
	}

	hash = mul64_128(A, B)
	hash = mul64_128(hash[0], 0x101).Add(Uint128{0, hash[1]})
	hash = mul64_128(hash[0], 0x101).Add(Uint128{0, hash[1]})

	result := hash[1]
	if hash[0] != 0 {
		result += 0x101
	}
	if result > 0xFFFFFFFFFFFFFEFE {
		result += 0x101
	}

	return result
}

/* hash block of input */
func hashBlock(block []byte) Uint128 {
	hash := Uint128{0, 0}
	magicIdx := 0
	for offset := 0; offset < len(block); offset += 16 {
		a := binary.LittleEndian.Uint64(block[offset:])
		a += magicTable[magicIdx]
		magicIdx++

		b := binary.LittleEndian.Uint64(block[offset+8:])
		b += magicTable[magicIdx]
		magicIdx++

		hash = hash.Add(mul64_128(a, b))
	}

	// Note: 0x3fffffffffffffffffffffffffffffff
	u3fff := Uint128{^uint64(3 << 62), ^uint64(0)}
	return hash.And(u3fff)
}

/* combine new block with previous hash */
func hashMulAdd(h, m, a Uint128) Uint128 {
	a0 := a[1] << 32 >> 32
	a1 := a[1] >> 32
	a23 := a[0]
	m0 := m[1] << 32 >> 32
	m1 := m[1] >> 32
	m2 := m[0] << 32 >> 32
	m3 := m[0] >> 32
	h0 := h[1] << 32 >> 32
	h1 := h[1] >> 32
	h2 := h[0] << 32 >> 32
	h3 := h[0] >> 32

	c0 := (h0 * m0)
	c1 := (h0 * m1) + (h1 * m0)
	c2 := (h0 * m2) + (h1 * m1) + (h2 * m0)
	c3 := (h0 * m3) + (h1 * m2) + (h2 * m1) + (h3 * m0)
	c4 := (h1 * m3) + (h2 * m2) + (h3 * m1)
	c5 := (h2 * m3) + (h3 * m2)
	c6 := (h3 * m3)

	r2 := c2 + (c6 << 1) + a23
	r3 := c3 + (r2 >> 32)
	r0 := c0 + (c4 << 1) + a0 + (r3 >> 31)
	r1 := c1 + (c5 << 1) + a1 + (r0 >> 32)

	return Uint128{
		((r3 << 33 >> 1) | (r2 << 32 >> 32)) + (r1 >> 32),
		(r1 << 32) | (r0 << 32 >> 32)}
}

/* compare */
func (a Uint128) Cmp(b Uint128) int {
	i := 0
	if a[0] == b[0] {
		i = 1
	}
	if a[i] < b[i] {
		return -1
	} else if a[i] > b[i] {
		return 1
	} else {
		return 0
	}
}

/* addition */
func (a Uint128) Add(b Uint128) Uint128 {
	sum := Uint128{a[0] + b[0], a[1] + b[1]}
	if sum[1] < b[1] {
		sum[0]++
	}
	return sum
}

/* bitwise and */
func (a Uint128) And(b Uint128) Uint128 {
	return Uint128{a[0] & b[0], a[1] & b[1]}
}

/* 64x64->128 multiply */
func mul64_128(a, b uint64) Uint128 {
	zprod := big.NewInt(0)
	zprod.Mul(new(big.Int).SetUint64(a), new(big.Int).SetUint64(b))
	zhi := big.NewInt(0)
	zhi.Rsh(zprod, 64)
	return Uint128{zhi.Uint64(), zprod.Uint64()}
}

func Hash32(buffer []byte) uint32 {
	return Hash32Salt(buffer, hashSeed)
}

func Hash32Salt(buffer []byte, salt uint32) uint32 {
	ret := Hash64Salt(buffer, salt)
	return uint32(ret) ^ uint32(ret>>32)
}

func Hash64(buffer []byte) uint64 {
	return Hash64Salt(buffer, hashSeed)
}

func Hash64Salt(buffer []byte, salt uint32) uint64 {
	newBuffer := make([]byte, len(buffer)+4)
	binary.BigEndian.PutUint32(newBuffer, salt)
	copy(newBuffer[4:], buffer)

	return hash(newBuffer)
}

//a
func Hash64Salt64(buffer []byte, salt uint64) uint64 {
	newBuffer := make([]byte, len(buffer)+8)
	binary.BigEndian.PutUint64(newBuffer, salt)
	copy(newBuffer[8:], buffer)

	return hash(newBuffer)
}
