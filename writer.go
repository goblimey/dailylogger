package dailylogger

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/user"
	"strings"
	"sync"

	"time"

	ps "github.com/goblimey/portablesyscall"
	"github.com/goblimey/switchwriter"
)

// Writer satisfies the io.Writer interface and writes data to a log file.
// The name of the logfile contains a datestamp in yyyy-mm-dd format. When the
// logger is created the caller can specify leading and trailing text.  For
// example the name for a logfile created on the 5th October 2020 with leader
// "data" and trailer "log" would be "data.20201005.log".
//
// The Writer rolls the log over at midnight at the start of each day - it
// closes yesterday's log and creates today's.
//
// On start up, the first call of New creates today's log file if it doesn't
// already exist.  If the file has already been created, the Writer appends to
// the existing contents.
//
// The Writer contains a mutex.  It's dangerous to copy an object that contain a
// mutex, so you should always call its methods via a pointer.  The New function
// returns a pointer, so that's a good way to create a DailyLogger.
type Writer struct {
	logMutex           sync.Mutex
	loggingDisabled    bool                 // True if logging is disable. (Logging is enabled by default.)
	startOfToday       time.Time            // The current datestamp for the log.
	logDir             string               // The log directory.
	leader             string               // The leading part of the log file name.
	trailer            string               // The trailing part of the log file name.
	userName           string               // The user that will own the log file (optional).
	groupName          string               // the group of the log file (optional).
	logDirPermissions  os.FileMode          // file permissions on the log directory (0 means leave as is)
	logFilePermissions os.FileMode          // file permissions to be set on the log file (0 means leave as is).
	switchwriter       *switchwriter.Writer // The connection to the log file.
}

// This is a compile-time check that Writer implements the io.Writer interface.
var _ io.Writer = (*Writer)(nil)

// New creates a Writer and returns it.  The writer writes to a log file in  a directory, either
// or both of which are created if necessary.  The form of the log file name is
// leader + YYYY-MM-DD + trailer, for example "payments.2026-02-14.log".  If the log file already
// exists, it's opened in append mode.  If the program continues to write beyond midnight, a new
// log file is created with a name refecting the new date.  The optional arguments are log directory
// permissions(uint32), log file permissions (uint32), user name and group name of the files.  If
// a permissions value is zero, the permissions are left as they are, they are NOT set to zero.  The
// optional arguments are only useful if the calling process is running under a POSIX system (not
// MS Windows) and is able to change the state of the file, for example, the caller is running as
// root or as the user that owns the files.
func New(now time.Time, logDir, leader, trailer string, args ...any) *Writer {

	// The logfile is of the form "logDir/leader.yyyy-mm-dd.trailer".  The default
	// is "./daily.yyyy-mm-dd.log".
	const defaultLeader = "daily."
	const defaultTrailer = ".log"
	const defaultLogDir = "."

	logDir = strings.TrimSpace(logDir)
	if len(logDir) == 0 {
		logDir = defaultLogDir
	}

	leader = strings.TrimSpace(leader)
	if len(leader) == 0 {
		leader = defaultLeader
	}

	trailer = strings.TrimSpace(trailer)
	if len(trailer) == 0 {
		trailer = defaultTrailer
	}

	var dirPermissions, filePermissions os.FileMode
	var userName, groupName string
	if ps.OSName != "windows" {
		// Get te log permissions and the log owner details.  These can only be set
		// under a POSIX system.  Under Windows leave the at their zero values.
		dirPermissions, filePermissions, userName, groupName = getLogFileDetails(args...)
	}

	dw := newWriter(now, logDir, leader, trailer, dirPermissions, filePermissions, userName, groupName)

	// Start a goroutine to roll the log over at the end of each day.
	go dw.logRotator()
	return dw
}

