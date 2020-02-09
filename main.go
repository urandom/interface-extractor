package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

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

	for _, p := range packages {
		c, err := locateType(*typeF, p)
		if err != nil {
			log.Fatalln("locating type", err)
		}

		if c == nil {
			log.Println("The requested type was not found in", p)
			continue
		}

		writer, err := getWriter(deriveName(c, cfg.Name), filepath.Dir(p.GoFiles[0]), cfg)
		if err != nil {
			log.Println("Error getting output writer:", err)
			continue
		}
		if err := writeTo(c, writer, cfg); err != nil {
			writer.Close()
			log.Printf("Error writing interface: %v", err)
			continue
		}
		writer.Close()
	}
}

func load(patterns ...string) ([]*packages.Package, error) {
	return packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedDeps | packages.NeedImports | packages.NeedSyntax,
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
	Pos         token.Pos
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
	for _, v := range p.TypesInfo.Defs {
		if v == nil {
			continue
		}
		m := methoderFromType(v.Type())
		if m == nil {
			continue
		}

		obj := m.Obj()
		if obj.Pkg() == nil || x != obj.Pkg().Name() || sel != obj.Name() {
			continue
		}
		c = newConcrete(p, x, sel, m, m.Obj().Pos())
		break
	}

	if c != nil {
		locateUsedMethods(c, p)
	}

	return c, nil
}

func locateInterface(c *Concrete, p *packages.Package, cfg config) {
	name := deriveName(c, cfg.Name)
	for i, obj := range p.TypesInfo.Defs {
		if name != i.Name {
			continue
		}

		if iface, ok := obj.Type().Underlying().(*types.Interface); ok {
			for i := 0; i < iface.NumExplicitMethods(); i++ {
				method := iface.ExplicitMethod(i)
				c.Used[method.Name()] = struct{}{}
			}
		}

		break
	}
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

func newConcrete(p *packages.Package, x, sel string, m Methoder, pos token.Pos) *Concrete {
	c := &Concrete{Name: sel, PackageName: x, PackagePath: m.Obj().Pkg().Path(), FoundIn: p.Name, AllMethods: make([]Method, 0, m.NumMethods()), Used: map[string]struct{}{}, Pos: pos}

	c.AllMethods = getMethods(m, x != p.Name)

	return c
}

func getMethods(m Methoder, differentPkg bool) []Method {
	methods := make([]Method, 0, m.NumMethods())
	for i := 0; i < m.NumMethods(); i++ {
		tM := m.Method(i)
		if !tM.Exported() && differentPkg {
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

		methods = append(methods, m)
	}

	return methods
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

		nPos := p.Fset.Position(n.Pos())

		var withinSelf bool
		for _, f := range p.Syntax {
			fPos := p.Fset.Position(f.Pos())
			if fPos.Filename != nPos.Filename {
				continue
			}

			// Walk the ast tree to see who called us
			ast.Inspect(f, func(node ast.Node) bool {
				if node == nil {
					return true
				}

				switch v := node.(type) {
				case *ast.FuncDecl:
					if v.Pos() < n.Pos() && v.End() > n.Pos() && v.Recv != nil {
						f := v.Recv.List[0]
						for i, t := range p.TypesInfo.Defs {
							if i.Pos() == f.Pos() {
								m := methoderFromType(t.Type())
								if m.Obj().Pos() == c.Pos {
									// The method receiver is the same type as the concrete
									withinSelf = true
								}
							}
						}
					}
				}
				return true
			})
		}

		if !withinSelf {
			c.Used[n.Name] = struct{}{}
		}
	}

	locateInterface(c, p, cfg)
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

func getWriter(name, path string, cfg config) (io.WriteCloser, error) {
	if cfg.Output == "-" {
		return nopCloser{os.Stdout}, nil
	}

	filename := deriveFileName(name, path, cfg.Output)
	f, err := os.Create(filename)
	if err != nil {
		return nil, fmt.Errorf("creating file %s: %v", filename, err)
	}

	return f, nil
}

func deriveFileName(name, path, output string) string {
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

	return filepath.Join(path, conv+"_gen.go")
}

func writeTo(c *Concrete, w io.Writer, cfg config) error {
	var b bytes.Buffer

	if cfg.Tags != "" {
		fmt.Fprintf(&b, "// +build %s\n\n", cfg.Tags)
	}

	fmt.Fprintf(&b, "// generated by %s !DO NOT EDIT!\n\n", strings.Join(os.Args, " "))

	b.WriteString("package ")
	if cfg.Package == "" {
		b.WriteString(c.FoundIn)
	} else {
		b.WriteString(cfg.Package)
	}
	b.WriteRune('\n')

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
		b.WriteString("\nimport (\n")
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

			if strings.HasSuffix(path, "/"+name) || path == name {
				fmt.Fprintf(&b, "\t%q\n", path)
			} else {
				fmt.Fprintf(&b, "\t%s %q\n", name, path)
				pkgRewrites[path[strings.LastIndex(path, "/")+1:]] = name
			}
			included[name] = struct{}{}
		}
		b.WriteString(")\n")
	}

	fmt.Fprintf(&b, "\ntype %s interface {\n", deriveName(c, cfg.Name))

	sort.Slice(c.AllMethods, func(i, j int) bool {
		return c.AllMethods[i].Name < c.AllMethods[j].Name
	})

	for _, m := range c.AllMethods {
		if _, ok := c.Used[m.Name]; !ok {
			continue
		}

		fmt.Fprintf(&b, "\t%s(", m.Name)
		writeParams(m.Params, &b, c.PackageName, pkgRewrites)
		b.WriteString(")")

		if len(m.Returns) > 0 {
			b.WriteString(" ")
			if len(m.Returns) > 1 {
				b.WriteString("(")
			}

			writeParams(m.Returns, &b, c.PackageName, pkgRewrites)

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

func writeParams(params []Param, w io.Writer, dstPkg string, pkgRewrites map[string]string) {
	for i, p := range params {
		if i > 0 {
			w.Write([]byte(", "))
		}
		kind := p.Type.String()
		identStart := strings.IndexFunc(kind, func(r rune) bool {
			return unicode.IsLetter(r)
		})
		idx := strings.LastIndex(kind, "/")
		if idx > -1 {
			kind = kind[:identStart] + kind[idx+1:]
		}
		dotIdx := strings.LastIndex(kind, ".")
		if dotIdx != -1 {
			pkg := kind[identStart:dotIdx]
			if pkg == dstPkg {
				kind = kind[:identStart] + kind[dotIdx+1:]
			} else if rewrite := pkgRewrites[pkg]; rewrite != "" {
				kind = kind[:identStart] + rewrite + kind[dotIdx:]
			}
		}

		if p.Name != "" {
			fmt.Fprintf(w, "%s ", p.Name)
		}

		fmt.Fprintf(w, "%s", kind)
	}
}

type nopCloser struct {
	io.Writer
}

func (c nopCloser) Close() error {
	return nil
}
