package testdata

import (
	"fmt"
	"log"

	baar "github.com/urandom/interface-extractor/testdata/bar"
)

type Baz struct{}

func (b *Baz) SomeMethod(i int) {
	b.impl()
}

func (b Baz) impl() {}

func NewBaz(data string) (Baz, error) {
	b := Baz{}
	b.impl()

	return b, nil
}

func ProcessBar(b *baar.Bar) int {
	a := b.Const()
	log.Println(b.EmbeddedMethod(a))

	return a * 4
}

func ProcessBaz(b Baz) {
	b.SomeMethod(42)
}

func ProcessAlpha(a baar.Alpha) {
	a.AnotherAlphaMethod()
}

func UseStreamer(s baar.Streamer) {
	fmt.Println(<-s.Stream())
}

func UseStreamConsumer(c baar.StreamConsumer) {
	c.Consume(nil)
}
