// =============================================================================
//  PROJ: pimmp
//  AUTH: ardnew
//  DATE: 26 Sept 2018
//  FILE: library.go
// -----------------------------------------------------------------------------
//
//  DESCRIPTION
//    defines types and operations for interacting with library databases,
//    traversing file systems, and managing user-defined collections of media.
//
// =============================================================================

package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/HouzuoGuo/tiedot/db"
	//"github.com/davecgh/go-spew/spew"
)

// type Library represents a collection of a specified kind of media files
// together with a rooted search path from which all media file discovery
// is performed.
type Library struct {
	workingDir string // current working directory
	absPath    string // absolute path to library
	name       string // library name (default: basename of path)
	maxDepth   uint   // maximum traversal depth (unlimited: 0)

	dataDir string    // directory containing all known library databases
	db      *Database // database containing all known media in this library

	busyState *BusyState // reference to the global busy state mutex

	tui *TUI // reference to the primary text user interface (TUI)

	loadComplete chan interface{} // synchronization lock
	loadStart    chan time.Time   // counting semaphore to limit number of concurrent loaders
	loadElapsed  time.Duration    // measures time elapsed for load to complete (use internally, not thread-safe!)

	scanComplete chan interface{} // synchronization lock
	scanStart    chan time.Time   // counting semaphore to limit number of concurrent scanners
	scanElapsed  time.Duration    // measures time elapsed for scan to complete (use internally, not thread-safe!)

	lastScan time.Time // the datetime at which this library was last scanned
}

// type PathHandlerFunc represents a function that accepts a Library, file path,
// and variable number of additional arguments. this is intended for use by the
// functions scanDive()/loadDive() when they encounter files and directories.
type PathHandlerFunc func(*Library, string, ...interface{})
type PathHandler struct {
	handleMedia, handleSupport, handleOther PathHandlerFunc
}

// type ExtTable is a mapping of the name of file types to their common file
// name extensions.
type ExtTable map[string][]string

// function kindOfFileExt() searches a given ExtTable for the provided extension
// string, returning both the name of the encoding and a boolean flag indicating
// whether or not it was found in the table.
func kindOfFileExt(table *ExtTable, ext string) (string, bool) {
	// iter: each entry in current media's file extension table
	for n, l := range *table {
		// iter: each file extension in current table entry
		for _, e := range l {
			// cond: wanted file extension matches current file extension
			if e == ext {
				// return: current media kind, file type of extension
				return n, true
			}
		}
	}
	return "", false
}

// type DiscoveryMethod represents the method by which media is located.
type DiscoveryMethod int

// local unexported constants which categorize the method by which media items
// are discovered. items discovered by "load" are previously-known items being
// loaded by the database, and items discovered by "scan" were encountered (for
// the first time) by file system traversal.
const (
	dmUnknown DiscoveryMethod = iota - 1 // = -1
	dmLoad                               // = 0 loaded from database
	dmScan                               // = 1 found by file system traversal
	dmCOUNT                              // = 2
)

// type Discovery represents any sort of file entity discovered during a file
// system traversal of the library; we can capture here any other useful info
// describing the state of the file system traversal / search at the exact
// moment in time in which it was discovered.
type Discovery struct {
	time time.Time
	data []interface{} // 0 = object, 1 = db ID
}

// function newDiscovery() constructs a new instance of a Discovery struct
// with the current time and the provided data.
func newDiscovery(d ...interface{}) *Discovery {
	return &Discovery{time: time.Now(), data: d}
}

// local unexported constants controlling the behavior of the library scanners.
const (
	depthUnlimited     = 0
	maxLibraryScanners = 1
)

// function init() initializes all of the locally-declared data for use both
// locally and globally
func init() {}

