// =============================================================================
//  PROJ: pimmp
//  AUTH: ardnew
//  DATE: 26 Sept 2018
//  FILE: main.go
// -----------------------------------------------------------------------------
//
//  DESCRIPTION
//    program entry-point and primary controller.
//
// =============================================================================

package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sync/atomic"
	"time"

	"ardnew.com/goutil"
)

// unexported local constants.
const (
	defaultCPUProfileName = "cpu.prof"
	defaultMEMProfileName = "mem.prof"
	defaultConfigName     = "config"
	defaultLibDataName    = "library.db"
)

// versioning information defined by compiler switches in Makefile.
var (
	identity  string
	version   string
	branch    string
	revision  string
	buildtime string
)

// adjectives used to construct the dynamic quit message in function greeting().
var (
	adjGood = [...]string{
		"an acceptable", "an excellent", "an exceptional", "a favorable",
		"a great", "a marvelous", "a positive", "a satisfactory",
		"a satisfying", "a superb", "a valuable", "a wonderful", "an ace",
		"a boss", "a bully", "a capital", "a choice", "a crack", "a nice",
		"a pleasing", "a prime", "a rad", "a sound", "a spanking", "a sterling",
		"a super", "a superior", "a welcome", "a worthy", "an admirable",
		"an agreeable", "a commendable", "a congenial", "a deluxe",
		"a first-class", "a first-rate", "a gnarly", "a gratifying",
		"a honorable", "a neat", "a precious", "a recherché", "a reputable",
		"a select", "a shipshape", "a splendid", "a stupendous",
		"a super-eminent", "a super-excellent", "a tip-top", "an up to snuff",
	}
	adjBad = [...]string{
		"an atrocious", "a bad", "an awful", "a cheap", "a crummy",
		"a dreadful", "a lousy", "a poor", "a rough", "a sad",
		"an unacceptable", "a blah", "a bummer", "a diddly", "a downer",
		"a garbage", "a gross", "an imperfect", "an inferior", "a junky",
		"a synthetic", "an abominable", "an amiss", "a bad news", "a beastly",
		"a bottom out", "a careless", "a cheesy", "a crappy", "a cruddy",
		"a defective", "a deficient", "a dissatisfactory", "an erroneous",
		"a fallacious", "a faulty", "a godawful", "a grody", "a grungy",
		"an icky", "an inadequate", "an incorrect", "a not good", "an off",
		"a raunchy", "a slipshod", "a stinking", "a substandard",
		"an unsatisfactory",
	}
)

// type BusyState keeps track of the number of goroutines that are wishing to
// indicate to the UI that they are active or busy, that the user should hold
// their horses.
type BusyState struct {
	changed   chan uint64 // signal when busy state changes
	_         uintptr     // padding, 64-bit atomic ops must be performed on 8-byte boundaries (see go1.10 sync/atomic bugs)
	busyCount uint64      // number of busy goroutines
	busyCycle uint64      // number of UI updates performed while busy
}

// function newBusyState() instantiates a new BusyState object with zeroized
// counter and update cycle.
func newBusyState() *BusyState {
	return &BusyState{
		changed:   make(chan uint64),
		busyCount: 0,
		busyCycle: 0,
	}
}

// function count() safely returns the number of goroutines currently declaring
// themselves as busy.
func (s *BusyState) count() int {
	count := atomic.LoadUint64(&s.busyCount)
	return int(count)
}

// function inc() safely increments the number of goroutines currently declaring
// themselves as busy by 1.
func (s *BusyState) inc() int {
	newCount := atomic.AddUint64(&s.busyCount, 1)
	s.changed <- newCount
	// reset the cycle if we were not busy before this increment
	if 1 == newCount {
		s.reset()
	}
	return int(newCount)
}

// function dec() safely decrements the number of goroutines currently declaring
// themselves as busy by 1.
func (s *BusyState) dec() int {
	newCount := atomic.AddUint64(&s.busyCount, ^uint64(0))
	s.changed <- newCount
	// reset the cycle if we are not busy after this increment
	if 0 == newCount {
		s.reset()
	}
	return int(newCount)
}

