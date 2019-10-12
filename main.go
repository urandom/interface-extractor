package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"go/types"
	"io"
	"log"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/davecgh/go-spew/spew"
	"golang.org/x/tools/go/packages"
)

type config struct {
	Name    string
	Tags    string
	Package string
	Output  string
}

var (
	cfg = config{}

	typeF = flag.String("type", "", "the concrete type")
)

func init() {
	flag.StringVar(&cfg.Name, "name", "", "name of the created interface. Blank for derived name")
	flag.StringVar(&cfg.Tags, "tags", "", "build tags to add")
	flag.StringVar(&cfg.Package, "package", "", "name of the package for the generated file. Blank for the first loaded package")
	flag.StringVar(&cfg.Output, "output", "", "output name. Blank for derived. '-' for stdout")

}

func main() {
	flag.Parse()

	if *typeF == "" {
		log.Fatalln("no type given")
	}

	packages, err := load(flag.Args()...)
	if err != nil {
		log.Fatalln("Error loading packages", err)
	}

	spew.Config.SortKeys = true
	spew.Config.DisableCapacities = true
	spew.Config.DisableMethods = true
	spew.Config.MaxDepth = 11

	for _, p := range packages {
		c, err := locateType(*typeF, p)
		if err != nil {
			log.Fatalln("locating type", err)
		}

		if c == nil {
			log.Println("The requested type was not found in", p)
			continue
		}

		if err := writeTo(c, os.Stdout, cfg); err != nil {
			log.Printf("Error writing interface: %v", err)
			continue
		}
	}
}

func load(patterns ...string) ([]*packages.Package, error) {
	return packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedDeps | packages.NeedImports,
	}, patterns...)
}

type Methoder interface {
	types.Type
	Method(i int) *types.Func
	NumMethods() int
	Obj() *types.TypeName
}

type Pointer interface {
	Elem() types.Type
}

type Concrete struct {
	Name        string
	PackageName string
	PackagePath string
	FoundIn     string
	AllMethods  []Method
	Used        map[string]struct{}
}

type Method struct {
	Name    string
	Params  []Param
	Returns []Param
}

type Param struct {
	Name string
	Type types.Type
}

func locateType(selector string, p *packages.Package) (*Concrete, error) {
	parts := strings.SplitN(selector, ".", 2)
	if len(parts) != 2 {
		return nil, errors.New("invalid selector")
	}

	var c *Concrete
	x, sel := parts[0], parts[1]
	for _, t := range p.TypesInfo.Types {
		m := exprFilter(t, sel, x)
		if m == nil {
			continue
		}

		c = newConcrete(p, x, sel, m)

		break
	}

	if c != nil {
		locateUsedMethods(c, p)
	}

	return c, nil
}

func methoderFromType(typ types.Type) Methoder {
	if pointer, ok := typ.(Pointer); ok {
		typ = pointer.Elem()
	}

	m, ok := typ.(Methoder)
	if !ok {
		return nil
	}

	return m
}

func exprFilter(t types.TypeAndValue, sel string, x string) Methoder {
	m := methoderFromType(t.Type)
	if m == nil {
		return nil
	}

	obj := m.Obj()
	if obj.Pkg() == nil || x != obj.Pkg().Name() || sel != obj.Name() {
		return nil
	}

	return m
}

func newConcrete(p *packages.Package, x, sel string, m Methoder) *Concrete {
	c := &Concrete{Name: sel, PackageName: x, PackagePath: m.Obj().Pkg().Path(), FoundIn: p.Name, AllMethods: make([]Method, 0, m.NumMethods()), Used: map[string]struct{}{}}

	for i := 0; i < m.NumMethods(); i++ {
		tM := m.Method(i)
		if !tM.Exported() {
			continue

		}

		m := Method{Name: tM.Name()}
		sig, ok := tM.Type().(*types.Signature)
		if !ok {
			continue
		}

		m.Params = make([]Param, sig.Params().Len())
		for j := 0; j < sig.Params().Len(); j++ {
			param := sig.Params().At(j)

			m.Params[j] = Param{
				Name: param.Name(),
				Type: param.Type(),
			}
		}
		m.Returns = make([]Param, sig.Results().Len())
		for j := 0; j < sig.Results().Len(); j++ {
			param := sig.Results().At(j)

			m.Returns[j] = Param{
				Name: param.Name(),
				Type: param.Type(),
			}
		}

		c.AllMethods = append(c.AllMethods, m)
	}

	return c
}

func locateUsedMethods(c *Concrete, p *packages.Package) {
	for n, obj := range p.TypesInfo.Uses {
		if obj == nil {
			continue
		}

		sig, ok := obj.Type().(*types.Signature)
		if !ok || sig.Recv() == nil {
			continue
		}

		m := methoderFromType(sig.Recv().Type())
		if m == nil || m.Obj() == nil || m.Obj().Pkg() == nil || m.Obj().Pkg().Path() != c.PackagePath || m.Obj().Name() != c.Name {
			continue
		}

		c.Used[n.Name] = struct{}{}
	}
}