// newWriter creates a daily writer with a supplied switchwriter
// and returns a pointer to it. This is called by New as a helper method and by
// unit tests.
func newWriter(now time.Time, logDir, leader, trailer string, dirPermissions, filePermissions os.FileMode, userName, groupName string) *Writer {

	startOfToday := getLastMidnight(now)

	sw := switchwriter.New()

	dw := Writer{
		logDir:             logDir,
		leader:             leader,
		trailer:            trailer,
		logDirPermissions:  dirPermissions,
		logFilePermissions: filePermissions,
		userName:           userName,
		groupName:          groupName,
		startOfToday:       startOfToday,
		switchwriter:       sw,
	}

	// Create the log directory if it doesn't already exist.
	createlogDirectory(logDir, userName, groupName, dirPermissions)

	// Create today's log file and switch the switchwriter to it.

	dw.openLog()

	return &dw
}

// getLogFileDetails gets the permissions, user name and group name from the optional arguments.
func getLogFileDetails(args ...any) (os.FileMode, os.FileMode, string, string) {
	// Args should be of length 0 to 4.  They are: log directory permissions(uint32) for example
	// 0777, log file permissions (uint32), user name and group name of the files.

	if len(args) <= 0 {
		return 0, 0, "", ""
	}

	var dirPermissions, filePermissions os.FileMode
	var userName, groupName string

	if len(args) >= 1 {
		u, ok := args[0].(int)
		if ok {
			dirPermissions = os.FileMode(u)
		}
	}

	if len(args) >= 2 {
		u, ok := args[1].(int)
		if ok {
			filePermissions = os.FileMode(u)
		}
	}

	if len(args) >= 3 {
		s, ok := args[2].(string)
		if ok {
			userName = strings.TrimSpace(s)
		}
	}
	if len(args) >= 4 {
		s, ok := args[3].(string)
		if ok {
			groupName = strings.TrimSpace(s)
		}
	}

	return dirPermissions, filePermissions, userName, groupName
}

// SetFileUserAndGroup sets the owner and group of a file (plain text or directory) to the
// given user and group.  The application must be running on a POSIX system (eg Linux or UNIX)
// to do this.  Under Windows the call returns a syscall.EWINDOWS error wrapped in an
// io.fs.PathError (which is what os.Chown does under Windows).
func SetFileUserAndGroup(filename, userName, groupName string) error {

	if ps.OSName == "windows" {
		// We are running under Windows, so Chown etc will not work.
		return &fs.PathError{Op: "SetFileUserAndGroup", Path: filename, Err: ps.EWINDOWS}
	}

	// We are running on a POSIX system.  Chown etc will work.

	if os.Getuid() != 0 {
		return errors.New("SetFileUserAndGroup: must be root")
	}

	// We are root so we can change file ownership.

	uid, ue := getUserIDFromName(userName)
	if ue != nil {
		return errors.New(filename + " userName " + userName + " " + ue.Error())
	}

	gid, ge := getGroupIDFromName(groupName)
	if ge != nil {
		return errors.New(filename + " groupName " + groupName + " " + ge.Error())
	}

	che := os.Chown(filename, uid, gid)

	return che
}

// Write writes the buffer to the daily log file, creating the file at the
// start of each day.
func (dw *Writer) Write(buffer []byte) (int, error) {
	// Avoid a race with rotateLogs.
	dw.logMutex.Lock()
	defer dw.logMutex.Unlock()

	// Write to the log.
	n, err := dw.switchwriter.Write(buffer)
	return n, err
}

// logRotator() runs forever, rotating the log files at the end of each day.
func (dw *Writer) logRotator() {

	// This should be run in a goroutine.
	//
	// As it runs forever it can't be unit tested.

	for {
		now := time.Now()
		dw.waitAndRotate(now)
	}
}

// waitToRotate sleeps until just after midnight.  It uses the supplied time rather
// than finding out the time for itself to support unit testing.
func waitToRotate(now time.Time) {

	// Find the duration between now and a little after the next midnight.
	waitTime := getDurationToJustAfterMidnight(now)

	// Sleep until the next day.
	time.Sleep(waitTime)
}