// function cycle() returns the number of iterations that have elapsed since the
// the beginning of the current busy state (returns 0 if not busy).
func (s *BusyState) cycle() int {
	cycle := atomic.LoadUint64(&s.busyCycle)
	return int(cycle)
}

// function next() safely increments by 1 the UI cycles elapsed since the
// current busy state was initiated.
func (s *BusyState) next() int {
	cycle := atomic.AddUint64(&s.busyCycle, 1)
	return int(cycle)
}

// function reset() safely resets the current UI cycles elapsed to 0.
func (s *BusyState) reset() {
	atomic.StoreUint64(&s.busyCycle, 0)
}

// various globals available to all units.
var (
	areOptionsParsed bool = false
)

// type Option struct can contain any possible individual option configuration
// including its command line flag identifier and usage info..
type Option struct {
	name  string
	usage string
	bool
	int
	uint
	float64
	string
	time.Duration
}

// type NamedOption is intended to map the name of an option to the actual
// *Option struct associated with it.
type NamedOption map[string]*Option

// type Options struct defines the collection of all command line options.
type Options struct {
	*flag.FlagSet // the builtin command-line parser

	Provided NamedOption // which options were provided by the user at runtime

	CPUProfile     *Option // flag indicating CPU profiling should be performed
	CPUProfileName *Option // name of file to store pprof data of CPU profiler
	MEMProfile     *Option // flag indicating MEM profiling should be performed
	MEMProfileName *Option // name of file to store pprof data of MEM profiler

	UsageHelp *Option // shows usage synopsis
	Verbose   *Option // prints additional status information
	Trace     *Option // prints very detailed status information
	Config    *Option // defines path to config file
	LibData   *Option // defines data directory path (where to store databases)
	CLIMode   *Option // defines the type of UI to use: CLI or TUI
	LogPath   *Option // file path where to write all log data

	DiskBufferSize *Option // size (bytes) of each collection's pre-allocated buffers on disk. num buffers = num CPU cores
	HashBufferSize *Option // size (bytes) by which each hash table will grow once individual capacity is exceeded.
}

// type TimeInterval struct contains a start and end time (together with a
// contains() function) as well as a description string.
type TimeInterval struct {
	start time.Time
	stop  time.Time
	desc  string
}

// function contains() verifies the given time is in the TimeInterval half-open
// range, i.e. time is in interval [start, end).
func (i *TimeInterval) contains(t time.Time) bool {
	return (t.After(i.start) || t.Equal(i.start)) && t.Before(i.stop)
}

// function greeting() generates a random adjective (synonym of "good" or "bad")
// followed by a nominal time of day using the actual current system time.
// e.g. "a crummy evening", or "a splendid morning"
func greeting() string {

	// first retrieve the current system time and convert it to a Date object.
	n := time.Now()
	d := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, n.Location())

	// next, randomly select a "good" or "bad" adjective using the low bit of
	// the current second of the current time; and then, randomly select one of
	// the elements of the respective adjective array.
	rand.Seed(n.UnixNano())
	var s, t string
	if (n.Second() & 1) == 1 {
		s = adjGood[rand.Intn(len(adjGood))]
	} else {
		s = adjBad[rand.Intn(len(adjBad))]
	}

	// lastly, check which time interval our current time is in to decide which
	// general time of day to describe.
	for _, ti := range []*TimeInterval{
		&TimeInterval{d.Add(time.Hour * 00), d.Add(time.Hour * 05), "night"},     // 12AM-04:59:59AM
		&TimeInterval{d.Add(time.Hour * 05), d.Add(time.Hour * 12), "morning"},   // 05AM-11:59:59AM
		&TimeInterval{d.Add(time.Hour * 12), d.Add(time.Hour * 17), "afternoon"}, // 12PM-04:59:59PM
		&TimeInterval{d.Add(time.Hour * 17), d.Add(time.Hour * 22), "evening"},   // 05PM-09:59:59PM
		&TimeInterval{d.Add(time.Hour * 22), d.Add(time.Hour * 24), "night"},     // 10PM-11:59:59PM
	} {
		if ti.contains(n) {
			t = ti.desc
			break
		}
	}

	// concatenate the result, ???, PROFIT
	return fmt.Sprintf("quitting, have %s %s!", s, t)
}

