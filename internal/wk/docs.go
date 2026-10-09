package wk

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"wk-cli/internal/docscheck"
)

var resolveDocsPath = filepath.EvalSymlinks

func readDocs(root string) ([]docscheck.File, error) {
	var files []docscheck.File
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if !docscheck.IsDoc(rel) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !utf8.Valid(data) {
			return fmt.Errorf("%s: неверная кодировка UTF-8", rel)
		}
		files = append(files, docscheck.File{Path: rel, Text: string(data)})
		return nil
	})
	return files, err
}

// os.walk visits files before subdirectories, in filesystem order. Retain that
// order because the Python oracle exposes it in marker locations and warnings.
func readCode(root, docs string) []docscheck.File {
	var files []docscheck.File
	var walk func(string)
	walk = func(dir string) {
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil || resolved == docs {
			return
		}
		f, err := os.Open(dir)
		if err != nil {
			return
		}
		entries, err := f.ReadDir(-1)
		f.Close()
		if err != nil {
			return
		}
		var dirs []string
		for _, e := range entries {
			path := filepath.Join(dir, e.Name())
			if e.IsDir() {
				if !slices.Contains(docscheck.SkipCodeDirs, e.Name()) {
					dirs = append(dirs, path)
				}
				continue
			}
			info, err := os.Stat(path)
			if err != nil || !info.Mode().IsRegular() || info.Size() > 2000000 {
				continue
			}
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			rel, _ := filepath.Rel(root, path)
			files = append(files, docscheck.File{Path: filepath.ToSlash(rel), Text: string(data)})
		}
		for _, dir := range dirs {
			walk(dir)
		}
	}
	walk(root)
	return files
}
func (a App) docsReport(m Manifest, docs, code string, registry bool) (docscheck.Report, error) {
	var empty docscheck.Report
	if !filepath.IsAbs(docs) {
		docs = filepath.Join(a.Root, docs)
	}
	if !filepath.IsAbs(code) {
		code = filepath.Join(a.Root, code)
	}
	info, err := os.Stat(docs)
	if errors.Is(err, os.ErrNotExist) || err == nil && !info.IsDir() {
		return empty, exitError{3, errors.New("не найден docs/")}
	}
	if err != nil {
		return empty, err
	}
	resolved, err := resolveDocsPath(docs)
	if err != nil {
		return empty, err
	}
	files, err := readDocs(docs)
	if err != nil {
		return empty, err
	}
	policies, err := localPolicies(a.Root, m)
	if err != nil {
		return empty, err
	}
	rep := docscheck.Check(files, readCode(code, resolved), policies)
	if registry {
		if err := a.write(docs, "_meta/registry.md", []byte(rep.Registry)); err != nil {
			return rep, err
		}
	}
	return rep, nil
}
func (a App) docsCommand(m Manifest, args []string) (result, error) {
	if len(args) == 0 {
		return result{}, exitError{2, errors.New("используй docs check или docs upgrade")}
	}
	if args[0] == "upgrade" {
		if len(args) != 1 {
			return result{}, exitError{2, errors.New("docs upgrade не принимает аргументы")}
		}
		path, err := safePath(a.Root, m.Docs.Root+"/_meta/check.py")
		if err != nil {
			return result{}, err
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return result{}, err
		}
		return result{Data: map[string]string{"message": "проверка теперь встроена в wk"}, Text: "проверка теперь встроена в wk"}, nil
	}
	if args[0] != "check" {
		return result{}, exitError{2, errors.New("неизвестная команда docs")}
	}
	f := flags("docs check")
	docs := f.String("docs", m.Docs.Root, "")
	code := f.String("code", ".", "")
	strict := f.Bool("strict", false, "")
	noRegistry := f.Bool("no-registry", false, "")
	if err := parseFlags(f, args[1:]); err != nil {
		return result{}, exitError{2, err}
	}
	if f.NArg() != 0 {
		return result{}, exitError{2, errors.New("docs check не принимает позиционные аргументы")}
	}
	rep, err := a.docsReport(m, *docs, *code, !*noRegistry)
	if err != nil {
		return result{}, err
	}
	return result{Data: rep, Text: rep.Text(), Code: rep.Code(*strict)}, nil
}
func (a App) check(ctx context.Context, c Client, m Manifest, args []string) (result, error) {
	f := flags("check")
	strict := f.Bool("strict", false, "")
	if err := parseFlags(f, args); err != nil {
		return result{}, exitError{2, err}
	}
	if f.NArg() != 0 {
		return result{}, exitError{2, errors.New("check не принимает аргументы")}
	}
	rep, err := a.docsReport(m, m.Docs.Root, ".", true)
	if err != nil {
		return result{}, err
	}
	pol, err := a.policies(ctx, c, m, false)
	if err != nil {
		code := 1
		var unavailable exitError
		if rep.Code(*strict) == 0 && errors.As(err, &unavailable) && unavailable.code == 3 {
			code = 3
		}
		return result{Data: map[string]any{"docs": rep, "policies": map[string]string{"error": err.Error()}}, Text: rep.Text(), Stderr: "wk: политики: " + err.Error(), Code: code}, nil
	}
	return result{Data: map[string]any{"docs": rep, "policies": pol.Data}, Text: rep.Text() + strings.TrimSpace(pol.Text) + "\n", Code: max(rep.Code(*strict), pol.Code)}, nil
}
