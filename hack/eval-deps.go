package main

import (
	"bufio"
	"bytes"
	"strconv"
	"encoding/json"
	"fmt"
	"log"
	"io"
	"os"
	"sort"
	"strings"
)

var (
	ignorePackage = map[string]bool{"runtime/cgo": true}
)

// Package is the representation of package required
// for analysis
type Package struct {
	Dir        string
	ImportPath string
	Standard   bool
	Ignore     bool
	Deps       []string
	Info 	   PackageInfo
}

// PackageInfo contains the information we need to
// take out after analysis for the package
type PackageInfo struct {
	IncomingDepdencyCount int
	OutgoingDependencyCount int
}

// setIgnore sets the package needs to be ignored
func (p *Package) setIgnore() {
	if p.Standard == true {
		p.Ignore = true
		return
	}

	if ignorePackage[p.ImportPath] {
		p.Ignore = true
		return
	}

	if strings.HasPrefix(p.ImportPath, "k8s.io/") {
		p.Ignore = true
		return
	}

	p.Ignore = false
}

// setPacakgeInfo sets the package info with incoming and outgoing deps
func (p *Package) setPackageInfo(in int, out int) {
	p.Info = PackageInfo{
		IncomingDepdencyCount: in,
		OutgoingDependencyCount: out,
	}
}

type Builder struct {
	packages []Package
	incomingPackages map[string]map[string]bool
	outgoingPackages map[string][]string
	ignoredPackages map[string]bool
}

func NewBuilder() *Builder {
	return &Builder{
		packages: []Package{},
		incomingPackages: map[string]map[string]bool{},
		outgoingPackages: map[string][]string{},
		ignoredPackages: map[string]bool{},
	}
}

// addPackages loads the packages reading the depedency file
func(b *Builder) addPackages(depFile *os.File) {
	decoder := json.NewDecoder(depFile)
	b.packages = []Package{}
	for {
		p := &Package{}
		err := decoder.Decode(p)
		if err == nil {
			b.packages = append(b.packages, *p)
			continue
		}
		if err == io.EOF {
			break
		}
		checkErr(err)
	}
}

func(b *Builder) setIgnoredPackages() {
	for i, _ := range b.packages {
		b.packages[i].setIgnore()
		if b.packages[i].Ignore == true {
			b.ignoredPackages[b.packages[i].ImportPath] = true
		}
	}
}

// evalDeps evaluates the incoming and outgoing dependencies
func(b *Builder) evalDeps() {
	for _, pkg := range b.packages {
		if pkg.Ignore == true {
			continue
		}
		b.outgoingPackages[pkg.ImportPath] = pkg.Deps
		for _, dep := range pkg.Deps {
			if _, ok := b.ignoredPackages[dep]; ok {
				continue
			}
			if len(b.incomingPackages[dep]) == 0 {
				b.incomingPackages[dep] = map[string]bool{}
			}
			b.incomingPackages[dep][pkg.ImportPath] = true
		}
	}
	for i, _ := range b.packages {
		b.packages[i].setPackageInfo(
			len(b.incomingPackages[b.packages[i].ImportPath]),
			len(b.outgoingPackages[b.packages[i].ImportPath]),
		)
	}

}

// sortDeps sorts the depdendecies as Name then Incoming then Outgoing order
func(b *Builder) sortDeps() {
	sort.SliceStable(b.packages, func(i, j int) bool {
		return b.packages[i].ImportPath < b.packages[j].ImportPath
	})

	sort.SliceStable(b.packages, func(i, j int) bool {
		return b.packages[i].Info.IncomingDepdencyCount > b.packages[j].Info.IncomingDepdencyCount
	})

	sort.SliceStable(b.packages, func(i, j int) bool {
		return b.packages[i].Info.OutgoingDependencyCount > b.packages[j].Info.OutgoingDependencyCount
	})
}

func(b *Builder) showDeps() {
	for _, pkg := range b.packages {
		if pkg.Ignore == true {
			continue
		}
		fmt.Printf("%s:%d:%d\n", pkg.ImportPath, pkg.Info.IncomingDepdencyCount, pkg.Info.OutgoingDependencyCount)
	}
}

// saveDeps save the result of analysis in the file
// this will help in seeing the diffs with every depdendecy change
func(b *Builder) saveDeps() {
	f, err := os.Create("./vendor/dependencies.csv")
	checkErr(err)
	defer f.Close()

	var buffer bytes.Buffer
	w := bufio.NewWriter(f)
	for _, pkg := range b.packages {
		if pkg.Ignore == true {
			continue
		}
		buffer.WriteString(pkg.ImportPath)
		buffer.WriteString(":")
		buffer.WriteString(strconv.Itoa(pkg.Info.IncomingDepdencyCount))
		buffer.WriteString(":")
		buffer.WriteString(strconv.Itoa(pkg.Info.OutgoingDependencyCount))
		buffer.WriteString("\n")
	}
	fmt.Fprint(
		w,
		buffer.String(),
		"\n",
	)
	err = w.Flush()
	checkErr(err)
}

// checkErr is the helper function to avoid writing err != nil
func checkErr(err error) {
	if err != nil {
		log.Fatalf("err: %v", err)
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: go run hack/eval-deps.go deps.json")
		fmt.Fprintln(os.Stderr, "deps.json is the output of `go list -mod=vendor -json -tags linux -e all`")
		os.Exit(1)
	}

	depFile, err := os.Open(os.Args[1])
	checkErr(err)
	defer depFile.Close()

	builder := NewBuilder()
	builder.addPackages(depFile)
	builder.setIgnoredPackages()
	builder.evalDeps()
	builder.sortDeps()
	builder.showDeps()
	builder.saveDeps()
}