// waitAndRotate sleeps until midnight and then switches to the new day's log file.
func (dw *Writer) waitAndRotate(now time.Time) {

	// Sleep until just after midnight.
	waitToRotate(now)

	// Wake up and rotate the log file using the new day as the date stamp.
	dw.rotateLogs(now)
}

// rotateLogs() rotates the daily log files.
func (dw *Writer) rotateLogs(now time.Time) {
	// Avoid a race with Write.
	dw.logMutex.Lock()
	defer dw.logMutex.Unlock()
	dw.closeLog()

	// Advance the current day.  If the system is running properly, It should by now
	// be a fraction of a second after midnight at the start of the next day.  If the
	// system gets very slow for some reason, it could be any amount of time later,
	// maybe on an even later day.
	dw.startOfToday = getLastMidnight(now)

	// Open the logfile using start of today as the timestamp.

	dw.openLog()
}

// CreateLogDirectory creates the log directory if it does not already exist.
func createlogDirectory(directory, owner, group string, permissions os.FileMode) {
	if uint32(permissions) == 0 {
		// The given permissons are zero (not set) so use ModePerm
		permissions = os.ModePerm
	}

	// Note - under Windows, Mkdirall creates the directory but ignores the permissions.
	err := os.MkdirAll(directory, permissions)
	if err != nil {
		// We don't have a log file so we can only write the error to stdout.
		log.Printf("%s: cannot create log directory %s - %v",
			"createlogDirectory", directory, err.Error())
	}

	if len(owner) > 0 && len(group) > 0 {
		if os.Getuid() == 0 {
			// Getuid return -1 under Windows so this is a POSIX system and the calling
			// program is running as root.  Set the owner and group of the log file.
			err := SetFileUserAndGroup(directory, owner, group)
			if err != nil {
				// We don't have a log file so we can only write the error to stdout.
				log.Printf("%s: error setting user and group on log directory %s - %v",
					"createlogDirectory", directory, err.Error())
			}
		}
	}
}

// closeLog is a helper function that closes the log file (which
// also flushes any uncommitted writes).  It doesn't apply the
// lock so it should only be called by a function that does.
func (dw *Writer) closeLog() {
	dw.switchwriter.SwitchTo(nil)
}

// openLog is a helper function that opens today's log.  It doesn't
// apply the lock, so it should only be done by something that does.
func (dw *Writer) openLog() {

	// Create the log directory
	pathname := dw.getLogPathname(dw.startOfToday)

	logFile, err := dw.openFile(pathname)
	if err != nil {
		log.Printf("openLog: error creating log file %s - %s\n",
			pathname, err.Error())
		// Continue - file is now nil.
	}

	dw.switchwriter.SwitchTo(logFile)
}

// getLogPathname returns today's log filename, for example "data.2020-01-19.rtcm3".
// The time is supplied to aid unit testing.
func (dw *Writer) getLogPathname(now time.Time) string {

	return fmt.Sprintf("%s/%s%04d-%02d-%02d%s",
		dw.logDir, dw.leader, now.Year(), int(now.Month()), now.Day(), dw.trailer)
}