// function newLibrary() creates and initializes a new Library ready to scan.
// the library database is also created if one doesn't already exist, otherwise
// it is opened for business.
func newLibrary(opt *Options, busy *BusyState, lib string, lim uint, curr []*Library) (*Library, *ReturnCode) {

	// pull only the relevant info we need from the Options struct.
	dat := opt.LibData.string

	// determine the user's current working dir -- from where they invoked us.
	dir, err := os.Getwd()
	if nil != err {
		return nil, rcInvalidLibrary.specf(
			"newLibrary(%q, %q): os.Getwd(): %s", dat, lib, err)
	}

	// determine the absolute path to the directory tree containing media.
	abs, err := filepath.Abs(lib)
	if nil != err {
		return nil, rcInvalidLibrary.specf(
			"newLibrary(%q, %q): filepath.Abs(): %s", dat, lib, err)
	}

	// verify we haven't already seen this path in our library list.
	for _, p := range curr {
		if p.absPath == abs {
			return nil, rcDuplicateLibrary.specf(
				"newLibrary(%q, %q): library already exists (skipping): %q", dat, lib, abs)
		}
	}

	// open the root directory of the library file system for reading.
	fds, err := os.Open(abs)
	if nil != err {
		return nil, rcInvalidLibrary.specf(
			"newLibrary(%q, %q): os.Open(): %s", dat, lib, err)
	}

	// read all content of the root directory in the library file system.
	_, err = fds.Readdir(0)
	fds.Close()
	if nil != err {
		return nil, rcInvalidLibrary.specf(
			"newLibrary(%q, %q): Readdir(): %s", dat, lib, err)
	}

	// open or create the library database if it doesn't exist.
	db, ret := newDatabase(opt, abs, dat)
	if nil != ret {
		return nil, ret
	}

	return &Library{
		workingDir: dir,
		absPath:    abs,
		name:       path.Base(abs),
		maxDepth:   lim,

		// path to the library database directory.
		dataDir: dat,
		db:      db,

		// mutex which controls interaction by the various goroutines to limited
		// system resources such as database tables, UI primitives, etc.
		busyState: busy,

		// we will not necessarily have a TUI reference in the event that the
		// user is running in CLI mode, so keep nil by default.
		tui: nil,

		loadComplete: make(chan interface{}),
		loadStart:    make(chan time.Time, maxLibraryScanners),
		loadElapsed:  0,

		scanComplete: make(chan interface{}),
		scanStart:    make(chan time.Time, maxLibraryScanners),
		scanElapsed:  0,

		lastScan: time.Time{},
	}, nil
}

// function String() creates a string representation of the Library for easy
// identification in logs.
func (l *Library) String() string {
	return fmt.Sprintf("{%q,%q,%s}", l.name, l.absPath, l.db)
}

// function recandidateSubtitles() attempts to find candidate VideoMedia in the
// library for all Subtitles that are currently unassociated with any VideoMedia
// objects. if force is true, then it attempts to find candidate VideoMedia for
// ALL Subtitles objects and not only the orphaned/unassociated ones.
func (l *Library) recandidateSubtitles(force bool) *ReturnCode {

	orphan := []RecordID{}
	remain := []RecordID{}

	l.db.col[ecSupport][skSubtitles].ForEachDoc(
		func(id int, data []byte) (willMoveOn bool) {
			subs := &Subtitles{}
			subs.fromRecord(data)
			if force || 0 >= len(subs.KnownVideoMedia) {
				orphan = append(orphan, RecordID{id: id, rec: subs})
			}
			return true // move on to next record
		})

	numOrphan := len(orphan)
	if numOrphan > 0 {
		warnLog.tracef("identified %d orphan subtitles in \"%s\" (unassociated with any media)", numOrphan, l.name)
		for _, o := range orphan {
			subs := o.rec.(*Subtitles)
			infoLog.tracef("scanning media for subtitles: %s", subs)
			vid, err := subs.findCandidates(l, true, o.id)
			if nil != err {
				return err
			}
			if 0 == len(vid) {
				remain = append(remain, o)
			}
		}
		warnLog.tracef("still unable to associate %d orphan subtitles with any media. consider renaming or moving the files to something more conventional.", len(remain))
	}

	return nil
}