func deriveName(c *Concrete, nameF string) string {
	if nameF != "" {
		return nameF
	}

	if strings.HasSuffix(c.Name, "er") {
		return c.Name
	} else if strings.HasSuffix(c.Name, "e") {
		return c.Name + "r"
	}

	return c.Name + "er"
}

func deriveFileName(name, output string) string {
	if output != "" {
		return output
	}

	re := regexp.MustCompile(`[A-Z]`)
	conv := re.ReplaceAllStringFunc(name, func(s string) string {
		return "_" + strings.ToLower(s)
	})

	if conv[0] == '_' {
		conv = conv[1:]
	}

	return conv + ".go"
}

func writeTo(c *Concrete, w io.Writer, cfg config) error {
	var b bytes.Buffer

	if cfg.Tags != "" {
		fmt.Fprintf(&b, "// +build %s\n\n", cfg.Tags)
	}

	fmt.Fprintf(&b, "// generated by %s. DO NOT EDIT!\n\n", strings.Join(os.Args, " "))

	b.WriteString("package ")
	if cfg.Package == "" {
		b.WriteString(c.FoundIn)
	} else {
		b.WriteString(cfg.Package)
	}

	imports := map[string]string{}
	for _, m := range c.AllMethods {
		params := make([]Param, len(m.Params)+len(m.Returns))
		copy(params, m.Params)
		copy(params[len(m.Params):], m.Returns)
		for _, p := range params {
			m := methoderFromType(p.Type)
			if m == nil || m.Obj().Pkg() == nil {
				continue
			}

			path := m.Obj().Pkg().Path()
			if path == c.PackagePath {
				continue
			}
			imports[path] = m.Obj().Pkg().Name()
		}
	}

	pkgRewrites := map[string]string{}
	if len(imports) > 0 {
		b.WriteString("\n\nimport (\n")
		included := map[string]struct{}{}
		for path, name := range imports {
			lastIdx := strings.LastIndex(path, "/")
			for _, ok := included[name]; ok; {
				lastIdx = strings.LastIndex(path[:lastIdx], "/")
				name = strings.Map(func(r rune) rune {
					switch r {
					case '.', '/':
						return '_'
					default:
						return r
					}
				}, path[lastIdx+1:])

				_, ok = included[name]
			}

			if strings.HasSuffix(path, "/"+name) {
				fmt.Fprintf(&b, "\t%q\n", path)
			} else {
				fmt.Fprintf(&b, "\t%s %q\n", name, path)
				pkgRewrites[path[strings.LastIndex(path, "/")+1:]] = name
			}
			included[name] = struct{}{}
		}
		b.WriteString(")\n\n")
	}

	fmt.Fprintf(&b, "type %s interface {\n", deriveName(c, cfg.Name))

	sort.Slice(c.AllMethods, func(i, j int) bool {
		return c.AllMethods[i].Name < c.AllMethods[j].Name
	})

	for _, m := range c.AllMethods {
		if _, ok := c.Used[m.Name]; !ok {
			continue
		}

		fmt.Fprintf(&b, "\t%s(", m.Name)
		writeParams(m.Params, &b, pkgRewrites)
		b.WriteString(")")

		if len(m.Returns) > 0 {
			b.WriteString(" ")
			if len(m.Returns) > 1 {
				b.WriteString("(")
			}

			writeParams(m.Returns, &b, pkgRewrites)

			if len(m.Returns) > 1 {
				b.WriteString(")")
			}
		}

		b.WriteString("\n")
	}

	b.WriteString("}\n")

	formatted, err := format.Source(b.Bytes())
	if err != nil {
		return fmt.Errorf("Error formatting code: %w (%s)", err, b.String())
	}

	_, err = w.Write(formatted)
	return err
}

func writeParams(params []Param, w io.Writer, pkgRewrites map[string]string) {
	for i, p := range params {
		if i > 0 {
			w.Write([]byte(", "))
		}
		kind := p.Type.String()
		idx := strings.LastIndex(kind, "/")
		if idx > -1 {
			kind = kind[idx+1:]
		}
		dotIdx := strings.LastIndex(kind, ".")
		if dotIdx != -1 {
			pkg := kind[:dotIdx]
			if rewrite := pkgRewrites[pkg]; rewrite != "" {
				kind = rewrite + kind[dotIdx:]
			}
		}

		if p.Name != "" {
			fmt.Fprintf(w, "%s ", p.Name)
		}

		fmt.Fprintf(w, "%s", kind)
	}
}