// openFile either creates and opens the file or, if it already exists, opens it
// in append mode.
func (dw *Writer) openFile(name string) (*os.File, error) {
	// Open the file for appending, creating it if necessary.
	file, oe := os.OpenFile(name, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if oe != nil {
		log.Printf("openFile: %v", oe)
	}

	if dw.logFilePermissions != 0 {
		// Set the file permissions.
		os.Chmod(name, os.FileMode(dw.logFilePermissions))
	}

	if len(dw.userName) > 0 && len(dw.groupName) > 0 {
		if os.Getuid() == 0 {
			// We are running under a POSIX system and logged in as root,
			// (If we were under Windows, Getuid would return -1.)  Change
			// the owner and group as specified.
			SetFileUserAndGroup(name, dw.userName, dw.groupName)
		}
	}

	// Seek to the end of the file.
	_, err := file.Seek(0, 2)
	if err != nil {
		log.Fatal(err)
	}
	return file, nil
}

// extraDuration is the extra time to wait after midnight.
const extraDuration = time.Duration(time.Microsecond)

// getDurationToMidnight gets the duration between the given time and a tiny fraction
// of a second after midnight at the beginning of the next day in the same timezone.
// (Adding a small amount of extra time removes the confusion over which day midnight
// is in.
func getDurationToJustAfterMidnight(givenTime time.Time) time.Duration {
	// Find midnight at the end of the day that the given time is in.
	// If now is exactly midnight within the discrimination of the system,
	// the result will be 0 otherwise it will be greater than zero.  It
	// can never be negative.
	nextMidnight := getNextMidnight(givenTime)

	// Calculate the duration to wait until a fraction of a second after
	// the next midnight.
	durationToWait := nextMidnight.Sub(givenTime)

	durationToWait += extraDuration

	return durationToWait
}

// getLastMidnight gets midnight at the beginning of the day of the given time.
func getLastMidnight(now time.Time) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

// getNextMidnight gets midnight at the beginning of the day after the given time.
func getNextMidnight(givenTime time.Time) time.Time {
	// Advance the date by one day.  Timezone issues could potentially make this more
	// complicated than it looks.  However, to quote the comment on AddDate:
	// "the same AddDate arguments can produce a different shift in absolute time
	// depending on the base Time value and its Location. For example, AddDate(0, 0, 1)
	// applied to 12:00 on March 27 always returns 12:00 on March 28."  So the day
	// value will always be one bigger than the given time's day value, which is all
	// that matters for our purposes - getNextMidnight will always return a time after
	// the given time and it will be the midnight after the one returned by
	// getLastMidnight.
	nextDay := givenTime.AddDate(0, 0, 1)
	return time.Date(nextDay.Year(), nextDay.Month(), nextDay.Day(), 0, 0, 0, 0, givenTime.Location())
}

// getUserIDFromName gets the user ID, given the user name.  This only works on a POSIX system.
// Under Windows it returns an error.
func getUserIDFromName(userName string) (int, error) {

	u, ue := user.Lookup(userName)
	if ue != nil {
		return 0, ue
	}

	var uid int
	nu, uide := fmt.Sscanf(u.Uid, "%d", &uid)
	if uide != nil {
		return 0, uide
	}
	if nu != 1 {
		em := fmt.Sprintf("%s: want 1 scanned uid, got %d", "SetFileUserAndGroup", nu)
		return 0, errors.New(em)
	}

	return uid, nil
}

// getGroupIDFromName gets the group ID, given the group name.  This only works on a POSIX system.
// Under Windows it returns an error.
func getGroupIDFromName(groupName string) (int, error) {
	g, ge := user.LookupGroup(groupName)
	if ge != nil {
		return 0, ge
	}

	var gid int
	ng, gide := fmt.Sscanf(g.Gid, "%d", &gid)
	if gide != nil {
		return 0, gide
	}
	if ng != 1 {
		em := fmt.Sprintf("%s: want 1 scanned uid, got %d", "SetFileUserAndGroup", ng)
		return 0, errors.New(em)
	}

	return gid, nil
}

// getUserFromID gets the user.User object, given the user ID.  This only works on a POSIX system.
// Under Windows it returns an error.
func getUserFromID(id uint32) (*user.User, error) {
	s := fmt.Sprintf("%d", id)
	u, err := user.LookupId(s)

	return u, err
}

// getGroupFromID gets the user.Group object, given the group ID.  This only works on a POSIX system.
// Under Windows it returns an error.
func getGroupFromID(id uint32) (*user.Group, error) {
	s := fmt.Sprintf("%d", id)
	g, err := user.LookupGroupId(s)

	return g, err
}
