package wk

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Version and Commit are set at build time (GoReleaser, Nix); a plain go build reports dev.
var Version = "dev"
var Commit = ""

const help = `wk — проекты и вика
  init [--url ORIGIN] [--profile ID] [--template ID] [--existing | --keep] [--dir DIR] [--force] [--offline]
  doctor [--json]
  version
  component add NAME [--lang LANG-id] [--kind svc|lib] [--with-ai]
  check [--strict] [--json]
  docs check [--docs DIR] [--code DIR] [--strict] [--no-registry] | upgrade
  policy sync | check | search QUERY | show RULE-ID
  template list | show ID
  propose --file PAGE.md [--title TITLE] [--kind manual]
  hook check | guard-block | guard-paths | guard-shell | policy-stale (JSON события на stdin)
Все команды поддерживают --json. URL для init: --url или WIKI_URL.
`

type App struct {
	Root         string
	Token        string
	WikiURL      string
	Cache        string
	Source       string
	HTTP         *http.Client
	Stdin        io.Reader
	docs         fs.FS
	integrations fs.FS
	writeFile    func(string, string, []byte) error
	readCache    func(string) ([]os.DirEntry, error)
	lookPath     func(string) (string, error)
	writable     func(string) bool
}

type result struct {
	Data   any
	Text   string
	Code   int
	Stderr string
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, "wk: рабочий каталог недоступен")
		return 1
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		cache = filepath.Join(root, ".cache")
	}
	source := os.Getenv("WK_FLAKE")
	if source == "" {
		source = defaultFlake
	}
	a := App{Root: root, Token: os.Getenv("WIKI_TOKEN"), WikiURL: os.Getenv("WIKI_URL"), Cache: filepath.Join(cache, "wk", "templates"), Source: source, Stdin: os.Stdin}
	return a.Run(ctx, args, stdout, stderr)
}

func redact(s, token string) string {
	if token == "" {
		return s
	}
	s = strings.ReplaceAll(s, token, "[REDACTED]")
	escaped, _ := json.Marshal(token)
	return strings.ReplaceAll(s, string(escaped[1:len(escaped)-1]), "[REDACTED]")
}

func (a App) Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	jsonMode := false
	clean := make([]string, 0, len(args))
	for i, arg := range args {
		if arg == "--" {
			clean = append(clean, args[i:]...)
			break
		}
		if arg == "--json" {
			jsonMode = true
		} else {
			clean = append(clean, arg)
		}
	}
	var r result
	var err error
	if len(clean) > 0 && clean[0] == "hook" {
		// Hook stdout is an event protocol, even when --json was passed.
		jsonMode = false
		r, err = a.hook(ctx, clean[1:])
	} else {
		r, err = a.execute(ctx, clean)
	}
	if err != nil {
		message := redact(err.Error(), a.Token)
		fmt.Fprintln(stderr, "wk:", message)
		if jsonMode {
			data, _ := json.Marshal(map[string]string{"error": message})
			fmt.Fprintln(stdout, string(data))
		}
		var coded exitError
		if errors.As(err, &coded) {
			return coded.code
		}
		return 1
	}
	if r.Stderr != "" {
		fmt.Fprintln(stderr, redact(r.Stderr, a.Token))
	}
	var output string
	if jsonMode {
		data, _ := json.Marshal(r.Data)
		output = string(data) + "\n"
	} else {
		output = r.Text
		if output != "" && !strings.HasSuffix(output, "\n") {
			output += "\n"
		}
	}
	if _, err := io.WriteString(stdout, redact(output, a.Token)); err != nil {
		fmt.Fprintln(stderr, "wk: не удалось записать stdout")
		return 1
	}
	return r.Code
}

func flags(name string) *flag.FlagSet {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	return f
}

