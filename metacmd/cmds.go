package metacmd

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/xo/usql/env"
	"github.com/xo/usql/text"
)

// Cmd is a command implementation.
type Cmd struct {
	Section Section
	Name    string
	Desc    string
	Min     int
	Aliases map[string]string
	Process func(*Params) error
}

// cmds is the set of commands.
var cmds []Cmd

// cmdMap is the map of commands and their aliases.
var cmdMap map[string]Metacmd

// sectMap is the map of sections to its respective commands.
var sectMap map[Section][]Metacmd

func init() {
	cmds = []Cmd{
		Question: {
			Section: SectionHelp,
			Name:    "?",
			Desc:    "show help on backslash commands,[commands]",
			Aliases: map[string]string{
				"?":  "show help on " + text.CommandName + " command-line options,options",
				"? ": "show help on special variables,variables",
			},
			Process: func(p *Params) error {
				Listing(p.Handler.IO().Stdout())
				return nil
			},
		},

		Quit: {
			Section: SectionGeneral,
			Name:    "q",
			Desc:    "quit " + text.CommandName,
			Aliases: map[string]string{"quit": ""},
			Process: func(p *Params) error {
				p.Result.Quit = true
				return nil
			},
		},

		Copyright: {
			Section: SectionGeneral,
			Name:    "copyright",
			Desc:    "show " + text.CommandName + " usage and distribution terms",
			Process: func(p *Params) error {
				out := p.Handler.IO().Stdout()
				fmt.Fprintln(out, text.Copyright)
				fmt.Fprintln(out)
				return nil
			},
		},

		ConnectionInfo: {
			Section: SectionConnection,
			Name:    "conninfo",
			Desc:    "display information about the current database connection",
			Process: func(p *Params) error {
				out := p.Handler.IO().Stdout()
				if db, u := p.Handler.DB(), p.Handler.URL(); db != nil && u != nil {
					fmt.Fprintf(out, text.ConnInfo, u.Driver, u.DSN)
					fmt.Fprintln(out)
				} else {
					fmt.Fprintln(out, text.NotConnected)
				}
				return nil
			},
		},

		Password: {
			Section: SectionConnection,
			Name:    "password",
			Desc:    "change the password for a user,[USERNAME]",
			Aliases: map[string]string{"passwd": ""},
			Process: func(p *Params) error {
				user, err := p.Handler.ChangePassword(p.Get())
				switch {
				case err == text.ErrPasswordNotSupportedByDriver || err == text.ErrNotConnected:
					return err
				case err != nil:
					return fmt.Errorf(text.PasswordChangeFailed, user, err)
				}

				/*fmt.Fprintf(p.Handler.IO().Stdout(), text.PasswordChangeSucceeded, user)
				fmt.Fprintln(p.Handler.IO().Stdout())*/

				return nil
			},
		},

		Print: {
			Section: SectionQueryBuffer,
			Name:    "p",
			Desc:    "show the contents of the query buffer",
			Aliases: map[string]string{
				"print": "",
				"raw":   "show the raw (non-interpolated) contents of the query buffer",
			},
			Process: func(p *Params) error {
				// get last statement
				var s string
				if p.Name == "raw" {
					s = p.Handler.LastRaw()
				} else {
					s = p.Handler.Last()
				}

				// use current statement buf if not empty
				buf := p.Handler.Buf()
				switch {
				case buf.Len != 0 && p.Name == "raw":
					s = buf.RawString()
				case buf.Len != 0:
					s = buf.String()
				}

				if s == "" {
					s = text.QueryBufferEmpty
				} else if p.Handler.IO().Interactive() && env.All()["SYNTAX_HL"] == "true" {
					b := new(bytes.Buffer)
					if p.Handler.Highlight(b, s) == nil {
						s = b.String()
					}
				}

				fmt.Fprintln(p.Handler.IO().Stdout(), s)
				return nil
			},
		},

		Reset: {
			Section: SectionQueryBuffer,
			Name:    "r",
			Desc:    "reset (clear) the query buffer",
			Aliases: map[string]string{"reset": ""},
			Process: func(p *Params) error {
				p.Handler.Reset(nil)
				fmt.Fprintln(p.Handler.IO().Stdout(), text.QueryBufferReset)
				return nil
			},
		},

		Transact: {
			Section: SectionTransaction,
			Name:    "begin",
			Desc:    "begin a transaction",
			Aliases: map[string]string{
				"commit":   "commit current transaction",
				"rollback": "rollback (abort) current transaction",
			},
			Process: func(p *Params) error {
				var f func() error
				switch p.Name {
				case "begin":
					f = p.Handler.Begin
				case "commit":
					f = p.Handler.Commit
				case "rollback":
					f = p.Handler.Rollback
				}
				return f()
			},
		},

		Prompt: {
			Section: SectionVariables,
			Name:    "prompt",
			Min:     1,
			Desc:    "prompt user to set variable,[-TYPE] [PROMPT] <VAR>",
			Process: func(p *Params) error {
				typ, n := p.GetOptional("string"), p.Get()
				if n == "" {
					return text.ErrMissingRequiredArgument
				}

				err := env.ValidIdentifier(n)
				if err != nil {
					return err
				}

				v, err := p.Handler.ReadVar(typ, strings.Join(p.GetAll(), " "))
				if err != nil {
					return err
				}

				return env.Set(n, v)
			},
		},

		SetVar: {
			Section: SectionVariables,
			Name:    "set",
			Desc:    "set internal variable, or list all if no parameters,[NAME [VALUE]]",
			Process: func(p *Params) error {
				if len(p.Params) == 0 {
					vars := env.All()
					out := p.Handler.IO().Stdout()
					n := make([]string, len(vars))
					var i int
					for k := range vars {
						n[i] = k
						i++
					}
					sort.Strings(n)

					for _, k := range n {
						fmt.Fprintln(out, k, "=", "'"+vars[k]+"'")
					}
					return nil
				}

				return env.Set(p.Get(), strings.Join(p.GetAll(), ""))
			},
		},

		Unset: {
			Section: SectionVariables,
			Name:    "unset",
			Min:     1,
			Desc:    "unset (delete) internal variable,NAME",
			Process: func(p *Params) error {
				return env.Unset(p.Get())
			},
		},

		SetFormatVar: {
			Section: SectionFormatting,
			Name:    "pset",
			Desc:    "set table output option,[NAME [VALUE]]",
			Aliases: map[string]string{
				"a": "toggle between unaligned and aligned output mode",
				"C": "set table title, or unset if none,[STRING]",
				"f": "show or set field separator for unaligned query output,[STRING]",
				"H": "toggle HTML output mode",
				"T": "set HTML <table> tag attributes, or unset if none,[STRING]",
				"t": "show only rows,[on|off]",
				"x": "toggle expanded output,[on|off|auto]",
			},
			Process: func(p *Params) error {
				out, l := p.Handler.IO().Stdout(), len(p.Params)

				// display variables
				if p.Name == "pset" && l == 0 {
					vars := env.Pall()
					n := make([]string, len(vars))
					var i, w int
					for k := range vars {
						n[i], w = k, max(len(k), w)
						i++
					}
					sort.Strings(n)

					for _, k := range n {
						v := vars[k]
						switch k {
						case "fieldsep", "recordsep", "null":
							v = strconv.QuoteToASCII(v)

						case "tableattr", "title":
							if v != "" {
								v = strconv.QuoteToASCII(v)
							}
						}
						fmt.Fprintln(out, k+strings.Repeat(" ", w-len(k)), v)
					}
					return nil
				}

				var field, extra string
				switch p.Name {
				case "pset":
					field = p.Get()
					l--
				case "a":
					field = "format"
				case "C":
					field = "title"
				case "f":
					field = "fieldsep"
				case "H":
					field, extra = "format", "html"
				case "T":
					field = "tableattr"
				case "t":
					field = "tuples_only"
				case "x":
					field = "expanded"
				}

				v, err := env.Pget(field)
				if err != nil {
					return err
				}

				switch {
				case l == 0:
					if v, err = env.Ptoggle(field, extra); err != nil {
						return err
					}
				case l >= 1:
					v, err = env.Pset(field, p.Get())
					if err != nil {
						return err
					}
				}

				// special replacement name for expanded field, when 'auto'
				if field == "expanded" && v == "auto" {
					field = "expanded_auto"
				}

				// format output
				mask := text.FormatFieldNameSetMap[field]
				unsetMask := text.FormatFieldNameUnsetMap[field]
				switch {
				case strings.Contains(mask, "%d"):
					i, _ := strconv.Atoi(v)
					fmt.Fprintf(out, mask, i)
				case unsetMask != "" && v == "":
					fmt.Fprint(out, unsetMask)
				case !strings.Contains(mask, "%"):
					fmt.Fprint(out, mask)
				default:
					fmt.Fprintf(out, mask, v)
				}
				fmt.Fprintln(out)
				return nil
			},
		},
	}

	// set up map
	cmdMap = make(map[string]Metacmd, len(cmds))
	sectMap = make(map[Section][]Metacmd, len(SectionOrder))
	for i, c := range cmds {
		mc := Metacmd(i)
		if mc == None {
			continue
		}

		cmdMap[c.Name] = mc
		for alias := range c.Aliases {
			cmdMap[alias] = mc
		}

		sectMap[c.Section] = append(sectMap[c.Section], mc)
	}
}
