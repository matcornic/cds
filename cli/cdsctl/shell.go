package main

import (
	"bytes"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/chzyer/readline"
	"github.com/pkg/browser"
	"github.com/spf13/cobra"

	"github.com/ovh/cds/cli"
	"github.com/ovh/cds/sdk"
)

var shellCmd = cli.Command{
	Name:  "shell",
	Short: "cdsctl interactive shell",
	Long: `
CDS Shell Mode. Keywords:

- cd: reset current object. running "ls" after "cd" will display Projects List
- cd <KEY>: go to an object, try to run "ls" after a cd <KEY>
- help: display this help
- ls: display current list
- ls <KEY>: display current object, ls MY_PRJ is the same as cdsctl project show MY_PRJ
- mode: display current mode. Choose mode with "mode vi" ou "mode emacs"
- open: open CDS WebUI with current context
- run: run current workflow
- version: same as cdsctl version command
`,
}

var current *shellCurrent

type shellCurrent struct {
	path  string
	rline *readline.Instance
}

func shellRun(v cli.Values) error {
	shellASCII()
	version, err := client.Version()
	if err != nil {
		return err
	}
	fmt.Printf("Connected. cdsctl version: %s connected to CDS API version:%s \n\n", sdk.VERSION, version.Version)
	fmt.Println("enter `exit` quit")

	// enable shell mode, this will prevent to os.Exit if there is an error on a command
	cli.ShellMode = true

	l, err := readline.NewEx(&readline.Config{
		Prompt:            "\033[31m»\033[0m ",
		HistoryFile:       path.Join(userHomeDir(), ".cdsctl_history"),
		AutoComplete:      getCompleter(),
		InterruptPrompt:   "^C",
		EOFPrompt:         "exit",
		HistorySearchFold: true,
	})

	if err != nil {
		fmt.Printf("Error: %s\n", err)
	}

	defer l.Close()

	current = &shellCurrent{rline: l}

	for {
		l.SetPrompt(fmt.Sprintf("%s \033[31m»\033[0m ", current.pwd()))
		line, err := l.Readline()
		if err == readline.ErrInterrupt {
			if len(line) == 0 {
				break
			} else {
				continue
			}
		} else if err == io.EOF {
			break
		}

		line = strings.TrimSpace(line)

		if line == "exit" || line == "quit" {
			break
		}
		if len(line) > 0 {
			current.shellProcessCommand(line)
		}
	}
	return nil
}

func getCompleter() *readline.PrefixCompleter {
	return readline.NewPrefixCompleter(
		readline.PcItem("mode",
			readline.PcItem("vi"),
			readline.PcItem("emacs"),
		),
		readline.PcItem("help"),
		readline.PcItem("cd",
			readline.PcItemDynamic(listCurrent()),
		),
		readline.PcItem("ls",
			readline.PcItemDynamic(listCurrent()),
		),
		readline.PcItem("open"),
		readline.PcItem("pwd"),
		readline.PcItem("version"),
		readline.PcItem("run"),
		readline.PcItem("exit"),
	)
}

func listCurrent() func(string) []string {
	return func(line string) []string {
		output, submenus, cmds := current.shellListCommand(current.path, nil)
		// delete empty values from list and return the list of string
		out := append(output, submenus...)
		return sdk.DeleteEmptyValueFromArray(append(out, cmds...))
	}
}

type shellCommandFunc func(current *shellCurrent, args []string)