// function loadDive() performs the actual iterated loading of all objects in
// this Library. as each object is instantiated using the data from the data
// store, it is handed off to the load handler for handling by all subscribers.
func (l *Library) loadDive(ph *PathHandler, class EntityClass, kind int) (uint, *ReturnCode) {

	var count uint = 0
	var ret *ReturnCode = nil

	// iterate over every record in the specified collection, unmarshalling the
	// data stored in the database into a real, fully-typed and populated object
	// before notifying the handler of what we found.
	l.db.col[class][kind].ForEachDoc(
		func(id int, data []byte) (willMoveOn bool) {
			switch class {
			case ecMedia:
				switch MediaKind(kind) {
				case mkAudio:
					audio := &AudioMedia{}
					audio.fromRecord(data)
					infoLog.tracef("loaded audio (ID={%q,%X}): %s", l.name, id, audio)
					if nil != ph && nil != ph.handleMedia {
						ph.handleMedia(l, audio.AbsPath, audio, id)
					}
				case mkVideo:
					video := &VideoMedia{}
					video.fromRecord(data)
					infoLog.tracef("loaded video (ID={%q,%X}): %s", l.name, id, video)
					if nil != ph && nil != ph.handleMedia {
						ph.handleMedia(l, video.AbsPath, video, id)
					}
				default:
				}
			case ecSupport:
				switch SupportKind(kind) {
				case skSubtitles:
					subs := &Subtitles{}
					subs.fromRecord(data)
					infoLog.tracef("loaded subtitles (ID={%q,%X}): %s", l.name, id, subs)
					if nil != ph && nil != ph.handleSupport {
						ph.handleSupport(l, subs.AbsPath, subs, id)
					}
				default:
				}
			default:
			}
			count++
			return true // move on to next record
		})

	return count, ret
}

// function load() is the entry point for initiating a load on the library's
// backing data store. currently, the load is dispatched and cannot be safely
// interrupted. you must wait for the load to finish before restarting.
func (l *Library) load(handler *PathHandler) (uint, *ReturnCode) {

	var (
		numLoad uint = 0 // number of known files loaded from database
		err     *ReturnCode
	)

	//
	// the loadStart channel is buffered so that we can limit the number of
	// goroutines concurrently reading this library's database:
	//
	//     writes to the channel will fail and fallback on the default select
	//     case if the max number of loaders is reached -- which sets an error
	//     code that is returned to the caller -- so be sure to check
	//     the return value when calling function load()!
	//

	// try writing to the buffered channel. this will succeed if and only if it
	// isn't already filled to capacity.
	select {
	case l.loadStart <- time.Now():

		// notify the user that a potentially time-intensive operation has
		// begun and user interactions will be limited.
		if !isCLIMode {
			l.busyState.inc()
		}

		// the write succeeded, so we can initiate loading. keep track of the
		// time at which we began so that the time elapsed can be calculated and
		// notified to the user.
		infoLog.verbosef("loading: %q", l.name)
		// multi-dimensional numRecordsLoad contains fixed outer-array dimension
		// equal to number of collections (i.e. classes) equal to ecCOUNT
		for classID, count := range l.db.numRecordsLoad {
			class := EntityClass(classID)
			for kind := range count {
				if count[kind], err = l.loadDive(handler, class, kind); nil != err {
					return numLoad, err
				}
			}
		}

		// we've finished the loading operations, so remove the busy indicator
		// to indicate that normal user interactions may resume (if no other
		// event has the semaphore still incremented).
		l.loadElapsed = time.Since(<-l.loadStart)
		if !isCLIMode {
			l.busyState.dec()
		}

		// construct a summary message for the load operation.
		total, summary := l.db.totalRecordsString(dmLoad, -1, -1)
		if total > 0 {
			infoLog.verbosef(
				"finished loading: %q (%s loaded in %s)",
				l.name, summary, l.loadElapsed.Round(time.Millisecond))
		} else {
			infoLog.verbosef(
				"finished loading: %q (no media loaded in %s)",
				l.name, l.loadElapsed.Round(time.Millisecond))
		}
		numLoad = total

	default:
		// if the write failed, we fall back to this default case. the only
		// reason it should fail is if the buffer is already filled to capacity,
		// meaning we already have the max allowed number of goroutines loading
		// this library's database.
		err = rcLibraryBusy.specf(
			"load(): max number of loaders reached: %q (max = %d)",
			l.absPath, maxLibraryScanners)
	}

	// return a count of the total number of entities successfully loaded.
	return numLoad, err
}

