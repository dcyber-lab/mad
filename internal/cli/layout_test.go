package cli

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The list of packages in CONTRIBUTING.md is held to the code: every
// package under internal/ is in it, each above the ones it imports, and
// nothing is listed that is not there. A new package fails this until it
// has its line; so does one moved to where it does not belong.
func TestContributingListsEveryPackageAboveItsImports(t *testing.T) {
	doc, err := os.ReadFile("../../CONTRIBUTING.md")
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]int{} // package, from internal/ → its line in the list
	for i, m := range regexp.MustCompile("(?m)^\\| `internal/([a-z/]+)`").FindAllStringSubmatch(string(doc), -1) {
		if _, twice := listed[m[1]]; twice {
			t.Errorf("internal/%s is listed twice", m[1])
		}
		listed[m[1]] = i
	}
	if len(listed) == 0 {
		t.Fatal("CONTRIBUTING.md has no list of packages")
	}

	// What each package imports of the others, from its own files (not
	// its tests', which may reach upwards).
	const mod = "github.com/dcyber-lab/mad/internal/"
	imports := map[string]map[string]bool{}
	err = filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		pkg := filepath.ToSlash(strings.TrimPrefix(filepath.Dir(path), ".."+string(filepath.Separator)))
		if imports[pkg] == nil {
			imports[pkg] = map[string]bool{}
		}
		for _, im := range f.Imports {
			if p, _ := strconv.Unquote(im.Path.Value); strings.HasPrefix(p, mod) {
				imports[pkg][strings.TrimPrefix(p, mod)] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(imports) < 20 {
		t.Fatalf("found %d packages under internal/: the walk is not where it thinks", len(imports))
	}

	var pkgs []string
	for pkg := range imports {
		pkgs = append(pkgs, pkg)
	}
	sort.Strings(pkgs)
	for _, pkg := range pkgs {
		at, ok := listed[pkg]
		if !ok {
			t.Errorf("internal/%s is not in CONTRIBUTING.md's list of packages", pkg)
			continue
		}
		for dep := range imports[pkg] {
			if below, ok := listed[dep]; ok && below <= at {
				t.Errorf("internal/%s imports internal/%s, which the list has above it", pkg, dep)
			}
		}
	}
	for pkg := range listed {
		if _, ok := imports[pkg]; !ok {
			t.Errorf("CONTRIBUTING.md lists internal/%s, which is not there", pkg)
		}
	}
}