func getShellCommands() map[string]shellCommandFunc {
	m := map[string]shellCommandFunc{
		"mode": func(current *shellCurrent, args []string) {
			if len(args) == 0 {
				if current.rline.IsVimMode() {
					println("current mode: vim")
				} else {
					println("current mode: emacs")
				}
			} else {
				switch args[0] {
				case "vi":
					current.rline.SetVimMode(true)
				case "emacs":
					current.rline.SetVimMode(false)
				default:
					fmt.Println("invalid mode:", args[0])
				}
			}
		},
		"help": func(current *shellCurrent, args []string) {
			fmt.Println(shellCmd.Long)
		},
		"cd": func(current *shellCurrent, args []string) {
			if len(args) == 0 {
				current.path = ""
				return
			}

			if args[0] == ".." {
				idx := strings.LastIndex(current.path, "/")
				current.path = current.path[:idx]
				return
			}

			// path must start with / and end without /
			if strings.HasPrefix(args[0], "/") { // absolute cd /...
				current.path = args[0]
			} else { // relative cd foo...
				current.path += "/" + args[0]
			}
			current.path = strings.TrimSuffix(current.path, "/")
		},
		"open": func(current *shellCurrent, args []string) {
			current.openBrowser()
		},
		"ls": func(current *shellCurrent, args []string) {
			inargs := args
			path := current.path
			if len(args) == 0 { // ls -> no path
				// default values
			} else {
				if strings.HasPrefix(args[0], "/") { // ls /foo -> absolute path
					path = args[0]
					inargs = args[1:]
				} else if strings.HasPrefix(args[0], "-") { // ls foo -> relative path
					// default values
				} else { // ls foo -> relative path
					path = current.path + args[0]
					inargs = args[1:]
				}
			}

			output, submenus, cmds := current.shellListCommand(path, inargs)
			for _, s := range output {
				if len(strings.TrimSpace(s)) > 0 {
					fmt.Println(s)
				}
			}
			if len(submenus) > 0 || len(cmds) > 0 {
				fmt.Println() // empty line between list data and sub-menus/commands list
			}
			if len(submenus) > 0 {
				fmt.Printf("\033[32m»\033[0m sub-menu: %s\n", strings.Join(submenus, " - "))
			}

			if len(cmds) > 0 {
				fmt.Printf("\033[32m»\033[0m additional commands: %s\n", strings.Join(cmds, " - "))
			}
		},
		"pwd": func(current *shellCurrent, args []string) {
			fmt.Println(current.pwd())
		},
		"version": func(current *shellCurrent, args []string) {
			versionRun(nil)
		},
	}
	return m
}

func (current *shellCurrent) pwd() string {
	if current.path == "" {
		return "/"
	}
	return current.path
}

func (current *shellCurrent) shellProcessCommand(input string) {
	tuple := strings.Split(input, " ")
	if f, ok := getShellCommands()[tuple[0]]; ok {
		if f == nil {
			fmt.Printf("Command %s not defined in this context\n", input)
			return
		}
		f(current, tuple[1:])
	}
}

func (current *shellCurrent) shellListCommand(path string, flags []string) ([]string, []string, []string) {
	spath := strings.Split(path, "/")
	cmd := getRoot(true)
	for index := 1; index < len(spath); index++ {
		key := spath[index]
		if f := findCommand(cmd, key); f != nil {
			cmd = f
		}
	}
	if cmd.Name() == "" {
		return []string{"root cmd NOT found"}, nil, nil
	}

	buf := new(bytes.Buffer)
	if cmd.Name() == spath[len(spath)-1] { // list command
		if lsCmd := findCommand(cmd, "list"); lsCmd != nil {
			if len(flags) == 0 {
				flags = []string{"-q"}
			}
			lsCmd.ParseFlags(flags)
			lsCmd.SetOutput(buf)
			lsCmd.Run(lsCmd, current.getArgs(lsCmd))
		}
	} else { // try show command
		if showCmd := findCommand(cmd, "show"); showCmd != nil {
			showCmd.ParseFlags(flags)
			showCmd.SetOutput(buf)
			showCmd.Run(showCmd, current.getArgs(showCmd))
		}
	}
	out := strings.Split(buf.String(), "\n")

	// compute list sub-menus and commands
	var submenus, cmds []string
	for _, c := range cmd.Commands() {
		// list only command with sub commands
		if len(c.Commands()) > 0 && current.isCtxOK(c) {
			submenus = append(submenus, c.Name())
		} else if c.Name() != "list" && c.Name() != "show" { // list and show are the "ls" cmd
			cmds = append(cmds, c.Name())
		}
	}

	return out, submenus, cmds
}

func (current *shellCurrent) isCtxOK(cmd *cobra.Command) bool {
	if a, withContext := current.extractArg(cmd, _ProjectKey); withContext && a == "" {
		return false
	}
	if a, withContext := current.extractArg(cmd, _ApplicationName); withContext && a == "" {
		return false
	}
	if a, withContext := current.extractArg(cmd, _WorkflowName); withContext && a == "" {
		return false
	}
	return true
}

// key: _ProjectKey, _ApplicationName, _WorkflowName
// pos: position to extract
func (current *shellCurrent) extractArg(cmd *cobra.Command, key string) (string, bool) {
	var inpath string
	switch key {
	case _ApplicationName:
		inpath = "application"
	case _WorkflowName:
		inpath = "workflow"
	}
	var cmdWithContext bool
	if strings.Contains(cmd.Use, strings.ToUpper(key)) {
		cmdWithContext = true
		if strings.HasPrefix(current.path, "/project/") {
			t := strings.Split(current.path, "/")
			if inpath == "" {
				return t[2], cmdWithContext
			} else if inpath != "" && len(t) >= 5 && t[3] == inpath {
				return t[4], cmdWithContext
			}
		}
	}
	return "", cmdWithContext
}

