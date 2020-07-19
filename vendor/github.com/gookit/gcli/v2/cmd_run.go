package gcli

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"strings"

	"github.com/gookit/color"
	"github.com/gookit/gcli/v2/helper"
	"github.com/gookit/goutil/strutil"
)

/*************************************************************
 * command run
 *************************************************************/

// do parse option flags, remaining is cmd args
func (c *Command) parseFlags(args []string) (error, []string) {
	// strict format options
	if gOpts.strictMode && len(args) > 0 {
		args = strictFormatArgs(args)
	}

	// fix and compatible
	args = moveArgumentsToEnd(args)

	Logf(VerbDebug, "[Cmd.parseFlags] flags on after format: %v", args)

	// disable output internal error message on parse flags
	c.Flags.SetOutput(ioutil.Discard)

	// parse options, don't contains command name.
	if err := c.Flags.Parse(args); err != nil {
		return err, []string{}
	}

	return nil, c.Flags.Args()
}

// prepare: before execute the command
func (c *Command) prepare(args []string) (status int, err error) {
	return
}

// do execute the command
func (c *Command) execute(args []string) (err error) {
	// collect named args
	if err := c.collectNamedArgs(args); err != nil {
		return err
	}

	c.Fire(EvtBefore, args)

	// call command handler func
	if c.Func == nil {
		Logf(VerbWarn, "[Command.Execute] the command '%s' no handler func to running.", c.Name)
	} else {
		// err := c.Func.Run(c, args)
		err = c.Func(c, args)
	}

	if err != nil {
		// if run in application, report error to app.
		if !c.alone {
			c.app.AddError(err)
		}

		c.Fire(EvtError, err)
	} else {
		c.Fire(EvtAfter, nil)
	}
	return
}

func (c *Command) collectNamedArgs(inArgs []string) error {
	var num int
	inNum := len(inArgs)

	for i, arg := range c.args {
		// num is equals to "index + 1"
		num = i + 1
		if num > inNum { // no enough arg
			if arg.Required {
				return fmt.Errorf("must set value for the argument: %s (position %d)", arg.ShowName, arg.index)
			}
			break
		}

		if arg.IsArray {
			arg.Value = inArgs[i:]
			inNum = num // must reset inNum
		} else {
			arg.Value = inArgs[i]
		}
	}

	if !c.alone && gOpts.strictMode && inNum > num {
		return fmt.Errorf("entered too many arguments: %v", inArgs[num:])
	}
	return nil
}

// Fire event handler by name
func (c *Command) Fire(event string, data interface{}) {
	Logf(VerbDebug, "[Cmd.Fire] command '%s' trigger the event: %s", c.Name, event)

	c.SimpleHooks.Fire(event, c, data)
}

// On add hook handler for a hook event
func (c *Command) On(name string, handler HookFunc) {
	Logf(VerbDebug, "[Cmd.On] command '%s' add hook: %s", c.Name, name)

	c.SimpleHooks.On(name, handler)
}

// Copy a new command for current
func (c *Command) Copy() *Command {
	nc := *c
	// reset some fields
	nc.Func = nil
	nc.SimpleHooks.ClearHooks()
	// nc.Flags = flag.FlagSet{}

	return &nc
}

/*************************************************************
 * alone running
 *************************************************************/

var errCallRun = errors.New("this method can only be called in standalone mode")

// MustRun Alone the current command, will panic on error
func (c *Command) MustRun(inArgs []string) {
	if err := c.Run(inArgs); err != nil {
		panicf("Run command error: %s", err.Error())
	}
}

// Run Alone the current command
func (c *Command) Run(inArgs []string) (err error) {
	// - Running in application.
	if c.app != nil {
		return errCallRun
	}

	// - Alone running command

	// init the command
	c.initialize()

	// check input args
	if len(inArgs) == 0 {
		inArgs = os.Args[1:]
	}

	// if Command.CustomFlags=true, will not run Flags.Parse()
	if !c.CustomFlags {
		// contains keywords "-h" OR "--help" on end
		if CLI.hasHelpKeywords() {
			c.ShowHelp()
			return
		}

		// if CustomFlags=true, will not run Flags.Parse()
		err, inArgs = c.parseFlags(inArgs)
		if err != nil {
			return
		}
	}

	return c.execute(inArgs)
}

/*************************************************************
 * display cmd help
 *************************************************************/