// function scanDive() is the recursive step for the file system traversal,
// invoked initially by function scan(). error codes generated in this routine
// will be returned to the caller of scanDive() -and- the caller of scan().
func (l *Library) scanDive(ph *PathHandler, absPath string, depth uint) *ReturnCode {

	// get a path to the file relative to the library root dir (useful for
	// displaying diagnostic info to the user).
	relPath, err := filepath.Rel(l.absPath, absPath)
	if nil != err {
		return rcInvalidPath.specf(
			"scanDive(%q, %d): filepath.Rel(%q): %s", absPath, depth, l.absPath, err)
	}

	// for concision, show the relative path by default in any diagnostics/logs.
	dispPath := relPath

	// read fs attributes to determine how we handle the file.
	fileInfo, err := os.Lstat(absPath)
	if nil != err {
		return rcInvalidStat.specf(
			"scanDive(%q, %d): os.Lstat(): %s", dispPath, depth, err)
	}
	mode := fileInfo.Mode()

	// operate on the file based on its file mode.
	switch {
	case (mode & os.ModeDir) > 0:
		// file is directory, scanDive its contents unless we are at max depth.
		if depthUnlimited != l.maxDepth && depth > l.maxDepth {
			return rcDirDepth.specf(
				"scanDive(%q, %d): limit = %d", dispPath, depth, l.maxDepth)
		}
		dir, err := os.Open(absPath)
		if nil != err {
			return rcDirOpen.specf(
				"scanDive(%q, %d): os.Open(): %s", dispPath, depth, err)
		}
		dirName, err := dir.Readdirnames(0)
		dir.Close()
		if nil != err {
			return rcDirOpen.specf(
				"scanDive(%q, %d): dir.Readdirnames(): %s", dispPath, depth, err)
		}

		// recursively scan all of this subdirectory's contents.
		var scanErr *ReturnCode
		for _, name := range dirName {
			scanErr = l.scanDive(ph, path.Join(absPath, name), depth+1)
			if nil != scanErr {
				// a file/subdir of the current directory threw an error.
				warnLog.trace(scanErr)
			}
		}
		return nil

	case (mode & os.ModeSymlink) > 0:
		// symlinks currently unhandled.
		return rcInvalidFile.specf(
			"scanDive(%q, %d): symlinks not supported! (skipping)", dispPath, depth)

	case (mode & (os.ModeDevice | os.ModeNamedPipe | os.ModeSocket | os.ModeCharDevice)) > 0:
		// file is not a regular file, not supported.
		return rcInvalidFile.specf(
			"scanDive(%q, %d): not a regular file (skipping)", dispPath, depth)

	default:
		// function seenFile() checks if the file specified by path and kind of
		// media exists in the associated collection of this library's database.
		seenFile := func(lib *Library, class EntityClass, kind int, path string) (bool, error) {

			indexRef := [ecCOUNT]int{
				int(mxPath), // ecMedia
				int(sxPath), // ecSupport
			}

			// verify we've received a file of a known specific class.
			var index int
			if class != ecUnknown && class < ecCOUNT {
				index = indexRef[class]
			} else {
				return false, fmt.Errorf("seenFile(): unrecognized class: %d", int(class))
			}

			// perform a simple database query on the appropriate table to check
			// if we've ever seen this file before based on its absolute path.
			result := make(map[int]struct{})
			if err := db.EvalQuery(map[string]interface{}{
				"eq": path,
				"in": []interface{}{(*lib.db.index[class][index])[0]},
			}, lib.db.col[class][kind], &result); nil != err {
				return false, err
			}
			return len(result) > 0, nil
		}

		// first extract the file name extension. this is how we determine file
		// type; not very intelligible, but fast and mostly reliable for media
		// files (~my~ media files, at least).
		ext := path.Ext(absPath)

		// check if it looks like a regular media file.
		switch kind, extName := mediaKindOfFileExt(ext); kind {
		case mkAudio:

			// select the audio database collection to determine if this is a
			// previously-known file or if we need to insert a new entity.
			ac := l.db.col[ecMedia][mkAudio]
			seen, err := seenFile(l, ecMedia, int(kind), absPath)
			if err != nil {
				return rcInvalidFile.specf(
					"scanDive(%q, %d): failed to evaluate query: %s (skipping)", dispPath, depth, err)
			}
			if !seen {
				// this is a legitimately unknown file, create a new AudioMedia
				// entity and insert it into the database.
				audio := newAudioMedia(l, absPath, relPath, ext, extName, fileInfo)
				if rec, recErr := audio.toRecord(); nil == recErr {
					if id, insErr := ac.Insert(*rec); nil == insErr {
						l.db.numRecordsScan[ecMedia][kind]++
						infoLog.tracef("discovered audio (ID={%q,%X}): %s", l.name, id, audio)
						if nil != ph && nil != ph.handleMedia {
							// notify the callback handler of a new AudioMedia.
							ph.handleMedia(l, absPath, audio, id)
						}
					} else {
						return rcDatabaseError.specf(
							"scanDive(%q, %d): failed to insert record: %s (skipping)", dispPath, depth, insErr)
					}
				} else {
					// failed to construct a new Audio object.
					return recErr
				}
			}

		case mkVideo:

			// select the video database collection to determine if this is a
			// previously-known file or if we need to insert a new entity.
			vc := l.db.col[ecMedia][mkVideo]
			seen, err := seenFile(l, ecMedia, int(kind), absPath)
			if err != nil {
				return rcInvalidFile.specf(
					"scanDive(%q, %d): failed to evaluate query: %s (skipping)", dispPath, depth, err)
			}
			if !seen {
				// this is a legitimately unknown file, create a new VideoMedia
				// entity and insert it into the database.
				video := newVideoMedia(l, absPath, relPath, ext, extName, fileInfo)
				if rec, recErr := video.toRecord(); nil == recErr {
					if id, insErr := vc.Insert(*rec); nil == insErr {
						l.db.numRecordsScan[ecMedia][kind]++
						infoLog.tracef("discovered video (ID={%q,%X}): %s", l.name, id, video)
						if nil != ph && nil != ph.handleMedia {
							// notify the callback handler of a new VideoMedia.
							ph.handleMedia(l, absPath, video, id)
						}
					} else {
						return rcDatabaseError.specf(
							"scanDive(%q, %d): failed to insert record: %s (skipping)", dispPath, depth, insErr)
					}
				} else {
					// failed to construct a new Video object.
					return recErr
				}
			}

		default:

			// doesn't have an extension typically associated with media files.
			// check if it is a media-supporting file.
			switch kind, extName := supportKindOfFileExt(ext); kind {
			case skSubtitles:
				// select the media support database collection to determine if
				// this is a previously-known file or if we need to insert a new
				// entity.
				sc := l.db.col[ecSupport][skSubtitles]
				seen, err := seenFile(l, ecSupport, int(kind), absPath)
				if err != nil {
					return rcInvalidFile.specf(
						"scanDive(%q, %d): failed to evaluate query: %s (skipping)", dispPath, depth, err)
				}
				if !seen {
					// this is a legitimately unknown file, create a new media
					// support entity and insert it into the database.
					subs := newSubtitles(l, absPath, relPath, ext, extName, fileInfo)
					if rec, recErr := subs.toRecord(); nil == recErr {
						if id, insErr := sc.Insert(*rec); nil == insErr {
							l.db.numRecordsScan[ecSupport][kind]++
							infoLog.tracef("discovered subtitles (ID={%q,%X}): %s", l.name, id, subs)
							// notify the callback handler of a new Subtitles.
							if nil != ph && nil != ph.handleSupport {
								ph.handleSupport(l, absPath, subs, id)
							}
						} else {
							return rcDatabaseError.specf(
								"scanDive(%q, %d): failed to insert record: %s (skipping)", dispPath, depth, insErr)
						}
					} else {
						// failed to construct a new Subtitles object.
						return recErr
					}
				}

			default:
				// cannot identify the file, probably an undesirable piece of
				// trash. well-suited for being ignored.
				if nil != ph && nil != ph.handleOther {
					ph.handleOther(l, absPath)
				}
			}
		}
		return nil
	}
}

