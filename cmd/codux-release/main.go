package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/edwmurph/codux/internal/release"
)

func main() {
	if len(os.Args) < 2 {
		fatal("usage: codux-release <next-version|render-formula>")
	}
	var err error
	switch os.Args[1] {
	case "next-version":
		err = nextVersion(os.Args[2:])
	case "render-formula":
		err = renderFormula(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fatal(err.Error())
	}
}

func nextVersion(args []string) error {
	fs := flag.NewFlagSet("next-version", flag.ContinueOnError)
	base := fs.String("base", "", "base commit/ref")
	head := fs.String("head", "HEAD", "head commit/ref")
	bumpOverride := fs.String("bump", "", "major, minor, or patch")
	versionFile := fs.String("version-file", "VERSION", "version file path")
	goVersionFile := fs.String("go-version-file", "internal/version/version.go", "Go version source path")
	write := fs.Bool("write", false, "write the next version")
	githubOutput := fs.String("github-output", "", "GitHub Actions output file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolvedBase := release.UsableBase(*base, *head)
	files, err := release.ChangedFiles(resolvedBase, *head)
	if err != nil {
		return err
	}
	messages, err := release.CommitMessages(resolvedBase, *head)
	if err != nil {
		return err
	}
	bump := *bumpOverride
	if bump == "" {
		bump = release.InferBump(messages, files)
	}
	previous, err := release.ReadVersion(*versionFile)
	if err != nil {
		return err
	}
	next, err := release.BumpVersion(previous, bump)
	if err != nil {
		return err
	}
	if *write {
		if err := release.WriteVersion(*versionFile, next); err != nil {
			return err
		}
		if err := release.WriteGoVersion(*goVersionFile, next); err != nil {
			return err
		}
	}
	values := map[string]string{
		"base": resolvedBase, "bump": bump, "previous_version": previous,
		"version": next, "tag": "v" + next,
	}
	if *githubOutput != "" {
		var builder strings.Builder
		for key, value := range values {
			builder.WriteString(key)
			builder.WriteString("=")
			builder.WriteString(value)
			builder.WriteString("\n")
		}
		file, err := os.OpenFile(*githubOutput, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		if _, err := file.WriteString(builder.String()); err != nil {
			file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
	}
	for _, key := range []string{"base", "bump", "previous_version", "version", "tag"} {
		fmt.Printf("%s=%s\n", key, values[key])
	}
	return nil
}

func renderFormula(args []string) error {
	fs := flag.NewFlagSet("render-formula", flag.ContinueOnError)
	formulaName := fs.String("formula-name", "codux", "formula name")
	url := fs.String("url", "", "source URL")
	sha256 := fs.String("sha256", "", "source sha256")
	output := fs.String("output", "", "output path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *url == "" || *sha256 == "" || *output == "" {
		return fmt.Errorf("--url, --sha256, and --output are required")
	}
	formula := release.RenderFormula(*formulaName, *url, *sha256)
	return os.WriteFile(*output, []byte(formula), 0o644)
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
