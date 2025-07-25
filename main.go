package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"strconv"
	"strings"

	_ "embed"
)

//go:embed doc.txt
var genDocString string

var (
	envPath  = os.Getenv("PATH")
	envLevel = os.Getenv("LF_LEVEL")
)

type arrayFlag []string

var (
	gSingleMode     bool
	gPrintLastDir   bool
	gPrintSelection bool
	gAutocd         bool
	gClientID       int
	gHostname       string
	gLastDirPath    string
	gSelectionPath  string
	gSocketProt     string
	gSocketPath     string
	gLogPath        string
	gSelect         string
	gConfigPath     string
	gCommands       arrayFlag
	gVersion        string
)

func (a *arrayFlag) Set(v string) error {
	*a = append(*a, v)
	return nil
}

func (a *arrayFlag) String() string {
	return strings.Join(*a, ", ")
}

func init() {
	h, err := os.Hostname()
	if err != nil {
		log.Printf("hostname: %s", err)
	}
	gHostname = h

	if envLevel == "" {
		envLevel = "0"
	}
}

func exportEnvVars() {
	os.Setenv("id", strconv.Itoa(gClientID))

	os.Setenv("OPENER", envOpener)
	os.Setenv("EDITOR", envEditor)
	os.Setenv("PAGER", envPager)
	os.Setenv("SHELL", envShell)

	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "getting current directory: %s\n", err)
	}
	os.Setenv("OLDPWD", dir)

	level, err := strconv.Atoi(envLevel)
	if err != nil {
		log.Printf("reading lf level: %s", err)
	}

	level++

	os.Setenv("LF_LEVEL", strconv.Itoa(level))
}

// used by exportOpts below
func fieldToString(field reflect.Value) string {
	kind := field.Kind()
	var value string

	switch kind {
	case reflect.Int:
		value = strconv.Itoa(int(field.Int()))
	case reflect.Bool:
		value = strconv.FormatBool(field.Bool())
	case reflect.Slice:
		for i := range field.Len() {
			element := field.Index(i)

			if i == 0 {
				value = fieldToString(element)
			} else {
				value += ":" + fieldToString(element)
			}
		}
	default:
		value = field.String()
	}

	return value
}

func getOptsMap() map[string]string {
	opts := make(map[string]string)
	v := reflect.ValueOf(gOpts)
	t := v.Type()

	for i := range v.NumField() {
		// Get field name and prefix it with lf_
		name := "lf_" + t.Field(i).Name

		// Skip maps
		if name == "lf_nkeys" || name == "lf_vkeys" || name == "lf_cmdkeys" || name == "lf_cmds" {
			continue
		}

		if name == "lf_user" {
			// set each user option
			for key, value := range gOpts.user {
				opts[name+"_"+key] = value
			}
		} else {
			opts[name] = fieldToString(v.Field(i))
		}
	}

	return opts
}

func exportLfPath() {
	lfPath, err := os.Executable()
	if err != nil {
		log.Printf("getting path to lf binary: %s", err)
		lfPath = "lf"
	}
	os.Setenv("lf", quoteString(lfPath))
}

func exportOpts() {
	for key, value := range getOptsMap() {
		os.Setenv(key, value)
	}
}

func startServer() {
	cmd := detachedCommand(os.Args[0], "-server")
	if err := cmd.Start(); err != nil {
		log.Printf("starting server: %s", err)
	}
}

func checkServer() {
	if gSocketProt == "unix" {
		if _, err := os.Stat(gSocketPath); os.IsNotExist(err) {
			startServer()
		} else if _, err := net.Dial(gSocketProt, gSocketPath); err != nil {
			os.Remove(gSocketPath)
			startServer()
		}
	} else {
		if _, err := net.Dial(gSocketProt, gSocketPath); err != nil {
			startServer()
		}
	}
}

func printVersion() {
	if gVersion != "" {
		fmt.Println(gVersion)
		return
	}

	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}

	var vcsRevision, vcsTime, vcsModified string
	for _, setting := range buildInfo.Settings {
		switch setting.Key {
		case "vcs.revision":
			vcsRevision = setting.Value
		case "vcs.time":
			vcsTime = setting.Value
		case "vcs.modified":
			if setting.Value == "true" {
				vcsModified = " (dirty)"
			}
		}
	}

	if vcsRevision != "" {
		fmt.Printf("Built at commit: %s%s %s\n", vcsRevision, vcsModified, vcsTime)
	}
	fmt.Printf("Go version: %s\n", buildInfo.GoVersion)
}

