// =============================================================================
//  PROJ: pimmp
//  AUTH: ardnew
//  DATE: 26 Sept 2018
//  FILE: media.go
// -----------------------------------------------------------------------------
//
//  DESCRIPTION
//    defines types related to media and their auxiliary files and also provides
//    subroutines for inspecting and classifying media files.
//
// =============================================================================

package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

//
// TODO: add tags in Media struct to define which fields are indexed in the
//       database, and then dynamically create the index construction/queries
//       (in (*Database).initialize(), (*Library).seenFile(), etc.) accordingly.
//
type MediaIndex []string
type MediaIndexID int

const (
	mxPath MediaIndexID = iota
	mxCOUNT
)

var (
	mediaIndex = [mxCOUNT]MediaIndex{
		MediaIndex{"absPath"}, // = mxPath (0)
	}
)

// type Media is used to reference every kind of playable media -- the struct
// fields are common among both audio and video.
type Media struct {
	// fixed, read-only system info
	absPath      string      // absolute path to media file
	relPath      string      // CWD-relative path to media file
	size         int64       // length in bytes for regular files; system-dependent for others
	mode         os.FileMode // file mode bits
	timeModified time.Time   // modification time
	sysInfo      interface{} // underlying data source (can return nil)
	kind         MediaKind   // type of media
	ext          string      // file name extension
	extName      string      // name of file type/encoding (per file name extension)
	// user-writable system info
	name            string    // displayed name
	timeAdded       time.Time // date media was discovered and added to library
	playbackCommand string    // full system command used to play media
	// user-writable public media info
	title       string    // official name of media
	description string    // synopsis/summary of media content
	releaseDate time.Time // date media was produced/released
}

// type AudioMedia is a specialized type of media containing struct fields
// relevant only to video.
type AudioMedia struct {
	*Media
	album string // name of the album on which the track appears
	track int    // numbered index of where track is located on album
}

// type VideoMedia is a specialized type of media containing struct fields
// relevant only to audio.
type VideoMedia struct {
	*Media
	knownSubtitles []Subtitles // absolute path to all associated subtitles
	subtitles      Subtitles   // absolute path to selected subtitles
}

// TODO: type Subtitles contains all of the important information needed to
//       associate a subtitles file with a given Media object.
type Subtitles struct {
	absPath      string      // absolute path to media file
	relPath      string      // CWD-relative path to media file
	size         int64       // length in bytes for regular files; system-dependent for others
	mode         os.FileMode // file mode bits
	timeModified time.Time   // modification time
	sysInfo      interface{} // underlying data source (can return nil)
	ext          string      // file name extension
	extName      string      // name of file type/encoding (per file name extension)
}

// type MediaRecord represents the struct stored in the database for an
// individual media record
type MediaRecord map[string]interface{}

// type StorableMedia defines the methods necessary to store and retrieve a
// Media record from the database.
type StorableMedia interface {
	toRecord() *MediaRecord // creates a struct capable of being stored in the database.
	fromRecord(MediaRecord) // creates a struct using the record stored in the database.
}

// type MediaKind is an enum identifying the different supported types of
// playable media.
type MediaKind int

// concrete values of the MediaKind enum type.
const (
	mkUnknown MediaKind = iota - 1 // = -1
	mkAudio                        // =  0
	mkVideo                        // =  1
	mkCOUNT                        // =  2
)

// type MediaSupportKind is an enum identifying different types of files that
// support media in some way. these files should somehow be associated with the
// media files; they are not useful on their own.
type MediaSupportKind int

// concrete values of the MediaSupportKind enum type
const (
	mskUnknown   MediaSupportKind = iota - 1 // = -1
	mskSubtitles                             // =  0
)