// function main() is the program entry point, obviously :)
func main() {

	// primary panic handler for the main thread. this is the last point at
	// which we can recover before terminating the entire process. the normal
	// exit case is also handled here for completeness. see the ReturnCode
	// switch cases to see how special case exit cleanup is implemented.
	defer func() {
		if r := recover(); nil != r {
			switch r.(type) {
			case *ReturnCode:
				c := r.(*ReturnCode)
				switch c {
				// non-errors, normal cleanup and exit
				case rcOK, rcUsage:
					infoLog.die(c, false)
				// common errors, not unusual enough reason for stack trace
				case rcInvalidConfig:
					errLog.die(c, false)
				// all other errors not specifically handled above
				default:
					errLog.die(c, true)
				}
			}
		}
	}()

	var busyState *BusyState = newBusyState()
	var initComplete chan bool = make(chan bool)

	// first things first, parse options and command line arguments which can
	// influence the operating modes of the program from a very high level.
	options, err := initOptions()
	if nil != err {
		// immediately terminate if we don't understand the runtime options.
		panic(err)
	}

	// if the user provided a log file, redirect all output to that file instead
	// of the default of STDOUT (or our LogView when running in TUI mode).
	logPath, isLogPathProvided := options.Provided[options.LogPath.name]
	if isLogPathProvided {
		of, err := os.Create(logPath.string)
		if err != nil {
			panic(rcInvalidPath.specf("could not create log file: %s", err))
		}
		defer func() {
			if err := of.Close(); err != nil {
				panic(rcInvalidPath.specf("could not close log file: %s", err))
			}
		}()
		ow := bufio.NewWriter(of)
		setWriterAll(ow)
	}

	// create the CPU profiler output if requested.
	if options.CPUProfile.bool && "" != options.CPUProfileName.string {
		infoLog.verbosef("writing CPU profile: %q", options.CPUProfileName.string)
		f, err := os.Create(options.CPUProfileName.string)
		if err != nil {
			panic(rcInvalidFile.specf("could not create CPU profile: %s", err))
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			panic(rcInvalidFile.specf("could not start CPU profile: %s", err))
		}
		defer pprof.StopCPUProfile()
	}

	// if no options were provided and no config file exists, then we are
	// totally lost and confused. display usage and bail out.
	config := options.Config.string
	configExists, _ := goutil.PathExists(config)
	if !configExists && len(os.Args) <= 1 {
		options.Usage()
		panic(rcUsage)
	}

	// create the directory hierarchy that will store our configuration data
	// permanently on disk.
	configDir := options.configDir()
	if !configExists {
		if dirExists, _ := goutil.PathExists(configDir); !dirExists {
			if err := os.MkdirAll(configDir, os.ModePerm); nil != err {
				panic(rcInvalidConfig.specf(
					"cannot create configuration directory: %q: %s", configDir, err))
			}
			infoLog.tracef("created configuration directory: %q", configDir)
		}

		// TODO: create configuration file
		infoLog.tracef("(TBD) -- created configuration: %q", config)
	}

	// if we haven't died yet, then config dir/file exists. load it.
	// NOTE: be careful not to overwrite any config options that were already
	//       provided via command line as those should always take precedence!
	infoLog.tracef("(TBD) -- loading configuration: %q", config)

	// create the directory hierarchy that will store our libraries' backing
	// data stores permanently on disk.
	libData := options.LibData.string
	if exists, _ := goutil.PathExists(libData); !exists {
		if err := os.MkdirAll(libData, os.ModePerm); nil != err {
			panic(rcInvalidConfig.specf(
				"cannot create shared data directory: %q: %s", libData, err))
		}
		infoLog.tracef("created shared data directory: %q", libData)
	} else {
		infoLog.tracef("(TBD) -- loading shared data directory: %q", libData)
	}

	// runtime environment defined, begin preparing the libs and databases.
	infoLog.log("initializing library databases ...")

	// remaining arguments are considered paths to libraries; verify the paths
	// before assuming valid ones exist for traversal.
	library := initLibrary(options, busyState)
	if 0 == len(library) {
		panic(rcInvalidConfig.spec("no valid libraries provided"))
	}

	// dispatch a goroutine that will listen for the database and file system
	// media discovery goroutines to finish (scanComplete will only be written
	// to once both the load and scan operations have completed).
	scanStart := time.Now()
	go func(lib []*Library, start time.Time) {

		var numFound uint = 0
		for _, l := range lib {
			// block this goroutine until each library has written to their
			// respective channel. the order in which we receive this channel
			// data is irrelevant because they -all- must complete.
			numFound += (<-l.scanComplete).(uint)
		}
		scanElapsed := time.Since(start)
		infoLog.logf("initialization complete (%d ~things~ found in %s)",
			numFound, scanElapsed.Round(time.Millisecond))

		// the only purpose of this channel is to safely handle the transition
		// from the initial CLI mode to the ncurses TUI mode by displaying
		// status information to the appropriate interface. if this channel is
		// still empty by the time we have our ncurses interface shown, then all
		// prior log/status messages have been obscured by the UI. so we want to
		// give an update to convince the user we are still working on it. this
		// determination is made by the select-true block below with a default
		// case that is selected when initComplete is empty.
		initComplete <- true

	}(library, scanStart)

	// libraries ready, spool up the library scanners.
	populateLibrary(options, library)

	// we don't wait for the scanning to finish. go ahead and launch the UI for
	// progress indicators and anything else the user can get away with while
	// the scanners/loaders work.
	if !isCLIMode {
		//layout := newLayout(options, busyState, library...)
		// associate the loggers with the navigable log viewer.
		if !isLogPathProvided {
			//setWriterAll(layout.logView)
		}
		select {
		case <-initComplete:
			// if there exists something in this channel, then we have already
			// printed "initialization complete ..." above. in which case, just
			// carry on and display the UI with no other status to the user.
			// we will hit this case block which is a NOOP.
		default:
			// otherwise, nothing exists in that channel and we are still
			// scanning. don't sit around like a deadbeat -- tell the user we're
			// working on it.
			infoLog.logf("still initializing library databases ...")
		}
		//if errCode := layout.show(); nil != errCode {
		//	panic(errCode)
		//}
	} else {
		<-initComplete
	}

	// create the memory profiler output if requested
	if options.MEMProfile.bool && "" != options.MEMProfileName.string {
		infoLog.verbosef("writing memory profile: %q", options.MEMProfileName.string)
		f, err := os.Create(options.MEMProfileName.string)
		if err != nil {
			panic(rcInvalidFile.specf("could not create memory profile: %s", err))
		}
		runtime.GC() // get up-to-date statistics
		if err := pprof.WriteHeapProfile(f); err != nil {
			panic(rcInvalidFile.specf("could not write memory profile: %s", err))
		}
		f.Close()
	}

	// exit cleanly but explicitly so that we have some control on exit codes
	// and resource cleanup.
	panic(rcOK.spec(greeting()))
}

