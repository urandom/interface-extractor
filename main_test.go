package main

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_locateType_write(t *testing.T) {
	type args struct {
		selector string
		pattern  string
	}
	tests := []struct {
		name    string
		args    args
		config  config
		want    string
		wantErr bool
	}{
		{name: "test", args: args{selector: "bar.Bar", pattern: "./testdata"}, want: constBarUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packages, err := load(tt.args.pattern)
			if err != nil {
				t.Fatal(err)
			}

			c, err := locateType(tt.args.selector, packages[0])
			if (err != nil) != tt.wantErr {
				t.Errorf("locateType() error = %v, wantErr %v", err, tt.wantErr)
			}

			var got bytes.Buffer
			writeTo(c, &got, tt.config)

			assert.Equal(t, normalizeComment(got.String()), normalizeComment(tt.want))

		})
	}
}

var re = regexp.MustCompile(`generated by .*interface-extractor`)

func normalizeComment(in string) string {
	return re.ReplaceAllString(strings.TrimSpace(in), "generated by interface-extractor")
}

const (
	constBarUsage = `
// generated by interface-extractor.test -test.timeout=10s -test.timeout=10s !DO NOT EDIT!

package testdata

type Barer interface {
	Const() int
}
	`
)