// help template for a command
var commandHelp = `{{.UseFor}}
{{if .Cmd.NotAlone}}
<comment>Name:</> {{.Cmd.Name}}{{if .Cmd.Aliases}} (alias: <info>{{.Cmd.AliasesString}}</>){{end}}{{end}}
<comment>Usage:</> {$binName} [Global Options...] {{if .Cmd.NotAlone}}<info>{{.Cmd.Name}}</> {{end}}[--option ...] [argument ...]

<comment>Global Options:</>
      <info>--verbose</>     Set error reporting level(quiet 0 - 4 debug)
      <info>--no-color</>    Disable color when outputting message
  <info>-h, --help</>        Display this help information{{if .Options}}

<comment>Options:</>
{{.Options}}{{end}}{{if .Cmd.Args}}

<comment>Arguments:</>{{range $a := .Cmd.Args}}
  <info>{{$a.Name | printf "%-12s"}}</>{{$a.Description | ucFirst}}{{if $a.Required}}<red>*</>{{end}}{{end}}
{{end}} {{if .Cmd.Examples}}
<comment>Examples:</>
{{.Cmd.Examples}}{{end}}{{if .Cmd.Help}}
<comment>Help:</>
{{.Cmd.Help}}{{end}}`

// ShowHelp show command help info
func (c *Command) ShowHelp(quit ...bool) {
	// commandHelp = color.ReplaceTag(commandHelp)
	// clear space and empty new line
	if c.Examples != "" {
		c.Examples = strings.Join([]string{"  ", strings.TrimSpace(c.Examples), "\n"}, "")
	}

	// clear space and empty new line
	if c.Help != "" {
		c.Help = strings.Join([]string{strings.TrimSpace(c.Help), "\n"}, "")
	}

	// render and output help info
	// RenderTplStr(os.Stdout, commandHelp, map[string]interface{}{
	// render but not output
	s := helper.RenderText(commandHelp, map[string]interface{}{
		"Cmd": c,
		// parse options to string
		"Options": c.ParseDefaults(),
		// always upper first char
		"UseFor": c.UseFor,
	}, nil)

	// parse help vars then print help
	color.Print(c.ReplaceVars(s))
	if len(quit) > 0 && quit[0] {
		Exit(OK)
	}
}

// ParseDefaults prints, to standard error unless configured otherwise, the
// default values of all defined command-line flags in the set. See the
// documentation for the global function PrintDefaults for more information.
//
// NOTICE: the func is copied from package 'flag', func 'PrintDefaults'
func (c *Command) ParseDefaults() string {
	var s string
	var ss []string

	c.Flags.VisitAll(func(fg *flag.Flag) {
		// is long option
		if len(fg.Name) > 1 {
			// find shortcut name
			if sn := c.ShortName(fg.Name); sn != "" {
				s = fmt.Sprintf("  <info>-%s, --%s</>", sn, fg.Name)
			} else {
				s = fmt.Sprintf("      <info>--%s</>", fg.Name)
			}
		} else {
			// is short option, skip it
			if c.isShortcut(fg.Name) {
				return
			}

			s = fmt.Sprintf("  <info>-%s</>", fg.Name)
		}

		name, usage := flag.UnquoteUsage(fg)
		// option value type
		if len(name) > 0 {
			s += fmt.Sprintf(" <magenta>%s</>", name)
		}
		// Boolean flags of one ASCII letter are so common we
		// treat them specially, putting their usage on the same line.
		if len(s) <= 4 { // space, space, '-', 'x'.
			s += "\t"
		} else {
			// Four spaces before the tab triggers good alignment
			// for both 4- and 8-space tab stops.
			s += "\n    \t"
		}
		s += strings.Replace(strutil.UpperFirst(usage), "\n", "\n    \t", -1)

		if !isZeroValue(fg, fg.DefValue) {
			if _, ok := fg.Value.(*stringValue); ok {
				// put quotes on the value
				s += fmt.Sprintf(" (default <cyan>%q</>)", fg.DefValue)
			} else {
				s += fmt.Sprintf(" (default <cyan>%v</>)", fg.DefValue)
			}
		}

		ss = append(ss, s)
	})

	return strings.Join(ss, "\n")
}

// isZeroValue guesses whether the string represents the zero
// value for a flag. It is not accurate but in practice works OK.
//
// NOTICE: the func is copied from package 'flag', func 'isZeroValue'
func isZeroValue(fg *flag.Flag, value string) bool {
	// Build a zero value of the flag's Value type, and see if the
	// result of calling its String method equals the value passed in.
	// This works unless the Value type is itself an interface type.
	typ := reflect.TypeOf(fg.Value)
	var z reflect.Value
	if typ.Kind() == reflect.Ptr {
		z = reflect.New(typ.Elem())
	} else {
		z = reflect.Zero(typ)
	}
	if value == z.Interface().(flag.Value).String() {
		return true
	}

	switch value {
	case "false", "", "0":
		return true
	}
	return false
}

// -- string Value
// NOTICE: the var is copied from package 'flag'
type stringValue string

func (s *stringValue) Set(val string) error {
	*s = stringValue(val)
	return nil
}
func (s *stringValue) Get() interface{} { return string(*s) }
func (s *stringValue) String() string   { return string(*s) }