// function configDir() constructs the full path to the directory containing all
// of the program's supporting configuration data. if the user has defined a
// specific config file (via -config arg), then use the _logical_ parent
// directory of that file path; otherwise, use the default path "~/.<identity>".
// -----------------------------------------------------------------------------
//  TODO: construct a more conventional path for Windows hosts.
// -----------------------------------------------------------------------------
func (o *Options) configDir() string {
	if nil == o {
		return filepath.Join(homeDir(), fmt.Sprintf(".%s", identity))
	} else {
		return filepath.Dir(o.Config.string)
	}
}

// function providedDBConfig() checks the "Provided" hash of the Options struct
// for any of the options related to initial database configuration. this is
// necessary to decide how to initialize the database. furthermore, a []string
// will be returned containing the name of each option the user provided.
func (o *Options) providedDBConfig() (bool, []string) {

	list := []string{}
	provided := false

	if d, ok := o.Provided[o.DiskBufferSize.name]; ok {
		list = append(list, d.name)
		provided = true
	}
	if d, ok := o.Provided[o.HashBufferSize.name]; ok {
		list = append(list, d.name)
		provided = true
	}

	return provided, list
}

// function initOptions() parses all command line arguments and prepares the
// environment.
func initOptions() (options *Options, err *ReturnCode) {

	defer func() {
		// without options parsed, we cannot know where to print any status or
		// other info, so we always print everything to the console until they
		// are. this flag controls that state change.
		areOptionsParsed = true
		// panic handler
		if recovered := recover(); nil != recovered {
			options = nil
			if flag.ErrHelp == recovered {
				// hide the flag.flagSet's default output status message,
				// because we will print our own.
				err = rcUsage
				return
			}
			// at this point we encountered an actual error, capture it and show
			// it with our error logger. (NOTE: this "err" is a named output
			// paramater of function initOptions()).
			err = rcInvalidArgs.specf("%s", recovered)
		}
	}()

	// by default,
	configPath := filepath.Join(options.configDir(), defaultConfigName)
	libDataPath := filepath.Join(options.configDir(), defaultLibDataName)

	// define the option properties that the command line parser recognizes.
	options = &Options{
		// PanicOnError gets trapped by the anon defer'd func() above. the
		// recover()'d  value will be set to flag.ErrHelp, which we want to
		// override by printing with our error logger.
		FlagSet:  flag.NewFlagSet(identity, flag.PanicOnError),
		Provided: NamedOption{},

		CPUProfile: &Option{
			name:  "cpuprofile",
			usage: "flag indicating CPU profiling should be performed",
			bool:  false,
		},
		CPUProfileName: &Option{
			name:   "cpuprofilename",
			usage:  "path to file to store pprof data of CPU profiler",
			string: filepath.Join(os.Getenv("PWD"), defaultCPUProfileName),
		},
		MEMProfile: &Option{
			name:  "memprofile",
			usage: "flag indicating MEM profiling should be performed",
			bool:  false,
		},
		MEMProfileName: &Option{
			name:   "memprofilename",
			usage:  "path to file to store pprof data of MEM profiler",
			string: filepath.Join(os.Getenv("PWD"), defaultMEMProfileName),
		},
		UsageHelp: &Option{
			name:  "help",
			usage: "display this helpful usage synopsis!",
			bool:  false,
		},
		Verbose: &Option{
			name:  "verbose",
			usage: "display additional status information",
			bool:  false,
		},
		Trace: &Option{
			name:  "trace",
			usage: "display additional status information (maximum verbosity)",
			bool:  false,
		},
		CLIMode: &Option{
			name:  "cli",
			usage: "disables the curses-style textual user interface, falling back to basic terminal I/O. useful when deugging.",
			bool:  false,
		},
		LogPath: &Option{
			name:   "log",
			usage:  "file path to where all normal and verbose log messages will be redirected",
			string: "",
		},
		Config: &Option{
			name:   "config",
			usage:  "path to config file",
			string: configPath,
		},
		LibData: &Option{
			name:   "libdata",
			usage:  "path to library data directory (database storage location)",
			string: libDataPath,
		},
		DiskBufferSize: &Option{
			name:  "diskbuffersize",
			usage: "size (in bytes) of each library's preallocated on-disk buffers (number of buffers = number of CPU cores)\n  (NOTE: this may not be changed after the corresponding library's database has been created)",
			int:   defaultDiskBufferSize,
		},
		HashBufferSize: &Option{
			name:  "hashbuffersize",
			usage: "size (in bytes) by which each hash table will grow to make room once it reaches capacity\n  (NOTE: this may not be changed after the corresponding library's database has been created)",
			int:   defaultHashBufferSize,
		},
	}
	knownOptions := NamedOption{
		"cpuprofile":     options.CPUProfile,
		"cpuprofilename": options.CPUProfileName,
		"memprofile":     options.MEMProfile,
		"memprofilename": options.MEMProfileName,
		"help":           options.UsageHelp,
		"verbose":        options.Verbose,
		"trace":          options.Trace,
		"cli":            options.CLIMode,
		"log":            options.LogPath,
		"config":         options.Config,
		"libdata":        options.LibData,
		"diskbuffersize": options.DiskBufferSize,
		"hashbuffersize": options.HashBufferSize,
	}

	// register the command line options we want to handle.
	options.BoolVar(&options.CPUProfile.bool, options.CPUProfile.name, options.CPUProfile.bool, options.CPUProfile.usage)
	options.StringVar(&options.CPUProfileName.string, options.CPUProfileName.name, options.CPUProfileName.string, options.CPUProfileName.usage)
	options.BoolVar(&options.MEMProfile.bool, options.MEMProfile.name, options.MEMProfile.bool, options.MEMProfile.usage)
	options.StringVar(&options.MEMProfileName.string, options.MEMProfileName.name, options.MEMProfileName.string, options.MEMProfileName.usage)
	options.BoolVar(&options.UsageHelp.bool, options.UsageHelp.name, options.UsageHelp.bool, options.UsageHelp.usage)
	options.BoolVar(&options.Verbose.bool, options.Verbose.name, options.Verbose.bool, options.Verbose.usage)
	options.BoolVar(&options.Trace.bool, options.Trace.name, options.Trace.bool, options.Trace.usage)
	options.BoolVar(&options.CLIMode.bool, options.CLIMode.name, options.CLIMode.bool, options.CLIMode.usage)
	options.StringVar(&options.LogPath.string, options.LogPath.name, options.LogPath.string, options.LogPath.usage)
	options.StringVar(&options.Config.string, options.Config.name, options.Config.string, options.Config.usage)
	options.StringVar(&options.LibData.string, options.LibData.name, options.LibData.string, options.LibData.usage)
	options.IntVar(&options.DiskBufferSize.int, options.DiskBufferSize.name, options.DiskBufferSize.int, options.DiskBufferSize.usage)
	options.IntVar(&options.HashBufferSize.int, options.HashBufferSize.name, options.HashBufferSize.int, options.HashBufferSize.usage)

	// hide the flag.flagSet's default output error message, because we will
	// display our own.
	options.SetOutput(ioutil.Discard)

	// the output provided with -help or when a option parse error occurred.
	options.Usage = func() {
		rawLog.logf("%s v%s (%s@%s) [%s]", identity, version, branch, revision, buildtime)
		rawLog.log()
		options.SetOutput(os.Stdout)
		options.PrintDefaults()
		rawLog.log()
	}

	// yeaaaaaaah, now we do it!
	options.Parse(os.Args[1:])
	options.Visit(
		func(f *flag.Flag) { options.Provided[f.Name] = knownOptions[f.Name] })

	// update the loggers' verbosity settings.
	isVerboseLog = options.Verbose.bool
	isTraceLog = options.Trace.bool
	isCLIMode = options.CLIMode.bool

	var parseError *ReturnCode = nil

	// update program state for global optons.
	if options.UsageHelp.bool {
		options.Usage()
		parseError = rcUsage
	}

	return options, parseError
}