func main() {
	flag.Usage = func() {
		f := flag.CommandLine.Output()
		fmt.Fprintln(f, "lf - Terminal file manager")
		fmt.Fprintln(f, "")
		fmt.Fprintf(f, "Usage:  %s [options] [cd-or-select-path]\n\n", os.Args[0])
		fmt.Fprintln(f, "  cd-or-select-path")
		fmt.Fprintln(f, "        set the initial dir or file selection to the given argument")
		fmt.Fprintln(f, "")
		fmt.Fprintln(f, "Options:")
		flag.PrintDefaults()
	}

	showDoc := flag.Bool(
		"doc",
		false,
		"show documentation")

	showVersion := flag.Bool(
		"version",
		false,
		"show version")

	serverMode := flag.Bool(
		"server",
		false,
		"start server (automatic)")

	singleMode := flag.Bool(
		"single",
		false,
		"start a client without server")

	printLastDir := flag.Bool(
		"print-last-dir",
		false,
		"print the last dir to stdout on exit (to use for cd)")

	printSelection := flag.Bool(
		"print-selection",
		false,
		"print the selected files to stdout on open (to use as open file dialog)")

	remoteCmd := flag.String(
		"remote",
		"",
		"send remote command to server")

	cpuprofile := flag.String(
		"cpuprofile",
		"",
		"path to the file to write the CPU profile")

	memprofile := flag.String(
		"memprofile",
		"",
		"path to the file to write the memory profile")

	flag.StringVar(&gLastDirPath,
		"last-dir-path",
		"",
		"path to the file to write the last dir on exit (to use for cd)")

	flag.StringVar(&gSelectionPath,
		"selection-path",
		"",
		"path to the file to write selected files on open (to use as open file dialog)")

	flag.StringVar(&gConfigPath,
		"config",
		"",
		"path to the config file (instead of the usual paths)")

	flag.Var(&gCommands,
		"command",
		"command to execute on client initialization")

	flag.BoolVar(&gAutocd,
		"autocd",
		false,
		"change to last directory using autocd on exit")

	flag.StringVar(&gLogPath,
		"log",
		"",
		"path to the log file to write messages")

	flag.Parse()

	gSocketProt = gDefaultSocketProt
	gSocketPath = gDefaultSocketPath

	if gLogPath != "" {
		path, err := filepath.Abs(gLogPath)
		if err != nil {
			log.Fatalf("getting log path: %s", err)
		}
		gLogPath = path
	}

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatalf("could not create CPU profile: %s", err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatalf("could not start CPU profile: %s", err)
		}
		defer pprof.StopCPUProfile()
	}

	switch {
	case *showDoc:
		fmt.Print(genDocString)
	case *showVersion:
		printVersion()
	case *remoteCmd != "":
		if err := remote(*remoteCmd); err != nil {
			log.Fatalf("remote command: %s", err)
		}
	case *serverMode:
		os.Chdir(gUser.HomeDir)
		serve()
	default:
		gSingleMode = *singleMode
		gPrintLastDir = *printLastDir
		gPrintSelection = *printSelection

		if !gSingleMode {
			checkServer()
		}

		gClientID = os.Getpid()

		switch flag.NArg() {
		case 0:
			_, err := os.Getwd()
			if err != nil {
				fmt.Fprintf(os.Stderr, "getting current directory: %s\n", err)
				os.Exit(2)
			}
		case 1:
			gSelect = flag.Arg(0)
		default:
			fmt.Fprintf(os.Stderr, "only single file or directory is allowed\n")
			os.Exit(2)
		}

		exportEnvVars()

		run()
	}

	if *memprofile != "" {
		f, err := os.Create(*memprofile)
		if err != nil {
			log.Fatal("could not create memory profile: ", err)
		}
		runtime.GC()
		if err := pprof.WriteHeapProfile(f); err != nil {
			log.Fatal("could not write memory profile: ", err)
		}
		f.Close()
	}
}