// function scan() is the entry point for initiating a scan on the library's
// root file system. currently, the scan is dispatched and cannot be safely
// interrupted. you must wait for the scan to finish before restarting.
func (l *Library) scan(handler *PathHandler) (uint, *ReturnCode) {

	var (
		numScan uint = 0 // number of -new- files discovered on file system
		err     *ReturnCode
	)

	//
	// the scanStart channel is buffered so that we can limit the number of
	// goroutines concurrently traversing this library's file system:
	//
	//     writes to the channel will fail and fallback on the default select
	//     case if the max number of scanners is reached -- which sets an error
	//     code that is returned to the caller -- so be sure to check
	//     the return value when calling function scan()!
	//

	// try writing to the buffered channel. this will succeed if and only if it
	// isn't already filled to capacity.
	select {
	case l.scanStart <- time.Now():

		// notify the user that a potentially time-intensive operation has
		// begun and user interactions will be limited.
		if !isCLIMode {
			l.busyState.inc()
		}

		// the write succeeded, so we can initiate scanning. keep track of the
		// time at which we began so that the time elapsed can be calculated and
		// notified to the user.
		infoLog.verbosef("scanning: %q", l.name)
		err = l.scanDive(handler, l.absPath, 1)
		if nil == err {
			l.recandidateSubtitles(false)
		}

		// we've finished the scanning operations, so remove the busy indicator
		// to indicate that normal user interactions may resume (if no other
		// event has the semaphore still incremented).
		l.lastScan = time.Now()
		l.scanElapsed = time.Since(<-l.scanStart)
		if !isCLIMode {
			l.busyState.dec()
		}

		// construct a summary message for the load operation.
		total, summary := l.db.totalRecordsString(dmScan, -1, -1)
		if total > 0 {
			infoLog.verbosef(
				"finished scanning: %q (%s found in %s)",
				l.name, summary, l.scanElapsed.Round(time.Millisecond))
		} else {
			infoLog.verbosef(
				"finished scanning: %q (no new media found in %s)",
				l.name, l.scanElapsed.Round(time.Millisecond))
		}
		numScan = total

	default:
		// if the write failed, we fall back to this default case. the only
		// reason it should fail is if the buffer is already filled to capacity,
		// meaning we already have the max allowed number of goroutines scanning
		// this library's file system.
		err = rcLibraryBusy.specf(
			"scan(): max number of scanners reached: %q (max = %d)",
			l.absPath, maxLibraryScanners)
	}

	// return a count of the total number of entities successfully loaded.
	return numScan, err
}
