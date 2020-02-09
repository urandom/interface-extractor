package testdata

import (
	"log"

	baar "github.com/urandom/interface-extractor/testdata/bar"
)

type Baz struct{}

func (b *Baz) SomeMethod(i int) {
	b.impl()
}

func (b Baz) impl() {}

func ProcessBar(b *baar.Bar) int {
	a := b.Const()
	log.Println(b.EmbeddedMethod(a))

	return a * 4
}

func ProcessBaz(b Baz) {
	b.SomeMethod(42)
}
