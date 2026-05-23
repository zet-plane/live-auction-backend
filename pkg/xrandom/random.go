package xrandom

import (
	"math/rand"
	"time"
)

const (
	RAND_NUM   = 0
	RAND_LOWER = 1
	RAND_UPPER = 2
	RAND_ALL   = 3
)

func GetRandom(size int, kind int) string {
	iKind, kinds, result := kind, [][]int{{10, 48}, {26, 97}, {26, 65}}, make([]byte, size)
	isAll := kind > 2 || kind < 0
	rand.Seed(time.Now().UnixNano())
	for i := 0; i < size; i++ {
		if isAll {
			iKind = rand.Intn(3)
		}
		scope, base := kinds[iKind][0], kinds[iKind][1]
		result[i] = uint8(base + rand.Intn(scope))
	}
	return string(result)
}