// function initLibrary() validates all library paths provided, returning a list
// of the valid ones.
func initLibrary(options *Options, busyState *BusyState) []*Library {

	var library []*Library

	// any remaining args were not handled by the options parser. they are then
	// considered to be file paths of libraries to scan.
	libArgs := options.Args()

	// dispatch a single goroutine per library to verify each concurrently.
	for _, libPath := range libArgs {
		lib, err := newLibrary(
			options, busyState, libPath, depthUnlimited, library)

		// if we encounter an error, issue a warning, do NOT add it to the list
		// of valid libraries, and continue. if it is truly a fatal error, then
		// all user-provided libraries will fail for the same reason; the list
		// of valid libraries will be empty on return, and the program will
		// terminate with error "no libraries found".
		if nil != err {
			warnLog.log(err)
		} else {
			// no error encountered, so the library is considered valid. add it
			// to the queue.
			infoLog.verbosef("using library: %s", lib)
			library = append(library, lib)
		}
	}

	return library
}

// function populateLibrary() spawns goroutines to scan each library
// concurrently.
func populateLibrary(options *Options, library []*Library) {

	// for each library, dispatch a pair (2) of goroutines in order:
	//   1. dump all of the content from the library's database, verifying it
	//       and notifying the discovery channels;
	//   2. recursively traverse the library's filesystem, identifying which
	//       content is valid and desirable, then notify the discovery channels
	//       accordingly.
	for _, lib := range library {

		// 1. pull all of the media already known to exist in the library from
		//    the local database, verify it still exists, and then notify via
		//    provided callback handler.
		go func(l *Library) {
			var numMedia uint = 0
			if !l.db.isFirstAppearance() {
				loadCount, loadErr := l.load(
					&PathHandler{
						// the loader identified some file in a subdirectory of
						// the library's file system as a media file.
						handleMedia: func(l *Library, p string, v ...interface{}) {
							//disco := newDiscovery(v...)
							if !isCLIMode {
								//l.layout.addDiscovery(l, disco)
							}
						},
						// the loader identified some file in a subdirectory of
						// the library's file system as a supporting auxiliary
						// file to a known or as-of-yet unknown media file.
						handleSupport: func(l *Library, p string, v ...interface{}) {
							//disco := newDiscovery(v...)
							if !isCLIMode {
								//l.layout.addDiscovery(l, disco)
							}
						},
						// the loader identified some file in a subdirectory of
						// the library's file system as an undesirable piece of
						// trash.
						handleOther: func(l *Library, p string, v ...interface{}) {
						},
					})
				numMedia += loadCount
				if nil != loadErr {
					errLog.verbose(loadErr)
				}
			}
			l.loadComplete <- numMedia
		}(lib)

		// 2. recursively walks a library's file system, notifying the provided
		//    callback handler whenever any sort of content is found.
		go func(l *Library) {
			// postpone the scanning until the load routine has completed.
			var numMedia uint = (<-l.loadComplete).(uint)
			scanCount, scanErr := l.scan(
				&PathHandler{
					// the scanner identified some file in a subdirectory of the
					// library's file system as a media file.
					handleMedia: func(l *Library, p string, v ...interface{}) {
						//disco := newDiscovery(v...)
						if !isCLIMode {
							//l.layout.addDiscovery(l, disco)
						}
					},
					// the scanner identified some file in a subdirectory of the
					// library's file system as a supporting auxiliary file to a
					// known or as-of-yet unknown media file.
					handleSupport: func(l *Library, p string, v ...interface{}) {
						//disco := newDiscovery(v...)
						if !isCLIMode {
							//l.layout.addDiscovery(l, disco)
						}
					},
					// the scanner identified some file in a subdirectory of the
					// library's file system as an undesirable piece of trash.
					handleOther: func(l *Library, p string, v ...interface{}) {
					},
				})
			numMedia += scanCount
			if nil != scanErr {
				errLog.verbose(scanErr)
			}
			if 0 == numMedia {
				warnLog.logf("no media in %q: library is empty!", l.name)
				if !isVerboseLog && !isTraceLog {
					warnLog.logf("try using program options -%s or -%s for more info",
						options.Verbose.name, options.Trace.name)
				}
			}
			l.scanComplete <- numMedia
		}(lib)
	}
}