func (current *shellCurrent) getArgs(cmd *cobra.Command) []string {
	args := []string{}
	if a, _ := current.extractArg(cmd, _ProjectKey); a != "" {
		args = append(args, a)
	}
	if a, _ := current.extractArg(cmd, _ApplicationName); a != "" {
		args = append(args, a)
	}
	if a, _ := current.extractArg(cmd, _WorkflowName); a != "" {
		args = append(args, a)
	}
	return args
}

func findCommand(cmd *cobra.Command, key string) *cobra.Command {
	for _, c := range cmd.Commands() {
		if c.Name() == key {
			return c
		}
	}
	return nil
}

func (current *shellCurrent) openBrowser() {
	var baseURL string
	configUser, err := client.ConfigUser()
	if err != nil {
		fmt.Printf("Error while getting URL UI: %s", err)
		return
	}

	if b, ok := configUser[sdk.ConfigURLUIKey]; ok {
		baseURL = b
	}

	if baseURL == "" {
		fmt.Println("Unable to retrieve webui uri")
		return
	}

	browser.OpenURL(baseURL + current.path)
}

func shellASCII() {
	fmt.Printf(`

               .';:looddddolc;'.               .,::::::::::::::::;;;,'..           .............................
            'cdOKKXXXXXXXXXXXXKOd:.            'OXXXXXXXXXXXXXXXXXXXKK0Oxo:...',;;::ccccccccccccccccccccccccccc;.
         .:x0XXXX0OxollllodxOKXXXXOl.          'OXXXX0OOOOOOOOOO0000KXXXXX0dccccccccccccccccccccccccccccccccccc;.
       .;kKXXX0d:..         .,lOKXXXOc.        'OXXX0c..............';cdOKKkdddl;,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,.
      .oKXXX0l.                .l0XXXKo.       'OXXX0;                  .cOKKKKO:.
     .dKXXXk,                    :0XXXKl.      'OXXX0;                    .dKXXX0:
    .lKXXXO,                      :xdoc,       'OXXX0;                     ,kOOOOx:,,,,,,,''..
    ;OXXXKc                                    'OXXX0;                    .cxxxxxxxxxxxxxxxxdoc.
   .lKXXXk'                                    'OXXX0;                     'oxxxxxxxxxxxxxxxxxx:
   .xXXXXo.                                    'OXXX0;                      .:kOOOko:;;;;;;;;;;.
   'kXXXKl                                     'OXXX0;                       ,OXXXK:
   'kXXXKl                                     'OXXX0;                       ,OXXXK:
   .xXXXXo.                                    'OXXX0;                       ;0XXX0;       .;;;;;;;;;;;;;;;;,'.
    lKXXXx.                                    'OXXX0;                       lKXXXk'      .cxxxxxxxxxxxxxxxxxdl'
    ,OXXX0:                        ;c:,..      'OXXX0;                      .xXXXKo.       'cdxxxxxxxxxxxxxxxxxc.
    .lKXXXx.                      ,OXXX0c      'OXXX0;                     .lKXXXO,          ..',,,;;;;;;;,;;;,.
     .xXXXKd.                    'kXXXXx.      'OXXX0;                    .l0XXX0c
      'xKXXKk;.                .:OXXXKx'       'OXXX0;                   ,dKXXX0c
       .o0XXXKxc'.           .:xKXXXKd.        'OXXX0:             ...,cx0K0OOOxc:;;;;;;;;;;;;;;;;;;;;;;;;;;;;;'
         ;xKXXXX0kdl:;;;;:cox0KXXXKx;.         'OXXXKOdddddddddddxxkO0KXXXKOxxkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkxkxc.
           ,lk0XXXXXXXXXXXXXXXXKko,.           'OXXXXXXXXXXXXXXXXXXXXXKKOxddkkxkkkkkkkkkkkkkkkkkkkkkkkxxxxdol:,.
             .';codkkOOOOOkxdl:'.              .cooooooooooooooooollc:;'.  .;;;;;;;;;;;;;;;;;;;;;;;,,,,'....
                    .......


connecting to cds api %s...
  > `, client.APIURL())
}