// function newMedia() creates and initializes a new Media object. the kind
// of media is determined automatically, and the MediaKind field is set
// accordingly. so once the media has been identified, a type assertion can be
// performed to handle the object appropriately and unambiguously.
func newMedia(lib *Library, absPath, relPath string, kind MediaKind, ext, extName string, info os.FileInfo) *Media {

	return &Media{
		absPath:         absPath,        // (string)      absolute path to media file
		relPath:         relPath,        // (string)      CWD-relative path to media file
		size:            info.Size(),    // (int64)       length in bytes for regular files; system-dependent for others
		mode:            info.Mode(),    // (os.FileMode) file mode bits
		timeModified:    info.ModTime(), // (time.Time)   modification time
		sysInfo:         info.Sys(),     // (interface{}) underlying data source (can return nil)
		kind:            kind,           // (MediaKind)   type of media
		ext:             ext,            // (string)      file name extension
		extName:         extName,        // (string)      name of file type/encoding (per file name extension)
		name:            info.Name(),    // (string)      displayed name
		timeAdded:       time.Now(),     // (time.Time)   date media was discovered and added to library
		playbackCommand: "--",           // (string)      full system command used to play media
		title:           info.Name(),    // (string)      official name of media
		description:     "--",           // (string)      synopsis/summary of media content
		releaseDate:     time.Time{},    // (time.Time)   date media was produced/released
	}
}

// creates a string representation of the Media for easy identification in logs.
func (m *Media) String() string {
	path := m.absPath
	if "" != m.relPath && len(m.relPath) < len(m.absPath) {
		path = m.relPath
	}
	return fmt.Sprintf("%s [%s (%s)] (%d bytes) %v",
		path, m.extName, m.ext, m.size, m.timeModified)
}

func newAudioMedia(lib *Library, absPath, relPath, ext, extName string, info os.FileInfo) *AudioMedia {

	media := newMedia(lib, absPath, relPath, mkAudio, ext, extName, info)

	return &AudioMedia{
		Media: media, // common media info
		album: "",    // name of the album on which the track appears
		track: -1,    // numbered index of where track is located on album
	}
}

func newVideoMedia(lib *Library, absPath, relPath, ext, extName string, info os.FileInfo) *VideoMedia {

	media := newMedia(lib, absPath, relPath, mkVideo, ext, extName, info)

	return &VideoMedia{
		Media:          media,         // common media info
		knownSubtitles: []Subtitles{}, // absolute path to all associated subtitles
		subtitles:      Subtitles{},   // absolute path to selected subtitles
	}
}