// The standard flag package stops at a positional argument. Move only declared
// flags ahead of positional values, preserving -- and flag values verbatim.
func parseFlags(f *flag.FlagSet, args []string) error {
	var opts, rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			rest = append(rest, args[i+1:]...)
			break
		}
		if len(arg) < 2 || arg[0] != '-' {
			rest = append(rest, arg)
			continue
		}
		name := strings.TrimLeft(strings.SplitN(arg, "=", 2)[0], "-")
		decl := f.Lookup(name)
		if decl == nil {
			return errors.New("неизвестный флаг; см. wk --help")
		}
		opts = append(opts, arg)
		if !strings.Contains(arg, "=") {
			b, ok := decl.Value.(interface{ IsBoolFlag() bool })
			if !ok || !b.IsBoolFlag() {
				i++
				if i == len(args) {
					return errors.New("флаг требует значения")
				}
				opts = append(opts, args[i])
			}
		}
	}
	opts = append(opts, "--")
	opts = append(opts, rest...)
	if err := f.Parse(opts); err != nil {
		return errors.New("неверное значение флага")
	}
	return nil
}

func (a App) execute(ctx context.Context, args []string) (result, error) {
	if len(args) == 0 || (len(args) == 1 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h")) {
		return result{Data: map[string]string{"help": help}, Text: help}, nil
	}
	if len(args) == 1 && (args[0] == "version" || args[0] == "--version") {
		text := "wk " + Version
		if Commit != "" {
			text += " (" + Commit + ")"
		}
		return result{Data: map[string]string{"version": Version, "commit": Commit}, Text: text}, nil
	}
	if args[0] == "init" {
		return a.init(ctx, args[1:])
	}
	// Diagnose a broken manifest as an ordered check result.
	if args[0] == "doctor" {
		if len(args) != 1 {
			return result{}, errors.New("doctor не принимает аргументы")
		}
		return a.doctor(ctx), nil
	}
	switch args[0] {
	case "component", "check", "docs", "policy", "template", "propose":
	default:
		return result{}, exitError{2, errors.New("неизвестная команда; см. wk --help")}
	}
	m, err := loadManifest(a.Root)
	if err != nil {
		if (args[0] == "docs" || args[0] == "check") && errors.Is(err, os.ErrNotExist) {
			return result{}, exitError{3, err}
		}
		return result{}, err
	}
	c := Client{Origin: m.Wiki.URL, Token: a.Token, HTTP: a.HTTP}
	switch args[0] {
	case "docs":
		return a.docsCommand(m, args[1:])
	case "component":
		if len(args) < 2 || args[1] != "add" {
			return result{}, errors.New("используй component add NAME")
		}
		f := flags("component add")
		lang := f.String("lang", "", "language")
		kind := f.String("kind", "svc", "kind")
		ai := f.Bool("with-ai", false, "AI documentation")
		if err := parseFlags(f, args[2:]); err != nil {
			return result{}, err
		}
		if f.NArg() != 1 {
			return result{}, errors.New("component add требует имя")
		}
		if err := addComponent(a.Root, m, Component{Name: f.Arg(0), Kind: *kind, Lang: *lang}, *ai); err != nil {
			return result{}, err
		}
		return result{Data: map[string]string{"component": f.Arg(0)}, Text: "Создан компонент " + f.Arg(0)}, nil
	case "check":
		return a.check(ctx, c, m, args[1:])
	case "policy":
		if len(args) == 2 && (args[1] == "sync" || args[1] == "check") {
			return a.policies(ctx, c, m, args[1] == "sync")
		}
		if len(args) != 3 {
			return result{}, errors.New("используй policy search QUERY или policy show RULE-ID")
		}
		path := "/api/v1/rules/related?q=" + url.QueryEscape(args[2])
		if args[1] == "show" {
			if !projectID.MatchString(args[2]) {
				return result{}, errors.New("невалидный ID политики")
			}
			path = "/api/v1/rules/" + args[2]
		} else if args[1] != "search" {
			return result{}, errors.New("неизвестная команда policy")
		}
		var data any
		if err := c.request(ctx, "GET", path, nil, &data); err != nil {
			return result{}, err
		}
		return result{Data: data, Text: pretty(data)}, nil
	case "template":
		return a.templates(ctx, c, args[1:])
	default:
		return a.propose(ctx, c, args[1:])
	}
}

func pretty(data any) string { b, _ := json.MarshalIndent(data, "", "  "); return string(b) }

func (a App) write(root, name string, data []byte) error {
	if a.writeFile != nil {
		return a.writeFile(root, name, data)
	}
	return atomicWrite(root, name, data)
}
