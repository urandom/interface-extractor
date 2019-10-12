package testdata

import (
	baar "github.com/urandom/interface-extractor/testdata/bar"
)

func ProcessBar(b *baar.Bar) int {
	a := b.Const()

	return a * 4
}