func newSubtitles(lib *Library, absPath, relPath, ext, extName string, info os.FileInfo) *Subtitles {
	return &Subtitles{
		absPath:      absPath,        // absolute path to media file
		relPath:      relPath,        // CWD-relative path to media file
		size:         info.Size(),    // length in bytes for regular files; system-dependent for others
		mode:         info.Mode(),    // file mode bits
		timeModified: info.ModTime(), // modification time
		sysInfo:      info.Sys(),     // underlying data source (can return nil)
		ext:          ext,            // file name extension
		extName:      extName,        // name of file type/encoding (per file name extension)
	}
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

// type MediaExt is a struct pairing MediaKind values to their corresponding
// ExtTable map.
type MediaExt struct {
	kind  MediaKind
	table *ExtTable
}

var (
	// var audioExt is a struct defining how mkAudio media files will be
	// identified through file name inspection. if a file name extension matches
	// at least one string in any of the string slices below, then that file is
	// assumed to be mkAudio. the audio type/encoding of that file is also
	// assumed to be the map key corresponding to the matching string slice.
	audioExt = MediaExt{
		kind: mkAudio,
		table: &ExtTable{
			"3D Solar UK Ltd":               []string{".ivs"},
			"ACT Lossy ADPCM":               []string{".act"},
			"Adaptive Multi-Rate":           []string{".amr"},
			"Adaptive Multi-Rate Wideband":  []string{".awb"},
			"Advanced Audio Coding":         []string{".aac"},
			"Apple AIFF":                    []string{".aiff"},
			"Audible Audiobook":             []string{".aa", ".aax"},
			"Dialogic ADPCM":                []string{".vox"},
			"Digital Speech Standard":       []string{".dss"},
			"Electronic Arts IFF-8SVX":      []string{".8svx"},
			"Free Lossless Audio Codec":     []string{".flac"},
			"GSM Telephony":                 []string{".gsm"},
			"iKlax Media":                   []string{".iklax"},
			"Linear PCM":                    []string{".sln"},
			"Microsoft WAV":                 []string{".wav"},
			"Microsoft Windows Media Audio": []string{".wma"},
			"Monkey's Audio":                []string{".ape"},
			"MPEG Layer III":                []string{".mp3"},
			"MPEG-4 Part 14":                []string{".m4a", ".m4b"},
			"Musepack/MPC/MPEG":             []string{".mpc"},
			"NCH Dictation":                 []string{".dct"},
			"Nintendo (NES) Sound Format":   []string{".nsf"},
			"Ogg Audio":                     []string{".oga", ".mogg"},
			"Opus":                          []string{".opus"},
			"RAW Audio Format":              []string{".raw"},
			"RealAudio":                     []string{".ra"},
			"Samsung Yamaha Ringtone":       []string{".mmf"},
			"Sony Compressed Voice":         []string{".dvf", ".msv"},
			"Sun/Unix/Java Audio":           []string{".au"},
			"True Audio Lossless":           []string{".tta"},
			"WavPack":                       []string{".wv"},
		},
	}
	// var videoExt is a struct defining how mkAudio media files will be
	// identified through file name inspection. see discussion of audioExt
	// above. the same assumptions are made here but with mkVideo instead.
	videoExt = MediaExt{
		kind: mkVideo,
		table: &ExtTable{
			"3GPP":                              []string{".3gp"},
			"3GPP2":                             []string{".3g2"},
			"Advanced Systems Format":           []string{".asf"},
			"AMV video format":                  []string{".amv"},
			"Audio Video Interleave":            []string{".avi"},
			"Dirac":                             []string{".drc"},
			"Flash Video":                       []string{".flv", ".f4v", ".f4p", ".f4a", ".f4b"},
			"Graphics Interchange Format Video": []string{".gifv"},
			"Material Exchange Format":          []string{".mxf"},
			"Matroska":                          []string{".mkv"},
			"MPEG Transport Stream":             []string{".mts", ".m2ts"},
			"MPEG-1":                            []string{".mp2", ".mpe", ".mpv"},
			"MPEG-1/MPEG-2":                     []string{".mpg", ".mpeg"},
			"MPEG-2":                            []string{".m2v"},
			"MPEG-4 Part 14":                    []string{".mp4", ".m4p", ".m4v"},
			"Multiple-image Network Graphics":   []string{".mng"},
			"Nullsoft Streaming Video":          []string{".nsv"},
			"Ogg Video":                         []string{".ogv", ".ogg"},
			"QuickTime File Format":             []string{".mov", ".qt"},
			"Raw video format":                  []string{".yuv"},
			"RealMedia":                         []string{".rm"},
			"RealMedia Variable Bitrate":        []string{".rmvb"},
			"RoQ FMV":                           []string{".roq"},
			"Standardized Video Interview":      []string{".svi"},
			"Video Object":                      []string{".vob"},
			"WebM":                              []string{".webm"},
			"Windows Media Video":               []string{".wmv"},
		},
	}
)

// function mediaKindOfFileExt() searches all MediaExt mappings for a given
// file name extension, returning both the MediaKind and the type/encoding name
// associated with that file name extension.
func mediaKindOfFileExt(ext string) (MediaKind, string) {

	// constant values in file extension tables are all lowercase. convert the
	// search key to lowercase for case-insensitivity.
	extLower := strings.ToLower(ext)

	// iter: all supported kinds of media
	for _, m := range []MediaExt{audioExt, videoExt} {
		if n, ok := kindOfFileExt(m.table, extLower); ok {
			return m.kind, n
		}
	}
	return mkUnknown, ""
}

// type MediaSupportExt is a struct pairing MediaSupportKind values to their
// corresponding ExtTable map.
type MediaSupportExt struct {
	kind  MediaSupportKind
	table *ExtTable
}

var (
	subsExt = MediaSupportExt{
		kind: mskSubtitles,
		table: &ExtTable{
			"AQTitle":                    []string{".aqt"},
			"CVD":                        []string{".cvd"},
			"DKS":                        []string{".dks"},
			"Gloss Subtitle":             []string{".gsub"},
			"JACOSub":                    []string{".jss"},
			"MPL2":                       []string{".mpl"},
			"Phoenix Subtitle":           []string{".pjs"},
			"PowerDivX":                  []string{".psb"},
			"RealText / SMIL":            []string{".rt"},
			"SAMI":                       []string{".smi"},
			"SubRip":                     []string{".srt"},
			"SubStation Alpha":           []string{".ssa"},
			"Advanced SubStation Alpha":  []string{".ass"},
			"Structured Subtitle Format": []string{".ssf"},
			"Spruce subtitle format":     []string{".stl"},
			"MicroDVD":                   []string{".sub"},
			"MPSub":                      []string{".sub"},
			"SubViewer":                  []string{".sub"},
			"VobSub":                     []string{".sub", ".idx"},
			"SVCD":                       []string{".svcd"},
			"MPEG-4 Timed Text":          []string{".ttxt"},
			"Universal Subtitle Format":  []string{".usf"},
		},
	}
)

// function mediaSupportKindOfFileExt() searches all MediaSupportExt mappings
// for a given file name extension, returning both the MediaSupportKind and the
// type/encoding name associated with that file name extension.
func mediaSupportKindOfFileExt(ext string) (MediaSupportKind, string) {

	// constant values in file extension tables are all lowercase. convert the
	// search key to lowercase for case-insensitivity.
	extLower := strings.ToLower(ext)

	// iter: all supported kinds of media
	for _, m := range []MediaSupportExt{subsExt} {
		if n, ok := kindOfFileExt(m.table, extLower); ok {
			return m.kind, n
		}
	}
	return mskUnknown, ""
}

// function record() creates a struct capable of being stored in the database.
// defines type AudioMedia's implementation of the MediaRecord interface.
func (m *AudioMedia) toRecord() *MediaRecord {
	return &MediaRecord{
		"absPath":         m.absPath,
		"relPath":         m.relPath,
		"size":            m.size,
		"mode":            m.mode,
		"timeModified":    m.timeModified,
		"sysInfo":         m.sysInfo,
		"kind":            m.kind,
		"ext":             m.ext,
		"extName":         m.extName,
		"name":            m.name,
		"timeAdded":       m.timeAdded,
		"playbackCommand": m.playbackCommand,
		"title":           m.title,
		"description":     m.description,
		"releaseDate":     m.releaseDate,
		// audio-specific fields
		"album": m.album,
		"track": m.track,
	}
}

// function fromRecord() creates a struct using the record stored in the
// database. defines type AudioMedia's implementation of the MediaRecord
// interface.
func (m *AudioMedia) fromRecord(r MediaRecord) {

	var (
		v  interface{}
		ok bool
	)

	// AudioMedia has an embedded Media struct -pointer- (not struct). so if we
	// create a zeroized AudioMedia, the embedded Media will be a null pointer.
	// we can protect this method from that null pointer by creating a zeroized
	// Media and updating AudioMedia's embedded pointer to reference it.
	if nil == m.Media {
		m.Media = &Media{}
	}

	v, ok = r["absPath"]
	if ok {
		m.absPath = v.(string)
	}
	v, ok = r["relPath"]
	if ok {
		m.relPath = v.(string)
	}

	v, ok = r["size"]
	if ok {
		m.size = int64(v.(float64))
	}
	v, ok = r["mode"]
	if ok {
		m.mode = os.FileMode(v.(float64))
	}
	v, ok = r["timeModified"]
	if ok {
		t, _ := time.Parse(defaultTimeLayout, v.(string))
		m.timeModified = t
	}
	v, ok = r["sysInfo"]
	if ok {
		m.sysInfo = v
	}

	v, ok = r["kind"]
	if ok {
		m.kind = MediaKind(v.(float64))
	}
	v, ok = r["ext"]
	if ok {
		m.ext = v.(string)
	}
	v, ok = r["extName"]
	if ok {
		m.extName = v.(string)
	}
	v, ok = r["name"]
	if ok {
		m.name = v.(string)
	}
	v, ok = r["timeAdded"]
	if ok {
		t, _ := time.Parse(defaultTimeLayout, v.(string))
		m.timeAdded = t
	}
	v, ok = r["playbackCommand"]
	if ok {
		m.playbackCommand = v.(string)
	}
	v, ok = r["title"]
	if ok {
		m.title = v.(string)
	}
	v, ok = r["description"]
	if ok {
		m.description = v.(string)
	}
	v, ok = r["releaseDate"]
	if ok {
		t, _ := time.Parse(defaultTimeLayout, v.(string))
		m.releaseDate = t
	}
	v, ok = r["album"]
	if ok {
		m.album = v.(string)
	}
	v, ok = r["track"]
	if ok {
		m.track = v.(int)
	}
}

// function record() creates a struct capable of being stored in the database.
// defines type VideoMedia's implementation of the MediaRecord interface.
func (m *VideoMedia) toRecord() *MediaRecord {
	return &MediaRecord{
		"absPath":         m.absPath,
		"relPath":         m.relPath,
		"size":            m.size,
		"mode":            m.mode,
		"timeModified":    m.timeModified,
		"sysInfo":         m.sysInfo,
		"kind":            m.kind,
		"ext":             m.ext,
		"extName":         m.extName,
		"name":            m.name,
		"timeAdded":       m.timeAdded,
		"playbackCommand": m.playbackCommand,
		"title":           m.title,
		"description":     m.description,
		"releaseDate":     m.releaseDate,
		// video-specific fields
		"knownSubtitles": m.knownSubtitles,
		"subtitles":      m.subtitles,
	}
}

// function fromRecord() creates a struct using the record stored in the
// database. defines type VideoMedia's implementation of the MediaRecord
// interface.
func (m *VideoMedia) fromRecord(r MediaRecord) {

	var (
		v  interface{}
		ok bool
	)

	// VideoMedia has an embedded Media struct -pointer- (not struct). so if we
	// create a zeroized VideoMedia, the embedded Media will be a null pointer.
	// we can protect this method from that null pointer by creating a zeroized
	// Media and updating VideoMedia's embedded pointer to reference it.
	if nil == m.Media {
		m.Media = &Media{}
	}

	v, ok = r["absPath"]
	if ok {
		m.absPath = v.(string)
	}
	v, ok = r["relPath"]
	if ok {
		m.relPath = v.(string)
	}

	v, ok = r["size"]
	if ok {
		m.size = int64(v.(float64))
	}
	v, ok = r["mode"]
	if ok {
		m.mode = os.FileMode(v.(float64))
	}
	v, ok = r["timeModified"]
	if ok {
		t, _ := time.Parse(defaultTimeLayout, v.(string))
		m.timeModified = t
	}
	v, ok = r["sysInfo"]
	if ok {
		m.sysInfo = v
	}

	v, ok = r["kind"]
	if ok {
		m.kind = MediaKind(v.(float64))
	}
	v, ok = r["ext"]
	if ok {
		m.ext = v.(string)
	}
	v, ok = r["extName"]
	if ok {
		m.extName = v.(string)
	}
	v, ok = r["name"]
	if ok {
		m.name = v.(string)
	}
	v, ok = r["timeAdded"]
	if ok {
		t, _ := time.Parse(defaultTimeLayout, v.(string))
		m.timeAdded = t
	}
	v, ok = r["playbackCommand"]
	if ok {
		m.playbackCommand = v.(string)
	}
	v, ok = r["title"]
	if ok {
		m.title = v.(string)
	}
	v, ok = r["description"]
	if ok {
		m.description = v.(string)
	}
	v, ok = r["releaseDate"]
	if ok {
		t, _ := time.Parse(defaultTimeLayout, v.(string))
		m.releaseDate = t
	}
	v, ok = r["knownSubtitles"]
	if ok {
		subs := []Subtitles{}
		for _, s := range v.([]interface{}) {
			subs = append(subs, s.(Subtitles))
		}
		m.knownSubtitles = subs
	}
	v, ok = r["subtitles"]
	if ok {
		//m.subtitles = v.(Subtitles)
	}
}
